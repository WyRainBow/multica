"use client";

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueResourceKeys = {
  // Workspace-scoped, per the repo's query-key rule: two workspaces must never
  // share a cache entry even when an issue id collides.
  all: (wsId: string) => ["issue-resources", wsId] as const,
  forIssue: (wsId: string, issueId: string) =>
    [...issueResourceKeys.all(wsId), issueId] as const,
};

export function issueResourcesOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueResourceKeys.forIssue(wsId, issueId),
    queryFn: () => api.listIssueResources(issueId),
  });
}
