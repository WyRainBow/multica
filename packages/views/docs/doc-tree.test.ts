import { describe, it, expect } from "vitest";
import type { Card } from "@multica/core/types";
import {
  buildDocTree,
  filterDocsByPath,
  kindSegments,
  allDocPaths,
  docLength,
} from "./doc-tree";

function doc(kind: string, id = kind): Card {
  return { id, kind, title: id, content: "" } as unknown as Card;
}

describe("kindSegments", () => {
  it("splits a path into levels", () => {
    expect(kindSegments("工作流架构演进/04-验证与交付")).toEqual([
      "工作流架构演进",
      "04-验证与交付",
    ]);
  });

  // A one-level kind is what every existing document already has, and it has
  // to keep working untouched — that is why this generalises `kind` instead of
  // adding a column.
  it("treats a plain kind as a single level", () => {
    expect(kindSegments("联调")).toEqual(["联调"]);
  });

  it("survives stray and doubled slashes", () => {
    expect(kindSegments("/a//b/")).toEqual(["a", "b"]);
    expect(kindSegments("")).toEqual([]);
    expect(kindSegments("   ")).toEqual([]);
  });
});

describe("buildDocTree", () => {
  it("nests by level", () => {
    const tree = buildDocTree([
      doc("工作流架构演进/04-验证与交付", "a"),
      doc("工作流架构演进/01-设计", "b"),
    ]);
    expect(tree).toHaveLength(1);
    expect(tree[0]!.name).toBe("工作流架构演进");
    expect(tree[0]!.children.map((c) => c.name).sort()).toEqual([
      "01-设计",
      "04-验证与交付",
    ]);
  });

  // A folder that said 0 while holding a hundred documents two levels down
  // would read as empty, and the number exists to answer "is anything here".
  it("counts cumulatively up the branch", () => {
    const tree = buildDocTree([
      doc("a/b/c", "1"),
      doc("a/b/c", "2"),
      doc("a/x", "3"),
    ]);
    const a = tree[0]!;
    expect(a.count).toBe(3);
    const b = a.children.find((c) => c.name === "b")!;
    expect(b.count).toBe(2);
    expect(b.children[0]!.count).toBe(2);
  });

  it("orders siblings by count, then name", () => {
    const tree = buildDocTree([
      doc("rare", "1"),
      doc("common", "2"),
      doc("common", "3"),
    ]);
    expect(tree.map((n) => n.name)).toEqual(["common", "rare"]);
  });

  // 全部 already shows them, and a blank folder label has nothing to render.
  it("gives uncategorised documents no node", () => {
    expect(buildDocTree([doc("", "1"), doc("   ", "2")])).toEqual([]);
  });
});

describe("filterDocsByPath", () => {
  it("includes everything below the selected folder", () => {
    const docs = [
      doc("a", "1"),
      doc("a/b", "2"),
      doc("a/b/c", "3"),
      doc("z", "4"),
    ];
    expect(filterDocsByPath(docs, "a").map((d) => d.id)).toEqual([
      "1",
      "2",
      "3",
    ]);
  });

  // Selecting a parent and being told it is empty, while its subfolders hold
  // documents, makes the parent look broken.
  it("a parent holding nothing directly still shows its children's", () => {
    const docs = [doc("a/b", "1")];
    expect(filterDocsByPath(docs, "a").map((d) => d.id)).toEqual(["1"]);
  });

  // Prefix matching on the raw string would make 本地联调 swallow 本地联调整理.
  it("matches whole segments, not string prefixes", () => {
    const docs = [doc("本地联调", "1"), doc("本地联调整理", "2")];
    expect(filterDocsByPath(docs, "本地联调").map((d) => d.id)).toEqual(["1"]);
  });

  it("selects everything when nothing is selected", () => {
    const docs = [doc("a", "1"), doc("", "2")];
    expect(filterDocsByPath(docs, "").map((d) => d.id)).toEqual(["1", "2"]);
  });
});

describe("allDocPaths", () => {
  it("lists every folder, parents included", () => {
    expect(allDocPaths([doc("a/b/c")]).sort()).toEqual(["a", "a/b", "a/b/c"]);
  });
});

// The count appears in three places in the app and a fourth in `doc list`.
// They have to agree, or none of them can be quoted.
describe("docLength", () => {
  it("counts an astral character once, the way the CLI's rune count does", () => {
    expect("🎉".length).toBe(2); // what JavaScript would have reported
    expect(docLength("🎉")).toBe(1);
  });

  it("matches plain .length for CJK and ASCII", () => {
    const s = "本地起 P0 workflow 全链路 SOP";
    expect(docLength(s)).toBe(s.length);
  });

  it("is 0 for an empty document", () => {
    expect(docLength("")).toBe(0);
  });
});
