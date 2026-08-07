import type { IssueResource } from "@multica/core/types";

/**
 * The visible label. A stored title wins; without one the host and path stand
 * in, because three Feishu docs would otherwise render as three identical rows
 * reading "feishu.cn".
 *
 * No favicon fetch. The documents most worth attaching here — Feishu, internal
 * wikis — return a login page to an anonymous request, so a fetched icon would
 * be wrong or missing exactly where it matters. The host, shown as text, tells
 * you where a link goes without pretending to.
 */
export function resourceLabel(resource: IssueResource): string {
  const title = resource.title?.trim();
  if (title) return title;
  const host = resourceHost(resource.url);
  if (!host) return resource.url;
  try {
    const path = new URL(resource.url).pathname.replace(/\/$/, "");
    return path && path !== "/" ? `${host}${path}` : host;
  } catch {
    return host;
  }
}

export function resourceHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return "";
  }
}

