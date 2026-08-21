"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, BookOpen, Sparkles } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueListOptions } from "@multica/core/issues/queries";
import type { Issue } from "@multica/core/types";
import { DocsPage } from "@multica/views/docs";
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
type OpenwikiTab = "docs" | "skills" | "worktree";

export function OpenwikiPage() {
  const [tab, setTab] = useState<OpenwikiTab>("docs");
  const { t } = useT("openwiki");

  const tabs: { key: OpenwikiTab; label: string; icon: typeof BookOpen }[] = [
    { key: "docs", label: t(($) => $.tab_docs), icon: BookOpen },
    { key: "skills", label: t(($) => $.tab_skills), icon: Sparkles },
    { key: "worktree", label: t(($) => $.tab_worktree), icon: GitBranch },
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
      {tab === "docs" && <DocsPage />}
      {tab === "skills" && <SkillsPage />}
      {tab === "worktree" && <WorktreeLedger />}
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
  };
}

function WorktreeLedger() {
  const wsId = useWorkspaceId();
  const { t } = useT("openwiki");
  const nav = useNavigation();
  const paths = useWorkspacePaths();
  const { data: issues = [], isLoading } = useQuery(issueListOptions(wsId));

  const rows = issues
    .map(deliveryRow)
    .filter((r): r is DeliveryRow => r !== null)
    .sort((a, b) => a.identifier.localeCompare(b.identifier));

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
