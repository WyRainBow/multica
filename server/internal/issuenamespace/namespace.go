// Package issuenamespace owns the fixed document directory every issue gets.
//
// An issue's documents used to appear only once somebody wrote one, which made
// "there is no design doc" and "nobody has looked at the design yet" the same
// observation. The directory is created with the issue instead: six named
// slots, each held open by a placeholder card until real content lands in it.
//
// The rules that make that safe live here rather than in the handler, because
// three different callers — issue creation, the terminal-status cleanup, and
// the namespace read — have to agree on what a slot is and where it lives, and
// a second copy of that list is how the three drift apart.
package issuenamespace

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SlotType separates the two shapes a slot can have.
//
// A document slot holds exactly one card at its own kind. A folder slot holds
// none of its own — its content is whatever is filed beneath it — and its
// placeholder exists only so the empty folder is still visible.
type SlotType string

const (
	SlotDocument SlotType = "document"
	SlotFolder   SlotType = "folder"
)

// Slot is one entry in the directory.
type Slot struct {
	// Name is the path segment and the stable key on the wire.
	Name string
	// Label is what a reader sees. Chinese, matching the product voice and the
	// labels the brief already uses for the live documents.
	Label string
	Type  SlotType
}

// Slots is the directory, in reading order: what is being asked for, how it
// will be built, what was frozen, then the three histories that explain how it
// got there.
//
// Fixed rather than open. A slot that any writer could invent would put the
// directory back where it started — present only where somebody happened to
// create one — and the point of this list is that the same six answers are
// askable of every issue.
var Slots = []Slot{
	{Name: "requirements", Label: "需求底稿", Type: SlotDocument},
	{Name: "design", Label: "技术方案", Type: SlotDocument},
	{Name: "spec", Label: "Spec", Type: SlotDocument},
	{Name: "decisions", Label: "决策", Type: SlotFolder},
	{Name: "rounds", Label: "评审轮次", Type: SlotFolder},
	{Name: "snapshots", Label: "快照", Type: SlotFolder},
}

// BodySnapshotKind is where the issue body as it read at creation is kept.
//
// R0 because it precedes every review round: R1 is the first verdict, and the
// document R1 was given needs a predecessor to be compared against. It is a
// real document, never a placeholder — it has content the moment it is
// written, and marking it otherwise would hide the only copy of the original
// body from every reader.
const BodySnapshotKind = "snapshots/body/R0-created"

var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)

// FallbackPrefix derives an issue prefix from a workspace name for workspaces
// that never chose one. "Jiayuan's Workspace" → "JIA", "My Team" → "MYT".
func FallbackPrefix(name string) string {
	letters := nonAlpha.ReplaceAllString(name, "")
	if len(letters) == 0 {
		return "WS"
	}
	letters = strings.ToUpper(letters)
	if len(letters) > 3 {
		letters = letters[:3]
	}
	return letters
}

// WorkspacePrefix is the prefix an issue key is built from.
func WorkspacePrefix(ws db.Workspace) string {
	if ws.IssuePrefix != "" {
		return ws.IssuePrefix
	}
	return FallbackPrefix(ws.Name)
}

// Key is the human identifier an issue's documents are filed under — the same
// "COC-338" a person types.
func Key(prefix string, number int32) string {
	return prefix + "-" + strconv.Itoa(int(number))
}

// KindFor is the kind path of one slot under an issue.
func KindFor(key, slot string) string { return key + "/" + slot }

// State is one slot as a reader sees it.
type State struct {
	Name  string   `json:"name"`
	Label string   `json:"label"`
	Kind  string   `json:"kind"`
	Type  SlotType `json:"type"`
	// Exists is false only for issues created before the directory existed:
	// nothing holds the slot and nothing was ever filed under it.
	Exists bool `json:"exists"`
	// Placeholder is the ONE answer to "is this real yet". It is read off the
	// card's is_placeholder column and from nothing else — not the title, not
	// whether the body is empty.
	Placeholder bool `json:"placeholder"`
	// CardID is the card sitting exactly at this slot's kind, placeholder or
	// document, so a writer can address it directly. Empty for a folder whose
	// placeholder is gone but which has documents beneath it.
	CardID string `json:"card_id,omitempty"`
	Title  string `json:"title,omitempty"`
	// Count is how many real documents are at or below this slot. Always 0 or
	// 1 for a document slot; the interesting number is on the folders.
	Count int `json:"count"`
}

// Namespace is the whole directory for one issue.
type Namespace struct {
	IssueID string  `json:"issue_id"`
	Key     string  `json:"key"`
	Root    string  `json:"root"`
	Slots   []State `json:"slots"`
}

// View computes the directory from the issue's cards.
//
// THIS IS THE SWITCH POINT. Whether `<KEY>/` is a directory that only ever
// exists as this computation, or is itself a stored index document, is COC-335's
// call and is not made here. Every reader of the namespace goes through this
// function, so changing the answer is an edit to this body — no caller has to
// move, and no caller has its own idea of what a slot is.
//
// cards must be the UNFILTERED set for the issue (ListIssueNamespaceCards):
// filtering placeholders out before this point would erase exactly what the
// view is for.
func View(issueID, key string, cards []db.Card) Namespace {
	ns := Namespace{
		IssueID: issueID,
		Key:     key,
		Root:    key,
		Slots:   make([]State, 0, len(Slots)),
	}
	for _, slot := range Slots {
		kind := KindFor(key, slot.Name)
		state := State{
			Name:  slot.Name,
			Label: slot.Label,
			Kind:  kind,
			Type:  slot.Type,
		}
		for _, card := range cards {
			if !kindWithin(card.Kind, kind) {
				continue
			}
			if card.Kind == kind {
				state.Exists = true
				state.CardID = util.UUIDToString(card.ID)
				state.Title = card.Title
				state.Placeholder = card.IsPlaceholder
			}
			if !card.IsPlaceholder {
				state.Exists = true
				state.Count++
			}
		}
		// A folder with documents under it is occupied even while its own
		// placeholder row still stands: the slot has been answered, and
		// reporting it as pending would send a reader to write what is already
		// there.
		if state.Count > 0 {
			state.Placeholder = false
		}
		ns.Slots = append(ns.Slots, state)
	}
	return ns
}

