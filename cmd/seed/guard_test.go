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

// Temporary pre-V1 exception: production + database "medcore" only, with every opt-in.
func preV1Guard() demoGuardInput {
	return demoGuardInput{AppEnv: "production", DatabaseName: "medcore", AllowFlag: "1", Password: "demo-only-passphrase-42", PreV1Flag: "1"}
}

func TestDemoGuardPreV1Exception(t *testing.T) {
	const pwd = "demo-only-passphrase-42"
	cases := []struct {
		name   string
		in     demoGuardInput
		allow  bool
		reason string
	}{
		{"legacy_dev_full_demo", validDemoGuard(), true, ""},
		{"legacy_dev_full_demo_with_pre_v1_flag", func() demoGuardInput { in := validDemoGuard(); in.PreV1Flag = "1"; return in }(), true, ""},
		{"prod_medcore_all_opt_ins", preV1Guard(), true, ""},
		{"prod_casing_spaces_medcore_all_opt_ins", func() demoGuardInput { in := preV1Guard(); in.AppEnv = " PRODUCTION "; return in }(), true, ""},
		{"prod_medcore_without_pre_v1", func() demoGuardInput { in := preV1Guard(); in.PreV1Flag = ""; return in }(), false, "production"},
		{"prod_medcore_pre_v1_not_1", func() demoGuardInput { in := preV1Guard(); in.PreV1Flag = "true"; return in }(), false, "production"},
		{"prod_spaced_medcore_without_pre_v1", func() demoGuardInput { in := preV1Guard(); in.AppEnv = " Production "; in.PreV1Flag = ""; return in }(), false, "production"},
		{"prod_medcore_pre_v1_without_allow", func() demoGuardInput { in := preV1Guard(); in.AllowFlag = ""; return in }(), false, EnvAllowDemoSeed},
		{"prod_medcore_pre_v1_no_password", func() demoGuardInput { in := preV1Guard(); in.Password = ""; return in }(), false, EnvDemoSeedPassword},
		{"prod_medcore_pre_v1_blank_password", func() demoGuardInput { in := preV1Guard(); in.Password = "   "; return in }(), false, EnvDemoSeedPassword},
		{"prod_medcore_pre_v1_short_password", func() demoGuardInput { in := preV1Guard(); in.Password = "eleven-char"; return in }(), false, "trop court"},
		{"prod_medcore_pre_v1_long_password", func() demoGuardInput { in := preV1Guard(); in.Password = strings.Repeat("x", 73); return in }(), false, "trop long"},
		{"prod_other_db_pre_v1", func() demoGuardInput { in := preV1Guard(); in.DatabaseName = "medcore_prod"; return in }(), false, "production"},
		{"prod_full_demo_db_pre_v1", func() demoGuardInput { in := preV1Guard(); in.DatabaseName = "medcore_full_demo"; return in }(), false, "production"},
		{"prod_medcore_case_variant_pre_v1", func() demoGuardInput { in := preV1Guard(); in.DatabaseName = "MEDCORE"; return in }(), false, "production"},
		{"prod_medcore_spaced_pre_v1", func() demoGuardInput { in := preV1Guard(); in.DatabaseName = " medcore"; return in }(), false, "production"},
		{"dev_medcore_pre_v1", func() demoGuardInput { in := preV1Guard(); in.AppEnv = "development"; return in }(), false, "refusé sur la base"},
		{"neon_prod_all_opt_ins", func() demoGuardInput { in := preV1Guard(); in.DatabaseName = "neondb"; return in }(), false, "Neon"},
		{"neon_named_medcore_all_opt_ins", func() demoGuardInput { in := preV1Guard(); in.DatabaseName = "medcore_neon"; return in }(), false, "Neon"},
		{"neon_dev_all_opt_ins", func() demoGuardInput {
			in := validDemoGuard()
			in.DatabaseName = "neondb"
			in.PreV1Flag = "1"
			return in
		}(), false, "Neon"},
		{"neon_host_prod_medcore_all_opt_ins", func() demoGuardInput {
			in := preV1Guard()
			in.DatabaseHost = "ep-x-123.eu-central-1.aws.neon.tech"
			return in
		}(), false, "Neon"},
		{"neon_fallback_host_prod_medcore_all_opt_ins", func() demoGuardInput {
			in := preV1Guard()
			in.DatabaseHost = "10.0.0.5,ep-x-pooler.neon.tech"
			return in
		}(), false, "Neon"},
		{"neon_host_dev_full_demo", func() demoGuardInput { in := validDemoGuard(); in.DatabaseHost = "ep-x.NEON.tech"; return in }(), false, "Neon"},
		{"local_host_prod_medcore_all_opt_ins", func() demoGuardInput { in := preV1Guard(); in.DatabaseHost = "postgres"; return in }(), true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkDemoGuard(tc.in)
			if tc.allow {
				if err != nil {
					t.Fatalf("must pass: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("must fail closed")
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("unexpected refusal reason %q (want %q)", err, tc.reason)
			}
			if strings.Contains(err.Error(), pwd) || (strings.TrimSpace(tc.in.Password) != "" && strings.Contains(err.Error(), tc.in.Password)) {
				t.Fatal("guard error must never echo the password")
			}
		})
	}
}

// The pre-V1 opt-in alone (or with production but no other opt-in) never passes.
func TestPreV1FlagAloneNeverSuffices(t *testing.T) {
	for _, env := range []string{"production", "development"} {
		for _, db := range []string{"medcore", "medcore_full_demo"} {
			if err := checkDemoGuard(demoGuardInput{AppEnv: env, DatabaseName: db, PreV1Flag: "1"}); err == nil {
				t.Fatalf("%s/%s: pre-V1 flag alone must fail", env, db)
			}
		}
	}
	if isPreV1DemoException(demoGuardInput{AppEnv: "development", DatabaseName: "medcore", PreV1Flag: "1"}) {
		t.Fatal("pre-V1 exception requires APP_ENV=production")
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

// main(): production is refused before any connection unless the pre-V1 opt-in is present;
// with it, the decision is deferred to checkDemoGuard after connecting. The database is
// unreachable on purpose (port 1): no database is ever touched by this test.
func TestSeedMainProductionEarlyRefusal(t *testing.T) {
	if os.Getenv("SEED_PROD_CHILD") == "1" {
		os.Args = []string{"seed", "--demo-full"}
		main()
		return
	}
	run := func(extra ...string) string {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSeedMainProductionEarlyRefusal$")
		cmd.Env = append(os.Environ(), append([]string{"SEED_PROD_CHILD=1", "APP_ENV=production",
			"DATABASE_URL=postgres://nobody@127.0.0.1:1/medcore?sslmode=disable", "JWT_SECRET=test-only-not-a-secret",
			EnvAllowDemoSeed + "=1", EnvDemoSeedPassword + "=demo-only-passphrase-42"}, extra...)...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("seed must not succeed here:\n%s", out)
		}
		if strings.Contains(string(out), "demo-only-passphrase-42") {
			t.Fatal("password leaked in output")
		}
		return string(out)
	}
	if out := run(EnvAllowPreV1DemoSeed + "="); !strings.Contains(out, "interdit en environnement production") {
		t.Fatalf("production without pre-V1 opt-in must be refused before connecting:\n%s", out)
	}
	if out := run(EnvAllowPreV1DemoSeed + "=1"); strings.Contains(out, "interdit en environnement production") {
		t.Fatalf("with pre-V1 opt-in, the early environment refusal must defer to checkDemoGuard:\n%s", out)
	}
}

func TestDatabaseHostsParsesDSNWithoutLeaking(t *testing.T) {
	cases := map[string]string{
		"postgres://u:s3cr3t-pw@ep-x.eu-central-1.aws.neon.tech/medcore?sslmode=require":   "ep-x.eu-central-1.aws.neon.tech",
		"host=postgres port=5432 user=u password=s3cr3t-pw dbname=medcore sslmode=disable": "postgres",
		"postgres://u:s3cr3t-pw@db-a:5432,ep-b.neon.tech:5432/medcore":                     "db-a,ep-b.neon.tech",
	}
	for dsn, want := range cases {
		got, err := databaseHosts(dsn)
		if err != nil || got != want {
			t.Fatalf("hosts=%q err=%v, want %q", got, err, want)
		}
	}
	_, err := databaseHosts("postgres://u:s3cr3t-pw@[bad")
	if err == nil || strings.Contains(err.Error(), "s3cr3t-pw") || strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("unparsable DSN must fail without echoing it: %v", err)
	}
	if !isNeonTarget("medcore", "ep-b.neon.tech") || isNeonTarget("medcore", "postgres") {
		t.Fatal("Neon host detection")
	}
}

// --demo-complete is a regular demo mode: listed in the usage and behind the common guard
// (refused in production before any connection, without the pre-V1 opt-in).
func TestDemoCompleteModeIsGuarded(t *testing.T) {
	if _, ok := demoSeedModes["--demo-complete"]; !ok {
		t.Fatal("--demo-complete must be a declared demo mode")
	}
	if os.Getenv("SEED_COMPLETE_CHILD") == "1" {
		os.Args = []string{"seed", "--demo-complete"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDemoCompleteModeIsGuarded$")
	cmd.Env = append(os.Environ(), "SEED_COMPLETE_CHILD=1", "APP_ENV=production", EnvAllowPreV1DemoSeed+"=",
		"DATABASE_URL=postgres://nobody@127.0.0.1:1/medcore_full_demo?sslmode=disable", "JWT_SECRET=test-only-not-a-secret",
		EnvAllowDemoSeed+"=1", EnvDemoSeedPassword+"=demo-only-passphrase-42")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "interdit en environnement production") {
		t.Fatalf("--demo-complete must be refused in production before connecting:\n%s", out)
	}
	if strings.Contains(string(out), "demo-only-passphrase-42") {
		t.Fatal("password leaked")
	}
}
