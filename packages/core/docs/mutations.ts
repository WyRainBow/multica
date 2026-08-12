"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { CreateCardRequest, UpdateCardRequest } from "../types";
import { cardKeys } from "./queries";

/**
 * Card writes invalidate rather than patch. A card is written once and read
 * occasionally — there is no drag, no toggle, nothing where a round trip is
 * felt — so the extra complexity of an optimistic cache patch would buy
 * nothing and could show a stale list after a failed save.
 */
function useCardInvalidation() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return () => qc.invalidateQueries({ queryKey: cardKeys.all(wsId) });
}

export function useCreateCard() {
  const invalidate = useCardInvalidation();
  return useMutation({
    mutationFn: (body: CreateCardRequest) => api.createCard(body),
    onSettled: invalidate,
  });
}

export function useUpdateCard() {
  const invalidate = useCardInvalidation();
  return useMutation({
    mutationFn: ({ id, ...body }: UpdateCardRequest & { id: string }) =>
      api.updateCard(id, body),
    onSettled: invalidate,
  });
}

export function useDeleteCard() {
  const invalidate = useCardInvalidation();
  return useMutation({
    mutationFn: (id: string) => api.deleteCard(id),
    onSettled: invalidate,
  });
}
