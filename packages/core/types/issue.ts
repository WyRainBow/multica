import type { Label } from "./label";
import type { IssuePropertyValues } from "./property";

export type IssueStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

/**
 * Whether a filter category keeps what matches or drops it.
 *
 * "exclude" exists because the include form cannot express "everything except
 * backlog" without ticking every other status by hand — which is also wrong
 * the moment a new status is added.
 */
export type IssueFilterMode = "include" | "exclude";

/** The filter categories that support exclusion. */
export type IssueFilterCategory =
  | "status"
  | "priority"
  | "assignee"
  | "creator"
  | "project"
  | "label"
  | "parent";

export type IssueFilterModes = Record<IssueFilterCategory, IssueFilterMode>;

export const ISSUE_FILTER_CATEGORIES: readonly IssueFilterCategory[] = [
  "status",
  "priority",
  "assignee",
  "creator",
  "project",
  "label",
  "parent",
];

/** Every category including, which is the behaviour that predates exclusion. */
export function defaultIssueFilterModes(): IssueFilterModes {
  return {
    status: "include",
    priority: "include",
    assignee: "include",
    creator: "include",
    project: "include",
    label: "include",
    parent: "include",
  };
}

/**
 * One station in a requirement's life: 开始 / 已冻结 / 实施中 / 等待部署 / 结束.
 *
 * A container, not a status. `status` is a single value that answers "where is
 * this now" and forgets everything it passed through; a phase stays, holding
 * the comments made while the issue was in it.
 *
 * Also not `stage`, which already exists and means a barrier group among
 * sibling sub-issues. That one schedules; this one archives.
 */
export interface IssuePhase {
  id: string;
  workspace_id: string;
  issue_id: string;
  name: string;
  /** Order along the track. Sparse, so a station can be inserted between two
   *  others without renumbering their neighbours. */
  position: number;
  /** Derived from transitions, never typed in. */
  entered_at: string | null;
  completed_at: string | null;
  /** How many comments hang under this phase. */
  comment_count: number;
  created_at: string;
  updated_at: string;
}

export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export type IssueAssigneeType = "member" | "agent" | "squad";

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

/**
 * Per-issue metadata is a flat KV map agents use to record pipeline state
 * (PR number, pipeline_status, waiting_on, ...). Values are primitives only —
 * string / number / bool — enforced by both the API and the DB. Always
 * present in responses (empty object when unset) so reads don't need a
 * nil guard on the parent field.
 */
export type IssueMetadataValue = string | number | boolean;
export type IssueMetadata = Record<string, IssueMetadataValue>;

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  creator_type: IssueAssigneeType;
  creator_id: string;
  parent_issue_id: string | null;
  project_id: string | null;
  /** The agent session that filed this card, snapshotted at birth and never
   *  updated. Empty when it was filed outside any agent session.
   *
   *  Optional on the type, required on nothing: a card filed by a backend that
   *  predates the field parses without it, and every fixture in the repo would
   *  otherwise have to name a value it does not care about. */
  created_by_session?: string;
  position: number;
  // Ordered barrier group among sibling sub-issues (null = unstaged). The
  // parent assignee is notified/woken only when every sub-issue in a stage
  // finishes; see server/internal/handler/issue_child_done.go.
  stage: number | null;
  // Calendar days as date-only "YYYY-MM-DD" (no time, no timezone). Use the
  // helpers in @multica/core/issues/date to format/compare — never `new Date()`
  // + local formatting, which shifts the day by the viewer's offset.
  start_date: string | null;
  due_date: string | null;
  metadata: IssueMetadata;
  // Custom property values keyed by property definition id. Always present
  // in responses (empty object when unset), mirroring `metadata`.
  properties: IssuePropertyValues;
  reactions?: IssueReaction[];
  labels?: Label[];
  created_at: string;
  updated_at: string;
  // Set when the issue has been taken out of view. Orthogonal to `status`:
  // an archived issue keeps whatever answer status gave for how the work
  // ended, which is exactly what folding archiving into status would destroy.
  archived_at: string | null;
  archived_by: string | null;
  // The requirement this issue was lifted out of, when it was parked. Only a
  // provenance note: a parked issue is top-level and no longer follows that
  // requirement's status or archiving. May point at an issue that has since
  // been deleted, so readers must tolerate it resolving to nothing.
  parked_from_issue_id: string | null;
  // The body's optimistic-concurrency counter, quoted back as
  // `base_description_revision` on a write to prove the writer edited the
  // text that is still there (COC-342). Optional: older backends and
  // list projections that don't carry the column send nothing, and absence
  // means "no claim possible", not revision 0.
  description_revision?: number;
}
