"use client";

import { useState } from "react";
import { Plus, Check, Circle, Loader2 } from "lucide-react";
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
 * a third source of truth that can disagree with the times it was supposed to
 * summarize.
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
 * Horizontal and compact because it is a header, not the content: it says
 * where the work is and lets you jump the state forward, while what happened
 * at each station lives further down the page under that station's heading.
 *
 * Clicking a station is a transition, not navigation — pending enters it,
 * current completes it. That is the whole interaction; there is no separate
 * edit mode for something with two timestamps.
 */
export function PhaseTrack({
  issueId,
  phases,
  className,
}: {
  issueId: string;
  phases: IssuePhase[];
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
        // Stays open: stations are added in a run — 开始, 已冻结, 实施中 —
        // and closing after each one would mean four extra clicks.
        setAdding(true);
      },
      onError: fail,
    });
  };

  const advance = (phase: IssuePhase) => {
    const state = phaseState(phase);
    if (state === "done") return;
    const mutation = state === "pending" ? enterPhase : completePhase;
    mutation.mutate(phase.id, { onError: fail });
  };

  // Nothing to show until the route exists, but the way to create one has to
  // be reachable — otherwise the feature is invisible on every issue that
  // does not already use it.
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
    <div className={cn("flex flex-wrap items-center gap-1", className)}>
      {phases.map((phase, index) => (
        <div key={phase.id} className="flex items-center gap-1">
          {index > 0 && <span aria-hidden className="w-4 border-t" />}
          <PhaseChip
            phase={phase}
            disabled={pending}
            onClick={() => advance(phase)}
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
  );
}

function PhaseChip({
  phase,
  disabled,
  onClick,
}: {
  phase: IssuePhase;
  disabled: boolean;
  onClick: () => void;
}) {
  const { t } = useT("issues");
  const state = phaseState(phase);

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled || state === "done"}
      title={
        state === "pending"
          ? t(($) => $.phases.enter_hint)
          : state === "current"
            ? t(($) => $.phases.complete_hint)
            : undefined
      }
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-caption transition-colors",
        state === "current" &&
          "border-primary bg-primary/10 font-medium text-foreground",
        state === "done" && "border-transparent bg-muted text-muted-foreground",
        state === "pending" &&
          "text-muted-foreground hover:border-foreground/30 hover:text-foreground",
      )}
    >
      {state === "done" ? (
        <Check className="size-3 shrink-0" />
      ) : state === "current" ? (
        <Loader2 className="size-3 shrink-0 animate-spin" />
      ) : (
        <Circle className="size-3 shrink-0" />
      )}
      <span className="truncate">{phase.name}</span>
      {/* The count is the point of the whole feature — it says where the
          discussion actually happened. Hidden at zero so an empty route is
          not a row of noughts. */}
      {phase.comment_count > 0 && (
        <span className="shrink-0 tabular-nums opacity-60">
          {phase.comment_count}
        </span>
      )}
    </button>
  );
}
