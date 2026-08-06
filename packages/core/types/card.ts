/**
 * A card is a short free-form note: something learned, a link worth keeping,
 * a thing to look at later. Title plus Markdown body, nothing else.
 *
 * Free-form on purpose. An earlier version asked eight named questions, and a
 * guessed structure turned out to be worse than none — it makes the writer
 * fight the form instead of writing.
 *
 * Kept separately from the issue it came out of because it has to outlive it:
 * a requirement gets closed, what it taught does not stop being true.
 */
export interface Card {
  id: string;
  workspace_id: string;
  /** The requirement this came out of. Null is normal — a note can come from
   *  reading or from an incident, not only from a ticket. */
  issue_id: string | null;
  author_type: string;
  author_id: string;
  title: string;
  content: string;
  /** Which tab this sits under. Empty means uncategorised, which is most of
   *  them — filing is optional and the note comes first. */
  kind: string;
  created_at: string;
  updated_at: string;
}

export interface CreateCardRequest {
  /** May be empty: naming a note is optional, writing it down is the point. */
  title?: string;
  content?: string;
  /** Free text; it becomes a tab. Omit or pass "" for uncategorised. */
  kind?: string;
  issue_id?: string | null;
}

export interface UpdateCardRequest {
  title?: string;
  content?: string;
  /** Omitting leaves the kind alone; "" moves the card back to uncategorised. */
  kind?: string;
  /** Explicit null detaches the card from its requirement; omitting the
   *  field leaves the link alone. */
  issue_id?: string | null;
}

export interface CardListResponse {
  cards: Card[];
  total: number;
}
