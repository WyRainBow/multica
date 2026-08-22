package handler

import (
	"sort"
	"strconv"
	"strings"
)

// Decisions on an issue, and which of them still hold.
//
// Nothing about a decision's status is stored. A card is superseded because a
// later card says it supersedes it; a question is open because no later card
// has closed it. That leaves exactly one way to change anything — write a new
// card referencing the old one — so there is no second copy of the state that
// can disagree with the text.
//
// Derived here rather than at write time because the claim already loads every
// card on the issue (ListCardsForIssue), so the whole computation is a pass
// over data that has already been fetched.

const decisionsKindSegment = "/decisions/"

// decisionBlock delimits the machine-readable header a decision card carries.
// An HTML comment: inert in every renderer, harmless as agent context.
const (
	decisionBlockOpen  = "<!-- decision"
	decisionBlockClose = "-->"
)

// IssueDecision is one decision as the brief needs to see it.
type IssueDecision struct {
	ID         string `json:"id"`                    // D1, D2, …
	DocID      string `json:"doc_id"`                // the card's own id
	Question   string `json:"question,omitempty"`    // what it answers
	Summary    string `json:"summary,omitempty"`     // what was chosen
	DecidedBy  string `json:"decided_by,omitempty"`  // who made the call
	RecordedBy string `json:"recorded_by,omitempty"` // who wrote it down; often not the same
	// Superseded is derived: some later card names this one. The card itself is
	// never touched, which is why this cannot be read off it.
	Superseded bool `json:"superseded,omitempty"`
	// SupersededBy names the card that replaced it, so a reader lands on the
	// decision that holds now instead of hunting for it.
	SupersededBy string `json:"superseded_by,omitempty"`
	// Affects names the live documents this decision changed, so a reader of
	// one of those documents can find why it says what it says.
	Affects []string `json:"affects,omitempty"`
}

// IssueOpenQuestion is a question some decision left open that none has closed.
type IssueOpenQuestion struct {
	Ref      string `json:"ref"`                 // D<n>#<i> — the card that raised it and its position
	Question string `json:"question"`            // the question itself
	RaisedBy string `json:"raised_by,omitempty"` // the card that raised it
}

// decisionCard is one parsed card, before cross-referencing.
type decisionCard struct {
	id         string
	docID      string
	number     int
	question   string
	summary    string
	decidedBy  string
	recordedBy string
	open       []string
	closes     []string
	supersedes []string
	affects    []string
}

// DeriveIssueDecisions turns the issue's decision cards into "what holds now"
// and "what is still open".
//
// Order is by decision number, not by write time: the number is what a reader
// cites and what --number exists to keep honest when a decision predates its
// card.
func DeriveIssueDecisions(cards []struct{ ID, Kind, Content string }) ([]IssueDecision, []IssueOpenQuestion) {
	var parsed []decisionCard
	for _, card := range cards {
		if !strings.Contains(card.Kind, decisionsKindSegment) {
			continue
		}
		if c, ok := parseDecisionCard(card.ID, card.Kind, card.Content); ok {
			parsed = append(parsed, c)
		}
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].number < parsed[j].number })

	// Who replaced whom. A card naming a decision that does not exist is kept
	// as a claim about nothing rather than dropped: silently ignoring it would
	// hide a typo that makes a supersede never take effect.
	supersededBy := map[string]string{}
	closed := map[string]bool{}
	for _, c := range parsed {
		for _, target := range c.supersedes {
			supersededBy[normalizeDecisionRef(target)] = c.id
		}
		for _, ref := range c.closes {
			closed[normalizeDecisionRef(ref)] = true
		}
	}

	decisions := make([]IssueDecision, 0, len(parsed))
	var open []IssueOpenQuestion
	for _, c := range parsed {
		by := supersededBy[c.id]
		decisions = append(decisions, IssueDecision{
			ID: c.id, DocID: c.docID,
			Question: c.question, Summary: c.summary,
			DecidedBy: c.decidedBy, RecordedBy: c.recordedBy,
			Superseded: by != "", SupersededBy: by, Affects: c.affects,
		})
		// A superseded card's open questions go with it. The decision that
		// replaced it owns the shape of the problem now, and carrying forward
		// questions from a decision nobody follows would ask the next run to
		// answer something that no longer applies.
		if by != "" {
			continue
		}
		for i, q := range c.open {
			ref := c.id + "#" + strconv.Itoa(i+1)
			if closed[ref] {
				continue
			}
			open = append(open, IssueOpenQuestion{Ref: ref, Question: q, RaisedBy: c.id})
		}
	}
	return decisions, open
}

// normalizeDecisionRef makes "d1", " D1 " and "D1" the same reference. Case and
// spacing are how a reference gets typed, not what it means, and a supersede
// that silently missed because of a capital letter is the worst kind of no-op.
func normalizeDecisionRef(ref string) string {
	return strings.ToUpper(strings.TrimSpace(ref))
}

// parseDecisionCard reads the machine-readable header. A card without one is
// skipped rather than guessed at: a hand-written file in the decisions folder
// is somebody's note, and inventing a decision from it would put words in
// their mouth.
func parseDecisionCard(docID, kind, content string) (decisionCard, bool) {
	start := strings.Index(content, decisionBlockOpen)
	if start < 0 {
		return decisionCard{}, false
	}
	rest := content[start+len(decisionBlockOpen):]
	end := strings.Index(rest, decisionBlockClose)
	if end < 0 {
		return decisionCard{}, false
	}

	c := decisionCard{docID: docID}
	for _, line := range strings.Split(rest[:end], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch key {
		case "id":
			c.id = value
		case "question":
			c.question = value
		case "summary":
			c.summary = value
		case "decided_by":
			c.decidedBy = value
		case "recorded_by":
			c.recordedBy = value
		case "open":
			c.open = append(c.open, value)
		case "closes":
			c.closes = append(c.closes, value)
		case "supersedes":
			c.supersedes = append(c.supersedes, value)
		case "affects":
			c.affects = append(c.affects, value)
		}
	}
	if c.id == "" {
		// Fall back to the kind's last segment, which is where the id came from
		// in the first place. A card that lost its header id is still placeable.
		if idx := strings.LastIndex(kind, "/"); idx >= 0 {
			c.id = strings.TrimSpace(kind[idx+1:])
		}
	}
	if c.id == "" {
		return decisionCard{}, false
	}
	c.id = normalizeDecisionRef(c.id)
	c.number = decisionNumberFromID(c.id)
	return c, true
}

// decisionNumberFromID reads the ordinal out of "D12". Unparseable ids sort
// last rather than first: an id nobody can order is more likely a mistake than
// the earliest decision on the issue.
func decisionNumberFromID(id string) int {
	if !strings.HasPrefix(id, "D") {
		return 1 << 30
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, "D"))
	if err != nil || n <= 0 {
		return 1 << 30
	}
	return n
}
