"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueListOptions } from "@multica/core/issues/queries";
import { worktreeListOptions } from "@multica/core/worktrees/queries";
import {
  useCreateWorktree,
  useCreateWorktreeEntry,
  useUpdateWorktreeSession,
} from "@multica/core/worktrees/mutations";
import type { Issue, Worktree } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../i18n";
import { useNavigation } from "../navigation";
import { WorktreeEntryList } from "./worktree-entry-list";
import { SessionPointer } from "../worktrees/session-pointer";

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
const BRANCH_ROLES = ["base", "feature", "integration", "release", "hotfix"] as const;

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

  // Grouped by role, in pipeline order. The roles are the branch naming rule —
  // feature/<card>, integration/<topic>, release/<date>, hotfix/<desc> — so a
  // reader can see at a glance which batch branches exist and what feeds them,
  // which a flat list of checkouts never showed.
  const groups = [...BRANCH_ROLES, "other"].map((role) => ({
    role,
    trees: visible.filter((tree: Worktree) =>
      role === "other" ? roleUnknown(tree.role) : tree.role === role,
    ),
  }));

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
        <div className="space-y-5">
          {groups
            .filter((group) => group.trees.length > 0)
            .map((group) => (
              <section key={group.role}>
                <h3 className="mb-1.5 flex items-baseline gap-2">
                  <span className="text-body font-medium">
                    {group.role === "other"
                      ? t(($) => $.role_unfiled)
                      : t(($) => $[`role_${group.role}` as "role_base"])}
                  </span>
                  {group.role !== "other" && group.role !== "base" && (
                    <span className="font-mono text-caption text-muted-foreground">
                      {group.role}/
                    </span>
                  )}
                  <span className="text-caption text-muted-foreground">
                    {group.trees.length}
                  </span>
                </h3>
                <div className="space-y-2">
                  {group.trees.map((tree: Worktree) => (
                    <WorktreeRow
                      key={tree.id}
                      tree={tree}
                      parent={trees.find((p: Worktree) => p.id === tree.parent_id)}
                      issues={issuesByTree.get(tree.name) ?? []}
                    />
                  ))}
                </div>
              </section>
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

/**
 * One checkout, laid out as the three questions someone actually opens this
 * page with: where is the code, who has it, and what happened in it. Each is a
 * labelled band, because the previous single line — name, role, status, branch,
 * owner, next action and a resume command run together — could be read only by
 * whoever wrote it.
 *
 * The bands are open by default. The information is the page; hiding it behind
 * a chevron made the ledger look empty.
 */
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
  const [editingSession, setEditingSession] = useState(false);
  const [handingOff, setHandingOff] = useState(false);
  const [showCommands, setShowCommands] = useState(false);

  const unclaimed =
    tree.session.agent === "" &&
    tree.session.next_action === "" &&
    tree.session.resume === "";

  return (
    <div className="rounded-lg border">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b px-3 py-2">
        <span className="font-medium">{tree.name}</span>
        <span
          className={cn(
            "text-caption",
            tree.status === "blocked" ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {tree.status}
        </span>
        {issues.map((issue) => (
          <button
            key={issue.id}
            type="button"
            className="font-mono text-caption hover:underline"
            onClick={() => nav.push(paths.issueDetail(issue.identifier))}
          >
            {issue.identifier}
          </button>
        ))}
        <span className="ml-auto text-caption text-muted-foreground">
          {tree.verified_at
            ? t(($) => $.worktree_measured, { when: timeAgo(tree.verified_at as string) })
            : t(($) => $.worktree_never_measured)}
        </span>
      </div>

      {/* Where the code is. Measured by `worktree sync`, never typed here. */}
      <Band label={t(($) => $.band_branch)}>
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="font-mono">{tree.branch || "—"}</span>
          {tree.base_ref !== "" && (
            <span className="font-mono text-muted-foreground">← {tree.base_ref}</span>
          )}
          {parent && (
            <span className="text-muted-foreground">
              {t(($) => $.band_feeds, { name: parent.name })}
            </span>
          )}
          {/* A dirty working copy is why a branch head and the ledger can
              disagree, so it is stated next to the branch, not hidden. */}
          {tree.dirty && <span className="text-destructive">{t(($) => $.worktree_dirty)}</span>}
        </div>
        {tree.merged_sha !== "" && (
          <div className="font-mono text-muted-foreground">
            {tree.merged_sha.slice(0, 12)}
            {tree.merged_into !== "" && ` → ${tree.merged_into}`}
          </div>
        )}
        {tree.path !== "" && (
          <div className="truncate font-mono text-muted-foreground">{tree.path}</div>
        )}
      </Band>

      {/* Who has it, and the command that puts you back in their session. */}
      <Band
        label={t(($) => $.band_session)}
        action={
          !handingOff && !unclaimed ? (
            <button
              type="button"
              className="text-caption text-muted-foreground hover:text-foreground"
              onClick={() => setHandingOff(true)}
            >
              {t(($) => $.handoff_start)}
            </button>
          ) : undefined
        }
      >
        {handingOff ? (
          <HandoffForm tree={tree} onDone={() => setHandingOff(false)} />
        ) : editingSession ? (
          <SessionEditor tree={tree} onDone={() => setEditingSession(false)} />
        ) : unclaimed ? (
          <button
            type="button"
            className="w-full text-left text-muted-foreground"
            onClick={() => setEditingSession(true)}
          >
            {t(($) => $.session_empty)}
          </button>
        ) : (
          <SessionPointer
            session={tree.session}
            onEdit={() => setEditingSession(true)}
          />
        )}
      </Band>

      {/* What happened, round by round. */}
      <Band label={t(($) => $.band_log)}>
        <WorktreeEntryList treeRef={tree.id} />
      </Band>

      <div className="border-t px-3 py-1.5">
        <button
          type="button"
          className="text-caption text-muted-foreground hover:text-foreground"
          onClick={() => setShowCommands((v) => !v)}
        >
          {showCommands ? t(($) => $.commands_hide) : t(($) => $.commands_show)}
        </button>
        {showCommands && <CommandList tree={tree} />}
      </div>
    </div>
  );
}

/** A labelled section of a tree card. The label is what makes the card
 *  readable by someone who did not build it. */
function Band({
  label,
  action,
  children,
}: {
  label: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="border-b px-3 py-2 last:border-b-0">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-caption font-medium text-muted-foreground">{label}</span>
        {action}
      </div>
      <div className="mt-1 space-y-0.5 text-caption">{children}</div>
    </div>
  );
}

/**
 * Handing the tree to the other agent.
 *
 * Three things have to happen together or the next person inherits a half
 * state: the ledger gets a line saying who handed what to whom, the session
 * slot names the incoming agent and what they are being asked to do, and the
 * outgoing resume pointer is cleared. That last one matters — leaving it would
 * offer the next reader a command that reopens the session that just left.
 */
function HandoffForm({ tree, onDone }: { tree: Worktree; onDone: () => void }) {
  const { t } = useT("openwiki");
  const { t: tc } = useT("common");
  const from = tree.session.agent;
  const [to, setTo] = useState(from === "codex" ? "claude" : "codex");
  const [note, setNote] = useState("");
  const updateSession = useUpdateWorktreeSession();
  const createEntry = useCreateWorktreeEntry();

  const pending = updateSession.isPending || createEntry.isPending;

  const submit = () => {
    const target = to.trim();
    const handover = note.trim();
    if (target === "" || handover === "" || pending) return;
    createEntry.mutate({
      ref: tree.id,
      kind: "handoff",
      body: `${from || "?"} → ${target}: ${handover}`,
    });
    updateSession.mutate(
      { ref: tree.id, agent: target, next_action: handover, resume: "" },
      { onSuccess: onDone },
    );
  };

  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-muted-foreground">{from || "?"} →</span>
        <Input
          className="h-7 w-28"
          value={to}
          placeholder={t(($) => $.handoff_to)}
          onChange={(e) => setTo(e.target.value)}
        />
        <Input
          className="h-7 min-w-40 flex-1"
          value={note}
          placeholder={t(($) => $.handoff_note)}
          onChange={(e) => setNote(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        <Button size="sm" onClick={submit} disabled={pending || note.trim() === ""}>
          {t(($) => $.handoff_confirm)}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDone}>
          {tc(($) => $.cancel)}
        </Button>
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.handoff_hint)}</p>
    </div>
  );
}

/**
 * The terminal commands for this tree, ready to paste.
 *
 * The ledger is written from the terminal — the facts account can only be
 * written there, since it is measured inside the checkout — so a page that
 * shows the state without naming the commands leaves the reader to remember
 * them. Each is filled in with this tree's name.
 */
function CommandList({ tree }: { tree: Worktree }) {
  const { t } = useT("openwiki");
  const rows: { label: string; command: string }[] = [
    { label: t(($) => $.cmd_log), command: `multica worktree log ${tree.name} "…" --issue COC-N` },
    { label: t(($) => $.cmd_sync), command: `multica worktree sync ${tree.name}` },
    { label: t(($) => $.cmd_session), command: `multica worktree session ${tree.name} --auto --next "…"` },
    { label: t(($) => $.cmd_handoff), command: `multica worktree session ${tree.name} --agent codex --next "…"` },
  ];
  return (
    <dl className="mt-1.5 space-y-1">
      {rows.map((row) => (
        <div key={row.label} className="flex items-center gap-2">
          <dt className="w-16 shrink-0 text-caption text-muted-foreground">{row.label}</dt>
          <dd className="min-w-0 flex-1">
            <CopyableCommand command={row.command} />
          </dd>
        </div>
      ))}
    </dl>
  );
}

function CopyableCommand({ command }: { command: string }) {
  const { t } = useT("openwiki");
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 truncate rounded bg-muted px-1.5 py-0.5 font-mono text-caption">
        {command}
      </code>
      <Button
        variant="ghost"
        size="sm"
        onClick={() =>
          void copyText(command).then((ok) => {
            if (!ok) return;
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          })
        }
      >
        {copied ? t(($) => $.session_copied) : t(($) => $.session_copy)}
      </Button>
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
