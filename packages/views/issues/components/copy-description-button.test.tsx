import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

const copyText = vi.hoisted(() => vi.fn());
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText }));

import { CopyDescriptionButton } from "./copy-description-button";

const MARKDOWN = "## 工作目标\n\n- 一条\n- 两条\n\n```sh\nmake dev\n```\n";

beforeEach(() => {
  copyText.mockReset();
  copyText.mockResolvedValue(true);
});

describe("copying the description", () => {
  // The Markdown source, not the rendered text: it is what `multica issue get`
  // returns and what an agent reads. Rendered text would paste a body whose
  // headings, lists and code fences had become ordinary lines.
  it("copies the Markdown source verbatim", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CopyDescriptionButton markdown={MARKDOWN} />);

    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(copyText).toHaveBeenCalledWith(MARKDOWN);
  });

  it("confirms in place after copying", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CopyDescriptionButton markdown={MARKDOWN} />);

    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(await screen.findByText("Copied")).toBeInTheDocument();
  });

  // A tick that appears when nothing was copied is a lie the reader acts on.
  it("does not confirm when the copy failed", async () => {
    copyText.mockResolvedValue(false);
    const user = userEvent.setup();
    renderWithI18n(<CopyDescriptionButton markdown={MARKDOWN} />);

    await user.click(screen.getByRole("button", { name: "Copy" }));
    await waitFor(() => expect(copyText).toHaveBeenCalled());
    expect(screen.queryByText("Copied")).not.toBeInTheDocument();
  });

  // Nothing to copy — an empty description is every issue on the day it is
  // filed, and a button that copies "" on all of them is worse than no button.
  // Braces, not a quoted attribute: JSX string literals do not process escapes,
  // so markdown="   \n" would pass a literal backslash and this would pass for
  // the wrong reason.
  it("renders nothing for a blank description", () => {
    const { container } = renderWithI18n(
      <CopyDescriptionButton markdown={"   \n\t"} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for an empty description", () => {
    const { container } = renderWithI18n(<CopyDescriptionButton markdown="" />);
    expect(container).toBeEmptyDOMElement();
  });
});
