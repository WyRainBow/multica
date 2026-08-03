import { describe, it, expect } from "vitest";
import {
  resolveCommentAnchors,
  unresolvedAnchorIds,
  type CommentAnchor,
} from "./comment-anchors";

const anchor = (
  commentId: string,
  text: string,
  offset?: number | null,
): CommentAnchor => ({ commentId, text, offset });

describe("resolveCommentAnchors", () => {
  const doc = "开头。V1 结论 是这样。中间段落。V1 结论 又出现一次。结尾。";

  it("finds the span a comment was written against", () => {
    const [got] = resolveCommentAnchors("请看 V1 结论 这一段", [
      anchor("c1", "V1 结论"),
    ]);
    expect(got).toBeDefined();
    if (!got) return;
    expect(got).toEqual({ commentId: "c1", from: 3, to: 8 });
  });

  it("drops an anchor whose text has been edited away", () => {
    expect(resolveCommentAnchors("完全不同的正文", [anchor("c1", "V1 结论")])).toEqual([]);
  });

  it("uses the offset hint to pick between repeated occurrences", () => {
    const second = doc.lastIndexOf("V1 结论");
    const [got] = resolveCommentAnchors(doc, [anchor("c1", "V1 结论", second)]);
    expect(got).toBeDefined();
    if (!got) return;
    expect(got.from).toBe(second);
  });

  it("picks the nearest occurrence when the hint has drifted", () => {
    // The document shifted by a few characters since the anchor was stored;
    // the hint no longer matches exactly but is still closest to the second
    // occurrence, which is the one the user commented on.
    const second = doc.lastIndexOf("V1 结论");
    const [got] = resolveCommentAnchors(doc, [anchor("c1", "V1 结论", second - 3)]);
    expect(got).toBeDefined();
    if (!got) return;
    expect(got.from).toBe(second);
  });

  it("falls back to the first occurrence when there is no hint", () => {
    const [got] = resolveCommentAnchors(doc, [anchor("c1", "V1 结论")]);
    expect(got).toBeDefined();
    if (!got) return;
    expect(got.from).toBe(doc.indexOf("V1 结论"));
  });

  it("keeps the earlier occurrence when the hint is exactly between two", () => {
    // A tie must not depend on scan order, or the same document would
    // highlight differently on different renders.
    const first = doc.indexOf("V1 结论");
    const second = doc.lastIndexOf("V1 结论");
    const midpoint = Math.floor((first + second) / 2);
    const [got] = resolveCommentAnchors(doc, [anchor("c1", "V1 结论", midpoint)]);
    expect(got).toBeDefined();
    if (!got) return;
    expect(got.from).toBe(first);
  });

  it("trims the stored anchor before matching", () => {
    // A selection almost always carries surrounding whitespace; matching it
    // verbatim would fail against the text it was taken from.
    const [got] = resolveCommentAnchors("请看 V1 结论 这一段", [
      anchor("c1", "  V1 结论\n"),
    ]);
    expect(got).toBeDefined();
    if (!got) return;
    expect(got).toEqual({ commentId: "c1", from: 3, to: 8 });
  });

  it("ignores an anchor that is only whitespace", () => {
    expect(resolveCommentAnchors("正文", [anchor("c1", "   ")])).toEqual([]);
  });

  it("returns spans in document order regardless of input order", () => {
    const first = doc.indexOf("V1 结论");
    const second = doc.lastIndexOf("V1 结论");
    const got = resolveCommentAnchors(doc, [
      anchor("late", "V1 结论", second),
      anchor("early", "V1 结论", first),
    ]);
    expect(got.map((a) => a.commentId)).toEqual(["early", "late"]);
  });

  it("lets two comments share one span", () => {
    // Two people commenting on the same sentence is ordinary; neither may be
    // silently dropped.
    const got = resolveCommentAnchors("请看 V1 结论 这一段", [
      anchor("c1", "V1 结论"),
      anchor("c2", "V1 结论"),
    ]);
    expect(got).toHaveLength(2);
    expect(got.map((a) => a.commentId).sort()).toEqual(["c1", "c2"]);
  });

  it("matches anchors that span whole sentences", () => {
    const text = "第一句。第二句很长很长。第三句。";
    const [got] = resolveCommentAnchors(text, [anchor("c1", "第二句很长很长。")]);
    expect(got).toBeDefined();
    if (!got) return;
    expect(text.slice(got.from, got.to)).toBe("第二句很长很长。");
  });
});

describe("unresolvedAnchorIds", () => {
  it("names the comments that no longer highlight", () => {
    const anchors = [anchor("kept", "还在"), anchor("gone", "已删除的句子")];
    expect(unresolvedAnchorIds("正文里只剩下 还在 了", anchors)).toEqual(["gone"]);
  });

  it("is empty when every anchor still matches", () => {
    expect(unresolvedAnchorIds("A 和 B", [anchor("a", "A"), anchor("b", "B")])).toEqual([]);
  });
});
