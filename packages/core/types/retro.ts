/**
 * A retro is what a piece of work taught someone — the technique that worked,
 * the trap that cost a day, the thing worth reusing next time.
 *
 * Kept separately from the issue it came out of because it has to outlive it:
 * a requirement gets closed, the lesson does not stop being true.
 */
export interface Retro {
  id: string;
  workspace_id: string;
  /** The requirement this came out of. Null is normal — a lesson can come
   *  from reading or from an incident, not only from a ticket. */
  issue_id: string | null;
  author_type: string;
  author_id: string;
  title: string;
  content: string;
  created_at: string;
  updated_at: string;
}

export interface CreateRetroRequest {
  title: string;
  content?: string;
  issue_id?: string | null;
}

export interface UpdateRetroRequest {
  title?: string;
  content?: string;
  /** Explicit null detaches the retro from its requirement; omitting the
   *  field leaves the link alone. */
  issue_id?: string | null;
}

export interface RetroListResponse {
  retros: Retro[];
  total: number;
}
