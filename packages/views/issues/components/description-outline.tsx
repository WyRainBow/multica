"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { PanelLeftClose, List } from "lucide-react";
import { useDescriptionOutlineStore } from "@multica/core/issues/stores";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";
import {
  activeOutlineId,
  outlineDepths,
  type OutlineHeading,
} from "../../editor/outline";

/**
 * How far down the viewport counts as "what the reader is looking at".
 * A third of the way down rather than the very top: the heading you just
 * scrolled past sits above that line, so the outline advances when the section
 * genuinely fills the screen instead of the instant its title clears the edge.
 */
const READING_LINE_RATIO = 1 / 3;

/**
 * A table of contents for the issue description.
 *
 * Reads the live document rather than the saved Markdown, so headings appear
 * as they are typed. Renders nothing below two headings — an outline with one
 * entry is a label, not navigation, and it would take gutter width from every
 * short issue in the workspace to say nothing.
 *
 * Must be mounted OUTSIDE the scroll container. Anything inside it scrolls
 * away with the prose, which is precisely what an outline must not do.
 */
export function DescriptionOutline({
  headings,
  scrollContainer,
  onJump,
  className,
}: {
  headings: readonly OutlineHeading[];
  /** The element the description scrolls inside; drives the active section. */
  scrollContainer: HTMLElement | null;
  onJump: (heading: OutlineHeading) => void;
  className?: string;
}) {
  const { t } = useT("issues");
  const [activeId, setActiveId] = useState<string | null>(null);
  const collapsed = useDescriptionOutlineStore((s) => s.collapsed);
  const toggleCollapsed = useDescriptionOutlineStore((s) => s.toggleCollapsed);
  const depths = useMemo(() => outlineDepths(headings), [headings]);

  const recomputeActive = useCallback(() => {
    const container = scrollContainer;
    if (!container || headings.length === 0) return;
    // Offsets are measured from the rendered headings, not from ProseMirror
    // positions: a document position says nothing about where a heading ended
    // up on screen once images, code blocks and wrapping have had their say.
    const offsets = new Map<string, number>();
    const containerTop = container.getBoundingClientRect().top;
    for (const heading of headings) {
      const element = container.querySelector<HTMLElement>(
        `[data-outline-pos="${heading.pos}"]`,
      );
      if (element) {
        offsets.set(heading.id, element.getBoundingClientRect().top - containerTop);
      }
    }
    setActiveId(
      activeOutlineId(headings, offsets, container.clientHeight * READING_LINE_RATIO),
    );
  }, [headings, scrollContainer]);

  useEffect(() => {
    const container = scrollContainer;
    // Skipped while collapsed: measuring every heading on every scroll frame
    // to drive a highlight nobody can see is pure cost.
    if (!container || collapsed) return;
    recomputeActive();
    container.addEventListener("scroll", recomputeActive, { passive: true });
    return () => container.removeEventListener("scroll", recomputeActive);
  }, [scrollContainer, recomputeActive, collapsed]);

  // One entry is a label, not navigation.
  if (headings.length < 2) return null;

  return (
    <TooltipProvider delay={300}>
      <div className={cn("flex flex-col items-start gap-1", className)}>
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={toggleCollapsed}
                aria-expanded={!collapsed}
                aria-label={
                  collapsed
                    ? t(($) => $.outline.expand)
                    : t(($) => $.outline.collapse)
                }
                className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              />
            }
          >
            {collapsed ? (
              <List className="size-3.5" />
            ) : (
              <PanelLeftClose className="size-3.5" />
            )}
          </TooltipTrigger>
          <TooltipContent side="right" sideOffset={6}>
            {collapsed ? t(($) => $.outline.expand) : t(($) => $.outline.collapse)}
          </TooltipContent>
        </Tooltip>

        {/* Collapsed leaves only the toggle. The headings are not hidden with
            CSS but removed, so a long outline stops costing a scroll listener
            and a measure pass per frame. */}
        {!collapsed && (
          <nav className="flex min-h-0 w-full flex-col gap-0.5 overflow-y-auto">
            {headings.map((heading, index) => {
              const active = heading.id === activeId;
              return (
                <button
                  key={heading.id}
                  type="button"
                  onClick={() => onJump(heading)}
                  style={{ paddingLeft: (depths[index] ?? 0) * 10 }}
                  className={cn(
                    "truncate rounded py-0.5 pr-1 text-left text-caption transition-colors",
                    active
                      ? "font-medium text-foreground"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                  title={heading.text}
                >
                  {heading.text}
                </button>
              );
            })}
          </nav>
        )}
      </div>
    </TooltipProvider>
  );
}
