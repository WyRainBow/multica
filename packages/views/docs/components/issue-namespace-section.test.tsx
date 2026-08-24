import { describe, it, expect, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueNamespace, IssueNamespaceSlot } from "@multica/core/types";
import type { SupportedLocale } from "@multica/core/i18n";
import { renderWithI18n } from "../../test/i18n";

const getIssueNamespace = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { getIssueNamespace },
  dispatchReasonCode: () => undefined,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    docs: () => "/acme/docs",
    docDetail: (id: string) => `/acme/docs/${id}`,
  }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    children,
    href,
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
}));

import { IssueNamespaceSection } from "./issue-namespace-section";

function slot(overrides: Partial<IssueNamespaceSlot> = {}): IssueNamespaceSlot {
  return {
    name: "requirements",
    label: "需求底稿",
    kind: "COC-338/requirements",
    type: "document",
    exists: true,
    placeholder: true,
    card_id: "card-1",
    title: "COC-338 需求底稿（待补）",
    count: 0,
    ...overrides,
  };
}

function render(namespace: IssueNamespace | null, locale: SupportedLocale = "en") {
  getIssueNamespace.mockResolvedValue(namespace);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueNamespaceSection issueId="issue-1" />
    </QueryClientProvider>,
    { locale },
  );
}

function directory(slots: IssueNamespaceSlot[]): IssueNamespace {
  return { issue_id: "issue-1", key: "COC-338", root: "COC-338", slots };
}

// A document used to appear only once somebody wrote it, which made "there is
// no design doc" and "nobody has looked at the design yet" the same
// observation. The directory exists to keep those apart, so every test here is
// about whether an unwritten slot is visibly unwritten.
describe("the issue's fixed document directory", () => {
  it("lists the six slots in the order they come off the wire", async () => {
    render(
      directory([
        slot({ name: "requirements", label: "需求底稿" }),
        slot({ name: "design", label: "技术方案" }),
        slot({ name: "spec", label: "Spec" }),
        slot({ name: "decisions", label: "决策", type: "folder" }),
        slot({ name: "rounds", label: "评审轮次", type: "folder" }),
        slot({ name: "snapshots", label: "快照", type: "folder" }),
      ]),
    );
    await screen.findByText("需求底稿");
    const rows = screen.getAllByRole("listitem");
    expect(rows.map((r) => r.textContent?.split("COC-338")[0])).toEqual([
      "需求底稿",
      "技术方案",
      "Spec",
      "决策",
      "评审轮次",
      "快照",
    ]);
  });

  // The labels come off the wire. A second copy of the six names in the
  // component is how the client and the server drift apart.
  it("shows the label the server sent, not one of its own", async () => {
    render(directory([slot({ label: "需求底稿" })]));
    expect(await screen.findByText("需求底稿")).toBeInTheDocument();
  });

  it("falls back to the slot name when the label is missing", async () => {
    render(directory([slot({ label: "" })]));
    expect(await screen.findByText("requirements")).toBeInTheDocument();
  });

  // `placeholder` is the one answer to "is this real yet" — read off the card's
  // is_placeholder column and from nothing else, not the title and not whether
  // the body is empty. Asserted in Chinese because 待补 is the word the product
  // uses, the same one the placeholder card's own title carries.
  it("marks a slot still on its placeholder as 待补", async () => {
    render(directory([slot({ placeholder: true })]), "zh-Hans");
    expect(await screen.findByText("待补")).toBeInTheDocument();
  });

  it("does not mark a written slot as 待补", async () => {
    render(directory([slot({ placeholder: false, count: 1 })]), "zh-Hans");
    await screen.findByText("需求底稿");
    expect(screen.queryByText("待补")).not.toBeInTheDocument();
  });

  // A placeholder has nothing at the other end. Offering a way in would send a
  // reader to read a stub — and the placeholder is filtered out of every other
  // card read precisely so nobody lands on one.
  it("gives a 待补 slot no link to follow", async () => {
    render(directory([slot({ placeholder: true, card_id: "card-1" })]));
    await screen.findByText("需求底稿");
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("links a written slot to the document behind it", async () => {
    render(directory([slot({ placeholder: false, card_id: "card-9", count: 1 })]));
    expect(await screen.findByRole("link", { name: "需求底稿" })).toHaveAttribute(
      "href",
      "/acme/docs/card-9",
    );
  });

  // A folder whose placeholder is gone sends no card_id at all; the row still
  // has to render and still has to be inert.
  it("renders a folder with no card behind it without linking it", async () => {
    render(
      directory([
        slot({
          name: "rounds",
          label: "评审轮次",
          type: "folder",
          placeholder: false,
          card_id: "",
          count: 3,
        }),
      ]),
    );
    expect(await screen.findByText("评审轮次")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("says how many documents a folder holds", async () => {
    render(
      directory([
        slot({ name: "rounds", label: "评审轮次", type: "folder", placeholder: false, count: 3 }),
      ]),
    );
    expect(await screen.findByText("3")).toBeInTheDocument();
  });

  // `exists: false` only happens on issues created before the directory did.
  // Those never had a placeholder, so calling them 待补 would report an
  // omission the writer never had a chance to make.
  it("distinguishes a slot that was never created from one that is 待补", async () => {
    render(
      directory([slot({ exists: false, placeholder: false, card_id: "" })]),
      "zh-Hans",
    );
    expect(await screen.findByText("未建")).toBeInTheDocument();
    expect(screen.queryByText("待补")).not.toBeInTheDocument();
  });

  it("carries the full kind path on the row", async () => {
    render(directory([slot({ kind: "COC-338/requirements" })]));
    expect(await screen.findByText("COC-338/requirements")).toBeInTheDocument();
  });

  it("names the root the documents are filed under", async () => {
    render(directory([slot()]));
    await screen.findByText("需求底稿");
    expect(screen.getByText("COC-338")).toBeInTheDocument();
  });

  // Six invented rows would claim the issue has a directory it may not have,
  // so a response that failed its schema draws nothing at all.
  it("renders nothing when the response could not be read", async () => {
    const { container } = render(null);
    await screen.findByText((_, el) => el?.tagName === "BODY");
    expect(container.querySelector("section")).toBeNull();
  });

  it("renders nothing when the directory came back with no slots", async () => {
    const { container } = render(directory([]));
    await screen.findByText((_, el) => el?.tagName === "BODY");
    expect(container.querySelector("section")).toBeNull();
  });

  // A slot type this build has never heard of still gets a row: the server owns
  // the enum, and a value added later must render rather than vanish.
  it("renders a slot whose type it does not recognise", async () => {
    render(directory([slot({ name: "index", label: "索引", type: "atlas" })]));
    expect(await screen.findByText("索引")).toBeInTheDocument();
  });
});
