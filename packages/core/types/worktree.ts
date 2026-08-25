/**
 * A checkout tracked as an object: which branch it carries, what it sits on,
 * which batch branch it feeds, who is driving it, and what happened in it.
 *
 * Distinct from an issue. A card says how far a decision has got; a worktree
 * says where the code is. They drift apart routinely — a card can close while
 * the branch is still open, a branch can merge before anyone touches the card —
 * so neither is inferred from the other.
 */
export interface Worktree {
  id: string;
  workspace_id: string;
  /** How everything else addresses this tree, including the CLI. */
  name: string;
  path: string;
  repo: string;
  /** What the checkout currently has, re-measured by every sync. Reads as the
   *  base branch once the work lands. */
  branch: string;
  /** The branch this card's work was opened on. Never re-measured, so it
   *  survives the merge that rewrites `branch`. */
  work_branch: string;
  base_ref: string;
  /** Pipeline position: base → feature → integration → launch. */
  role: string;
  status: string;
  /** Measured facts. Full 40-character object names or empty; written only by
   *  `worktree sync` running inside the checkout. */
  head_sha: string;
  merged_sha: string;
  merged_into: string;
  dirty: boolean;
  /** When the repo last confirmed the facts above, as opposed to when someone
   *  last claimed them. Null means never measured. */
  verified_at: string | null;
  session: WorktreeSession;
  /** The card this account belongs to, as an identifier (COC-348) and as a
   *  UUID. Either may be empty: an account can predate its card. */
  issue: string;
  issue_id: string;
  /** Accounts this one waits on, by key. */
  depends_on: string[];
  /** Where this card's output landed. */
  artifacts: string[];
  parent_id: string | null;
  entry_count: number;
  created_at: string;
  updated_at: string;
}

/** Who is driving this tree right now and what it waits on. One slot per tree,
 *  overwritten in place. */
export interface WorktreeSession {
  agent: string;
  /** The exact command that resumes that session. */
  resume: string;
  owner: string;
  /** The session's own id, as the agent reports it. Recorded rather than parsed
   *  back out of `resume`, so two sessions on one tree stay distinguishable. */
  session_id: string;
  /** Stopped waiting on a person. Recorded by whoever stopped, never inferred:
   *  a guessed wait status is one nobody can disprove. */
  waiting_for_human: boolean;
  next_action: string;
  updated_at: string | null;
}

/** One session that worked on a card, read from the code-progress ledger. */
export interface IssueSession {
  /** Addressable name of the ledger account. */
  worktree: string;
  worktree_id: string;
  role: string;
  status: string;
  branch: string;
  work_branch: string;
  agent: string;
  session_id: string;
  resume: string;
  owner: string;
  next_action: string;
  waiting_for_human: boolean;
  updated_at: string | null;
  /** True when the account belongs to this card; false when it only mentions it
   *  in a log line. */
  direct: boolean;
}

/** One line of what happened, appended and never edited. */
export interface WorktreeEntry {
  id: string;
  workspace_id: string;
  worktree_id: string;
  /** The card this line is about, when it is about one. Many entries are about
   *  the tree itself and carry none. */
  issue_id: string | null;
  /** The same card as the identifier a person reads. */
  issue: string;
  /** One of WorktreeEntryKind, but typed loosely on read: a kind added by a
   *  newer backend should render as itself rather than fail parsing. */
  kind: string;
  body: string;
  sha: string;
  author_type: string;
  author_id: string;
  created_at: string;
}

/** The kinds this build can write. Reads accept anything. */
export type WorktreeEntryKind =
  | "progress"
  | "branch"
  | "merge"
  | "blocked"
  | "handoff"
  | "verify";

export interface CreateWorktreeRequest {
  name: string;
  issue?: string;
  depends_on?: string[];
  path?: string;
  repo?: string;
  branch?: string;
  base_ref?: string;
  role?: string;
  status?: string;
  parent_id?: string;
}

export interface UpdateWorktreeRequest {
  name?: string;
  issue?: string;
  depends_on?: string[];
  artifacts?: string[];
  path?: string;
  repo?: string;
  branch?: string;
  base_ref?: string;
  role?: string;
  status?: string;
  /** An empty string detaches the tree from its parent. */
  parent_id?: string;
}

export interface UpdateWorktreeSessionRequest {
  agent?: string;
  resume?: string;
  owner?: string;
  session_id?: string;
  waiting_for_human?: boolean;
  next_action?: string;
}

export interface CreateWorktreeEntryRequest {
  kind?: WorktreeEntryKind;
  body: string;
  sha?: string;
  issue_id?: string;
}

/** A review request recorded by hand against a card.
 *
 *  A URL and who recorded it, nothing fetched. This workspace integrates with
 *  no forge, so there is no state to show and none is claimed. */
export interface IssuePRLink {
  id: string;
  url: string;
  title: string;
  /** Display name as it stood when the link was recorded — a snapshot, because
   *  this is a log line. */
  added_by: string;
  added_by_type: string;
  added_at: string;
}
