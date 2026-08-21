"use client";

import { useState } from "react";
import { BookMarked, BookOpen, GitBranch, Sparkles } from "lucide-react";
import { DocsPage } from "@multica/views/docs";
import { AgentWikiOverview } from "./agentwiki-overview";
import { SkillsPage } from "@multica/views/skills";
import { WorktreeLedger } from "./worktree-ledger";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

/**
 * openwiki — the workspace's total asset surface (COC-295). Three views of
 * what the workspace knows: the docs (issue artifacts, guides, cases), the
 * skills agents are mounted with, and the worktree ledger (where the code
 * is: which checkout carries which branch, and what happened in it).
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
