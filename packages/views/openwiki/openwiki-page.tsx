"use client";

import { BookMarked, BookOpen, GitBranch, ScrollText, Sparkles, Webhook } from "lucide-react";
import { DocsPage } from "@multica/views/docs";
import { SkillsPage } from "@multica/views/skills";
import { WorktreeLedger } from "./worktree-ledger";
import { HooksTab } from "../settings/components/hooks-tab";
import { InstructionsPage } from "./instructions-page";
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
/** Everything filed under it belongs to the Agent wiki; everything else to
 *  the Multica wiki. One prefix, read from both sides. */
const AGENT_WIKI_PREFIX = "AgentWiki/";

export type OpenwikiTab =
  | "wiki"
  | "skills"
  | "instructions"
  | "worktree"
  | "agentwiki"
  | "hooks";

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
      key: "instructions",
      label: t(($) => $.tab_instructions),
      icon: ScrollText,
      href: paths.workspaceInstructions(),
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
    {
      key: "hooks",
      label: t(($) => $.tab_hooks),
      icon: Webhook,
      href: paths.workspaceHooks(),
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
      {/* Both wikis and skills scroll their own lists — wrapping them would
        nest a second scrollbar inside the first. The ledger is a plain page
        and needs the container.

        The two wikis are one page with opposite filters, so the Agent wiki
        gets the same directory, search and empty states rather than a second,
        thinner implementation of them. */}
      {tab === "wiki" && (
        <DocsPage hideKinds={(kind) => kind.startsWith(AGENT_WIKI_PREFIX)} />
      )}
      {tab === "agentwiki" && (
        <DocsPage
          hideKinds={(kind) => !kind.startsWith(AGENT_WIKI_PREFIX)}
          title={t(($) => $.tab_agentwiki)}
          newKindPrefix={AGENT_WIKI_PREFIX}
        />
      )}
      {tab === "skills" && <SkillsPage />}
      {/* Its own scroll: the editor fills the pane and must not add a second
        scrollbar inside the shell's. */}
      {tab === "instructions" && <InstructionsPage />}
      {tab === "worktree" && (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <WorktreeLedger />
        </div>
      )}
      {/* Same container as the ledger: a plain page that scrolls inside the
        shell rather than bringing its own. */}
      {tab === "hooks" && (
        <div className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
          <HooksTab />
        </div>
      )}
    </div>
  );
}
