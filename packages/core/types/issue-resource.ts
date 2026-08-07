/**
 * An external page attached to an issue: a design doc, a meeting note, a
 * vendor page — anything whose home is outside Multica.
 *
 * Not an attachment (a file we store) and not a pull-request link (written by
 * the webhook, and only ever a PR).
 */
export interface IssueResource {
  id: string;
  workspace_id: string;
  issue_id: string;
  url: string;
  /** The reader's label, not the page's. Empty is normal — the row falls back
   *  to the host and path so it is never blank. */
  title: string;
  author_type: string;
  author_id: string;
  position: number;
  created_at: string;
  updated_at: string;
}

export interface CreateIssueResourceRequest {
  url: string;
  title?: string;
}

export interface UpdateIssueResourceRequest {
  url?: string;
  /** An empty string clears the label back to the host fallback. */
  title?: string;
}
