"use client";

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * The hook inventory read (COC-341). Workspace-scoped and grouped by runtime,
 * because the reader needs to tell three different answers apart: this machine
 * has no Multica hooks, this machine cannot have them, and this machine has
 * never been scanned. Only a group that exists can carry that distinction, so
 * the UI never flattens the groups into one list.
 */
export const runtimeHookKeys = {
  // Workspace-scoped per the repo's query-key rule.
  all: (wsId: string) => ["runtime-hooks", wsId] as const,
};

export function runtimeHookListOptions(wsId: string) {
  return queryOptions({
    queryKey: runtimeHookKeys.all(wsId),
    queryFn: () => api.listWorkspaceHooks(wsId),
    enabled: wsId !== "",
    // The inventory only moves when a daemon rescans, which is heartbeat
    // paced. Refetching on every tab focus would be pure noise.
    staleTime: 60 * 1000,
  });
}
