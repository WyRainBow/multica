"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  CreateWorktreeEntryRequest,
  CreateWorktreeRequest,
  UpdateWorktreeRequest,
  UpdateWorktreeSessionRequest,
} from "../types";
import { worktreeKeys } from "./queries";

/**
 * Ledger writes invalidate rather than patch.
 *
 * None of them clear the bar for an optimistic update: the server normalises
 * and can reject (a name collision, a role this build has not heard of), and a
 * sync writes fields the client did not send. Showing a row the server never
 * accepted is exactly the failure a ledger cannot afford.
 */
function useWorktreeInvalidation() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return () => qc.invalidateQueries({ queryKey: worktreeKeys.all(wsId) });
}

export function useCreateWorktree() {
  const invalidate = useWorktreeInvalidation();
  return useMutation({
    mutationFn: (body: CreateWorktreeRequest) => api.createWorktree(body),
    onSettled: invalidate,
  });
}

export function useUpdateWorktree() {
  const invalidate = useWorktreeInvalidation();
  return useMutation({
    mutationFn: ({ ref, ...body }: UpdateWorktreeRequest & { ref: string }) =>
      api.updateWorktree(ref, body),
    onSettled: invalidate,
  });
}

export function useDeleteWorktree() {
  const invalidate = useWorktreeInvalidation();
  return useMutation({
    mutationFn: (ref: string) => api.deleteWorktree(ref),
    onSettled: invalidate,
  });
}

export function useUpdateWorktreeSession() {
  const invalidate = useWorktreeInvalidation();
  return useMutation({
    mutationFn: ({ ref, ...body }: UpdateWorktreeSessionRequest & { ref: string }) =>
      api.updateWorktreeSession(ref, body),
    onSettled: invalidate,
  });
}

export function useCreateWorktreeEntry() {
  const invalidate = useWorktreeInvalidation();
  return useMutation({
    mutationFn: ({ ref, ...body }: CreateWorktreeEntryRequest & { ref: string }) =>
      api.createWorktreeEntry(ref, body),
    // Invalidates the whole ledger key: a new entry changes the tree's count
    // and the workspace feed as well as that tree's own list.
    onSettled: invalidate,
  });
}

/**
 * Recording a review link is a create that stays on the page and rarely fails,
 * but it is not optimistic either: the server assigns the id, stamps who
 * recorded it, and returns an existing row unchanged when the same URL is
 * pasted twice. A locally invented row would get all three wrong.
 */
export function useCreateIssuePRLink(issueId: string) {
  const invalidate = useWorktreeInvalidation();
  return useMutation({
    mutationFn: (body: { url: string; title?: string }) =>
      api.createIssuePRLink(issueId, body),
    onSettled: invalidate,
  });
}

export function useDeleteIssuePRLink(issueId: string) {
  const invalidate = useWorktreeInvalidation();
  return useMutation({
    mutationFn: (linkId: string) => api.deleteIssuePRLink(issueId, linkId),
    onSettled: invalidate,
  });
}