// kindWithin reports whether a card's kind is the slot itself or sits beneath
// it. The slash is what keeps the boundary on a whole segment — a bare prefix
// test would let "COC-1/roundsomething" count as a round.
func kindWithin(kind, slotKind string) bool {
	return kind == slotKind || strings.HasPrefix(kind, slotKind+"/")
}

// Seed creates a new issue's directory: a placeholder for every slot, plus the
// body snapshot, which is a real document.
//
// Runs inside the issue-creation transaction. An issue that committed without
// its directory would be an issue nobody can tell apart from one whose
// documents were deliberately deleted, and a second round trip after commit
// can half-fail and leave precisely that.
// IsTerminalStatus reports whether an issue has stopped being worked on.
//
// The canonical definition lives here because the namespace lifecycle is what
// hangs on this boundary: entering it prunes the slots still standing empty,
// leaving it restores them. `cancelled` counts with `done` — both are
// decisions about how the work ended.
func IsTerminalStatus(status string) bool {
	return status == "done" || status == "cancelled"
}

// SkipPlaceholders reports whether a newly created issue should be born
// without them.
//
// A card created straight into a terminal status never crosses the boundary
// the cleanup hangs on, so its placeholders would stand empty forever — the
// exact "no empty documents once finished" invariant they exist to serve. An
// issue born finished has no active phase for them to make predictable.
func SkipPlaceholders(status string) bool {
	return IsTerminalStatus(status)
}

func Seed(ctx context.Context, q *db.Queries, ws db.Workspace, issue db.Issue, body string, skipPlaceholders bool) error {
	key := Key(WorkspacePrefix(ws), issue.Number)
	if skipPlaceholders {
		return createSlot(ctx, q, issue,
			KindFor(key, BodySnapshotKind),
			fmt.Sprintf("%s 建卡正文 @ R0", key),
			body,
			false,
		)
	}
	for _, slot := range Slots {
		if err := createSlot(ctx, q, issue, KindFor(key, slot.Name), placeholderTitle(key, slot), "", true); err != nil {
			return err
		}
	}
	return createSlot(ctx, q, issue,
		KindFor(key, BodySnapshotKind),
		fmt.Sprintf("%s 建卡正文 @ R0", key),
		body,
		false,
	)
}

// Reseed restores slots an issue no longer has, and touches nothing it does.
//
// Called when an issue comes back out of a terminal status: the cleanup that
// ran on the way in removed the slots still standing empty, and reopening the
// work means they are askable again. Slots that hold a real document are left
// exactly as they are — a reopen must never overwrite what the issue already
// concluded. The body snapshot is NOT recreated: R0 records the body at
// creation, and rebuilding it now from today's body would file a claim about
// the past that is false.
func Reseed(ctx context.Context, q *db.Queries, ws db.Workspace, issue db.Issue) error {
	cards, err := q.ListIssueNamespaceCards(ctx, db.ListIssueNamespaceCardsParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		return fmt.Errorf("list issue namespace cards: %w", err)
	}
	key := Key(WorkspacePrefix(ws), issue.Number)
	view := View("", key, cards)
	for i, slot := range Slots {
		if view.Slots[i].Exists {
			continue
		}
		if err := createSlot(ctx, q, issue, KindFor(key, slot.Name), placeholderTitle(key, slot), "", true); err != nil {
			return err
		}
	}
	return nil
}

// Prune drops the slots a finished issue never filled.
//
// Only placeholders go. The rule is the column, so a document that happens to
// be empty survives — it was written, and an issue's own record of what it
// chose not to say is still its record.
func Prune(ctx context.Context, q *db.Queries, issue db.Issue) error {
	if err := q.DeleteIssuePlaceholderCards(ctx, db.DeleteIssuePlaceholderCardsParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	}); err != nil {
		return fmt.Errorf("delete issue placeholder cards: %w", err)
	}
	return nil
}

func placeholderTitle(key string, slot Slot) string {
	return fmt.Sprintf("%s %s（待补）", key, slot.Label)
}

func createSlot(
	ctx context.Context,
	q *db.Queries,
	issue db.Issue,
	kind, title, content string,
	placeholder bool,
) error {
	if _, err := q.CreateIssueNamespaceCard(ctx, db.CreateIssueNamespaceCardParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		// The issue's creator, not the current actor: a slot nobody has
		// written into is attributable to whoever opened the work, and the
		// author moves to the real writer when the slot is promoted.
		AuthorType:    issue.CreatorType,
		AuthorID:      issue.CreatorID,
		Title:         title,
		Content:       content,
		Kind:          kind,
		IsPlaceholder: placeholder,
	}); err != nil {
		return fmt.Errorf("create issue namespace card %q: %w", kind, err)
	}
	return nil
}
