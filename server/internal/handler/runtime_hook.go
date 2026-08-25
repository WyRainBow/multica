package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The hook inventory answers one question that used to require SSH: is the
// Multica hook actually installed on that machine, and has anyone ever seen it
// run.
//
// V1 is read-only. Nothing here writes a user's agent config; the daemon
// reports what it found and the server keeps the LAST FULL inventory per
// runtime. "Last full" is why the report both upserts and deletes: a hook that
// stopped being reported was uninstalled, and an inventory that can only grow
// stops being an inventory.

const (
	// Wide enough for a machine with every provider hook installed several
	// times over; narrow enough that a malformed or hostile report cannot
	// turn one request into an unbounded write.
	maxRuntimeHookReportEntries = 500
	maxRuntimeHookNameLength    = 200
	maxRuntimeHookEventLength   = 100
	maxRuntimeHookTriggerLength = 500
	maxRuntimeHookPathLength    = 1000
)

// runtimeHookTelemetryStates is the closed vocabulary from migration 302.
// never_fired and unobserved are the pair worth being careful about: the first
// is an observation, the second is the absence of one, and treating them as
// synonyms accuses a hook of being dead when nobody was watching it.
var runtimeHookTelemetryStates = map[string]bool{
	"fired":         true,
	"never_fired":   true,
	"unobserved":    true,
	"uncollectable": true,
}

// ---------------------------------------------------------------------------
// Report: POST /api/daemon/runtimes/{runtimeId}/hooks
// ---------------------------------------------------------------------------

type runtimeHookReportEntry struct {
	HookName    string `json:"hook_name"`
	Event       string `json:"event"`
	TriggerSpec string `json:"trigger_spec"`
	CommandPath string `json:"command_path"`
	Enabled     *bool  `json:"enabled"`
	Telemetry   string `json:"telemetry"`
	LastFiredAt string `json:"last_fired_at"`
}

type runtimeHookReportBody struct {
	// Whether the provider has a hook mechanism at all. Informational: the
	// server derives the same fact from the runtime's provider when it
	// answers reads. It is accepted so a daemon that knows better than this
	// server's provider table can say so in the logs.
	Supported *bool                    `json:"supported"`
	Hooks     []runtimeHookReportEntry `json:"hooks"`
}

// ReportRuntimeHooks replaces one runtime's hook inventory with what the
// daemon just scanned.
func (h *Handler) ReportRuntimeHooks(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	var body runtimeHookReportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Hooks) > maxRuntimeHookReportEntries {
		writeError(w, http.StatusBadRequest, "too many hooks in one report")
		return
	}

	params := make([]db.UpsertRuntimeHookParams, 0, len(body.Hooks))
	keys := make([]string, 0, len(body.Hooks))
	seen := make(map[string]bool, len(body.Hooks))
	for _, entry := range body.Hooks {
		if entry.HookName == "" || len(entry.HookName) > maxRuntimeHookNameLength {
			writeError(w, http.StatusBadRequest, "hook_name is required and must be at most 200 characters")
			return
		}
		if entry.Event == "" || len(entry.Event) > maxRuntimeHookEventLength {
			writeError(w, http.StatusBadRequest, "event is required and must be at most 100 characters")
			return
		}
		if len(entry.TriggerSpec) > maxRuntimeHookTriggerLength {
			writeError(w, http.StatusBadRequest, "trigger_spec must be at most 500 characters")
			return
		}
		if len(entry.CommandPath) > maxRuntimeHookPathLength {
			writeError(w, http.StatusBadRequest, "command_path must be at most 1000 characters")
			return
		}
		telemetry := entry.Telemetry
		if telemetry == "" {
			// Matching the column default rather than assuming an observation
			// was made: a report that omits the field has not told us the hook
			// never fired, only that it did not say.
			telemetry = "unobserved"
		}
		if !runtimeHookTelemetryStates[telemetry] {
			writeError(w, http.StatusBadRequest, "telemetry must be one of fired, never_fired, unobserved, uncollectable")
			return
		}

		var lastFiredAt pgtype.Timestamptz
		if entry.LastFiredAt != "" {
			parsed, err := time.Parse(time.RFC3339, entry.LastFiredAt)
			if err != nil {
				writeError(w, http.StatusBadRequest, "last_fired_at must be an RFC3339 timestamp")
				return
			}
			lastFiredAt = pgtype.Timestamptz{Time: parsed, Valid: true}
		}
		if telemetry == "fired" && !lastFiredAt.Valid {
			writeError(w, http.StatusBadRequest, "last_fired_at is required when telemetry is fired")
			return
		}

		// The unique index would reject a duplicate identity mid-transaction
		// and lose the whole report. Rejecting it up front says which report
		// was wrong instead of failing an unrelated-looking write.
		key := entry.HookName + "\x1f" + entry.Event
		if seen[key] {
			writeError(w, http.StatusBadRequest, "duplicate hook_name/event in report")
			return
		}
		seen[key] = true
		keys = append(keys, key)

		enabled := true
		if entry.Enabled != nil {
			enabled = *entry.Enabled
		}
		params = append(params, db.UpsertRuntimeHookParams{
			WorkspaceID: rt.WorkspaceID,
			RuntimeID:   rt.ID,
			Provider:    rt.Provider,
			HookName:    entry.HookName,
			Event:       entry.Event,
			TriggerSpec: entry.TriggerSpec,
			CommandPath: entry.CommandPath,
			Enabled:     enabled,
			Telemetry:   telemetry,
			LastFiredAt: lastFiredAt,
		})
	}

	if body.Supported != nil && !*body.Supported && len(params) > 0 {
		slog.Warn("runtime reported hooks while declaring no hook mechanism",
			"runtime_id", runtimeID, "provider", rt.Provider, "count", len(params))
	}

	// Upserts and the sweep of what vanished commit together. Half-applied,
	// the inventory would claim a hook exists that the machine no longer has.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record hook inventory")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	for _, p := range params {
		if err := qtx.UpsertRuntimeHook(r.Context(), p); err != nil {
			slog.Error("upsert runtime hook failed", "runtime_id", runtimeID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record hook inventory")
			return
		}
	}
	if err := qtx.DeleteRuntimeHooksNotIn(r.Context(), db.DeleteRuntimeHooksNotInParams{
		WorkspaceID: rt.WorkspaceID,
		RuntimeID:   rt.ID,
		Keys:        keys,
	}); err != nil {
		slog.Error("sweep runtime hooks failed", "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record hook inventory")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record hook inventory")
		return
	}

	slog.Debug("runtime hook inventory recorded",
		"runtime_id", runtimeID, "provider", rt.Provider, "count", len(params))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": len(params)})
}

