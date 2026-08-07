import { describe, it, expect } from "vitest";
import { buildQuoteHandle, countQuoteSpans, type QuoteHandleInput } from "./quote-handle";

const DOC = "概述\n背景说明。\n路由决策链路\n第一处正文，讲进域资格。\n附录\n再次提到 路由决策链路 这个词。";

function input(overrides: Partial<QuoteHandleInput> = {}): QuoteHandleInput {
  const selected = overrides.selected ?? "路由决策链路";
  return {
    issueKey: "COC-45",
    markdown: DOC,
    before: "",
    after: "",
    firstNodeText: selected,
    lastNodeText: selected,
    ...overrides,
    selected,
  };
}

describe("buildQuoteHandle", () => {
  // A short selection is quoted whole: exact, and it needs no end edge.
  it("quotes a short selection in full, with no end edge", () => {
    const handle = buildQuoteHandle(input({ selected: "第一处正文", markdown: DOC }));
    expect(handle).toBe("multica issue get COC-45 --quote-start '第一处正文'");
  });

  // The point of the feature: the handle must not grow with the selection.
  it("keeps the handle short when the selection is long", () => {
    const long = "开头的话。" + "填充内容。".repeat(200) + "结尾的话。";
    const handle = buildQuoteHandle(
      input({ selected: long, firstNodeText: long, lastNodeText: long, markdown: long }),
    );
    expect(long.length).toBeGreaterThan(1000);
    expect(handle.length).toBeLessThan(120);
    expect(handle).toContain("--quote-start");
    expect(handle).toContain("--quote-end");
  });

  it("takes the end edge from the end of the selection", () => {
    const long = "开头的话。" + "填充内容。".repeat(200) + "结尾的话。";
    const handle = buildQuoteHandle(
      input({ selected: long, firstNodeText: long, lastNodeText: long, markdown: long }),
    );
    expect(handle).toContain("结尾的话。");
  });

  // Edges come from a single text node so they cannot straddle an inline mark:
  // the editor renders `**已交付**` as `已交付`, and an edge crossing that
  // boundary would not exist in the stored Markdown.
  it("takes each edge from within one text node, not across the selection", () => {
    const handle = buildQuoteHandle(
      input({
        selected: "状态：已交付（2026-08-03）补充说明写在后面，这里要足够长才会带上结束边",
        firstNodeText: "状态：",
        lastNodeText: "这里要足够长才会带上结束边",
        markdown: "状态：已交付（2026-08-03）补充说明写在后面，这里要足够长才会带上结束边",
      }),
    );
    expect(handle).toContain("--quote-start '状态：'");
    expect(handle).not.toContain("状态：已交付");
  });

  // The failure this feature exists to prevent: a repeated phrase must not
  // silently resolve to whichever came first.
  it("adds a tie-breaker when the edges alone match more than one span", () => {
    const handle = buildQuoteHandle(
      input({ selected: "路由决策链路", after: " 这个词。", markdown: DOC }),
    );
    expect(handle).toContain("--quote-suffix");
    expect(handle).toContain("这个词。");
  });

  // The text after the passage is preferred over the text before it because
  // Markdown decoration sits in FRONT of a passage. Here the second occurrence
  // is preceded by "再次提到 " in the rendering — which is real — but a heading
  // would be preceded by "## ", which exists only in the source. Reaching
  // backwards is the direction that goes wrong, so it is the fallback.
  it("falls back to the text before the passage when there is nothing after it", () => {
    const handle = buildQuoteHandle(
      input({ selected: "路由决策链路", before: "再次提到 ", after: "", markdown: DOC }),
    );
    expect(handle).toContain("--quote-prefix");
    expect(handle).toContain("再次提到");
  });

  it("leaves out the tie-breaker when the edges are already unique", () => {
    const handle = buildQuoteHandle(
      input({ selected: "第一处正文", before: "路由决策链路\n", after: "附录", markdown: DOC }),
    );
    expect(handle).not.toContain("--quote-prefix");
    expect(handle).not.toContain("--quote-suffix");
  });

  // A heading is the case that motivated matching against Markdown rather than
  // the rendering: the selection reads "工作目标", the source reads
  // "## 工作目标". The edge still has to be found.
  it("finds an edge whose Markdown carries syntax the rendering does not", () => {
    const handle = buildQuoteHandle(
      input({
        selected: "工作目标",
        markdown: "## 工作目标\n\n记录每轮任务。\n",
        after: "记录每轮任务。",
      }),
    );
    expect(handle).toContain("--quote-start '工作目标'");
  });

  // Nothing is copied rather than something broken: a handle that resolves to
  // nothing wastes a round trip and reads like the feature is broken.
  it("returns nothing when the edge cannot be found in the Markdown at all", () => {
    expect(
      buildQuoteHandle(input({ selected: "屏幕上有但源文里没有", markdown: "完全不同的正文。" })),
    ).toBe("");
  });

  it("returns nothing for a selection that is only whitespace", () => {
    expect(buildQuoteHandle(input({ selected: "   \n  " }))).toBe("");
  });

  // Descriptions carry `$`, backticks and quotes often enough that double
  // quoting would eventually mangle one.
  it("single-quotes the values and escapes an embedded quote", () => {
    const handle = buildQuoteHandle(
      input({ selected: "it's $HOME `now`", markdown: "it's $HOME `now`" }),
    );
    expect(handle).toContain(`'it'\\''s $HOME \`now\`'`);
  });

  it("collapses newlines inside an edge", () => {
    const handle = buildQuoteHandle(
      input({ selected: "第一行\n第二行", markdown: "第一行\n第二行" }),
    );
    expect(handle).toContain("'第一行 第二行'");
  });
});

describe("countQuoteSpans", () => {
  it("counts every occurrence of the start", () => {
    expect(countQuoteSpans(DOC, "路由决策链路", "")).toBe(2);
  });

  it("counts one when the phrase is unique", () => {
    expect(countQuoteSpans(DOC, "第一处正文", "")).toBe(1);
  });

  // A start with no end after it is not a span, which is what stops a handle
  // from being generated against a passage that runs backwards.
  it("does not count a start with no end after it", () => {
    expect(countQuoteSpans(DOC, "附录", "背景说明")).toBe(0);
  });

  it("counts by characters, so CJK does not skew the scan", () => {
    expect(countQuoteSpans("中文前缀 target 中文后缀 target", "target", "")).toBe(2);
  });

  it("returns zero for a blank start", () => {
    expect(countQuoteSpans(DOC, "   ", "")).toBe(0);
  });
});
