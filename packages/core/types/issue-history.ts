/**
 * One card's history, in the three shapes it actually has.
 *
 * Decisions, review rounds and documents come from three separate sources with
 * two separate derivations (COC-328 D1): a decision's status is derived only
 * from other decisions, a round's verdict only from the round document. They
 * are kept apart here for the same reason they are kept apart on screen — a
 * mixed timeline makes the reader work out which system each line belongs to.
 */

/** Row kinds. The last two mark an absence rather than state a decision. */
export type IssueDecisionRowStatus =
  | "open"
  | "current"
  | "superseded"
  /** A decision number nothing was filed under. Never renumbered, never filled. */
  | "gap"
  /** A decisions/ document with no machine header. Status is not derived and
   *  never guessed. */
  | "legacy";

/** One line of the decision table, whatever kind of line it is. */
export interface IssueHistoryDecisionRow {
  /** "D5" for a decision, "D8#1" for an open question, "D3" for a gap. */
  id: string;
  /** One of IssueDecisionRowStatus, typed loosely on read. */
  status: string;
  doc_id: string;
  number: number;
  question: string;
  summary: string;
  decided_by: string;
  recorded_by: string;
  /** As written on the card. Empty for open questions, gaps and legacy rows. */
  decided_at: string;
  superseded_by: string;
  raised_by: string;
  affects: string[];
  /** A legacy card's document title — all a reader gets before opening it. */
  title: string;
}

/** One closed review round. Numbered per station, so two rows may both read R1. */
export interface IssueHistoryRound {
  id: string;
  number: number;
  station: string;
  /** approve / request_changes / block. Answers "did this round end", not "did
   *  the work pass". */
  verdict: string;
  summary: string;
  doc_id: string;
  title: string;
  closed_at: string;
  /** From reviews/, the system rounds/ replaced. Read-only, no longer written. */
  legacy: boolean;
}

/** A frozen snapshot, or the live document it was taken from. */
export interface IssueHistoryDocument {
  id: string;
  kind: string;
  title: string;
  snapshot: boolean;
  /** Which document this froze — "spec", "requirements". */
  snapshot_of: string;
  /** Which round it was frozen at — "R4-代码评审". */
  taken_at: string;
  updated_at: string;
  created_at: string;
}

export interface IssueHistory {
  decisions: IssueHistoryDecisionRow[];
  rounds: IssueHistoryRound[];
  documents: IssueHistoryDocument[];
}
