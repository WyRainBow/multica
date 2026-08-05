"use client";

import { useState } from "react";
import { Plus, Check, Circle, CircleDot, Play, RotateCw } from "lucide-react";
import { nextRoundName } from "./phase-round";
import type { IssuePhase } from "@multica/core/types";
import {
  useCreateIssuePhase,
  useEnterIssuePhase,
  useCompleteIssuePhase,
} from "@multica/core/issues/mutations";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
 * The one ready-made route: what a single issue passes through.
 *
 * Three stations, and the same three whatever the issue is about — a
 * requirement, a design, an implementation all get written, reviewed, and
 * frozen. That sameness is what makes it a property of an issue rather than a
 * process for one kind of work, and properties are what belong in a tool.
 *
 * An earlier version listed nine: 需求 → 技术方案 → 方案评审 → Spec → Task →
 * Plan → 实施 → 测试验证 → 合并 MR. That is a chain of separate ISSUES, each
 * with its own owner and its own comments, not the life of one — the two were
 * conflated.
 *
 * These three do overlap with `status` (开始 ≈ in_progress, 评审 ≈ in_review,
 * 冻结 ≈ done), and on their own they would add nothing. What they add is
 * rounds: review happens more than once, status forgets every round but the
 * current one, and a station per round keeps what each asked for.
 */
const PHASE_TEMPLATES = [
  { key: "requirement", names: ["开始", "评审", "冻结"] },
] as const;

type TemplateKey = (typeof PHASE_TEMPLATES)[number]["key"];

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

  // Applying a template is several creates in a row, deliberately sequential:
  // position is derived from the current maximum, so firing them in parallel
  // would have every one read the same maximum and land on the same spot.
  const applyTemplate = async (key: TemplateKey) => {
    const template = PHASE_TEMPLATES.find((entry) => entry.key === key);
    if (!template) return;
    try {
      for (const phaseName of template.names) {
        await createPhase.mutateAsync(phaseName);
      }
    } catch (err) {
      // Partial application is left in place rather than rolled back: half a
      // route is still a route, and the stations already created are the ones
      // the person will keep.
      fail(err);
    }
  };

  const selected = phases.find((phase) => phase.id === selectedPhaseId) ?? null;

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

  const addMenu = (trigger: React.ReactElement) => (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align="start" className="w-64">
        {PHASE_TEMPLATES.map((template) => (
          <DropdownMenuItem
            key={template.key}
            onClick={() => void applyTemplate(template.key)}
            className="flex-col items-start gap-0.5"
          >
            <span className="font-medium">
              {t(($) => $.phases.templates[template.key])}
            </span>
            {/* The station names, so the choice is made on content rather than
                on a label nobody recognises. */}
            <span className="text-caption text-muted-foreground">
              {t(($) => $.phases.templates[`${template.key}_desc` as const])}
            </span>
          </DropdownMenuItem>
        ))}
        {/* Another round of whatever is being looked at. Placed here rather
            than on the chip because it is a third action, and a chip with
            three targets stops being readable. Hidden with nothing selected:
            "another round" has no meaning without a station to repeat. */}
        {selected && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() =>
                createPhase.mutate(nextRoundName(phases, selected.name), {
                  onError: fail,
                })
              }
            >
              <RotateCw className="size-3.5" />
              {t(($) => $.phases.another_round, {
                name: nextRoundName(phases, selected.name),
              })}
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => setAdding(true)}>
          <Plus className="size-3.5" />
          {t(($) => $.phases.templates.custom)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  if (phases.length === 0 && !adding) {
    return (
      <div className={className}>
        {addMenu(
          <Button size="sm" variant="ghost" className="text-muted-foreground">
            <Plus className="size-3.5" />
            {t(($) => $.phases.add_first)}
          </Button>,
        )}
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
              selected={phase.id === selectedPhaseId}
              busy={pending}
              // Clicking the selected one clears the filter, so the way back
              // to "everything" is the same gesture that got you here — no
              // separate "show all" control is needed, and one that existed
              // would only restate what the highlight already says.
              onClick={() =>
                onSelect(phase.id === selectedPhaseId ? null : phase.id)
              }
              onAdvance={() => {
                const mutation =
                  phaseState(phase) === "pending" ? enterPhase : completePhase;
                mutation.mutate(phase.id, { onError: fail });
              }}
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
        addMenu(
          <Button
            size="icon-xs"
            variant="ghost"
            className="ml-1 text-muted-foreground"
            aria-label={t(($) => $.phases.add)}
            disabled={pending}
          >
            <Plus className="size-3.5" />
          </Button>,
        )
      )}
    </div>
  );
}

/**
 * One station.
 *
 * A div wrapping two buttons rather than one button doing both jobs: the label
 * selects, and — only while selected — a trailing control advances the work.
 * Nesting a button inside a button is invalid markup, and one target doing
 * both would put a write behind the gesture used to browse.
 */
function PhaseChip({
  phase,
  selected,
  busy,
  onClick,
  onAdvance,
}: {
  phase: IssuePhase;
  selected: boolean;
  busy: boolean;
  onClick: () => void;
  onAdvance: () => void;
}) {
  const { t } = useT("issues");
  const state = phaseState(phase);

  return (
    <div
      className={cn(
        "inline-flex items-center rounded-full border text-caption transition-colors",
        // Selection is the loud state: it is the one that changes what the
        // rest of the page is showing. Progress is carried by the icon.
        selected
          ? "border-primary bg-primary/10 text-foreground"
          : "text-muted-foreground hover:border-foreground/30 hover:text-foreground",
        !selected && state === "done" && "border-transparent bg-muted",
      )}
    >
      <button
        type="button"
        onClick={onClick}
        aria-pressed={selected}
        className={cn(
          "inline-flex items-center gap-1.5 py-1 pl-3",
          selected ? "font-medium" : "",
          // The label keeps the full pill's padding until the action appears
          // beside it, so an unselected row does not shift when one is picked.
          selected && state !== "done" ? "pr-1.5" : "pr-3",
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

      {/* Advancing appears only on the station being looked at. A finished one
          has nowhere left to go, so it gets nothing. */}
      {selected && state !== "done" && (
        <button
          type="button"
          onClick={onAdvance}
          disabled={busy}
          title={
            state === "pending"
              ? t(($) => $.phases.enter_action)
              : t(($) => $.phases.complete_action)
          }
          aria-label={
            state === "pending"
              ? t(($) => $.phases.enter_action)
              : t(($) => $.phases.complete_action)
          }
          className="mr-1 rounded-full p-1 text-muted-foreground transition-colors hover:bg-primary/20 hover:text-foreground disabled:opacity-50"
        >
          {state === "pending" ? (
            <Play className="size-3" />
          ) : (
            <Check className="size-3" />
          )}
        </button>
      )}
    </div>
  );
}
