"use client";

/**
 * MentionView — NodeView for rendering @mentions inline in the editor.
 *
 * Member/agent mentions: plain "@Name" text with .mention class styling.
 * Issue mentions: IssueChip inside a custom <a> that supports cmd/shift-click
 * to open in a new tab (AppLink doesn't expose that intent hook).
 *
 * Issue chip sizing: must fit within the paragraph line box (14px * 1.625 =
 * 22.75px). Card is text-caption (12px) + py-0.5 + border ≈ 22px total. The
 * `vertical-align: middle` rule on `[data-node-view-wrapper]` in CSS handles
 * line-box alignment; setting it on the inner <a> has no effect because the
 * wrapper is the outermost inline element.
 */

import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIssueLinkStore } from "@multica/core/issues/stores";
import { useNavigation } from "../../navigation";
import { IssueChip } from "../../issues/components/issue-chip";
import { ProjectChip } from "../../projects/components/project-chip";
import { useQuery } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { cardDetailOptions, cardListOptions } from "@multica/core/docs/queries";

export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;

  if (type === "issue") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <IssueMention issueId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === "project") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <ProjectMention projectId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === "doc") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <DocMention docId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="mention">@{label ?? id}</span>
    </NodeViewWrapper>
  );
}

function ProjectMention({
  projectId,
  fallbackLabel,
}: {
  projectId: string;
  fallbackLabel?: string;
}) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const projectPath = p.projectDetail(projectId);

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(projectPath, fallbackLabel);
      }
      // Web: no adapter — leave the event alone so the browser's native
      // modifier-click on the anchor opens the tab (or window for shift),
      // preserving background/foreground semantics window.open would flatten.
      return;
    }
    e.preventDefault();
    push(projectPath);
  };

  return (
    <a href={projectPath} onClick={handleClick} className="project-mention">
      <ProjectChip
        projectId={projectId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}

function IssueMention({
  issueId,
  fallbackLabel,
}: {
  issueId: string;
  fallbackLabel?: string;
}) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const newTabPreferred = useIssueLinkStore((s) => s.openInNewTab);
  const issuePath = p.issueDetail(issueId);

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(issuePath, fallbackLabel);
      }
      // Web: no adapter — native modifier-click on the anchor opens a tab.
      return;
    }
    if (newTabPreferred) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(issuePath, fallbackLabel, { activate: true });
      }
      // Web: native target="_blank" opens a browser tab.
      return;
    }
    e.preventDefault();
    push(issuePath);
  };

  return (
    <a
      href={issuePath}
      target={newTabPreferred ? "_blank" : undefined}
      rel={newTabPreferred ? "noopener noreferrer" : undefined}
      onClick={handleClick}
      className="issue-mention"
    >
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}

/**
 * A wiki page inside the editor: its title, and a real link.
 *
 * The document detail page IS an editor — a page is read there, not only
 * written — so a reference that cannot be followed there cannot be followed at
 * all. Same shape as the issue chip beside it: an anchor, so it is reachable by
 * Tab, opens in a new tab on modifier-click, and exposes a URL to copy.
 *
 * Resolved from the list first, since a page citing several others would
 * otherwise be one request per citation.
 */
function DocMention({
  docId,
  fallbackLabel,
}: {
  docId: string;
  fallbackLabel?: string;
}) {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: listResponse } = useQuery(cardListOptions(wsId));
  const listed = listResponse?.cards?.find((card) => card.id === docId);
  const { data: detail } = useQuery({
    ...cardDetailOptions(wsId, docId),
    enabled: Boolean(wsId) && !listed,
  });
  const doc = listed ?? detail;
  const docPath = p.docDetail(docId);

  return (
    <a
      href={docPath}
      onClick={(event) => {
        event.stopPropagation();
        // Let the browser handle a modifier-click into a new tab.
        if (event.metaKey || event.ctrlKey || event.shiftKey) return;
        event.preventDefault();
        push(docPath);
      }}
      className="doc-mention inline-flex items-baseline gap-1 rounded bg-muted px-1 align-baseline transition-colors hover:bg-accent"
      title={doc?.title ?? docId}
    >
      <FileText className="size-3 self-center text-muted-foreground" />
      {doc?.title ?? fallbackLabel ?? docId}
    </a>
  );
}
