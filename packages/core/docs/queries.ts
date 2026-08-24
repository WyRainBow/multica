"use client";

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const cardKeys = {
  all: (wsId: string) => ["cards", wsId] as const,
  list: (wsId: string) => [...cardKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...cardKeys.all(wsId), "detail", id] as const,
  /** Cards written for one requirement. Keyed under the issue so the issue
   *  page and the cards page invalidate independently. */
  forIssue: (wsId: string, issueId: string) =>
    [...cardKeys.all(wsId), "for-issue", issueId] as const,
  /** The fixed slot directory for one requirement. Kept apart from `forIssue`
   *  because it is the one read that can see placeholders: the two answer
   *  different questions off different rows and go stale independently. */
  namespace: (wsId: string, issueId: string) =>
    [...cardKeys.all(wsId), "namespace", issueId] as const,
};

export function cardListOptions(wsId: string) {
  return queryOptions({
    queryKey: cardKeys.list(wsId),
    queryFn: () => api.listCards({ limit: 200 }),
  });
}

export function cardDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: cardKeys.detail(wsId, id),
    queryFn: () => api.getCard(id),
    enabled: !!id,
  });
}

export function cardsForIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: cardKeys.forIssue(wsId, issueId),
    queryFn: () => api.listCardsForIssue(issueId).then((r) => r.cards),
    enabled: !!issueId,
  });
}

/**
 * The issue's fixed document directory: which of the six slots have been
 * written and which are still held open by a placeholder.
 *
 * Separate from `cardsForIssueOptions` because the placeholders it reports are
 * filtered out of every other card read — asking the same question of the card
 * list would always answer "nothing here".
 */
export function issueNamespaceOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: cardKeys.namespace(wsId, issueId),
    queryFn: () => api.getIssueNamespace(issueId),
    enabled: !!issueId,
  });
}
