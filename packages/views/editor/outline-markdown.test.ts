import { describe, it, expect } from "vitest";
import { extractOutlineFromMarkdown } from "./outline";

// A finished issue's body is rendered as HTML, not loaded into the editor, so
// there is no ProseMirror document to walk — and until now that meant no
// outline at all on exactly the issues people go back to read.
describe("extractOutlineFromMarkdown", () => {
  it("reports each heading's level, text and source offset", () => {
    const md = "intro\n\n## 工作目标\n正文\n\n### 验收条件\n";
    expect(extractOutlineFromMarkdown(md)).toEqual([
      { id: "h7", level: 2, text: "工作目标", pos: 7 },
      { id: "h19", level: 3, text: "验收条件", pos: 19 },
    ]);
  });

  // The offset must be the one react-markdown reports, or the anchor the
  // readonly renderer stamps would not be the one the outline looks up.
  it("offsets index the source string exactly", () => {
    const md = "intro\n\n## 工作目标\n";
    const [heading] = extractOutlineFromMarkdown(md);
    expect(md.slice(heading!.pos, heading!.pos + 2)).toBe("##");
  });

  it("ignores a hash inside a fenced code block", () => {
    const md = "## 真标题\n\n```sh\n# 这是注释不是标题\n```\n\n## 另一个\n";
    expect(extractOutlineFromMarkdown(md).map((h) => h.text)).toEqual([
      "真标题",
      "另一个",
    ]);
  });

  it("ignores a hash with no text and a bare hash run", () => {
    expect(extractOutlineFromMarkdown("#\n##   \n#hashtag\n")).toEqual([]);
  });

  it("strips closing hashes", () => {
    expect(extractOutlineFromMarkdown("## 标题 ##")[0]!.text).toBe("标题");
  });
});
