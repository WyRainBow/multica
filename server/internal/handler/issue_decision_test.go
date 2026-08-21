package handler

import (
	"strings"
	"testing"
)

// Nothing about a decision's status is stored. A card is superseded because a
// later card says so; a question is open because no later card closed it.
// These tests pin the derivation, because it is the only thing standing
// between "write-once cards" and "nobody can tell what still holds".

func card(id, kind string, fields ...string) struct{ ID, Kind, Content string } {
	body := decisionBlockOpen + "\n" + strings.Join(fields, "\n") + "\n" + decisionBlockClose + "\n\n# body"
	return struct{ ID, Kind, Content string }{ID: id, Kind: kind, Content: body}
}

func TestALaterCardIsWhatMakesAnEarlierOneSuperseded(t *testing.T) {
	t.Parallel()
	decisions, _ := DeriveIssueDecisions([]struct{ ID, Kind, Content string }{
		card("doc1", "K/decisions/D1", "id: D1", "question: 载体", "summary: metadata"),
		card("doc2", "K/decisions/D2", "id: D2", "question: 载体", "summary: 独立卡", "supersedes: D1"),
	})
	if len(decisions) != 2 {
		t.Fatalf("expected both decisions, got %d", len(decisions))
	}
	if !decisions[0].Superseded || decisions[0].SupersededBy != "D2" {
		t.Errorf("D1 should read as superseded by D2, got %+v", decisions[0])
	}
	if decisions[1].Superseded {
		t.Error("D2 has no successor and must still hold")
	}
	// Ordering is by decision number, which is what a reader cites.
	if decisions[0].ID != "D1" || decisions[1].ID != "D2" {
		t.Errorf("decisions out of order: %s then %s", decisions[0].ID, decisions[1].ID)
	}
}

func TestAnOpenQuestionStaysOpenUntilSomethingClosesIt(t *testing.T) {
	t.Parallel()
	cards := []struct{ ID, Kind, Content string }{
		card("doc1", "K/decisions/D1", "id: D1", "summary: 一",
			"open: 命令叫什么", "open: 字段选哪些"),
	}
	_, open := DeriveIssueDecisions(cards)
	if len(open) != 2 {
		t.Fatalf("expected both questions open, got %d: %+v", len(open), open)
	}
	if open[0].Ref != "D1#1" || open[1].Ref != "D1#2" {
		t.Errorf("references must name the card and the position: %+v", open)
	}

	// A later card closes one of them. The earlier card is untouched — it reads
	// as closed only because something else says so.
	cards = append(cards, card("doc2", "K/decisions/D2", "id: D2", "summary: 二", "closes: D1#1"))
	_, open = DeriveIssueDecisions(cards)
	if len(open) != 1 || open[0].Ref != "D1#2" {
		t.Errorf("only the unclosed question should remain: %+v", open)
	}
}

func TestASupersededCardTakesItsOpenQuestionsWithIt(t *testing.T) {
	t.Parallel()
	// The decision that replaced it owns the shape of the problem now. Carrying
	// its predecessor's questions forward would ask the next run to answer
	// something that no longer applies.
	_, open := DeriveIssueDecisions([]struct{ ID, Kind, Content string }{
		card("doc1", "K/decisions/D1", "id: D1", "summary: 一", "open: 只对旧方案成立的问题"),
		card("doc2", "K/decisions/D2", "id: D2", "summary: 二", "supersedes: D1"),
	})
	if len(open) != 0 {
		t.Errorf("a superseded card's questions must not stay open: %+v", open)
	}
}

func TestReferencesAreCaseAndSpaceInsensitive(t *testing.T) {
	t.Parallel()
	// Case and spacing are how a reference gets typed, not what it means. A
	// supersede that silently missed because of a capital letter is the worst
	// kind of no-op: the card claims to replace something and does not.
	decisions, _ := DeriveIssueDecisions([]struct{ ID, Kind, Content string }{
		card("doc1", "K/decisions/D1", "id: D1", "summary: 一"),
		card("doc2", "K/decisions/D2", "id: D2", "summary: 二", "supersedes:  d1 "),
	})
	if !decisions[0].Superseded {
		t.Error("a lowercase reference failed to supersede")
	}
}

func TestTheRecorderAndTheDeciderAreKeptApart(t *testing.T) {
	t.Parallel()
	// The signature identifies whoever ran the command, which is usually an
	// agent writing down a person's choice. Collapsing them would attribute
	// every human decision to an agent.
	decisions, _ := DeriveIssueDecisions([]struct{ ID, Kind, Content string }{
		card("doc1", "K/decisions/D1", "id: D1", "summary: 一",
			"decided_by: 用户", "recorded_by: Claude"),
	})
	if decisions[0].DecidedBy != "用户" || decisions[0].RecordedBy != "Claude" {
		t.Errorf("decider and recorder must both survive: %+v", decisions[0])
	}
}

func TestANoteInTheDecisionsFolderIsNotADecision(t *testing.T) {
	t.Parallel()
	// A hand-written file with no header is somebody's note. Inventing a
	// decision from it would put words in their mouth.
	decisions, open := DeriveIssueDecisions([]struct{ ID, Kind, Content string }{
		{ID: "doc1", Kind: "K/decisions/notes", Content: "# 随手记\n\n没有头部。"},
	})
	if len(decisions) != 0 || len(open) != 0 {
		t.Errorf("a headerless file must not become a decision: %+v / %+v", decisions, open)
	}
}

func TestNonDecisionCardsAreIgnored(t *testing.T) {
	t.Parallel()
	decisions, _ := DeriveIssueDecisions([]struct{ ID, Kind, Content string }{
		card("doc1", "K/rounds/R1-方案评审", "id: D1", "summary: 不该被当成决策"),
		card("doc2", "K/spec", "id: D2", "summary: 也不该"),
	})
	if len(decisions) != 0 {
		t.Errorf("only cards under decisions/ are decisions: %+v", decisions)
	}
}

func TestASupersedeNamingNothingIsNotSilentlyDropped(t *testing.T) {
	t.Parallel()
	// A typo that makes a supersede never take effect is worth surfacing, not
	// hiding. Here it simply has no target, and the live card still holds —
	// but the card that was meant to be replaced is unaffected, which is the
	// observable symptom a reader can act on.
	decisions, _ := DeriveIssueDecisions([]struct{ ID, Kind, Content string }{
		card("doc1", "K/decisions/D1", "id: D1", "summary: 一"),
		card("doc2", "K/decisions/D2", "id: D2", "summary: 二", "supersedes: D9"),
	})
	if decisions[0].Superseded {
		t.Error("D1 was superseded by a card that never named it")
	}
	if decisions[1].Superseded {
		t.Error("D2 superseded nothing that exists; it must still hold")
	}
}