// ---------------------------------------------------------------------------
// Read: GET /api/workspaces/{id}/hooks
// ---------------------------------------------------------------------------

type RuntimeHookResponse struct {
	ID          string  `json:"id"`
	HookName    string  `json:"hook_name"`
	Event       string  `json:"event"`
	TriggerSpec string  `json:"trigger_spec"`
	CommandPath string  `json:"command_path"`
	Enabled     bool    `json:"enabled"`
	Telemetry   string  `json:"telemetry"`
	LastFiredAt *string `json:"last_fired_at"`
	ObservedAt  string  `json:"observed_at"`
}

type RuntimeHookGroupResponse struct {
	RuntimeID string `json:"runtime_id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Host      string `json:"host"`
	// Liveness comes from agent_runtime, never from the inventory. An offline
	// runtime still shows the hooks it last reported; what it does not do is
	// claim they were confirmed just now.
	Status     string  `json:"status"`
	LastSeenAt *string `json:"last_seen_at"`
	// When this runtime's inventory was last confirmed by a scan. Null when it
	// has never reported one, which is a third thing again: not unsupported,
	// not empty, just never asked.
	ObservedAt *string `json:"observed_at"`
	// False means the provider has no hook mechanism. A client that renders
	// this the same as an empty list sends the user to debug an installation
	// that was never possible.
	Supported bool                  `json:"supported"`
	Hooks     []RuntimeHookResponse `json:"hooks"`
}

// ListWorkspaceHooks returns every runtime in the workspace with its last
// reported hook inventory.
//
// Runtimes with no rows are still listed. That is the point: the reader needs
// to tell "this machine has no Multica hooks" from "this machine cannot have
// them" from "this machine has never been scanned", and only a group that
// exists can carry that distinction.
func (h *Handler) ListWorkspaceHooks(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}

	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), workspaceID)
	if err != nil {
		slog.Error("list agent runtimes failed", "workspace_id", uuidToString(workspaceID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return
	}
	hooks, err := h.Queries.ListRuntimeHooksForWorkspace(r.Context(), workspaceID)
	if err != nil {
		slog.Error("list runtime hooks failed", "workspace_id", uuidToString(workspaceID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load hooks")
		return
	}

	byRuntime := make(map[string][]db.RuntimeHook, len(runtimes))
	for _, hook := range hooks {
		id := uuidToString(hook.RuntimeID)
		byRuntime[id] = append(byRuntime[id], hook)
	}

	groups := make([]RuntimeHookGroupResponse, 0, len(runtimes))
	for _, rt := range runtimes {
		id := uuidToString(rt.ID)
		rows := byRuntime[id]

		group := RuntimeHookGroupResponse{
			RuntimeID:  id,
			Name:       runtimeDisplayName(rt),
			Provider:   rt.Provider,
			Host:       rt.DeviceInfo,
			Status:     rt.Status,
			LastSeenAt: timestampToPtr(rt.LastSeenAt),
			// A runtime that actually reported hooks is supported whatever
			// this server's provider table says — that disagreement means a
			// newer daemon, not a phantom inventory.
			Supported: agent.HookMechanismSupported(rt.Provider) || len(rows) > 0,
			Hooks:     make([]RuntimeHookResponse, 0, len(rows)),
		}

		var observedAt pgtype.Timestamptz
		for _, row := range rows {
			if row.ObservedAt.Valid && (!observedAt.Valid || row.ObservedAt.Time.After(observedAt.Time)) {
				observedAt = row.ObservedAt
			}
			group.Hooks = append(group.Hooks, RuntimeHookResponse{
				ID:          uuidToString(row.ID),
				HookName:    row.HookName,
				Event:       row.Event,
				TriggerSpec: row.TriggerSpec,
				CommandPath: row.CommandPath,
				Enabled:     row.Enabled,
				Telemetry:   row.Telemetry,
				LastFiredAt: timestampToPtr(row.LastFiredAt),
				ObservedAt:  timestampToString(row.ObservedAt),
			})
		}
		group.ObservedAt = timestampToPtr(observedAt)
		groups = append(groups, group)
	}

	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"runtimes": groups})
}

// runtimeDisplayName prefers the user's rename over the daemon's proposal, the
// same way every other runtime surface does.
func runtimeDisplayName(rt db.AgentRuntime) string {
	if name := textToPtr(rt.CustomName); name != nil && *name != "" {
		return *name
	}
	return rt.Name
}
