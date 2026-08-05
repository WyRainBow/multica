import type { IssuePhase } from "@multica/core/types";

/**
 * The name for another round of a station.
 *
 * Review happens more than once, and each round is its own container — that is
 * the one thing a phase does that `status` cannot. Status is a single value:
 * the second review sets it back to in_review and the first round stops
 * existing, so "what did round one ask for, and what changed in round two" has
 * no answer. A round per station keeps both.
 *
 * Rounds are separate stations rather than repeat visits to one. A visit log
 * would model it more precisely, but "评审 2" is a thing a person can read on
 * a chip, and the unique-name rule keeps working unchanged.
 *
 * The first round keeps the plain name; numbering starts at 2, so a route that
 * only ever reviews once never shows a "1" that implies a missing sequel.
 */
export function nextRoundName(
  phases: readonly IssuePhase[],
  base: string,
): string {
  const trimmed = base.trim();
  // Strip any round number already on the name, so asking for another round of
  // "评审 2" continues the sequence instead of starting "评审 2 2".
  const root = trimmed.replace(/\s+\d+$/, "");
  if (!root) return trimmed;

  let highest = 0;
  for (const phase of phases) {
    const name = phase.name.trim();
    if (name === root) {
      highest = Math.max(highest, 1);
      continue;
    }
    if (!name.startsWith(root)) continue;
    const suffix = name.slice(root.length).trim();
    if (!/^\d+$/.test(suffix)) continue;
    highest = Math.max(highest, Number(suffix));
  }
  return highest === 0 ? root : `${root} ${highest + 1}`;
}
