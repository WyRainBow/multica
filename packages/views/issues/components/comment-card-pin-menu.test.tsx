import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { forwardRef, useEffect, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { renderWithI18n } from "../../test/i18n";

const apiUploadFile = vi.hoisted(() => vi.fn());
const uploadWithToast = vi.hoisted(() => vi.fn());
const editorDefaultValues = vi.hoisted(() => ({
  values: [] as Array<string | undefined>,
}));

// The real handle mints an id when it inserts the placeholder and hands it to
// the uploader, which adopts it as the draft `clientUploadId`. Mocks must do
// the same or the two records drift apart only in tests.
let mockUploadIdSeq = 0;

vi.mock("@multica/core/api", () => ({
  // Uploads flow through the coordinator, which calls api.uploadFile (MUL-5181).
  api: { uploadFile: apiUploadFile },
  dispatchReasonCode: () => undefined,
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/acme/issues",
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada" }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

// The trigger-preview chips have their own suite; inert here so this file
// stays about the edit gate.
vi.mock("../hooks/use-comment-trigger-preview", () => ({
  useCommentTriggerPreview: () => ({ agents: [], blocked: [] }),
}));

vi.mock("../../editor", async () => ({
  // Real submit gate (pure React) driven by the mock editor below.
  ...(await vi.importActual<typeof import("../../editor/use-upload-gate")>(
    "../../editor/use-upload-gate",
  )),
  // The card nests a ReplyInput, which is readonly-first — real controller.
  ...(await vi.importActual<typeof import("../../editor/use-lazy-editor")>(
    "../../editor/use-lazy-editor",
  )),
  // Real await-then-render submit contract (pure React) — the edit save path
  // now delegates to it.
  ...(await vi.importActual<typeof import("../../editor/use-composer-submit")>(
    "../../editor/use-composer-submit",
  )),
  useEditorUpload: () => ({ uploadWithToast, upload: vi.fn(), uploading: false }),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  Attachment: () => null,
  AttachmentDownloadProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
      onUploadFile,
      onUploadingChange,
      onSubmit,
      placeholder,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      onUploadFile?: (file: File, uploadId: string) => Promise<UploadResult | null>;
      onUploadingChange?: (uploading: boolean) => void;
      onSubmit?: () => void;
      placeholder?: string;
    },
    ref: Ref<unknown>,
  ) {
    editorDefaultValues.values.push(defaultValue);
    const valueRef = useRef(defaultValue ?? "");
    // Mirrors the real editor's `uploading` node attrs — see the sibling
    // composer suite for the same stand-in.
    const inFlightRef = useRef(0);
    // Mirrors the real editor publishing its current answer on subscribe: a
    // fresh instance owns no pending upload, so it reports "not uploading".
    useEffect(() => {
      onUploadingChange?.(inFlightRef.current > 0);
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => { valueRef.current = ""; },
      focus: () => {},
      blur: () => {},
      uploadFile: async (file: File) => {
        inFlightRef.current += 1;
        if (inFlightRef.current === 1) onUploadingChange?.(true);
        try {
          const result = await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
          if (!result) return;
          valueRef.current = `${valueRef.current}\n${result.url}`.trim();
          onUpdate?.(valueRef.current);
        } finally {
          inFlightRef.current -= 1;
          if (inFlightRef.current === 0) onUploadingChange?.(false);
        }
      },
      hasActiveUploads: () => inFlightRef.current > 0,
      // Placeholder rebuild contract: the real handle draws a card for an
      // upload the document is not showing and reports whether it landed.
      // Mocks track ids only — no document to draw into.
      insertUploadPlaceholder: () => true,
      settleUploadPlaceholder: () => false,
    }));
    return (
      <textarea
        data-testid="editor"
        defaultValue={defaultValue}
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          onUpdate?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") onSubmit?.();
        }}
      />
    );
  }),
}));

import { CommentCard } from "./comment-card";

// Pinning is a ROOT action, and the root's ⋯ menu is rendered by
// CommentCardImpl — not by CommentRow, which renders replies. The first cut of
// this feature put the item in CommentRow, where the prop is never passed, so
// it type-checked, shipped, and rendered nothing. Only a render test catches
// that class of mistake.

const mockBody = "Original body";

function rootEntry(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    id: "comment-1",
    issue_id: "issue-1",
    parent_id: null,
    actor_type: "member",
    actor_id: "user-1",
    content: mockBody,
    type: "comment",
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-01T00:00:00Z",
    attachments: [],
    reactions: [],
    ...overrides,
  } as unknown as TimelineEntry;
}

