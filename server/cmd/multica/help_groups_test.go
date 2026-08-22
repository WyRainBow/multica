package main

import (
	"strings"
	"testing"
)

// The root help template renders only commands that belong to a declared
// group (see rootHelpTemplate: `commandsInGroup $.Commands .ID`). A command
// registered without a GroupID is therefore INVISIBLE in `multica --help` —
// it works when typed, and does not exist to anyone reading the list.
//
// That happened to `card`: the concept existed, the help did not show it, and
// a reader concluded there was no such thing and filed documents as issues
// instead. The cost is not the missing line; it is that "not in the list" is
// indistinguishable from "not implemented".
//
// Adding a command is two steps and only the first one is enforced by the
// compiler. This is the second.

func TestEveryRootCommandAppearsInHelp(t *testing.T) {
	var ungrouped []string
	for _, cmd := range rootCmd.Commands() {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			continue
		}
		if cmd.GroupID == "" {
			ungrouped = append(ungrouped, cmd.Name())
		}
	}
	if len(ungrouped) > 0 {
		t.Fatalf("these commands have no GroupID and are invisible in `multica --help`: %s\n"+
			"set one in main.go (groupCore / groupRuntime / groupAdditional)",
			strings.Join(ungrouped, ", "))
	}
}

// A GroupID that names a group the root never declared renders nowhere
// either — same invisibility, different typo.
func TestRootCommandGroupsAreDeclared(t *testing.T) {
	declared := map[string]bool{}
	for _, group := range rootCmd.Groups() {
		declared[group.ID] = true
	}
	for _, cmd := range rootCmd.Commands() {
		if cmd.GroupID == "" || cmd.Hidden {
			continue
		}
		if !declared[cmd.GroupID] {
			t.Fatalf("command %q is in group %q, which the root never declared",
				cmd.Name(), cmd.GroupID)
		}
	}
}

// The one that started this: cards are a top-level noun, not a subcommand of
// issue, and someone scanning the list has to be able to find them.
func TestCardCommandIsListed(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "wiki" {
			if cmd.GroupID != groupCore {
				t.Fatalf("wiki is in group %q, want %q", cmd.GroupID, groupCore)
			}
			return
		}
	}
	t.Fatal("wiki is not registered on the root command")
}
