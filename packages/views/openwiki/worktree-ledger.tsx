"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, GitBranch, Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueListOptions } from "@multica/core/issues/queries";
import { worktreeListOptions } from "@multica/core/worktrees/queries";
import {
  useCreateWorktree,
  useUpdateWorktreeSession,
} from "@multica/core/worktrees/mutations";
import type { Issue, Worktree } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../i18n";
import { useNavigation } from "../navigation";
import { WorktreeEntryList } from "./worktree-entry-list";

/**
 * The worktree ledger: where the code is, as opposed to how far a decision has
 * got (which is what a card says).
 *
 * Each tree carries three things that rot at different rates, so they are read
 * and written differently:
 *
 *   facts     branch, HEAD, merge SHA — measured by `multica worktree sync`
 *             from inside the checkout. Read-only here on purpose: a merge
 *             claim typed into a form is a claim nobody can re-check.
 *   session   who is driving and what is next. One slot, edited in place.
 *   entries   what happened, round by round. Append-only.
 *
 * Below the trees sit the delivery declarations still living on cards. They are
 * a different account — one card's own branch — and stay visible until they are
 * attached to a tree (`multica issue metadata set git.worktree <name>`).
 */

/** Pipeline order: what everything sits on, the work, then the batch carriers. */
const BRANCH_ROLES = ["base", "feature", "integration", "launch"] as const;

const roleUnknown = (role: string) =>
  !BRANCH_ROLES.includes(role as (typeof BRANCH_ROLES)[number]);

/** The metadata key binding a card to a tree, by tree name. */
const WORKTREE_KEY = "git.worktree";

function issueWorktreeName(issue: Issue): string {
  const meta = (issue.metadata ?? {}) as Record<string, unknown>;
  const value = meta[WORKTREE_KEY];
  return typeof value === "string" ? value : "";
}

export function WorktreeLedger() {
  const wsId = useWorkspaceId();
  const { t } = useT("openwiki");
  const [adding, setAdding] = useState(false);
  const [showClosed, setShowClosed] = useState(false);

  const { data: trees = [], isLoading } = useQuery(worktreeListOptions(wsId));
  const { data: issues = [] } = useQuery(issueListOptions(wsId));

  const issuesByTree = new Map<string, Issue[]>();
  for (const issue of issues) {
    const name = issueWorktreeName(issue);
    if (name === "") continue;
    const bucket = issuesByTree.get(name);
    if (bucket) bucket.push(issue);
    else issuesByTree.set(name, [issue]);
  }

  // Merged and archived trees stay reachable but out of the way: the ledger is
  // read to decide what to work on, and finished trees are not that.
  const closed = trees.filter(
    (tree: Worktree) => tree.status === "merged" || tree.status === "archived",
  );
  const open = trees.filter(
    (tree: Worktree) => tree.status !== "merged" && tree.status !== "archived",
  );
  const visible = showClosed ? [...open, ...closed] : open;

  return (
    <div className="p-4">
      <div className="mb-3 flex items-start justify-between gap-4">
        <p className="text-caption text-muted-foreground">
          {t(($) => $.worktree_hint)}
        </p>
        <Button size="sm" variant="secondary" onClick={() => setAdding((v) => !v)}>
          <Plus className="size-3.5" />
          {t(($) => $.worktree_add)}
        </Button>
      </div>

      {adding && <NewWorktreeForm onDone={() => setAdding(false)} />}

      {isLoading ? (
        <p className="text-body text-muted-foreground">{t(($) => $.worktree_loading)}</p>
      ) : trees.length === 0 ? (
        <p className="text-body text-muted-foreground">{t(($) => $.worktree_empty)}</p>
      ) : (
        <div className="space-y-2">
          {visible.map((tree: Worktree) => (
            <WorktreeRow
              key={tree.id}
              tree={tree}
              parent={trees.find((p: Worktree) => p.id === tree.parent_id)}
              issues={issuesByTree.get(tree.name) ?? []}
            />
          ))}
        </div>
      )}

      {closed.length > 0 && (
        <button
          type="button"
          className="mt-3 text-caption text-muted-foreground hover:text-foreground"
          onClick={() => setShowClosed((v) => !v)}
        >
          {showClosed
            ? t(($) => $.worktree_hide_closed)
            : t(($) => $.worktree_show_closed, { count: closed.length })}
        </button>
      )}

      <UnattachedDeclarations issues={issues} />
    </div>
  );
}

