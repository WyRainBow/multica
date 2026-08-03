"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
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
 * A table of contents for the issue description, in the gutter beside it.
 *
 * Reads the live document rather than the saved Markdown: headings appear as
 * they are typed. Renders nothing at all below two headings — an outline with
 * one entry is a label, not navigation, and it would take gutter width from
 * every short issue in the workspace to say nothing.
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
  const [activeId, setActiveId] = useState<string | null>(null);
  const listRef = useRef<HTMLElement | null>(null);
  const depths = useMemo(() => outlineDepths(headings), [headings]);

  const recomputeActive = useCallback(() => {
    const container = scrollContainer;
    if (!container || headings.length === 0) return;
    // Offsets are measured from the rendered headings, not from ProseMirror
    // positions: a document position says nothing about where a heading ended
    // up on screen once images, code blocks and wrapping have had their say.
    const offsets = new Map<string, number>();
    for (const heading of headings) {
      const element = container.querySelector<HTMLElement>(
        `[data-outline-pos="${heading.pos}"]`,
      );
      if (element) {
        offsets.set(
          heading.id,
          element.getBoundingClientRect().top - container.getBoundingClientRect().top,
        );
      }
    }
    setActiveId(
      activeOutlineId(headings, offsets, container.clientHeight * READING_LINE_RATIO),
    );
  }, [headings, scrollContainer]);

  useEffect(() => {
    const container = scrollContainer;
    if (!container) return;
    recomputeActive();
    container.addEventListener("scroll", recomputeActive, { passive: true });
    return () => container.removeEventListener("scroll", recomputeActive);
  }, [scrollContainer, recomputeActive]);

  // One entry is a label, not navigation.
  if (headings.length < 2) return null;

  return (
    <nav ref={listRef} className={cn("flex flex-col gap-0.5", className)}>
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
  );
}
