"use client";

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const retroKeys = {
  all: (wsId: string) => ["retros", wsId] as const,
  list: (wsId: string) => [...retroKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...retroKeys.all(wsId), "detail", id] as const,
  /** Retros written for one requirement. Keyed under the issue so the issue
   *  page and the retros page invalidate independently. */
  forIssue: (wsId: string, issueId: string) =>
    [...retroKeys.all(wsId), "for-issue", issueId] as const,
};

export function retroListOptions(wsId: string) {
  return queryOptions({
    queryKey: retroKeys.list(wsId),
    queryFn: () => api.listRetros({ limit: 200 }),
  });
}

export function retroDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: retroKeys.detail(wsId, id),
    queryFn: () => api.getRetro(id),
    enabled: !!id,
  });
}

export function retrosForIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: retroKeys.forIssue(wsId, issueId),
    queryFn: () => api.listRetrosForIssue(issueId).then((r) => r.retros),
    enabled: !!issueId,
  });
}