function WorktreeRow({
  tree,
  parent,
  issues,
}: {
  tree: Worktree;
  parent?: Worktree;
  issues: Issue[];
}) {
  const { t } = useT("openwiki");
  const timeAgo = useTimeAgo();
  const nav = useNavigation();
  const paths = useWorkspacePaths();
  const [expanded, setExpanded] = useState(false);
  const [editingSession, setEditingSession] = useState(false);

  const roleLabel = roleUnknown(tree.role)
    ? t(($) => $.role_unknown, { role: tree.role })
    : t(($) => $[`role_${tree.role}` as "role_base"]);

  return (
    <div className="rounded-lg border">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-2">
        <button
          type="button"
          className="flex items-center gap-1.5"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? (
            <ChevronDown className="size-3.5 text-muted-foreground" />
          ) : (
            <ChevronRight className="size-3.5 text-muted-foreground" />
          )}
          <span className="font-medium">{tree.name}</span>
        </button>
        <span className="rounded bg-muted px-1.5 py-0.5 text-caption">{roleLabel}</span>
        <span
          className={cn(
            "text-caption",
            tree.status === "blocked" ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {tree.status}
        </span>
        <span className="truncate font-mono text-caption text-muted-foreground">
          {tree.branch || "—"}
          {tree.base_ref !== "" && ` ← ${tree.base_ref}`}
          {/* A dirty working copy is why a branch head and the ledger can
              disagree, so it is stated next to the branch, not hidden. */}
          {tree.dirty && ` · ${t(($) => $.worktree_dirty)}`}
        </span>
        <span className="ml-auto text-caption text-muted-foreground">
          {tree.verified_at
            ? t(($) => $.worktree_measured, { when: timeAgo(tree.verified_at as string) })
            : t(($) => $.worktree_never_measured)}
        </span>
      </div>

      <div className="border-t px-3 py-2">
        {editingSession ? (
          <SessionEditor tree={tree} onDone={() => setEditingSession(false)} />
        ) : (
          <button
            type="button"
            className="w-full text-left"
            onClick={() => setEditingSession(true)}
          >
            {tree.session.agent === "" && tree.session.next_action === "" ? (
              <span className="text-caption text-muted-foreground">
                {t(($) => $.session_empty)}
              </span>
            ) : (
              <span className="flex flex-wrap items-baseline gap-x-2 text-caption">
                <span className="font-medium">
                  {tree.session.agent || t(($) => $.session_no_agent)}
                </span>
                {tree.session.owner !== "" && (
                  <span className="text-muted-foreground">{tree.session.owner}</span>
                )}
                {tree.session.next_action !== "" && (
                  <span>→ {tree.session.next_action}</span>
                )}
                {tree.session.resume !== "" && (
                  <span className="truncate font-mono text-muted-foreground">
                    {tree.session.resume}
                  </span>
                )}
              </span>
            )}
          </button>
        )}
      </div>

      {expanded && (
        <div className="border-t px-3 py-2">
          <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1 text-caption">
            <dt className="text-muted-foreground">{t(($) => $.field_path)}</dt>
            <dd className="truncate font-mono">{tree.path || "—"}</dd>
            {parent && (
              <>
                <dt className="text-muted-foreground">{t(($) => $.field_feeds)}</dt>
                <dd>{parent.name}</dd>
              </>
            )}
            {tree.merged_sha !== "" && (
              <>
                <dt className="text-muted-foreground">{t(($) => $.field_merged)}</dt>
                <dd className="font-mono">
                  {tree.merged_sha.slice(0, 12)}
                  {tree.merged_into !== "" && ` → ${tree.merged_into}`}
                </dd>
              </>
            )}
            {issues.length > 0 && (
              <>
                <dt className="text-muted-foreground">{t(($) => $.field_issues)}</dt>
                <dd className="flex flex-wrap gap-x-2">
                  {issues.map((issue) => (
                    <button
                      key={issue.id}
                      type="button"
                      className="font-mono hover:underline"
                      onClick={() => nav.push(paths.issueDetail(issue.identifier))}
                    >
                      {issue.identifier}
                    </button>
                  ))}
                </dd>
              </>
            )}
          </dl>
          <WorktreeEntryList treeRef={tree.id} />
        </div>
      )}
    </div>
  );
}

function SessionEditor({ tree, onDone }: { tree: Worktree; onDone: () => void }) {
  const { t } = useT("openwiki");
  const { t: tc } = useT("common");
  const [agent, setAgent] = useState(tree.session.agent);
  const [owner, setOwner] = useState(tree.session.owner);
  const [resume, setResume] = useState(tree.session.resume);
  const [next, setNext] = useState(tree.session.next_action);
  const updateSession = useUpdateWorktreeSession();

  const save = () => {
    if (updateSession.isPending) return;
    updateSession.mutate(
      { ref: tree.id, agent, owner, resume, next_action: next },
      { onSuccess: onDone },
    );
  };

  return (
    <div className="space-y-1.5">
      <div className="flex gap-1.5">
        <Input
          className="h-7 w-32"
          value={agent}
          placeholder={t(($) => $.session_agent)}
          onChange={(e) => setAgent(e.target.value)}
        />
        <Input
          className="h-7 w-32"
          value={owner}
          placeholder={t(($) => $.session_owner)}
          onChange={(e) => setOwner(e.target.value)}
        />
        <Input
          className="h-7 flex-1"
          value={next}
          placeholder={t(($) => $.session_next)}
          onChange={(e) => setNext(e.target.value)}
        />
      </div>
      <div className="flex gap-1.5">
        <Input
          className="h-7 flex-1 font-mono"
          value={resume}
          placeholder={t(($) => $.session_resume)}
          onChange={(e) => setResume(e.target.value)}
        />
        <Button size="sm" onClick={save} disabled={updateSession.isPending}>
          {tc(($) => $.save)}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDone}>
          {tc(($) => $.cancel)}
        </Button>
      </div>
    </div>
  );
}

