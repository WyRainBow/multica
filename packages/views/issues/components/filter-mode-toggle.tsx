"use client";

import type { IssueFilterCategory, IssueFilterMode } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * The "is / is not" switch at the top of a filter submenu.
 *
 * Lives inside the category it governs rather than as a separate "exclude"
 * menu: one concept, one place. A parallel menu would let a user build
 * "status is todo AND status is not todo", which then needs rules nobody can
 * see to resolve.
 *
 * Flipping the mode deliberately does NOT clear the selection — picking
 * backlog and then realising you meant "everything but backlog" is the common
 * path, and losing the tick would make the switch feel like a reset.
 */
export function FilterModeToggle({
  category,
  mode,
  onChange,
}: {
  category: IssueFilterCategory;
  mode: IssueFilterMode;
  onChange: (category: IssueFilterCategory, mode: IssueFilterMode) => void;
}) {
  const { t } = useT("issues");

  return (
    <div
      className="flex items-center gap-1 px-2 py-1.5"
      // The menu treats arrow keys as item navigation; this row is a control,
      // not an item, so it opts out of that roving focus.
      role="group"
      aria-label={t(($) => $.filters.mode_label)}
    >
      {(["include", "exclude"] as const).map((value) => (
        <button
          key={value}
          type="button"
          onClick={(event) => {
            // Selecting a mode must not close the menu — the user's next act
            // is almost always ticking what to exclude.
            event.preventDefault();
            event.stopPropagation();
            onChange(category, value);
          }}
          aria-pressed={mode === value}
          className={cn(
            "flex-1 rounded px-2 py-0.5 text-caption transition-colors",
            mode === value
              ? "bg-muted font-medium text-foreground"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {value === "include"
            ? t(($) => $.filters.mode_include)
            : t(($) => $.filters.mode_exclude)}
        </button>
      ))}
    </div>
  );
}
