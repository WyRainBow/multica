"use client";

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const growthCardKeys = {
  all: (wsId: string) => ["growth-cards", wsId] as const,
  list: (wsId: string) => [...growthCardKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...growthCardKeys.all(wsId), "detail", id] as const,
  /** Cards written about one requirement. Keyed under the issue so the issue
   *  page and the cards page invalidate independently. */
  forIssue: (wsId: string, issueId: string) =>
    [...growthCardKeys.all(wsId), "for-issue", issueId] as const,
};

export function growthCardListOptions(wsId: string) {
  return queryOptions({
    queryKey: growthCardKeys.list(wsId),
    queryFn: () => api.listGrowthCards({ limit: 200 }),
  });
}

export function growthCardDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: growthCardKeys.detail(wsId, id),
    queryFn: () => api.getGrowthCard(id),
    enabled: !!id,
  });
}

export function growthCardsForIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: growthCardKeys.forIssue(wsId, issueId),
    queryFn: () => api.listGrowthCardsForIssue(issueId).then((r) => r.cards),
    enabled: !!issueId,
  });
}
