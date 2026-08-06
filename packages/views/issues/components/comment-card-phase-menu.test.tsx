import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { forwardRef, useEffect, useImperativeHandle, useRef, type ReactNode, type Ref } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssuePhase, TimelineEntry } from "@multica/core/types";
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

// Filing a comment under a station is done from the ⋯ menu. The submenu was
// on replies only, which is backwards: a thread ROOT is the comment most
// likely to be filed, because it is the one that opened the discussion.

const phases = [
  { id: "phase-1", name: "开始" },
  { id: "phase-2", name: "评审" },
] as unknown as IssuePhase[];

function rootEntry(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    id: "comment-1",
    issue_id: "issue-1",
    parent_id: null,
    actor_type: "member",
    actor_id: "user-1",
    content: "Original body",
    type: "comment",
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-01T00:00:00Z",
    attachments: [],
    reactions: [],
    ...overrides,
  } as unknown as TimelineEntry;
}

function renderRoot(entry: TimelineEntry, onSetPhase = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommentCard
        issueId="issue-1"
        entry={entry}
        replies={[]}
        currentUserId="user-1"
        phases={phases}
        onSetPhase={onSetPhase}
        onReply={vi.fn().mockResolvedValue(true)}
        onEdit={vi.fn().mockResolvedValue(undefined)}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />
    </QueryClientProvider>,
  );
  return { ...view, onSetPhase };
}

function openMenu() {
  const trigger = document.querySelector('button[aria-haspopup="menu"]');
  if (!trigger) throw new Error("Expected the comment actions menu trigger");
  fireEvent.click(trigger);
}

describe("root comment — move to a phase", () => {
  beforeEach(() => vi.clearAllMocks());

  it("offers the phase submenu on a thread root", async () => {
    renderRoot(rootEntry());
    openMenu();

    await waitFor(() => {
      expect(screen.getByText("Move to phase")).toBeInTheDocument();
    });
  });

  // Without phases the submenu would be an empty list, which reads as a
  // broken menu rather than as an issue that has no route.
  it("hides the submenu on an issue with no phases", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <CommentCard
          issueId="issue-1"
          entry={rootEntry()}
          replies={[]}
          currentUserId="user-1"
          phases={[]}
          onSetPhase={vi.fn()}
          onReply={vi.fn().mockResolvedValue(true)}
          onEdit={vi.fn().mockResolvedValue(undefined)}
          onDelete={vi.fn()}
          onToggleReaction={vi.fn()}
        />
      </QueryClientProvider>,
    );
    openMenu();

    await waitFor(() => {
      expect(screen.getByText("Copy")).toBeInTheDocument();
    });
    expect(screen.queryByText("Move to phase")).not.toBeInTheDocument();
  });

  // Clearing is only meaningful once the comment is filed somewhere; offering
  // it on an unfiled comment is an action with no effect.
  it("offers clearing only when the comment is already filed", async () => {
    renderRoot(rootEntry({ phase_id: "phase-2" } as Partial<TimelineEntry>));
    openMenu();

    await waitFor(() => {
      expect(screen.getByText("Move to phase")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText("Move to phase"));

    await waitFor(() => {
      expect(screen.getByText("Remove from phase")).toBeInTheDocument();
    });
  });
});
