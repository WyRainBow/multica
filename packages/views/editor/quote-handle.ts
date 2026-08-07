/**
 * Turns a selection in an issue description into a one-line command that
 * returns just that passage.
 *
 * The handle quotes the passage's EDGES, not the passage. That is the whole
 * point: a 1000-character selection produces a handle of a few dozen
 * characters, short enough to paste into a chat, where quoting the passage
 * itself would be no better than pasting it.
 *
 * Everything here is measured against the stored MARKDOWN, not the rendered
 * text on screen, because Markdown is what `locateQuote` in
 * server/cmd/multica/cmd_issue_quote.go matches. Generating a handle against
 * the rendering and resolving it against the source is how a handle ends up
 * pointing at nothing: `## 工作目标` renders as `工作目标`, and a heading's
 * neighbours differ by two characters that only exist in one of the two.
 */

/** Characters taken from each end. Long enough to be distinctive in CJK prose,
 *  short enough that the handle stays readable in a chat message. */
const EDGE_LENGTH = 16;

/** Below this, the selection IS the start edge — exact, and unambiguous
 *  without needing an end at all. */
const WHOLE_SELECTION_LIMIT = 40;

/** An edge shorter than this is too weak to be worth emitting; the handle
 *  would match half the document. */
const MIN_EDGE_LENGTH = 4;

export interface QuoteHandleInput {
  /** Human-readable issue key (COC-45). A UUID works too, but nobody reading
   *  the pasted line would know which issue it names. */
  issueKey: string;
  /** The description as stored Markdown — the text the CLI will match. */
  markdown: string;
  /** Rendered text immediately before the selection, and immediately after.
   *  Used as tie-breakers when the edges alone are ambiguous. */
  before: string;
  after: string;
  /** First text node of the selection, and the last. Edges are taken from
   *  within a single node so they cannot straddle an inline mark: the editor
   *  renders `**done**` as `done`, and an edge spanning that boundary would
   *  not exist in the stored Markdown. */
  firstNodeText: string;
  lastNodeText: string;
  /** The whole selection, used to decide whether an end edge is needed. */
  selected: string;
}

function isSpace(ch: string): boolean {
  return ch === " " || ch === "\t" || ch === "\r" || ch === "\n";
}

/**
 * How many characters of `hay` the `needle` covers from `at`, or -1.
 *
 * A port of matchLenAt in cmd_issue_quote.go, including its loose whitespace:
 * a run of whitespace in the needle matches any run in the haystack. The two
 * must agree, or the button emits handles the CLI rejects.
 */
function matchLenAt(hay: string[], at: number, needle: string[]): number {
  let h = at;
  let n = 0;
  while (n < needle.length) {
    if (isSpace(needle[n]!)) {
      while (n < needle.length && isSpace(needle[n]!)) n++;
      if (h >= hay.length || !isSpace(hay[h]!)) return -1;
      while (h < hay.length && isSpace(hay[h]!)) h++;
      continue;
    }
    if (h >= hay.length || hay[h] !== needle[n]) return -1;
    h++;
    n++;
  }
  return h - at;
}

/**
 * Counts how many spans a start/end pair matches — the same question
 * `locateQuote` asks before refusing to guess.
 */
export function countQuoteSpans(markdown: string, start: string, end: string): number {
  const startText = start.trim();
  if (!startText) return 0;
  const endText = end.trim();

  const hay = Array.from(markdown);
  const startChars = Array.from(startText);
  const endChars = Array.from(endText);
  let count = 0;

  // Every landing place for the end counts as its own span, matching
  // locateQuote: an end of "。" has a candidate at the close of every sentence,
  // and taking the nearest silently would hand back a truncated passage that
  // reads exactly like a complete one.
  const endAt: number[] = [];
  if (endChars.length > 0) {
    for (let j = 0; j < hay.length; j++) {
      if (matchLenAt(hay, j, endChars) >= 0) endAt.push(j);
    }
  }

  for (let i = 0; i < hay.length; i++) {
    const startLen = matchLenAt(hay, i, startChars);
    if (startLen < 0) continue;
    if (endChars.length === 0) {
      count++;
      continue;
    }
    for (const j of endAt) {
      if (j >= i + startLen) count++;
    }
    if (count > 1) return count; // already ambiguous; the exact number is moot
  }
  return count;
}

