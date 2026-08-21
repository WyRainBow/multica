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
  branch: string;
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
  next_action: string;
  updated_at: string | null;
}

/** One line of what happened, appended and never edited. */
export interface WorktreeEntry {
  id: string;
  workspace_id: string;
  worktree_id: string;
  /** The card this line is about, when it is about one. Many entries are about
   *  the tree itself and carry none. */
  issue_id: string | null;
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
  next_action?: string;
}

export interface CreateWorktreeEntryRequest {
  kind?: WorktreeEntryKind;
  body: string;
  sha?: string;
  issue_id?: string;
}
