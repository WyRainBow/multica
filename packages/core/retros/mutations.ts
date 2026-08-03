"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { CreateRetroRequest, UpdateRetroRequest } from "../types";
import { retroKeys } from "./queries";

/**
 * Retro writes invalidate rather than patch. A retro is written once and read
 * occasionally — there is no drag, no toggle, nothing where a round trip is
 * felt — so the extra complexity of an optimistic cache patch would buy
 * nothing and could show a stale list after a failed save.
 */
function useRetroInvalidation() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return () => qc.invalidateQueries({ queryKey: retroKeys.all(wsId) });
}

export function useCreateRetro() {
  const invalidate = useRetroInvalidation();
  return useMutation({
    mutationFn: (body: CreateRetroRequest) => api.createRetro(body),
    onSettled: invalidate,
  });
}

export function useUpdateRetro() {
  const invalidate = useRetroInvalidation();
  return useMutation({
    mutationFn: ({ id, ...body }: UpdateRetroRequest & { id: string }) =>
      api.updateRetro(id, body),
    onSettled: invalidate,
  });
}

export function useDeleteRetro() {
  const invalidate = useRetroInvalidation();
  return useMutation({
    mutationFn: (id: string) => api.deleteRetro(id),
    onSettled: invalidate,
  });
}
