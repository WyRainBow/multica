import type { IssuePhase } from "@multica/core/types";

/**
 * Which station was current when something happened.
 *
 * Activities — status changes, description edits, assignments — carry no
 * phase of their own, and giving them one would mean writing a column that
 * duplicates information the phase timestamps already hold. They are placed by
 * time instead: an activity belongs to whatever station the work was standing
 * in when it occurred.
 *
 * This matters because "what happened while it was frozen" includes who
 * changed what, not only who said what. Dropping activities from a narrowed
 * timeline answers the question with half the evidence.
 *
 * Returns null when nothing was current — before the first station was
 * entered, or in a gap between a completion and the next arrival. Those
 * activities are real and stay visible in the unfiltered timeline; they simply
 * belong to no station.
 */
export function phaseAtTime(
  phases: readonly IssuePhase[],
  at: string,
): IssuePhase | null {
  const moment = new Date(at).getTime();
  if (Number.isNaN(moment)) return null;

  let best: IssuePhase | null = null;
  let bestEntered = -Infinity;

  for (const phase of phases) {
    if (!phase.entered_at) continue;
    const entered = new Date(phase.entered_at).getTime();
    if (Number.isNaN(entered) || entered > moment) continue;

    // An unfinished station is still current, so its window runs to now.
    const completed = phase.completed_at
      ? new Date(phase.completed_at).getTime()
      : Infinity;
    if (Number.isNaN(completed) || completed < moment) continue;

    // Windows can overlap — re-entering a station clears its completion while
    // a later one is already running. The most recently entered wins, because
    // that is where the work actually was.
    if (entered >= bestEntered) {
      best = phase;
      bestEntered = entered;
    }
  }
  return best;
}