/** Wraps a value for a POSIX shell. Single quotes so nothing inside is
 *  expanded — descriptions contain `$`, backticks and quotes often enough that
 *  double quotes would eventually mangle one. */
function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

/** Collapses newlines and runs of spaces: an edge is matched verbatim, and the
 *  CLI trims it, so a multi-line edge only makes the handle harder to read.
 *  Loose whitespace matching on both sides is what makes this safe. */
function edgeFlat(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

function clip(flat: string, side: "start" | "end"): string {
  if (flat.length <= EDGE_LENGTH) return flat;
  return side === "start" ? flat.slice(0, EDGE_LENGTH) : flat.slice(-EDGE_LENGTH);
}

/**
 * Shortens an edge until it actually occurs in the Markdown.
 *
 * Usually it occurs on the first try — edges come from a single text node, so
 * no inline mark splits them. The retry covers what is left: Markdown escaping
 * (`*` stored as `\*`) and other serializer detail that would otherwise ship a
 * handle resolving to nothing. Returns "" when even a minimal edge is absent,
 * so the caller can say so instead of copying something broken.
 */
function edgeThatExists(markdown: string, text: string, side: "start" | "end"): string {
  const flat = edgeFlat(text);
  const hay = Array.from(markdown);
  // The floor applies to SHRINKING, not to the edge as given: a first text
  // node of "状态：" is three characters and perfectly real, and refusing it
  // would copy nothing for a selection that has a valid handle.
  const shortest = Math.min(MIN_EDGE_LENGTH, flat.length);
  for (let len = Math.min(flat.length, EDGE_LENGTH); len >= shortest; len--) {
    const candidate = side === "start" ? flat.slice(0, len) : flat.slice(-len);
    const needle = Array.from(candidate);
    for (let i = 0; i < hay.length; i++) {
      if (matchLenAt(hay, i, needle) >= 0) return candidate;
    }
  }
  return "";
}

/**
 * Returns the command line, or an empty string when no handle can be built
 * that would actually resolve.
 */
export function buildQuoteHandle(input: QuoteHandleInput): string {
  const selected = edgeFlat(input.selected);
  if (!selected) return "";

  // Quoting the selection whole is exact and needs no end edge — but only when
  // the selection sits inside ONE text node. A short selection that still
  // crosses an inline mark (`状态：**已交付**`) does not exist as a single run
  // in the stored Markdown, so it falls back to edges like a long one.
  const withinOneNode = edgeFlat(input.firstNodeText).includes(selected);
  const short = selected.length <= WHOLE_SELECTION_LIMIT && withinOneNode;

  const start = short
    ? edgeThatExists(input.markdown, selected, "start")
    : edgeThatExists(input.markdown, clip(edgeFlat(input.firstNodeText), "start"), "start");
  if (!start) return "";

  const end = short ? "" : edgeThatExists(input.markdown, clip(edgeFlat(input.lastNodeText), "end"), "end");

  const parts = [
    `multica issue get ${input.issueKey}`,
    `--quote-start ${shellQuote(start)}`,
  ];
  if (end && end !== start) parts.push(`--quote-end ${shellQuote(end)}`);

  // Only when the edges alone are ambiguous. An unconditional tie-breaker
  // would make every handle longer and more fragile — one more string that has
  // to still be there when the handle is used.
  //
  // The text AFTER the passage is tried first: in Markdown the decoration
  // sits in front of a passage, not behind it. Before a heading lies "\n\n## ",
  // before a list item "- ", before a quote "> " — none of which appears in
  // the rendering the selection came from, so a leading tie-breaker is the one
  // more likely to be a phantom.
  if (countQuoteSpans(input.markdown, start, end) > 1) {
    const suffix = edgeThatExists(input.markdown, clip(edgeFlat(input.after), "start"), "start");
    if (suffix) {
      parts.push(`--quote-suffix ${shellQuote(suffix)}`);
    } else {
      const prefix = edgeThatExists(input.markdown, clip(edgeFlat(input.before), "end"), "end");
      if (prefix) parts.push(`--quote-prefix ${shellQuote(prefix)}`);
    }
  }

  return parts.join(" ");
}