function renderRoot(
  entry: TimelineEntry,
  onPinToggle?: (id: string, pinned: boolean) => void,
  replies: TimelineEntry[] = [],
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommentCard
        issueId="issue-1"
        entry={entry}
        replies={replies}
        currentUserId="user-1"
        onReply={vi.fn(async () => true)}
        onEdit={vi.fn(async () => {})}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
        onPinToggle={onPinToggle}
      />
    </QueryClientProvider>,
  );
}

function openMenu() {
  const trigger = document.querySelector('button[aria-haspopup="menu"]');
  if (!trigger) throw new Error("Expected the comment actions menu trigger");
  fireEvent.click(trigger);
}

describe("root comment — pin", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("offers Pin to top on a thread root", async () => {
    renderRoot(rootEntry(), vi.fn());
    openMenu();
    await waitFor(() => {
      expect(screen.getByText("Pin to top")).toBeInTheDocument();
    });
  });

  it("calls back with the flipped state", async () => {
    const onPinToggle = vi.fn();
    renderRoot(rootEntry(), onPinToggle);
    openMenu();
    await waitFor(() => screen.getByText("Pin to top"));
    fireEvent.click(screen.getByText("Pin to top"));
    expect(onPinToggle).toHaveBeenCalledWith("comment-1", true);
  });

  it("offers Unpin, and the badge, once pinned", async () => {
    renderRoot(rootEntry({ pinned_at: "2026-08-09T00:00:00Z" } as Partial<TimelineEntry>), vi.fn());
    // The badge has to be visible without opening anything — a pin is how a
    // reader finds the thread in the first place.
    expect(screen.getByText("Pinned")).toBeInTheDocument();
    openMenu();
    await waitFor(() => {
      expect(screen.getByText("Unpin")).toBeInTheDocument();
    });
  });

  it("omits the action entirely when the parent passes no handler", async () => {
    renderRoot(rootEntry(), undefined);
    openMenu();
    await waitFor(() => screen.getByText("Copy"));
    expect(screen.queryByText("Pin to top")).not.toBeInTheDocument();
  });
});

// A flat thread renders every reply at the same indent, so "who is answering
// whom" is invisible once more than two people are in it. The label is derived
// from parent_id, and deliberately absent when the parent IS the root: that is
// the common case, it sits directly above, and labelling it would be noise on
// every thread to buy a signal on a few.

function reply(id: string, parentId: string, actorId: string): TimelineEntry {
  return rootEntry({ id, parent_id: parentId, actor_id: actorId } as Partial<TimelineEntry>);
}

describe("reply — who it answers", () => {
  beforeEach(() => vi.clearAllMocks());

  it("labels a reply that answers another reply", () => {
    renderRoot(rootEntry(), vi.fn(), [
      reply("r1", "comment-1", "user-codex"),
      reply("r2", "r1", "user-claude"),
    ]);
    expect(screen.getByText(/replying to/i)).toBeInTheDocument();
  });

  it("leaves a first-level reply unlabelled — the root is directly above", () => {
    renderRoot(rootEntry(), vi.fn(), [reply("r1", "comment-1", "user-codex")]);
    expect(screen.queryByText(/replying to/i)).not.toBeInTheDocument();
  });
});

// A collapsed comment shows a preview clipped at 80 characters, which cannot
// say whether the rest is two lines or four thousand — and how long a comment
// is turns out to be what decides whether anyone opens it. The counter is on
// both the root and the reply headers for that reason.
describe("comment — how long it is", () => {
  beforeEach(() => vi.clearAllMocks());

  it("shows the character count on a root", () => {
    renderRoot(rootEntry({ content: "12345" } as Partial<TimelineEntry>), vi.fn());
    expect(screen.getByText("5 chars")).toBeInTheDocument();
  });

  it("shows it on a reply too", () => {
    renderRoot(rootEntry(), vi.fn(), [
      reply("r1", "comment-1", "user-codex"),
    ]);
    // Both rows carry the same body from the fixture, so two nodes is the
    // proof that the reply header got one as well as the root.
    expect(screen.getAllByText(`${mockBody.length} chars`)).toHaveLength(2);
  });

  // A "0 chars" label on an empty comment is noise, the same call the
  // description's counter makes.
  it("stays hidden when there is nothing to count", () => {
    renderRoot(rootEntry({ content: "" } as Partial<TimelineEntry>), vi.fn());
    expect(screen.queryByText(/chars$/)).not.toBeInTheDocument();
  });
});