function NewWorktreeForm({ onDone }: { onDone: () => void }) {
  const { t } = useT("openwiki");
  const { t: tc } = useT("common");
  const [name, setName] = useState("");
  const [branch, setBranch] = useState("");
  const [base, setBase] = useState("");
  const [role, setRole] = useState<string>("feature");
  const createWorktree = useCreateWorktree();

  const submit = () => {
    const trimmed = name.trim();
    if (trimmed === "" || createWorktree.isPending) return;
    createWorktree.mutate(
      { name: trimmed, branch: branch.trim(), base_ref: base.trim(), role },
      { onSuccess: onDone },
    );
  };

  return (
    <div className="mb-3 flex flex-wrap items-center gap-1.5 rounded-lg border p-2">
      <Input
        className="h-7 w-40"
        value={name}
        placeholder={t(($) => $.field_name)}
        onChange={(e) => setName(e.target.value)}
      />
      <Input
        className="h-7 w-52 font-mono"
        value={branch}
        placeholder={t(($) => $.field_branch)}
        onChange={(e) => setBranch(e.target.value)}
      />
      <Input
        className="h-7 w-40 font-mono"
        value={base}
        placeholder={t(($) => $.field_base)}
        onChange={(e) => setBase(e.target.value)}
      />
      <NativeSelect size="sm" value={role} onChange={(e) => setRole(e.target.value)}>
        {BRANCH_ROLES.map((r) => (
          <NativeSelectOption key={r} value={r}>
            {t(($) => $[`role_${r}` as "role_base"])}
          </NativeSelectOption>
        ))}
      </NativeSelect>
      <Button size="sm" onClick={submit} disabled={name.trim() === "" || createWorktree.isPending}>
        {tc(($) => $.save)}
      </Button>
      <Button size="sm" variant="ghost" onClick={onDone}>
        {tc(($) => $.cancel)}
      </Button>
      <p className="w-full text-caption text-muted-foreground">
        {t(($) => $.worktree_add_hint)}
      </p>
    </div>
  );
}

