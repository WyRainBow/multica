package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Where this machine keeps its copies of the workspace's writing.
//
// `instructions status` and `skills status` can each answer "is this copy
// still current" — but only about a path they are handed. Nothing records the
// paths, so the daily patrol would have to guess at conventional locations,
// report on files the user does not use, and stay quiet about the ones they
// do. That is the same shape of failure as the checks themselves: a mechanism
// that works if someone remembers to point it at the right thing.
//
// So a pull writes down where it pulled to, and the patrol reads that. Pulling
// somewhere new makes the check follow on its own.

// pullTargetKindInstructions and pullTargetKindSkills label the two mirrors.
const (
	pullTargetKindInstructions = "instructions"
	pullTargetKindSkills       = "skills"
)

// pullTarget is one recorded destination.
type pullTarget struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"` // absolute, as written
	Workspace string `json:"workspace"`
}

// pullTargetsPath is the registry file. Under the CLI's own directory rather
// than beside the copies: the copies live in whatever agent config directory
// the user chose, and several of them can exist at once.
func pullTargetsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".multica", "pull-targets.json"), nil
}

// loadPullTargets reads the registry. A missing or unreadable file yields no
// targets and no error: the patrol reporting nothing is correct for a machine
// that has never pulled, and a hard failure here would take the other four
// categories down with it.
func loadPullTargets() []pullTarget {
	path, err := pullTargetsPath()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var targets []pullTarget
	if err := json.Unmarshal(raw, &targets); err != nil {
		return nil
	}
	return targets
}

// recordPullTarget adds or updates one destination.
//
// Keyed on (kind, path) so re-pulling the same file does not accumulate rows,
// and so a file re-pulled from a different workspace overwrites rather than
// duplicates — a path holds one workspace's copy at a time, which is a rule
// the pull itself already enforces.
func recordPullTarget(target pullTarget) error {
	target.Path = strings.TrimSpace(target.Path)
	if target.Path == "" {
		return nil
	}
	path, err := pullTargetsPath()
	if err != nil {
		return err
	}
	targets := loadPullTargets()
	replaced := false
	for i := range targets {
		if targets[i].Kind == target.Kind && targets[i].Path == target.Path {
			targets[i] = target
			replaced = true
			break
		}
	}
	if !replaced {
		targets = append(targets, target)
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Kind != targets[j].Kind {
			return targets[i].Kind < targets[j].Kind
		}
		return targets[i].Path < targets[j].Path
	})
	encoded, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return writeFileAtomic(path, append(encoded, '\n'))
}

// notePullTarget records a destination without letting a registry problem fail
// the pull that succeeded. The copy on disk is the thing that matters; the
// registry only decides whether anything will later notice it went stale.
func notePullTarget(kind, path, workspace string) {
	if err := recordPullTarget(pullTarget{Kind: kind, Path: path, Workspace: workspace}); err != nil {
		fmt.Fprintf(os.Stderr,
			"Note: pulled successfully, but this destination was not recorded (%v); "+
				"the daily patrol will not report it going stale.\n", err)
	}
}
