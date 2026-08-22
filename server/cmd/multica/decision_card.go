package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The decision card's machine-readable header.
//
// Status is never stored, so everything a reader needs to work out whether a
// decision still holds has to be legible in the cards themselves: which
// decision this one replaces, which open question it settles, and which
// questions it leaves open. Those three references are the only mutation
// mechanism in the system — a card is superseded because a later one says so,
// and a question is open because no later one has closed it.
//
// The block is an HTML comment: inert in every Markdown renderer, harmless when
// the card is fed to an agent as context, and unambiguous to parse. The prose
// below it is for people; this is what the derivation reads.

const (
	decisionBlockOpen  = "<!-- decision"
	decisionBlockClose = "-->"
)

// DecisionMeta is the header a decision card carries.
type DecisionMeta struct {
	ID         string   // D1, D2, …
	Question   string   // what this decision answers
	Summary    string   // what was chosen
	DecidedBy  string   // who made the call
	RecordedBy string   // who ran the command; often not the same person
	DecidedAt  string   // RFC3339
	SHA        string   // baseline it was taken against
	Open       []string // questions deliberately left open
	Closes     []string // open questions this settles, as D<n>#<i>
	Supersedes []string // decisions this replaces, as D<n>
	// Affects names the live documents this decision changed. A decision is
	// not a fourth document type — it is the event that moves one of the
	// three, so the link belongs on the decision rather than in the document
	// it changed, where it would have to be maintained by hand.
	Affects []string
}

var decisionKindRe = regexp.MustCompile(`^D(\d+)$`)

// ParseDecisionNumber reads the number off a decision card's last kind
// segment: "D2" → 2.
func ParseDecisionNumber(segment string) (int, bool) {
	m := decisionKindRe.FindStringSubmatch(strings.TrimSpace(segment))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// NextDecisionNumber is one past the highest already recorded on this issue.
//
// Counted from the cards rather than a stored counter, so a decision cannot be
// recorded twice under one number and a gap stays visible. Decisions taken in a
// comment thread leave no card and are invisible here, which is what --number
// exists to correct.
func NextDecisionNumber(key string, docs []docRow) int {
	prefix := key + "/" + decisionsFolder + "/"
	highest := 0
	for _, doc := range docs {
		kind := strings.TrimSpace(doc.Kind)
		if !strings.HasPrefix(kind, prefix) {
			continue
		}
		if n, ok := ParseDecisionNumber(strings.TrimPrefix(kind, prefix)); ok && n > highest {
			highest = n
		}
	}
	return highest + 1
}

// RenderDecisionCard builds the card: the machine-readable header, a summary a
// person can read without parsing anything, and the full record beneath.
func RenderDecisionCard(meta DecisionMeta, body string) string {
	var b strings.Builder

	b.WriteString(decisionBlockOpen + "\n")
	writeDecisionField(&b, "id", meta.ID)
	writeDecisionField(&b, "question", meta.Question)
	writeDecisionField(&b, "summary", meta.Summary)
	writeDecisionField(&b, "decided_by", meta.DecidedBy)
	writeDecisionField(&b, "recorded_by", meta.RecordedBy)
	writeDecisionField(&b, "decided_at", meta.DecidedAt)
	writeDecisionField(&b, "sha", meta.SHA)
	for _, v := range meta.Open {
		writeDecisionField(&b, "open", v)
	}
	for _, v := range meta.Closes {
		writeDecisionField(&b, "closes", v)
	}
	for _, v := range meta.Supersedes {
		writeDecisionField(&b, "supersedes", v)
	}
	for _, v := range meta.Affects {
		writeDecisionField(&b, "affects", v)
	}
	b.WriteString(decisionBlockClose + "\n\n")

	fmt.Fprintf(&b, "# %s · %s\n\n", meta.ID, meta.Summary)
	fmt.Fprintf(&b, "- **问题**：%s\n", meta.Question)
	// Named separately even when identical, so a reader never has to guess
	// whether the single name means "decided" or "typed it up".
	fmt.Fprintf(&b, "- **拍板者**：%s\n", meta.DecidedBy)
	fmt.Fprintf(&b, "- **记录者**：%s\n", meta.RecordedBy)
	if meta.DecidedAt != "" {
		fmt.Fprintf(&b, "- **定于**：%s\n", meta.DecidedAt)
	}
	if meta.SHA != "" {
		fmt.Fprintf(&b, "- **基线 SHA**：`%s`\n", meta.SHA)
	}
	if len(meta.Supersedes) > 0 {
		fmt.Fprintf(&b, "- **取代**：%s（前卡正文不动，它读作被取代是因为本卡这么说）\n",
			strings.Join(meta.Supersedes, "、"))
	}
	if len(meta.Closes) > 0 {
		fmt.Fprintf(&b, "- **关闭未决**：%s\n", strings.Join(meta.Closes, "、"))
	}
	if len(meta.Affects) > 0 {
		fmt.Fprintf(&b, "- **改动了**：%s（决策的正身在本卡，那些文档里只留结果）\n",
			strings.Join(meta.Affects, "、"))
	}
	b.WriteString("\n")

	if trimmed := strings.TrimSpace(body); trimmed != "" {
		b.WriteString(strings.TrimRight(body, "\n"))
		b.WriteString("\n")
	}

	if len(meta.Open) > 0 {
		b.WriteString("\n## 本卡留下的未决\n\n")
		b.WriteString("> 这些不会自己消失。它们保持未决，直到有一张后继卡 `--closes` 掉。\n\n")
		for i, q := range meta.Open {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
	}
	return b.String()
}

// writeDecisionField omits empty values rather than writing a blank line: an
// absent field and a field set to nothing are different claims.
func writeDecisionField(b *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	// A newline inside a value would end the field early and turn the rest into
	// an unparseable line, so it is folded rather than trusted.
	value = strings.Join(strings.Fields(value), " ")
	fmt.Fprintf(b, "%s: %s\n", key, value)
}
