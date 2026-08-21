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
import { attentionItems, type AttentionItem } from "../worktrees/attention";

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
  const [selectedId, setSelectedId] = useState<string | null>(null);

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
  const selected = trees.find((tree: Worktree) => tree.id === selectedId);

  // Grouped by repository. A checkout belongs to one codebase and several are
  // in flight at once, so a heading spanning three of them tells you nothing
  // you can act on.
  const repoNames: string[] = [];
  for (const tree of visible) {
    if (!repoNames.includes(tree.repo)) repoNames.push(tree.repo);
  }
  // A tree registered without a repo still has to appear, and it sorts last:
  // it is a gap in the record, not a codebase.
  repoNames.sort((a, b) => (a === "" ? 1 : b === "" ? -1 : a.localeCompare(b)));

  const attention = attentionItems(trees, issuesByTree);

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

      {adding && (
        <NewWorktreeForm
          knownRepos={repoNames.filter((r) => r !== "")}
          onDone={() => setAdding(false)}
        />
      )}

      <AttentionPanel items={attention} onSelect={setSelectedId} />

      {isLoading ? (
        <p className="text-body text-muted-foreground">{t(($) => $.worktree_loading)}</p>
      ) : trees.length === 0 ? (
        <p className="text-body text-muted-foreground">{t(($) => $.worktree_empty)}</p>
      ) : (
        <div
          className={cn(
            "gap-4",
            // Without a selection the list takes the full width: branch names
            // are the one column that must not be truncated, and
            // feature/wy/COC-295/openwiki-tab and …-tab-v2 truncate alike.
            selected ? "grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_22rem]" : "block",
          )}
        >
          <div className="min-w-0 space-y-5">
            {repoNames.map((repo) => (
              <RepoSection
                key={repo || "—"}
                repo={repo}
                trees={visible.filter((tree: Worktree) => tree.repo === repo)}
                allTrees={trees}
                issuesByTree={issuesByTree}
                selectedId={selectedId}
                onSelect={setSelectedId}
              />
            ))}
          </div>

          {selected && (
            <TreeDetail
              tree={selected}
              parent={trees.find((p: Worktree) => p.id === selected.parent_id)}
              issues={issuesByTree.get(selected.name) ?? []}
              onClose={() => setSelectedId(null)}
            />
          )}
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
 * What needs a decision, above the ledger rather than inside it.
 *
 * Every item here is a join or a comparison — a tree measured before the
 * working copy moved, a branch that landed while its cards stayed open — so no
 * column in the table below can show it. At three trees this is one line; at
 * thirty it is the only part of the page anyone reads.
 */
function AttentionPanel({
  items,
  onSelect,
}: {
  items: AttentionItem[];
  onSelect: (id: string) => void;
}) {
  const { t } = useT("openwiki");
  if (items.length === 0) return null;

  return (
    <section className="mb-4 rounded-lg border">
      <h3 className="flex items-baseline gap-2 border-b px-3 py-2">
        <span className="text-body font-medium">{t(($) => $.attention_title)}</span>
        <span className="text-caption text-muted-foreground">{items.length}</span>
      </h3>
      <ul>
        {items.map((item) => (
          <li
            key={`${item.tree.id}-${item.kind}`}
            className="flex flex-wrap items-baseline gap-x-2 border-b px-3 py-1.5 text-caption last:border-b-0"
          >
            <button
              type="button"
              className="font-medium hover:underline"
              onClick={() => onSelect(item.tree.id)}
            >
              {item.tree.name}
            </button>
            <span
              className={cn(
                item.kind === "blocked" || item.kind === "uncommitted"
                  ? "text-destructive"
                  : "text-foreground",
              )}
            >
              {t(($) => $[`attention_${item.kind}` as "attention_blocked"])}
            </span>
            {item.issues.length > 0 && (
              <span className="font-mono text-muted-foreground">
                {item.issues.map((issue) => issue.identifier).join(" ")}
              </span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** One codebase's checkouts, in merge order. */
function RepoSection({
  repo,
  trees,
  allTrees,
  issuesByTree,
  selectedId,
  onSelect,
}: {
  repo: string;
  trees: Worktree[];
  allTrees: Worktree[];
  issuesByTree: Map<string, Issue[]>;
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const { t } = useT("openwiki");

  // Laid out by what merges into what, not by role. Roles put every feature
  // branch in one bucket; the question being asked is which branch carries
  // which, and that is the parent chain.
  const ordered: { tree: Worktree; depth: number }[] = [];
  const place = (parentId: string | null, depth: number) => {
    for (const tree of trees) {
      const parent = tree.parent_id ?? null;
      // A tree whose parent is not in this section is a root here, or the
      // chain would silently drop it.
      const isRoot =
        parent === null || !trees.some((candidate) => candidate.id === parent);
      if (parentId === null ? isRoot : parent === parentId) {
        if (ordered.some((row) => row.tree.id === tree.id)) continue;
        ordered.push({ tree, depth });
        place(tree.id, depth + 1);
      }
    }
  };
  place(null, 0);

  return (
    <section>
      <h2 className="mb-1.5 flex items-baseline gap-2 border-b pb-1">
        <span className="text-title-sm font-medium">
          {repo || t(($) => $.repo_unset)}
        </span>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.repo_count, { count: trees.length })}
        </span>
      </h2>
      <div>
        {ordered.map(({ tree, depth }) => (
          <TreeRow
            key={tree.id}
            tree={tree}
            depth={depth}
            parent={allTrees.find((p: Worktree) => p.id === tree.parent_id)}
            issues={issuesByTree.get(tree.name) ?? []}
            selected={tree.id === selectedId}
            onSelect={() => onSelect(tree.id)}
          />
        ))}
      </div>
    </section>
  );
}

/** One checkout as a row: where the code is, who has it, and whether the
 *  ledger's claims about it were measured. */
function TreeRow({
  tree,
  depth,
  parent,
  issues,
  selected,
  onSelect,
}: {
  tree: Worktree;
  depth: number;
  parent?: Worktree;
  issues: Issue[];
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("openwiki");
  const timeAgo = useTimeAgo();

  const roleLabel = roleUnknown(tree.role)
    ? t(($) => $.role_unknown, { role: tree.role })
    : t(($) => $[`role_${tree.role}` as "role_base"]);

  return (
    <button
      type="button"
      onClick={onSelect}
      // Selection is carried by weight and a left rule, which hover does not
      // touch — expressing it with background alone would make hovering a
      // selected row look like deselecting it.
      className={cn(
        "flex w-full flex-wrap items-baseline gap-x-2 gap-y-0.5 border-b px-2 py-2 text-left text-caption last:border-b-0 hover:bg-muted/50",
        selected && "border-l-2 border-l-foreground bg-muted/40 font-medium",
      )}
      style={{ paddingLeft: `${0.5 + depth * 1.25}rem` }}
    >
      <span className="min-w-32">{tree.name}</span>
      <span className="text-muted-foreground">{roleLabel}</span>

      <span className="min-w-0 flex-1 font-mono text-muted-foreground">
        {tree.branch || "—"}
        {tree.base_ref !== "" && ` ← ${tree.base_ref}`}
        {parent && ` → ${parent.name}`}
      </span>

      {issues.map((issue) => (
        <span key={issue.id} className="font-mono text-muted-foreground">
          {issue.identifier}
        </span>
      ))}

      <span className="text-muted-foreground">
        {tree.session.agent === ""
          ? t(($) => $.session_unclaimed)
          : tree.session.agent}
      </span>

      {/* The evidence, stated on the row. It is the one claim here that git
        re-checks on every sync, so it is worth more than a status word. */}
      <span className="min-w-40 text-right">
        {tree.merged_sha !== "" ? (
          <span className="font-mono">
            {tree.merged_sha.slice(0, 10)}
            {tree.merged_into !== "" && ` → ${tree.merged_into}`}
          </span>
        ) : tree.dirty ? (
          <span className="text-destructive">{t(($) => $.worktree_dirty)}</span>
        ) : tree.verified_at === null ? (
          <span className="text-muted-foreground">
            {t(($) => $.worktree_never_measured)}
          </span>
        ) : (
          <span className="text-muted-foreground">
            {t(($) => $.worktree_measured, { when: timeAgo(tree.verified_at) })}
          </span>
        )}
      </span>
    </button>
  );
}

/**
 * The selected checkout in full: its measured facts, the session driving it,
 * what happened in it, and the terminal commands for this tree.
 *
 * A panel rather than an expanded row because the log and the forms are only
 * wanted for one tree at a time, and putting them inline pushed every other
 * tree off the screen.
 */
function TreeDetail({
  tree,
  parent,
  issues,
  onClose,
}: {
  tree: Worktree;
  parent?: Worktree;
  issues: Issue[];
  onClose: () => void;
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
    <aside className="h-fit rounded-lg border lg:sticky lg:top-0">
      <div className="flex items-baseline gap-2 border-b px-3 py-2">
        <span className="font-medium">{tree.name}</span>
        <span className="text-caption text-muted-foreground">{tree.status}</span>
        <button
          type="button"
          className="ml-auto text-caption text-muted-foreground hover:text-foreground"
          onClick={onClose}
        >
          {t(($) => $.detail_close)}
        </button>
      </div>

      <dl className="grid grid-cols-[4.5rem_1fr] gap-x-3 gap-y-1 border-b px-3 py-2 text-caption">
        <dt className="text-muted-foreground">{t(($) => $.band_branch)}</dt>
        <dd className="min-w-0 break-all font-mono">{tree.branch || "—"}</dd>
        <dt className="text-muted-foreground">{t(($) => $.field_base)}</dt>
        <dd className="min-w-0 break-all font-mono">{tree.base_ref || "—"}</dd>
        {parent && (
          <>
            <dt className="text-muted-foreground">{t(($) => $.field_feeds)}</dt>
            <dd>{parent.name}</dd>
          </>
        )}
        <dt className="text-muted-foreground">{t(($) => $.field_merged)}</dt>
        <dd className="min-w-0 break-all font-mono">
          {tree.merged_sha === ""
            ? "—"
            : `${tree.merged_sha.slice(0, 12)}${tree.merged_into !== "" ? ` → ${tree.merged_into}` : ""}`}
        </dd>
        <dt className="text-muted-foreground">{t(($) => $.worktree_measured_label)}</dt>
        <dd>
          {tree.verified_at === null
            ? t(($) => $.worktree_never_measured)
            : timeAgo(tree.verified_at)}
        </dd>
        {tree.path !== "" && (
          <>
            <dt className="text-muted-foreground">{t(($) => $.field_path)}</dt>
            <dd className="min-w-0 break-all font-mono text-muted-foreground">
              {tree.path}
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

      <div className="border-b px-3 py-2">
        <div className="mb-1 flex items-baseline justify-between gap-2">
          <span className="text-caption font-medium text-muted-foreground">
            {t(($) => $.band_session)}
          </span>
          {!handingOff && !unclaimed && (
            <button
              type="button"
              className="text-caption text-muted-foreground hover:text-foreground"
              onClick={() => setHandingOff(true)}
            >
              {t(($) => $.handoff_start)}
            </button>
          )}
        </div>
        {handingOff ? (
          <HandoffForm tree={tree} onDone={() => setHandingOff(false)} />
        ) : editingSession ? (
          <SessionEditor tree={tree} onDone={() => setEditingSession(false)} />
        ) : unclaimed ? (
          <button
            type="button"
            className="w-full text-left text-caption text-muted-foreground"
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
      </div>

      <div className="border-b px-3 py-2">
        <span className="text-caption font-medium text-muted-foreground">
          {t(($) => $.band_log)}
        </span>
        <WorktreeEntryList treeRef={tree.id} />
      </div>

      <div className="px-3 py-1.5">
        <button
          type="button"
          className="text-caption text-muted-foreground hover:text-foreground"
          onClick={() => setShowCommands((v) => !v)}
        >
          {showCommands ? t(($) => $.commands_hide) : t(($) => $.commands_show)}
        </button>
        {showCommands && <CommandList tree={tree} />}
      </div>
    </aside>
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

function NewWorktreeForm({
  knownRepos,
  onDone,
}: {
  knownRepos: string[];
  onDone: () => void;
}) {
  const { t } = useT("openwiki");
  const { t: tc } = useT("common");
  const [name, setName] = useState("");
  // The ledger groups by repository, so a tree registered without one lands in
  // a pile called "unset". Default to the repository already in use when there
  // is only one — the common case is a second tree in the same codebase.
  const [repo, setRepo] = useState(knownRepos.length === 1 ? knownRepos[0]! : "");
  const [branch, setBranch] = useState("");
  const [base, setBase] = useState("");
  const [role, setRole] = useState<string>("feature");
  const createWorktree = useCreateWorktree();

  const submit = () => {
    const trimmed = name.trim();
    if (trimmed === "" || createWorktree.isPending) return;
    createWorktree.mutate(
      {
        name: trimmed,
        repo: repo.trim(),
        branch: branch.trim(),
        base_ref: base.trim(),
        role,
      },
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
        className="h-7 w-32"
        value={repo}
        list="worktree-repos"
        placeholder={t(($) => $.field_repo)}
        onChange={(e) => setRepo(e.target.value)}
      />
      <datalist id="worktree-repos">
        {knownRepos.map((r) => (
          <option key={r} value={r} />
        ))}
      </datalist>
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
  const [showSettled, setShowSettled] = useState(false);

  const all = issues
    .map(declarationRow)
    .filter((r): r is DeclarationRow => r !== null)
    .sort((a, b) => a.identifier.localeCompare(b.identifier));

  // This list exists to be worked through — each row is a card whose branch
  // nobody filed under a tree. A card that is already done or cancelled is not
  // work, and there are years of them: leaving them in buries the few rows that
  // still need a decision.
  const settled = all.filter(
    (r) => r.status === "done" || r.status === "cancelled",
  );
  const rows = showSettled ? all : all.filter((r) => !settled.includes(r));

  if (all.length === 0) return null;

  return (
    <section className="mt-6">
      <h3 className="flex items-center gap-1.5 text-body font-medium">
        <GitBranch className="size-3.5 text-muted-foreground" />
        {t(($) => $.declarations_title)}
        <span className="text-caption text-muted-foreground tabular-nums">
          {rows.length}
        </span>
        {settled.length > 0 && (
          <button
            type="button"
            className="text-caption font-normal text-muted-foreground hover:text-foreground"
            onClick={() => setShowSettled((v) => !v)}
          >
            {showSettled
              ? t(($) => $.declarations_hide_settled)
              : t(($) => $.declarations_show_settled, { count: settled.length })}
          </button>
        )}
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
