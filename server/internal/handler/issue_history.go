package handler

import (
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// One card's history, in the three shapes it actually has.
//
// Decisions, review rounds and documents are three kinds of object with three
// separate sources of truth and two separate derivations (COC-328 D1): a
// decision's status comes only from other decisions, a round's verdict only
// from the round document. Serving them mixed into one timeline would force the
// reader to work out which system each line belongs to, and the field sets do
// not overlap enough for one table to hold all three without mostly-empty
// columns.
//
// Everything here is derived per request from documents already stored. Nothing
// is written, and no status is persisted — the whole point of the decision
// model is that there is exactly one way to change what holds, which is to
// write another decision.

const (
	roundsKindSegment    = "/rounds/"
	reviewsKindSegment   = "/reviews/"
	snapshotsKindSegment = "/snapshots/"
)

// Row statuses. The first three are decisions proper; the last two are rows
// that exist to make an absence visible rather than to state a decision.
const (
	decisionStatusOpen       = "open"
	decisionStatusCurrent    = "current"
	decisionStatusSuperseded = "superseded"
	// gap: a decision number nothing was ever filed under. Rendered as a row
	// reading "no record" rather than closed up, because renumbering would
	// invalidate references other cards already made, and filling it in would
	// invent a decision nobody took.
	decisionStatusGap = "gap"
	// legacy: a document in decisions/ with no machine-readable header. It
	// predates `issue decide`. Its status is not derived and never guessed —
	// inventing a decision from somebody's note puts words in their mouth.
	decisionStatusLegacy = "legacy"
)

// IssueHistoryDecisionRow is one line of the decision table, whatever kind of
// line it is. One row per decision or open question, never a detail row: the
// full record lives in the document the row points at.
type IssueHistoryDecisionRow struct {
	// ID is "D5" for a decision, "D8#1" for an open question, "D3" for a gap.
	ID     string `json:"id"`
	Status string `json:"status"`
	DocID  string `json:"doc_id,omitempty"`
	// Number is the decision's ordinal, so the client can keep the numeric
	// spine intact without re-parsing ids.
	Number       int      `json:"number"`
	Question     string   `json:"question,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	DecidedBy    string   `json:"decided_by,omitempty"`
	RecordedBy   string   `json:"recorded_by,omitempty"`
	DecidedAt    string   `json:"decided_at,omitempty"`
	SupersededBy string   `json:"superseded_by,omitempty"`
	RaisedBy     string   `json:"raised_by,omitempty"`
	Affects      []string `json:"affects,omitempty"`
	// Title carries a legacy card's document title. Those have no question and
	// no summary — the title is all a reader gets before opening it.
	Title string `json:"title,omitempty"`
}

// IssueHistoryRound is one closed review round.
type IssueHistoryRound struct {
	ID      string `json:"id"`      // R2
	Number  int    `json:"number"`  // 2
	Station string `json:"station"` // 代码评审
	// Verdict is whatever the closer recorded — approve, request_changes,
	// block. It answers "did this round end", not "did the work pass": nearly
	// every round closes approve, including cards that ran four rounds at one
	// station, so it is not a quality signal on its own.
	Verdict  string `json:"verdict,omitempty"`
	Summary  string `json:"summary,omitempty"`
	DocID    string `json:"doc_id"`
	Title    string `json:"title,omitempty"`
	ClosedAt string `json:"closed_at,omitempty"`
	// Legacy marks a document from reviews/, the system that rounds/ replaced.
	// Read-only and no longer written to (COC-328 D1); listed so eleven of them
	// do not silently disappear from every view.
	Legacy bool `json:"legacy,omitempty"`
}

// IssueHistoryDocument is one document row: either a frozen snapshot or the
// live document it was taken from.
//
// Snapshots are real records, written by `issue round close` as
// <KEY>/snapshots/<doc>/R<n>-<station>. They are the only thing on this card
// that holds a document as it stood at a moment; the live rows beneath them
// hold whatever it says now, which is a different question and is why they are
// not mixed together.
type IssueHistoryDocument struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Snapshot separates the frozen rows from the live ones. A live row's
	// content moves under the reader; a snapshot's does not.
	Snapshot bool `json:"snapshot,omitempty"`
	// SnapshotOf names the document this froze — "spec", "requirements".
	SnapshotOf string `json:"snapshot_of,omitempty"`
	// TakenAt names the round it was frozen at — "R4-代码评审". A snapshot with
	// no round in its kind keeps this empty rather than guessing one.
	TakenAt   string `json:"taken_at,omitempty"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"created_at"`
}

type IssueHistoryResponse struct {
	Decisions []IssueHistoryDecisionRow `json:"decisions"`
	Rounds    []IssueHistoryRound       `json:"rounds"`
	Documents []IssueHistoryDocument    `json:"documents"`
}

var roundKindSegmentRE = regexp.MustCompile(`^R(\d+)-(.+)$`)

// ListIssueHistory handles GET /api/issues/{id}/history.
func (h *Handler) ListIssueHistory(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListCardsForIssue(r.Context(), db.ListCardsForIssueParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("list issue history failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load history")
		return
	}

	resp := IssueHistoryResponse{
		Decisions: buildDecisionRows(rows),
		Rounds:    buildRoundRows(rows),
		Documents: buildDocumentRows(rows),
	}
	writeJSON(w, http.StatusOK, resp)
}

// buildDecisionRows assembles every line of the decision table.
//
// Four sources, one list: the derived decisions, the open questions they left,
// the numbers nothing was filed under, and the header-less documents the
// deriver drops. The last two exist only because the deriver is silent about
// them — a card that fails to parse produces no output at all, so without the
// difference taken here five real documents are invisible in every view.
func buildDecisionRows(cards []db.Card) []IssueHistoryDecisionRow {
	forDerivation := make([]struct{ ID, Kind, Content string }, 0, len(cards))
	for _, c := range cards {
		forDerivation = append(forDerivation, struct{ ID, Kind, Content string }{
			ID: uuidToString(c.ID), Kind: c.Kind, Content: c.Content,
		})
	}
	decisions, open := DeriveIssueDecisions(forDerivation)

	rows := make([]IssueHistoryDecisionRow, 0, len(decisions)+len(open))
	seenNumbers := map[int]bool{}
	derivedDocs := map[string]bool{}
	highest := 0

	for _, d := range decisions {
		n := decisionNumberFromID(d.ID)
		status := decisionStatusCurrent
		if d.Superseded {
			status = decisionStatusSuperseded
		}
		rows = append(rows, IssueHistoryDecisionRow{
			ID: d.ID, Status: status, DocID: d.DocID, Number: n,
			Question: d.Question, Summary: d.Summary,
			DecidedBy: d.DecidedBy, RecordedBy: d.RecordedBy, DecidedAt: d.DecidedAt,
			SupersededBy: d.SupersededBy, Affects: d.Affects,
		})
		derivedDocs[d.DocID] = true
		if n > 0 && n < 1<<30 {
			seenNumbers[n] = true
			if n > highest {
				highest = n
			}
		}
	}
	for _, q := range open {
		rows = append(rows, IssueHistoryDecisionRow{
			ID: q.Ref, Status: decisionStatusOpen, Number: decisionNumberFromID(q.RaisedBy),
			Question: q.Question, RaisedBy: q.RaisedBy,
		})
	}

	// The difference: decisions/ documents the deriver never emitted. Taken by
	// document id rather than by re-parsing, so this stays correct if the
	// parser's rules change.
	legacy := make([]IssueHistoryDecisionRow, 0, 4)
	for _, c := range cards {
		if !strings.Contains(c.Kind, decisionsKindSegment) {
			continue
		}
		id := uuidToString(c.ID)
		if derivedDocs[id] {
			continue
		}
		label := c.Kind
		if idx := strings.LastIndex(c.Kind, "/"); idx >= 0 {
			label = strings.TrimSpace(c.Kind[idx+1:])
		}
		n := decisionNumberFromID(strings.ToUpper(label))
		legacy = append(legacy, IssueHistoryDecisionRow{
			ID: label, Status: decisionStatusLegacy, DocID: id, Number: n, Title: c.Title,
		})
		if n > 0 && n < 1<<30 {
			seenNumbers[n] = true
			if n > highest {
				highest = n
			}
		}
	}

	// Gaps. Only inside the range that exists: a card with D1 and D2 is not
	// missing D3, it simply has not got there yet.
	for n := 1; n < highest; n++ {
		if seenNumbers[n] {
			continue
		}
		rows = append(rows, IssueHistoryDecisionRow{
			ID: "D" + strconv.Itoa(n), Status: decisionStatusGap, Number: n,
		})
	}

	sortDecisionRows(rows)
	sort.SliceStable(legacy, func(i, j int) bool { return legacy[i].Number < legacy[j].Number })
	return append(rows, legacy...)
}

// sortDecisionRows puts open questions first, then everything else newest
// first.
//
// Open first because an unanswered question is the only row that can block the
// next run. After that, plain descending number rather than grouping by status:
// it keeps a superseded decision directly under the one that replaced it, and
// that pair is the relationship this table exists to show. Gaps keep their slot
// in the same sequence, which is what makes them read as a gap.
func sortDecisionRows(rows []IssueHistoryDecisionRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		iOpen := rows[i].Status == decisionStatusOpen
		jOpen := rows[j].Status == decisionStatusOpen
		if iOpen != jOpen {
			return iOpen
		}
		if rows[i].Number != rows[j].Number {
			return rows[i].Number > rows[j].Number
		}
		return rows[i].ID < rows[j].ID
	})
}

// buildRoundRows lists the closed review rounds, newest first.
//
// Rounds are numbered per station, not per issue, so two rows can both read R1.
// That is correct and deliberate: counting across the card once made a first
// code review print as "R2", which reads as a second attempt and sends the
// reader looking for an R1 nobody held.
func buildRoundRows(cards []db.Card) []IssueHistoryRound {
	rounds := make([]IssueHistoryRound, 0, 8)
	for _, c := range cards {
		legacy := strings.Contains(c.Kind, reviewsKindSegment)
		if !legacy && !strings.Contains(c.Kind, roundsKindSegment) {
			continue
		}
		segment := c.Kind
		if idx := strings.LastIndex(c.Kind, "/"); idx >= 0 {
			segment = c.Kind[idx+1:]
		}
		round := IssueHistoryRound{
			DocID: uuidToString(c.ID), Title: c.Title, Legacy: legacy,
			ClosedAt: timestampToString(c.CreatedAt),
		}
		if m := roundKindSegmentRE.FindStringSubmatch(strings.TrimSpace(segment)); m != nil {
			n, err := strconv.Atoi(m[1])
			if err == nil && n > 0 {
				round.Number = n
			}
			round.ID = "R" + m[1]
			round.Station = m[2]
		} else {
			round.ID = segment
		}
		round.Verdict, round.Summary = parseRoundBody(c.Content)
		rounds = append(rounds, round)
	}
	sort.SliceStable(rounds, func(i, j int) bool {
		// Current rounds above the retired reviews/ system, then newest first.
		if rounds[i].Legacy != rounds[j].Legacy {
			return !rounds[i].Legacy
		}
		if rounds[i].Number != rounds[j].Number {
			return rounds[i].Number > rounds[j].Number
		}
		return rounds[i].Station < rounds[j].Station
	})
	return rounds
}

// roundBodyFields are the lines `issue round close` writes into a round
// document. Read rather than re-derived: the document is the record, and a
// second computation of the verdict would be a second answer to the same
// question.
var roundBodyFields = []struct {
	prefix  string
	verdict bool
}{
	{"- 结论：", true},
	{"- 要点：", false},
}

func parseRoundBody(content string) (verdict, summary string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		for _, f := range roundBodyFields {
			if !strings.HasPrefix(line, f.prefix) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, f.prefix))
			if f.verdict {
				if verdict == "" {
					verdict = value
				}
			} else if summary == "" {
				summary = value
			}
		}
		if verdict != "" && summary != "" {
			break
		}
	}
	return verdict, summary
}

// buildDocumentRows lists the card's snapshots, then its live documents.
//
// Decisions and rounds are excluded: they have their own tabs, and a document
// counted in two places invites the reader to treat the second sighting as a
// second object.
func buildDocumentRows(cards []db.Card) []IssueHistoryDocument {
	docs := make([]IssueHistoryDocument, 0, 8)
	for _, c := range cards {
		if strings.Contains(c.Kind, decisionsKindSegment) ||
			strings.Contains(c.Kind, roundsKindSegment) ||
			strings.Contains(c.Kind, reviewsKindSegment) {
			continue
		}
		doc := IssueHistoryDocument{
			ID:        uuidToString(c.ID),
			Kind:      c.Kind,
			Title:     c.Title,
			UpdatedAt: timestampToString(c.UpdatedAt),
			CreatedAt: timestampToString(c.CreatedAt),
		}
		if idx := strings.Index(c.Kind, snapshotsKindSegment); idx >= 0 {
			doc.Snapshot = true
			rest := c.Kind[idx+len(snapshotsKindSegment):]
			doc.SnapshotOf, doc.TakenAt, _ = strings.Cut(rest, "/")
		}
		docs = append(docs, doc)
	}
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].Snapshot != docs[j].Snapshot {
			return docs[i].Snapshot
		}
		return docs[i].CreatedAt > docs[j].CreatedAt
	})
	return docs
}
