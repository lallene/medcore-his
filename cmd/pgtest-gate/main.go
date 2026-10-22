// Command pgtest-gate is the Release Gate fail-closed verifier for PostgreSQL suites (LOT30-A′).
//
// It reads `go test -json` events and the per-test evidence file written by
// internal/testsupport/pgtest, then fails unless every required package:
//   - reported no failure,
//   - has at least one test that obtained a real PostgreSQL DSN,
//   - has at least one of those PostgreSQL tests passing.
//
// A run where the database was missing therefore cannot turn green: with
// MEDCORE_REQUIRE_POSTGRES_TESTS=1 the tests fail, and without it they skip
// and leave no evidence.
//
// Usage:
//
//	pgtest-gate -events go-test.json -evidence pg-evidence.txt -require billing,cash,...
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

type event struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
}

type pkgStats struct {
	pass, fail, skip int
	pkgFailed        bool
	passedTests      map[string]bool
	pgTests          map[string]bool
}

func main() {
	eventsPath := flag.String("events", "", "go test -json output file")
	evidencePath := flag.String("evidence", "", "pgtest evidence file")
	require := flag.String("require", "", "comma-separated package path suffixes (e.g. modules/billing)")
	flag.Parse()
	if *eventsPath == "" || *evidencePath == "" || strings.TrimSpace(*require) == "" {
		fmt.Fprintln(os.Stderr, "pgtest-gate: -events, -evidence and -require are mandatory")
		os.Exit(2)
	}
	stats, err := readEvents(*eventsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgtest-gate:", err)
		os.Exit(2)
	}
	if err := readEvidence(*evidencePath, stats); err != nil {
		fmt.Fprintln(os.Stderr, "pgtest-gate:", err)
		os.Exit(2)
	}
	ok := report(os.Stdout, stats, splitRequired(*require))
	if !ok {
		fmt.Println("PGTEST GATE: FAIL — PostgreSQL suites not proven executed")
		os.Exit(1)
	}
	fmt.Println("PGTEST GATE: PASS — PostgreSQL suites executed")
}

func splitRequired(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func readEvents(p string) (map[string]*pkgStats, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stats := map[string]*pkgStats{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Package == "" {
			continue
		}
		s := stats[e.Package]
		if s == nil {
			s = &pkgStats{passedTests: map[string]bool{}, pgTests: map[string]bool{}}
			stats[e.Package] = s
		}
		if e.Test == "" {
			if e.Action == "fail" {
				s.pkgFailed = true
			}
			continue
		}
		switch e.Action {
		case "pass":
			s.pass++
			s.passedTests[e.Test] = true
		case "fail":
			s.fail++
		case "skip":
			s.skip++
		}
	}
	return stats, sc.Err()
}

// readEvidence maps "<pkg>.test\t<TestName>" lines onto packages by binary name.
func readEvidence(p string, stats map[string]*pkgStats) error {
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	byBinary := map[string][]*pkgStats{}
	for pkg, s := range stats {
		byBinary[path.Base(pkg)+".test"] = append(byBinary[path.Base(pkg)+".test"], s)
	}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if line = strings.TrimSpace(line); line != "" {
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) == 2 {
				for _, s := range byBinary[parts[0]] {
					s.pgTests[parts[1]] = true
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func report(w io.Writer, stats map[string]*pkgStats, required []string) bool {
	ok := true
	pkgs := make([]string, 0, len(stats))
	for p := range stats {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, want := range required {
		var matched []string
		for _, p := range pkgs {
			if p == want || strings.HasSuffix(p, "/"+want) {
				matched = append(matched, p)
			}
		}
		if len(matched) == 0 {
			fmt.Fprintf(w, "MISSING  %-45s no go test events\n", want)
			ok = false
			continue
		}
		for _, p := range matched {
			s := stats[p]
			pgPassed := 0
			for name := range s.pgTests {
				if s.passedTests[name] {
					pgPassed++
				}
			}
			status := "OK"
			switch {
			case s.pkgFailed || s.fail > 0:
				status = "FAILED"
			case len(s.pgTests) == 0:
				status = "NO-PG"
			case pgPassed == 0:
				status = "PG-NOT-PASSED"
			}
			if status != "OK" {
				ok = false
			}
			fmt.Fprintf(w, "%-13s %-60s pass=%d fail=%d skip=%d pg_tests=%d pg_passed=%d\n",
				status, p, s.pass, s.fail, s.skip, len(s.pgTests), pgPassed)
		}
	}
	return ok
}
