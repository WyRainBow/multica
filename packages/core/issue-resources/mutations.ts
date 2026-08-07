"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { CreateIssueResourceRequest, UpdateIssueResourceRequest } from "../types";
import { issueResourceKeys } from "./queries";

/**
 * Resource writes invalidate rather than patch.
 *
 * The repo allows an optimistic update only when the outcome is locally
 * predictable — and it is not here: the server normalises the URL and can
 * reject it outright, so a patched row could show a link the server never
 * accepted. Adding one is also a deliberate act with a dialog, not a toggle,
 * so the round trip is not felt.
 */
function useIssueResourceInvalidation(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return () =>
    qc.invalidateQueries({ queryKey: issueResourceKeys.forIssue(wsId, issueId) });
}

export function useCreateIssueResource(issueId: string) {
  const invalidate = useIssueResourceInvalidation(issueId);
  return useMutation({
    mutationFn: (body: CreateIssueResourceRequest) =>
      api.createIssueResource(issueId, body),
    onSettled: invalidate,
  });
}

export function useUpdateIssueResource(issueId: string) {
  const invalidate = useIssueResourceInvalidation(issueId);
  return useMutation({
    mutationFn: ({ id, ...body }: UpdateIssueResourceRequest & { id: string }) =>
      api.updateIssueResource(issueId, id, body),
    onSettled: invalidate,
  });
}

export function useDeleteIssueResource(issueId: string) {
  const invalidate = useIssueResourceInvalidation(issueId);
  return useMutation({
    mutationFn: (id: string) => api.deleteIssueResource(issueId, id),
    onSettled: invalidate,
  });
}
