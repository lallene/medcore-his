// Package backuprehearsal guards the GRC backup/restore rehearsal.
// It is test-only. It never accepts the production database names.
package backuprehearsal

import (
	"fmt"
	"regexp"
)

const (
	ForbiddenMedcore    = "medcore"
	ForbiddenMedcoreHIS = "medcore_his"
)

var (
	sourceName  = regexp.MustCompile(`^medcore_b03_source_[0-9]{10,}$`)
	restoreName = regexp.MustCompile(`^medcore_restore_test_[0-9]{10,}$`)
	corruptName = regexp.MustCompile(`^medcore_restore_corrupt_[0-9]{10,}$`)
)

// CheckNames refuses production-like names, a missing name, and a restore
// target that is the source database.
func CheckNames(source, restore, corrupt string) error {
	if err := checkOne("source", source, sourceName); err != nil {
		return err
	}
	if err := checkOne("restore", restore, restoreName); err != nil {
		return err
	}
	if err := checkOne("corrupt", corrupt, corruptName); err != nil {
		return err
	}
	if restore == source || corrupt == source || corrupt == restore {
		return fmt.Errorf("restore target must be a different database")
	}
	return nil
}

func checkOne(role, name string, pattern *regexp.Regexp) error {
	if name == "" {
		return fmt.Errorf("%s database name is missing", role)
	}
	if name == ForbiddenMedcore || name == ForbiddenMedcoreHIS {
		return fmt.Errorf("%s database %q is forbidden", role, name)
	}
	if !pattern.MatchString(name) {
		return fmt.Errorf("%s database %q is not an allowed rehearsal name", role, name)
	}
	return nil
}
