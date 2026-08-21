"use client";

import { BookMarked, BookOpen, GitBranch, Sparkles } from "lucide-react";
import { DocsPage } from "@multica/views/docs";
import { AgentWikiOverview } from "./agentwiki-overview";
import { SkillsPage } from "@multica/views/skills";
import { WorktreeLedger } from "./worktree-ledger";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

/**
 * The workspace's own assets: what it knows (wiki), what its agents are mounted
 * with (skills), where its code is (the worktree ledger), and the experience
 * distilled out of finished work (Agent Wiki).
 *
 * Each view has an address. They were tabs in component state, which meant a
 * view could not be linked to or shared, a reload dropped you back on the first
 * one, and the back button left the page entirely.
 */
export type OpenwikiTab = "wiki" | "skills" | "worktree" | "agentwiki";

export function OpenwikiPage({ tab = "wiki" }: { tab?: OpenwikiTab }) {
  const { t } = useT("openwiki");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const tabs: {
    key: OpenwikiTab;
    label: string;
    icon: typeof BookOpen;
    href: string;
  }[] = [
    {
      key: "wiki",
      label: t(($) => $.tab_docs),
      icon: BookOpen,
      href: paths.workspaceWiki(),
    },
    {
      key: "skills",
      label: t(($) => $.tab_skills),
      icon: Sparkles,
      href: paths.workspaceSkills(),
    },
    {
      key: "worktree",
      label: t(($) => $.tab_worktree),
      icon: GitBranch,
      href: paths.workspaceWorktree(),
    },
    {
      key: "agentwiki",
      label: t(($) => $.tab_agentwiki),
      icon: BookMarked,
      href: paths.workspaceAgentWiki(),
    },
  ];

  return (
    // The dashboard shell clips its own overflow, so every page owns its
    // scrolling. A plain div here left the taller tabs cut off at the fold with
    // no way to reach the rest.
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b px-4 pt-3">
        {tabs.map(({ key, label, icon: Icon, href }) => (
          <button
            key={key}
            type="button"
            onClick={() => nav.push(href)}
            className={cn(
              "flex items-center gap-1.5 rounded-t px-3 py-1.5 text-body transition-colors",
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
      {/* Docs and skills scroll their own lists — wrapping them would nest a
        second scrollbar inside the first. The other two are plain pages and
        need the container. */}
      {tab === "wiki" && (
        <DocsPage hideKinds={(kind) => kind.startsWith("AgentWiki/")} />
      )}
      {tab === "skills" && <SkillsPage />}
      {(tab === "worktree" || tab === "agentwiki") && (
        <div className="min-h-0 flex-1 overflow-y-auto">
          {tab === "worktree" ? <WorktreeLedger /> : <AgentWikiOverview />}
        </div>
      )}
    </div>
  );
}
