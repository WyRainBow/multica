package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/progressledger"
	"github.com/multica-ai/multica/server/internal/util"
)

// Review requests recorded by hand against a card.
//
// This workspace talks to GitHub and GitLab and integrates with neither, so
// there is no API to ask whether a request is open, merged or abandoned. These
// links carry a URL and nothing else derived — no state badge, no title
// fetched from the page. A status this build cannot verify would be a claim
// nobody could check, and the GitHub-derived list next to these is where a
// verified status belongs.
//
// What each link does carry is who recorded it and when. That is the part worth
// keeping: a bare URL on a card says a review exists somewhere, while a URL
// with an author says who to ask about it.

const (
	maxPRLinkURLLength   = 2000
	maxPRLinkTitleLength = 300
	// Enough that nobody hits it in real use, low enough that a loop writing
	// links cannot grow the account without bound.
	maxPRLinksPerCard = 50
)

// IssuePRLinkResponse is one recorded link.
type IssuePRLinkResponse struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
	// AddedBy is the display name as it stood when the link was recorded — a
	// snapshot, because this is a log line and a log line that renames itself
	// later stops being a record of what happened.
	AddedBy     string `json:"added_by"`
	AddedByType string `json:"added_by_type"`
	AddedAt     string `json:"added_at"`
}

func prLinkToResponse(link progressledger.PullRequestLink) IssuePRLinkResponse {
	return IssuePRLinkResponse{
		ID:          link.ID,
		URL:         link.URL,
		Title:       link.Title,
		AddedBy:     link.AddedBy,
		AddedByType: link.AddedByType,
		AddedAt:     ledgerTime(link.At),
	}
}

// validatePRLinkURL accepts an absolute http(s) URL and nothing else.
//
// A relative path or a bare host would render as a dead link, and the whole
// value of this field is that clicking it reaches the review. Rejecting on
// write is the only place that can still be fixed cheaply.
func validatePRLinkURL(raw string) (string, bool, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false, "url is required"
	}
	if len(value) > maxPRLinkURLLength {
		return "", false, "url is too long"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "", false, "url must be an absolute link, starting with http:// or https://"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, "url must be an absolute link, starting with http:// or https://"
	}
	return value, true, ""
}

// actorDisplayName resolves an actor to the name a reader recognises. A miss
// leaves it empty rather than falling back to the id: an unresolvable uuid on a
// card tells the reader nothing and looks like corruption.
func (h *Handler) actorDisplayName(ctx context.Context, actorType, actorID string) string {
	id, err := util.ParseUUID(actorID)
	if err != nil {
		return ""
	}
	switch actorType {
	case "agent":
		if a, err := h.Queries.GetAgent(ctx, id); err == nil {
			return a.Name
		}
	case "member":
		if u, err := h.Queries.GetUser(ctx, id); err == nil {
			return u.Name
		}
	}
	return ""
}

// loadLedgerForIssue finds the card's progress account, which is where its
// branch, session and review links all live together.
func (h *Handler) loadLedgerForIssue(
	w http.ResponseWriter,
	r *http.Request,
) (progressledger.Record, string, bool) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return progressledger.Record{}, "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	identifier := h.issueIdentifier(r.Context(), issue)
	if identifier == "" {
		writeError(w, http.StatusInternalServerError, "could not resolve the card identifier")
		return progressledger.Record{}, "", false
	}
	rec, err := h.ledger().FindByIssue(workspaceID, identifier)
	if err == nil {
		return rec, identifier, true
	}
	// No account yet is not an error on read, and on write it is the reason to
	// open one: recording a review request is exactly the kind of first fact a
	// card's account exists to hold.
	return progressledger.Record{
		Key:         progressledger.SanitizeKey(identifier),
		WorkspaceID: workspaceID,
		Name:        strings.ToLower(identifier),
		Issue:       identifier,
		IssueID:     uuidToString(issue.ID),
		Role:        "discussion",
		Status:      "active",
	}, identifier, true
}

// ListIssuePRLinks handles GET /api/issues/{id}/pr-links.
func (h *Handler) ListIssuePRLinks(w http.ResponseWriter, r *http.Request) {
	rec, _, ok := h.loadLedgerForIssue(w, r)
	if !ok {
		return
	}
	resp := make([]IssuePRLinkResponse, 0, len(rec.PullRequests))
	for _, link := range rec.PullRequests {
		resp = append(resp, prLinkToResponse(link))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pr_links": resp})
}

type createIssuePRLinkRequest struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// CreateIssuePRLink handles POST /api/issues/{id}/pr-links.
func (h *Handler) CreateIssuePRLink(w http.ResponseWriter, r *http.Request) {
	rec, identifier, ok := h.loadLedgerForIssue(w, r)
	if !ok {
		return
	}
	var req createIssuePRLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	link, valid, msg := validatePRLinkURL(req.URL)
	if !valid {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	title := strings.TrimSpace(req.Title)
	if len([]rune(title)) > maxPRLinkTitleLength {
		writeError(w, http.StatusBadRequest, "title is too long")
		return
	}
	// The same review recorded twice is a duplicate, not a second review.
	for _, existing := range rec.PullRequests {
		if existing.URL == link {
			writeJSON(w, http.StatusOK, prLinkToResponse(existing))
			return
		}
	}
	if len(rec.PullRequests) >= maxPRLinksPerCard {
		writeError(w, http.StatusBadRequest, "this card already carries the maximum number of review links")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	rec.PullRequests = append(rec.PullRequests, progressledger.PullRequestLink{
		ID:          uuid.NewString(),
		URL:         link,
		Title:       title,
		AddedBy:     h.actorDisplayName(r.Context(), actorType, actorID),
		AddedByType: actorType,
		AddedByID:   actorID,
		At:          time.Now(),
	})

	if err := h.ledger().Save(&rec, "pr("+rec.Key+"): "+link); err != nil {
		writeLedgerError(w, r, "record the review link", err)
		return
	}
	slog.Info("recorded a review link", append(logger.RequestAttrs(r), "issue", identifier)...)
	writeJSON(w, http.StatusCreated, prLinkToResponse(rec.PullRequests[len(rec.PullRequests)-1]))
}

// DeleteIssuePRLink handles DELETE /api/issues/{id}/pr-links/{linkID}.
//
// Removable, unlike a ledger entry: a pasted URL is a pointer, and a wrong
// pointer is worth deleting rather than annotating. The account's log is where
// the append-only rule lives.
func (h *Handler) DeleteIssuePRLink(w http.ResponseWriter, r *http.Request) {
	rec, _, ok := h.loadLedgerForIssue(w, r)
	if !ok {
		return
	}
	linkID := strings.TrimSpace(chi.URLParam(r, "linkID"))
	kept := make([]progressledger.PullRequestLink, 0, len(rec.PullRequests))
	found := false
	for _, link := range rec.PullRequests {
		if link.ID == linkID {
			found = true
			continue
		}
		kept = append(kept, link)
	}
	if !found {
		writeError(w, http.StatusNotFound, "review link not found")
		return
	}
	rec.PullRequests = kept
	if err := h.ledger().Save(&rec, "pr("+rec.Key+"): removed a link"); err != nil {
		writeLedgerError(w, r, "remove the review link", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
