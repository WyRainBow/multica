"use client";

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const worktreeKeys = {
  // Workspace-scoped, per the repo's query-key rule: two workspaces must never
  // share a cache entry even when a tree name collides.
  all: (wsId: string) => ["worktrees", wsId] as const,
  entries: (wsId: string, ref: string) =>
    [...worktreeKeys.all(wsId), "entries", ref] as const,
  recentEntries: (wsId: string) =>
    [...worktreeKeys.all(wsId), "entries", "recent"] as const,
};

export function worktreeListOptions(wsId: string) {
  return queryOptions({
    queryKey: worktreeKeys.all(wsId),
    queryFn: () => api.listWorktrees(),
  });
}

export function worktreeEntriesOptions(wsId: string, ref: string, limit?: number) {
  return queryOptions({
    queryKey: worktreeKeys.entries(wsId, ref),
    queryFn: () => api.listWorktreeEntries(ref, limit),
    // Only fetched once a tree is expanded: the ledger page opens on the
    // workspace-wide feed, so per-tree entries are a drill-down, not a preload.
    enabled: ref !== "",
  });
}

export function recentWorktreeEntriesOptions(wsId: string, limit?: number) {
  return queryOptions({
    queryKey: worktreeKeys.recentEntries(wsId),
    queryFn: () => api.listRecentWorktreeEntries(limit),
  });
}
