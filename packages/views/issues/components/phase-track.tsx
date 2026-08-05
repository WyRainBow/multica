"use client";

import { useState } from "react";
import { Plus, Check, Circle, CircleDot, X } from "lucide-react";
import type { IssuePhase } from "@multica/core/types";
import {
  useCreateIssuePhase,
  useEnterIssuePhase,
  useCompleteIssuePhase,
} from "@multica/core/issues/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useT } from "../../i18n";

/**
 * Which of the three shapes a station is currently in.
 *
 * Derived from the two timestamps rather than stored: a stored state would be
 * a third source of truth able to disagree with the times it summarizes.
 */
export type PhaseState = "pending" | "current" | "done";

export function phaseState(phase: IssuePhase): PhaseState {
  if (phase.completed_at) return "done";
  if (phase.entered_at) return "current";
  return "pending";
}

/**
 * The route a requirement takes, as a row of stations.
 *
 * Clicking a station SELECTS it, which filters the timeline below to what was
 * said there. Reading is the primary act — the whole feature exists because a
 * flat comment list cannot answer "what happened while it was frozen" — so
 * reading gets the plain click, and advancing the work gets an explicit button
 * that only appears once a station is selected.
 *
 * An earlier version had the click advance the state. That put a write behind
 * the most casual gesture on the page and left no way at all to read one
 * station's discussion, which was the point.
 */
export function PhaseTrack({
  issueId,
  phases,
  selectedPhaseId,
  onSelect,
  className,
}: {
  issueId: string;
  phases: IssuePhase[];
  /** Null when the timeline is showing everything. */
  selectedPhaseId: string | null;
  onSelect: (phaseId: string | null) => void;
  className?: string;
}) {
  const { t } = useT("issues");
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");

  const createPhase = useCreateIssuePhase(issueId);
  const enterPhase = useEnterIssuePhase(issueId);
  const completePhase = useCompleteIssuePhase(issueId);
  const pending =
    createPhase.isPending || enterPhase.isPending || completePhase.isPending;

  const fail = (err: unknown) =>
    toast.error(
      err instanceof Error && err.message
        ? err.message
        : t(($) => $.detail.update_failed),
    );

  const submitNew = () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setAdding(false);
      return;
    }
    createPhase.mutate(trimmed, {
      onSuccess: () => {
        setName("");
        // Stays open: stations are added in a run — 开始, 已冻结, 实施中 — and
        // closing after each one would mean reopening it four times.
        setAdding(true);
      },
      onError: fail,
    });
  };

  const selected = phases.find((phase) => phase.id === selectedPhaseId) ?? null;

  if (phases.length === 0 && !adding) {
    return (
      <div className={className}>
        <Button
          size="sm"
          variant="ghost"
          className="text-muted-foreground"
          onClick={() => setAdding(true)}
        >
          <Plus className="size-3.5" />
          {t(($) => $.phases.add_first)}
        </Button>
      </div>
    );
  }

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="flex flex-wrap items-center gap-1">
        {phases.map((phase, index) => (
          <div key={phase.id} className="flex items-center gap-1">
            {index > 0 && <span aria-hidden className="w-4 border-t" />}
            <PhaseChip
              phase={phase}
              selected={phase.id === selectedPhaseId}
              // Clicking the selected one clears the filter, so the way back
              // to "everything" is the same gesture that got you here.
              onClick={() =>
                onSelect(phase.id === selectedPhaseId ? null : phase.id)
              }
            />
          </div>
        ))}

        {adding ? (
          <Input
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
            onBlur={submitNew}
            onKeyDown={(event) => {
              if (event.key === "Enter") submitNew();
              if (event.key === "Escape") {
                setName("");
                setAdding(false);
              }
            }}
            placeholder={t(($) => $.phases.name_placeholder)}
            className="h-7 w-32 text-caption"
          />
        ) : (
          <Button
            size="icon-xs"
            variant="ghost"
            className="ml-1 text-muted-foreground"
            onClick={() => setAdding(true)}
            aria-label={t(($) => $.phases.add)}
            disabled={pending}
          >
            <Plus className="size-3.5" />
          </Button>
        )}
      </div>

      {/* The selected station's own bar. Advancing lives here rather than on
          the chip because it is a write, and a write should not share a target
          with the gesture used to browse. */}
      {selected && (
        <div className="flex flex-wrap items-center gap-2 rounded-md bg-muted/50 px-3 py-1.5 text-caption">
          <span className="text-muted-foreground">
            {t(($) => $.phases.viewing, { name: selected.name })}
          </span>
          <span className="text-muted-foreground tabular-nums">
            {t(($) => $.phases.comment_count, { count: selected.comment_count })}
          </span>

          <div className="ml-auto flex items-center gap-1">
            {phaseState(selected) === "pending" && (
              <Button
                size="sm"
                variant="ghost"
                disabled={pending}
                onClick={() =>
                  enterPhase.mutate(selected.id, { onError: fail })
                }
              >
                {t(($) => $.phases.enter_action)}
              </Button>
            )}
            {phaseState(selected) === "current" && (
              <Button
                size="sm"
                variant="ghost"
                disabled={pending}
                onClick={() =>
                  completePhase.mutate(selected.id, { onError: fail })
                }
              >
                {t(($) => $.phases.complete_action)}
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => onSelect(null)}>
              <X className="size-3.5" />
              {t(($) => $.phases.show_all)}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function PhaseChip({
  phase,
  selected,
  onClick,
}: {
  phase: IssuePhase;
  selected: boolean;
  onClick: () => void;
}) {
  const state = phaseState(phase);

  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={selected}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-caption transition-colors",
        // Selection is the loud state, because it is the one that changes what
        // the rest of the page is showing. Progress is carried by the icon.
        selected
          ? "border-primary bg-primary/10 font-medium text-foreground"
          : "text-muted-foreground hover:border-foreground/30 hover:text-foreground",
        !selected && state === "done" && "border-transparent bg-muted",
      )}
    >
      {state === "done" ? (
        <Check className="size-3 shrink-0" />
      ) : state === "current" ? (
        <CircleDot className="size-3 shrink-0 text-primary" />
      ) : (
        <Circle className="size-3 shrink-0" />
      )}
      <span className="truncate">{phase.name}</span>
      {/* The count is the point of the feature — it says where the discussion
          actually happened. Hidden at zero so an empty route is not a row of
          noughts. */}
      {phase.comment_count > 0 && (
        <span className="shrink-0 tabular-nums opacity-60">
          {phase.comment_count}
        </span>
      )}
    </button>
  );
}
