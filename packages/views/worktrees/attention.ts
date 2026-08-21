import type { Issue, Worktree } from "@multica/core/types";

/**
 * What is wrong right now, computed rather than read.
 *
 * A ledger of twenty checkouts is scanned, not read, and the states worth
 * acting on are the ones no column shows on its own: a tree measured before
 * the working copy moved, a branch that landed while its cards stayed open, a
 * checkout nobody has claimed. Each of these is a join or a comparison, which
 * is why a table of rows never surfaced them.
 *
 * Ordered by how much it costs to leave alone, not by severity in the
 * abstract: work that is already lost outranks work that is merely unrecorded.
 */
export type AttentionKind =
  | "blocked"
  | "uncommitted"
  | "never_measured"
  | "merged_open_cards"
  | "unclaimed";

export interface AttentionItem {
  kind: AttentionKind;
  tree: Worktree;
  /** Cards this item is about, when it is about cards. */
  issues: Issue[];
}

const ORDER: AttentionKind[] = [
  "blocked",
  "uncommitted",
  "merged_open_cards",
  "never_measured",
  "unclaimed",
];

const OPEN_CARD = (issue: Issue) =>
  issue.status !== "done" && issue.status !== "cancelled";

export function attentionItems(
  trees: Worktree[],
  issuesByTree: Map<string, Issue[]>,
): AttentionItem[] {
  const items: AttentionItem[] = [];

  for (const tree of trees) {
    if (tree.status === "archived") continue;
    const issues = issuesByTree.get(tree.name) ?? [];

    if (tree.status === "blocked") {
      items.push({ kind: "blocked", tree, issues: [] });
    }
    // Uncommitted work is the only state here that can actually be lost, so it
    // outranks everything except a tree someone has already flagged as stuck.
    if (tree.dirty) {
      items.push({ kind: "uncommitted", tree, issues: [] });
    }
    // The branch landed and the cards did not close. This is the loop the
    // ledger exists to close, and neither side can see it alone: the tree does
    // not know the card's status, and the card does not know the branch merged.
    if (tree.status === "merged") {
      const open = issues.filter(OPEN_CARD);
      if (open.length > 0) {
        items.push({ kind: "merged_open_cards", tree, issues: open });
      }
    }
    // Never measured means every fact on the row is somebody's claim.
    if (tree.verified_at === null) {
      items.push({ kind: "never_measured", tree, issues: [] });
    }
    if (tree.session.agent === "" && tree.status === "active") {
      items.push({ kind: "unclaimed", tree, issues: [] });
    }
  }

  return items.sort(
    (a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind),
  );
}
