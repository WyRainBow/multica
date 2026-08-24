/**
 * The fixed document directory every issue gets.
 *
 * An issue's documents used to appear only once somebody wrote one, which made
 * "there is no design doc" and "nobody has looked at the design yet" the same
 * observation. The directory is created with the issue instead: six named
 * slots, each held open by a placeholder card until real content lands in it.
 *
 * Placeholders are invisible everywhere else — the card list, search and the
 * brief all filter them out in SQL — so this shape is the only place a reader
 * can tell an unanswered slot from a missing one.
 */

/** The two shapes a slot can have. Reads accept anything: the server owns this
 *  enum, so a value this build has not heard of must still render. */
export type IssueNamespaceSlotType = "document" | "folder";

/** One slot as a reader sees it. */
export interface IssueNamespaceSlot {
  /** The path segment and the stable key on the wire. One of the six:
   *  requirements, design, spec, decisions, rounds, snapshots. */
  name: string;
  /** What a reader sees, in Chinese, matching the product voice. Server-owned
   *  so this build never keeps a second copy of the slot list. */
  label: string;
  /** The full kind path of this slot, e.g. `COC-338/requirements`. */
  kind: string;
  /** `document` | `folder`, but typed loosely on read: a type added by a newer
   *  backend should render as itself rather than blank the section. Every
   *  switch over it needs a `default` branch. */
  type: string;
  /** False only for issues created before the directory existed: the slot has
   *  neither a placeholder nor any document under it. */
  exists: boolean;
  /** The ONE answer to "is this real yet". Read off the card's `is_placeholder`
   *  column and from nothing else — not the title, not whether the body is
   *  empty. True renders as 待补. */
  placeholder: boolean;
  /** The card sitting exactly at this slot's kind, placeholder or document, so
   *  a writer can address it directly. Empty for a folder whose placeholder is
   *  gone but which has documents beneath it. */
  card_id: string;
  title: string;
  /** How many real documents are at or below this slot. Always 0 or 1 for a
   *  document slot; the interesting number is on the folders. */
  count: number;
}

/** The whole directory for one issue. */
export interface IssueNamespace {
  issue_id: string;
  /** The human identifier the documents are filed under — the same "COC-338"
   *  a person types. */
  key: string;
  root: string;
  /** Fixed order: requirements, design, spec, decisions, rounds, snapshots —
   *  what is being asked for, how it will be built, what was frozen, then the
   *  three histories that explain how it got there. */
  slots: IssueNamespaceSlot[];
}
