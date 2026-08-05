package main

import (
	"strings"
	"testing"
)

// `multica issue get` on a finished issue prints what `status: "done"` means,
// because nothing else does. The two facts it carries are separate: the body
// is closed (409 on write), and the body describes what was true when the work
// finished. A reader who only learns the first one still treats a
// three-month-old design as current.

func terminalNotice(t *testing.T, issue map[string]any) string {
	t.Helper()
	capture := captureStderr(t)
	warnTerminalIssueIsARecord(issue)
	return capture.read()
}

func TestTerminalNotice_FiresOnDone(t *testing.T) {
	out := terminalNotice(t, map[string]any{"identifier": "COC-40", "status": "done"})
	if !strings.Contains(out, "COC-40") {
		t.Fatalf("notice does not name the issue: %q", out)
	}
	if !strings.Contains(out, "closed record") {
		t.Fatalf("notice does not say it is a record: %q", out)
	}
	if !strings.Contains(out, "409") {
		t.Fatalf("notice does not say writes are refused: %q", out)
	}
	// The staleness half is the one a reader can act on wrongly, so it has to
	// survive any future rewording of this string.
	if !strings.Contains(out, "not necessarily what is true now") {
		t.Fatalf("notice does not warn that the facts may have moved on: %q", out)
	}
	if !strings.Contains(out, "superseded_by") {
		t.Fatalf("notice does not point at where a successor would be recorded: %q", out)
	}
}

// Cancelled closes the body for the same reason done does — it records how the
// work ended — so it gets the same notice.
func TestTerminalNotice_FiresOnCancelled(t *testing.T) {
	out := terminalNotice(t, map[string]any{"identifier": "COC-41", "status": "cancelled"})
	if !strings.Contains(out, "cancelled") {
		t.Fatalf("notice does not name the status: %q", out)
	}
}

// On every other status the notice would be noise on every read, and noise
// that appears everywhere stops being read where it matters.
func TestTerminalNotice_SilentOnUnfinishedStatuses(t *testing.T) {
	for _, status := range []string{"backlog", "todo", "in_progress", "in_review"} {
		out := terminalNotice(t, map[string]any{"identifier": "COC-42", "status": status})
		if out != "" {
			t.Fatalf("status %q printed a notice: %q", status, out)
		}
	}
}

// The notice must not imply the content is worthless. Why a decision was made
// and what was rejected are usually written down nowhere else and are still
// true; an agent told the issue is "expired" skips exactly that.
func TestTerminalNotice_DoesNotCallTheRecordExpired(t *testing.T) {
	out := terminalNotice(t, map[string]any{"identifier": "COC-43", "status": "done"})
	for _, banned := range []string{"expired", "outdated", "obsolete", "stale"} {
		if strings.Contains(strings.ToLower(out), banned) {
			t.Fatalf("notice calls the record %q, which invites an agent to discount it: %q",
				banned, out)
		}
	}
	if !strings.Contains(out, "Accurate about the past") {
		t.Fatalf("notice drops the half that says the record is still worth reading: %q", out)
	}
}
