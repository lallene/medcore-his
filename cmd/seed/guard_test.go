package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func validDemoGuard() demoGuardInput {
	return demoGuardInput{AppEnv: "development", DatabaseName: "medcore_full_demo", AllowFlag: "1", Password: "demo-only-passphrase-42"}
}

func TestDemoGuardAcceptsCompleteOptIn(t *testing.T) {
	if err := checkDemoGuard(validDemoGuard()); err != nil {
		t.Fatalf("complete opt-in must pass: %v", err)
	}
}

func TestDemoGuardFailsClosed(t *testing.T) {
	cases := map[string]func(*demoGuardInput){
		"production":          func(in *demoGuardInput) { in.AppEnv = "production" },
		"production_spaced":   func(in *demoGuardInput) { in.AppEnv = " Production " },
		"db_not_allowed":      func(in *demoGuardInput) { in.DatabaseName = "medcore" },
		"db_neon":             func(in *demoGuardInput) { in.DatabaseName = "neondb" },
		"no_opt_in":           func(in *demoGuardInput) { in.AllowFlag = "" },
		"opt_in_not_1":        func(in *demoGuardInput) { in.AllowFlag = "true" },
		"no_password":         func(in *demoGuardInput) { in.Password = "" },
		"blank_password":      func(in *demoGuardInput) { in.Password = "   " },
		"short_password":      func(in *demoGuardInput) { in.Password = "eleven-char" },
		"bcrypt_overflow_pwd": func(in *demoGuardInput) { in.Password = strings.Repeat("x", 73) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := validDemoGuard()
			mutate(&in)
			err := checkDemoGuard(in)
			if err == nil {
				t.Fatalf("%s must fail closed", name)
			}
			if in.Password != "" && strings.TrimSpace(in.Password) != "" && strings.Contains(err.Error(), in.Password) {
				t.Fatal("guard error must never echo the password")
			}
		})
	}
}

// Without a flag the seed prints usage and exits before loading config or touching a database.
func TestSeedWithoutModePrintsUsageWithoutMutation(t *testing.T) {
	if os.Getenv("SEED_MAIN_CHILD") == "1" {
		os.Args = append([]string{"seed"}, strings.Fields(os.Getenv("SEED_MAIN_ARGS"))...)
		main()
		return
	}
	for _, args := range []string{"", "--unknown", "--demo-full extra"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSeedWithoutModePrintsUsageWithoutMutation$")
		// Unreachable database: any connection attempt would fail with a different error.
		cmd.Env = append(os.Environ(), "SEED_MAIN_CHILD=1", "SEED_MAIN_ARGS="+args,
			"DATABASE_URL=postgres://nobody@127.0.0.1:1/none?sslmode=disable", "APP_ENV=development")
		out, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 2 {
			t.Fatalf("args %q: expected exit 2, got %v\n%s", args, err, out)
		}
		if !strings.Contains(string(out), "Usage: seed") || strings.Contains(string(out), "Connexion PostgreSQL") {
			t.Fatalf("args %q: expected usage without DB access:\n%s", args, out)
		}
	}
}

// demoHash refuses to run when the common guard was not passed.
func TestDemoHashRequiresGuard(t *testing.T) {
	if os.Getenv("SEED_HASH_CHILD") == "1" {
		demoPasswordHash = ""
		_ = demoHash()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDemoHashRequiresGuard$")
	cmd.Env = append(os.Environ(), "SEED_HASH_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "garde-fou non validé") {
		t.Fatalf("demoHash without guard must exit: %v\n%s", err, out)
	}
}
