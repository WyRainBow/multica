package handler

import (
	"errors"
	"strings"
	"testing"
)

// An unreachable database used to exit 0, so `go test` printed `ok` — byte for
// byte what a full pass looks like. These pin the two halves of the fix: the
// exit code, and a notice that says what to type.

func TestAnUnreachableDatabaseFailsUnlessSkipIsSanctioned(t *testing.T) {
	t.Setenv(allowDatabaseSkipEnv, "")
	if got := databaseUnreachableExitCode(); got != 1 {
		t.Errorf("exit code = %d, want 1: a silent 0 reads as a full pass", got)
	}
	t.Setenv(allowDatabaseSkipEnv, "1")
	if got := databaseUnreachableExitCode(); got != 0 {
		t.Errorf("exit code = %d, want 0 when the skip is opted into", got)
	}
}

func TestTheNoticeSaysWhatToType(t *testing.T) {
	err := errors.New(`role "multica" does not exist`)
	dsn := "postgres://multica:multica@localhost:5432/multica?sslmode=disable"

	t.Setenv(allowDatabaseSkipEnv, "")
	notice := unreachableDatabaseNotice(dsn, err)
	for _, want := range []string{
		"FAIL",
		// The DSN matters: the usual cause is the wrong port, which looks
		// identical to having no database at all.
		dsn,
		`role "multica" does not exist`,
		"DATABASE_URL=postgres://multica:multica@localhost:5433",
		allowDatabaseSkipEnv,
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice is missing %q:\n%s", want, notice)
		}
	}

	// With the skip sanctioned it must still be unmistakable that nothing ran.
	t.Setenv(allowDatabaseSkipEnv, "1")
	skipped := unreachableDatabaseNotice(dsn, err)
	if strings.Contains(skipped, "FAIL") {
		t.Error("a sanctioned skip must not claim failure")
	}
	if !strings.Contains(skipped, "did NOT run") {
		t.Errorf("a sanctioned skip must still say the tests did not run:\n%s", skipped)
	}
}
