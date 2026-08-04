"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  CreateGrowthCardRequest,
  UpdateGrowthCardRequest,
} from "../types";
import { growthCardKeys } from "./queries";

/**
 * Growth card writes invalidate rather than patch. A card is written once and
 * read occasionally — there is no drag, no toggle, nothing where a round trip
 * is felt — so an optimistic cache patch would buy nothing and could show a
 * stale card after a failed save.
 */
function useGrowthCardInvalidation() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return () => qc.invalidateQueries({ queryKey: growthCardKeys.all(wsId) });
}

export function useCreateGrowthCard() {
  const invalidate = useGrowthCardInvalidation();
  return useMutation({
    mutationFn: (body: CreateGrowthCardRequest) => api.createGrowthCard(body),
    onSettled: invalidate,
  });
}

export function useUpdateGrowthCard() {
  const invalidate = useGrowthCardInvalidation();
  return useMutation({
    mutationFn: ({ id, ...body }: UpdateGrowthCardRequest & { id: string }) =>
      api.updateGrowthCard(id, body),
    onSettled: invalidate,
  });
}

export function useDeleteGrowthCard() {
  const invalidate = useGrowthCardInvalidation();
  return useMutation({
    mutationFn: (id: string) => api.deleteGrowthCard(id),
    onSettled: invalidate,
  });
}
