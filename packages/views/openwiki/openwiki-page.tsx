"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BookMarked, BookOpen, GitBranch, Sparkles } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueListOptions } from "@multica/core/issues/queries";
import type { Issue } from "@multica/core/types";
import { DocsPage } from "@multica/views/docs";
import { AgentWikiOverview } from "./agentwiki-overview";
import { SkillsPage } from "@multica/views/skills";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

/**
 * openwiki — the workspace's total asset surface (COC-295). Three views of
 * what the workspace knows: the docs (issue artifacts, guides, cases), the
 * skills agents are mounted with, and the worktree ledger (which branch
 * every claimed delivery lives on). A changelog view, not a tool: every
 * tab reads; writing happens where the data already lives.
 */
type OpenwikiTab = "docs" | "skills" | "worktree" | "agentwiki";

export function OpenwikiPage() {
  const [tab, setTab] = useState<OpenwikiTab>("docs");
  const { t } = useT("openwiki");

  const tabs: { key: OpenwikiTab; label: string; icon: typeof BookOpen }[] = [
    { key: "docs", label: t(($) => $.tab_docs), icon: BookOpen },
    { key: "skills", label: t(($) => $.tab_skills), icon: Sparkles },
    { key: "worktree", label: t(($) => $.tab_worktree), icon: GitBranch },
    { key: "agentwiki", label: t(($) => $.tab_agentwiki), icon: BookMarked },
  ];

  return (
    <div>
      <div className="flex items-center gap-1 border-b px-4 pt-3">
        {tabs.map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            className={cn(
              "flex items-center gap-1.5 rounded-t px-3 py-1.5 text-sm transition-colors",
              tab === key
                ? "bg-muted font-medium text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <Icon className="size-3.5" />
            {label}
          </button>
        ))}
      </div>
      {tab === "docs" && (
        <DocsPage hideKinds={(kind) => kind.startsWith("AgentWiki/")} />
      )}
      {tab === "skills" && <SkillsPage />}
      {tab === "worktree" && <WorktreeLedger />}
      {tab === "agentwiki" && <AgentWikiOverview />}
    </div>
  );
}

// The git.*/legacy delivery declarations an issue carries, in the shape the
// ledger needs. Mirrors scripts/issue_deliveries.py's key mapping so the CLI
// and this view never disagree about what counts as a claimed delivery.
interface DeliveryRow {
  issueId: string;
  identifier: string;
  title: string;
  status: string;
  baseRef: string;
  deliveryRef: string;
  mrUrl: string;
  deprecatedKeys: boolean;
  /** git.branch_role — base | feature | integration | launch. Defaults to
   *  feature: the common case is a card delivering one feature branch. */
  role: string;
}

function deliveryRow(issue: Issue): DeliveryRow | null {
  const meta = (issue.metadata ?? {}) as Record<string, unknown>;
  const str = (k: string) =>
    typeof meta[k] === "string" ? (meta[k] as string) : "";
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
    deprecatedKeys: !str("git.base_ref") && !!str("baseline_ref"),
    role: str("git.branch_role") || "feature",
  };
}

// Branch roles render in pipeline order: what everything is based on
// (base) → the work (feature) → the batch carriers (integration, launch).
const BRANCH_ROLES = ["base", "feature", "integration", "launch"] as const;
const roleOrder = (role: string) => {
  const i = BRANCH_ROLES.indexOf(role as (typeof BRANCH_ROLES)[number]);
  return i === -1 ? BRANCH_ROLES.length : i;
};
const roleUnknown = (role: string) =>
  !BRANCH_ROLES.includes(role as (typeof BRANCH_ROLES)[number]);

function WorktreeLedger() {
  const wsId = useWorkspaceId();
  const { t } = useT("openwiki");
  const nav = useNavigation();
  const paths = useWorkspacePaths();
  const { data: issues = [], isLoading } = useQuery(issueListOptions(wsId));

  const rows = issues
    .map(deliveryRow)
    .filter((r): r is DeliveryRow => r !== null)
    .sort((a, b) =>
      roleOrder(a.role) - roleOrder(b.role) ||
      a.identifier.localeCompare(b.identifier),
    );

  return (
    <div className="p-4">
      <p className="mb-3 text-caption text-muted-foreground">
        {t(($) => $.worktree_hint)}
      </p>
      {isLoading ? (
        <p className="text-sm text-muted-foreground">
          {t(($) => $.worktree_loading)}
        </p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t(($) => $.worktree_empty)}
        </p>
      ) : (
        <table className="w-full text-sm">
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
                    <span className="text-muted-foreground">{r.title}</span>
                  </button>
                </td>
                <td className="py-1.5 pr-3 text-muted-foreground">{r.status}</td>
                <td className="py-1.5 pr-3">
                  <span className="rounded bg-muted px-1.5 py-0.5 text-caption">
                    {roleUnknown(r.role)
                      ? t(($) => $.role_unknown, { role: r.role })
                      : t(($) => $[`role_${r.role}` as "role_base"])}
                  </span>
                </td>
                <td className="py-1.5 pr-3 font-mono text-caption">{r.baseRef || "—"}</td>
                <td className="py-1.5 pr-3 font-mono text-caption">
                  {r.deliveryRef || "—"}
                  {r.deprecatedKeys && (
                    <span className="ml-1 text-muted-foreground">
                      ({t(($) => $.deprecated_keys)})
                    </span>
                  )}
                </td>
                <td className="py-1.5">
                  {r.mrUrl ? (
                    <a
                      href={r.mrUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="hover:underline"
                    >
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
      )}
    </div>
  );
}
