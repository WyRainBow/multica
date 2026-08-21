import type { Card } from "@multica/core/types";

/**
 * The document tree, derived from `kind` read as a PATH.
 *
 * `kind` was already free text producing a flat row of tabs. Reading a slash in
 * it as a level turns the same field into a folder tree without a new column, a
 * migration, or a second place to file something. `联调` stays a one-level
 * path; `工作流架构演进/04-验证与交付` is two.
 *
 * That last property is the point. A tree PLUS separate tags would put every
 * document in front of the question "which one does this go in", and that
 * question is the disease — the folders these documents came from
 * (cocoLocal/knowledge/本地联调/) were the categories, not a second axis
 * beside them.
 *
 * Folders are not rows in a table. A folder exists exactly as long as a
 * document names it, so renaming one is editing the documents that live there,
 * and an empty folder cannot linger — the same reason the tabs were derived
 * rather than configured.
 */
export interface DocTreeNode {
  /** Full path from the root: `工作流架构演进/04-验证与交付`. */
  path: string;
  /** Just this level's name: `04-验证与交付`. */
  name: string;
  /** Documents at this exact path, plus every one below it. */
  count: number;
  children: DocTreeNode[];
}

/** Splits a kind into path segments, dropping blanks from `a//b` or a stray `/`. */
export function kindSegments(kind: string): string[] {
  return kind
    .split("/")
    .map((s) => s.trim())
    .filter(Boolean);
}

/** Rejoins segments into the canonical stored form. */
export function joinKindSegments(segments: readonly string[]): string {
  return segments.join("/");
}

/**
 * Builds the tree.
 *
 * Counts are CUMULATIVE — a folder shows how much is under it, including
 * deeper levels. A folder that said 0 while holding a hundred documents two
 * levels down would read as empty, and the number is there to answer "is there
 * anything in here".
 *
 * Siblings are ordered most-used first, then by name so two with the same count
 * keep a stable order between renders.
 *
 * Uncategorised documents get no node. They are reachable by selecting nothing,
 * which shows everything; a blank folder label has nothing to render.
 */
export function buildDocTree(cards: readonly Card[]): DocTreeNode[] {
  const roots: DocTreeNode[] = [];
  const byPath = new Map<string, DocTreeNode>();

  for (const card of cards) {
    const segments = kindSegments(card.kind ?? "");
    if (segments.length === 0) continue;

    let parentChildren = roots;
    let prefix = "";
    for (const name of segments) {
      prefix = prefix ? `${prefix}/${name}` : name;
      let node = byPath.get(prefix);
      if (!node) {
        node = { path: prefix, name, count: 0, children: [] };
        byPath.set(prefix, node);
        parentChildren.push(node);
      }
      // Every level on the way down holds this document.
      node.count += 1;
      parentChildren = node.children;
    }
  }

  const sortLevel = (nodes: DocTreeNode[]) => {
    nodes.sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
    for (const node of nodes) sortLevel(node.children);
  };
  sortLevel(roots);
  return roots;
}

/**
 * The documents a selected folder shows: the ones filed exactly there AND
 * everything below it.
 *
 * Descendants included deliberately. Selecting `工作流架构演进` and being told
 * it is empty, while three of its subfolders hold documents, makes the parent
 * look broken — and a reader picking a folder is asking what is in that part of
 * the tree, not what is in one level of it.
 *
 * An empty `path` selects nothing and returns everything, uncategorised
 * included.
 */
export function filterDocsByPath(cards: readonly Card[], path: string): Card[] {
  const wanted = kindSegments(path);
  if (wanted.length === 0) return [...cards];
  const prefix = joinKindSegments(wanted);
  return cards.filter((card) => {
    const kind = joinKindSegments(kindSegments(card.kind ?? ""));
    // Guard the boundary: `本地联调` must not match `本地联调整理`.
    return kind === prefix || kind.startsWith(`${prefix}/`);
  });
}

/** Every folder path that exists, deepest-first-per-branch, for suggestions. */
export function allDocPaths(cards: readonly Card[]): string[] {
  const out: string[] = [];
  const walk = (nodes: readonly DocTreeNode[]) => {
    for (const node of nodes) {
      out.push(node.path);
      walk(node.children);
    }
  };
  walk(buildDocTree(cards));
  return out;
}

/** Documents sharing one first-level kind segment, for a flat grouped list. */
export interface DocKindRootGroup {
  /** The first path segment: `COC-199` for a card filed under
   *  `COC-199/rounds/R1`. */
  root: string;
  cards: Card[];
}

/**
 * One-level grouping by the first kind segment, for the issue's document
 * section.
 *
 * The full tree would be wrong here: an issue rarely holds more than a handful
 * of documents, and nesting a handful two levels deep hides more than it
 * organises. One heading per root keeps rounds of the same review visibly
 * together, which is all the grouping has to do.
 *
 * Uncategorised documents come back separately rather than under a made-up
 * root — same reason `buildDocTree` gives them no node: a blank heading has
 * nothing to render. Groups are ordered most-used first then by name, matching
 * the tree's sibling order; cards inside a group keep the order they arrived
 * in.
 */
export function groupDocsByKindRoot(cards: readonly Card[]): {
  unfiled: Card[];
  groups: DocKindRootGroup[];
} {
  const unfiled: Card[] = [];
  const byRoot = new Map<string, Card[]>();
  for (const card of cards) {
    const [root] = kindSegments(card.kind ?? "");
    if (!root) {
      unfiled.push(card);
      continue;
    }
    const group = byRoot.get(root);
    if (group) group.push(card);
    else byRoot.set(root, [card]);
  }
  const groups = [...byRoot.entries()]
    .map(([root, groupCards]) => ({ root, cards: groupCards }))
    .sort(
      (a, b) =>
        b.cards.length - a.cards.length || a.root.localeCompare(b.root),
    );
  return { unfiled, groups };
}

/**
 * How long a document is, in characters.
 *
 * Counts CODE POINTS, not UTF-16 units. `"🎉".length` is 2 in JavaScript and 1
 * everywhere a person counts — and 1 in the CLI, which counts runes. The number
 * is shown in three places in the app and a fourth in `doc list`; they have to
 * agree, or none of them can be quoted.
 *
 * CJK text is unaffected either way, which is why the divergence hides: the
 * 11674-character SOP counts identically under both.
 */
export function docLength(content: string): number {
  return [...content].length;
}
