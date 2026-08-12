"use client";

import { useState } from "react";
import { ChevronRight, Folder, Layers } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { DocTreeNode } from "../doc-tree";

/**
 * The folder tree, down the left of the documents list.
 *
 * Replaced a horizontal tab strip. Tabs could only ever show one level, which
 * is why the folders these documents came from arrived flattened — a path like
 * 工作流架构演进/04-验证与交付 had nowhere to go and became a label.
 *
 * Selecting a folder shows it AND everything under it; see filterDocsByPath.
 */
export function DocTreeNav({
  tree,
  total,
  selected,
  onSelect,
  className,
}: {
  tree: readonly DocTreeNode[];
  /** Every document, for the "all" row — including the uncategorised. */
  total: number;
  selected: string;
  onSelect: (path: string) => void;
  className?: string;
}) {
  const { t } = useT("docs");

  return (
    <nav className={cn("flex flex-col gap-0.5", className)}>
      <Row
        label={t(($) => $.page.all_kinds)}
        count={total}
        depth={0}
        active={selected === ""}
        onSelect={() => onSelect("")}
        icon={<Layers className="size-3.5 shrink-0 text-muted-foreground" />}
      />
      {tree.map((node) => (
        <TreeBranch
          key={node.path}
          node={node}
          depth={0}
          selected={selected}
          onSelect={onSelect}
        />
      ))}
    </nav>
  );
}

function TreeBranch({
  node,
  depth,
  selected,
  onSelect,
}: {
  node: DocTreeNode;
  depth: number;
  selected: string;
  onSelect: (path: string) => void;
}) {
  // Open when the selection is inside this branch, so arriving at a nested
  // folder does not land on a collapsed tree that hides where you are.
  const [open, setOpen] = useState(
    () => selected === node.path || selected.startsWith(`${node.path}/`),
  );
  const hasChildren = node.children.length > 0;

  return (
    <>
      <Row
        label={node.name}
        count={node.count}
        depth={depth}
        active={selected === node.path}
        onSelect={() => onSelect(node.path)}
        icon={<Folder className="size-3.5 shrink-0 text-muted-foreground" />}
        // The twisty is its own control: expanding a folder and filtering to it
        // are different intentions, and one click doing both means you cannot
        // look inside without also narrowing the list.
        twisty={
          hasChildren ? (
            <button
              type="button"
              aria-label={node.name}
              aria-expanded={open}
              onClick={(e) => {
                e.stopPropagation();
                setOpen((v) => !v);
              }}
              className="grid size-4 shrink-0 place-items-center rounded text-muted-foreground hover:bg-muted"
            >
              <ChevronRight
                className={cn("size-3 transition-transform", open && "rotate-90")}
              />
            </button>
          ) : (
            <span className="size-4 shrink-0" />
          )
        }
      />
      {open &&
        node.children.map((child) => (
          <TreeBranch
            key={child.path}
            node={child}
            depth={depth + 1}
            selected={selected}
            onSelect={onSelect}
          />
        ))}
    </>
  );
}

function Row({
  label,
  count,
  depth,
  active,
  onSelect,
  icon,
  twisty,
}: {
  label: string;
  count: number;
  depth: number;
  active: boolean;
  onSelect: () => void;
  icon: React.ReactNode;
  twisty?: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={active}
      style={{ paddingLeft: `${depth * 12 + 4}px` }}
      className={cn(
        "flex w-full items-center gap-1.5 rounded-md py-1 pr-2 text-left text-body transition-colors",
        // Weight and colour carry the selection — dimensions hover does not
        // touch — so hovering the selected folder cannot visually downgrade it.
        active
          ? "bg-muted font-medium text-foreground"
          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
      )}
    >
      {twisty}
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="shrink-0 text-caption tabular-nums text-faint-foreground">
        {count}
      </span>
    </button>
  );
}