// --- delivery declarations still living on cards ---

interface DeclarationRow {
  issueId: string;
  identifier: string;
  title: string;
  status: string;
  baseRef: string;
  deliveryRef: string;
  mrUrl: string;
  role: string;
}

function declarationRow(issue: Issue): DeclarationRow | null {
  const meta = (issue.metadata ?? {}) as Record<string, unknown>;
  const str = (k: string) => (typeof meta[k] === "string" ? (meta[k] as string) : "");
  // A card already bound to a tree is accounted for above; listing it twice
  // would suggest two different branches.
  if (str(WORKTREE_KEY) !== "") return null;
  const baseRef = str("git.base_ref") || str("baseline_ref");
  const deliveryRef = str("git.delivery_ref") || str("delivery_branch");
  const mrUrl = str("vcs.primary_mr_url") || str("mr_url");
  if (!baseRef && !deliveryRef && !mrUrl) return null;
  return {
    issueId: issue.id,
    identifier: issue.identifier,
    title: issue.title,
    status: issue.status,
    baseRef,
    deliveryRef,
    mrUrl,
    role: str("git.branch_role") || "feature",
  };
}

function UnattachedDeclarations({ issues }: { issues: Issue[] }) {
  const { t } = useT("openwiki");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const rows = issues
    .map(declarationRow)
    .filter((r): r is DeclarationRow => r !== null)
    .sort((a, b) => a.identifier.localeCompare(b.identifier));

  if (rows.length === 0) return null;

  return (
    <section className="mt-6">
      <h3 className="flex items-center gap-1.5 text-body font-medium">
        <GitBranch className="size-3.5 text-muted-foreground" />
        {t(($) => $.declarations_title)}
        <span className="text-caption text-muted-foreground tabular-nums">
          {rows.length}
        </span>
      </h3>
      <p className="mt-1 text-caption text-muted-foreground">
        {t(($) => $.declarations_hint)}
      </p>
      <table className="mt-2 w-full text-body">
        <thead>
          <tr className="border-b text-left text-caption text-muted-foreground">
            <th className="py-1.5 pr-3 font-medium">{t(($) => $.col_issue)}</th>
            <th className="py-1.5 pr-3 font-medium">{t(($) => $.col_status)}</th>
            <th className="py-1.5 pr-3 font-medium">{t(($) => $.col_base)}</th>
            <th className="py-1.5 pr-3 font-medium">{t(($) => $.col_delivery)}</th>
            <th className="py-1.5 font-medium">{t(($) => $.col_mr)}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.issueId} className="border-b last:border-0">
              <td className="py-1.5 pr-3">
                <button
                  type="button"
                  className="text-left hover:underline"
                  onClick={() => nav.push(paths.issueDetail(r.identifier))}
                >
                  <span className="font-mono text-caption">{r.identifier}</span>{" "}
                  <span className="inline-block max-w-[24ch] truncate align-middle text-muted-foreground">
                    {r.title}
                  </span>
                </button>
              </td>
              <td className="whitespace-nowrap py-1.5 pr-3 text-muted-foreground">
                {r.status}
              </td>
              <td className="whitespace-nowrap py-1.5 pr-3 font-mono text-caption">
                {r.baseRef || "—"}
              </td>
              <td className="whitespace-nowrap py-1.5 pr-3 font-mono text-caption">
                {r.deliveryRef || "—"}
              </td>
              <td className="py-1.5">
                {r.mrUrl ? (
                  <a href={r.mrUrl} target="_blank" rel="noreferrer" className="hover:underline">
                    MR
                  </a>
                ) : (
                  "—"
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
