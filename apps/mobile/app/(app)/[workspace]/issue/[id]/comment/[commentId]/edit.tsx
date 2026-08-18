/**
 * Edit a comment's text. Modal presentation, configured in
 * `[workspace]/_layout.tsx`. Save runs the optimistic `useEditComment`
 * mutation (patches the TimelineEntry in the timeline cache); the modal
 * dismisses on success and the comment row picks up the "(edited)" marker
 * from `updated_at` automatically.
 *
 * Mirrors `issue/[id]/edit.tsx` so users get the same gesture on both
 * record types (cancel/save in header, dirty Alert on dismiss-while-dirty,
 * `useMentionInput` + `<DescriptionField>` for the @-mention pipeline).
 * Mention round-trip has the same v1 note as issue edit: existing
 * `[@name](mention://...)` literals in the server-side content pass
 * through unchanged; newly added mentions serialize via the marker
 * pipeline.
 *
 * Self-contained route body per apps/mobile/CLAUDE.md Lesson 5: reads the
 * comment from the timeline cache (already warm when the user gets here),
 * calls its own mutation on submit, `router.back()`s. No callbacks up to
 * the issue screen.
 *
 * Parity with web (packages/views/issues/components/comment-card.tsx):
 *   - Same endpoint (`PUT /api/comments/:id`) and content-only body.
 *   - Omitting `attachment_ids` preserves existing attachments
 *     (server/internal/handler/comment.go: `replaceAttachments :=
 *     req.AttachmentIDs != nil`) — this screen edits text only and never
 *     drops attachments.
 *
 * Intentional divergences from web (documented per apps/mobile/CLAUDE.md
 * §Behavioral parity):
 *   - Text-only editing. Web's edit composer can also replace
 *     attachments; mobile keeps existing attachments untouched.
 *   - Entry point is offered on the user's OWN comments only (same gate
 *     as mobile's existing Delete item). Web additionally lets workspace
 *     admins edit member comments (`canEditEntry` in comment-card.tsx);
 *     mobile has no workspace-role plumbing yet.
 */
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
} from "react-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { stripChannelMediaMarkers } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { DescriptionField } from "@/components/issue/description-field";
import { MentionSuggestionBar } from "@/components/issue/mention-suggestion-bar";
import { issueTimelineOptions } from "@/data/queries/issues";
import { useEditComment } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useMentionInput } from "@/lib/use-mention-input";

export default function EditComment() {
  const { id, commentId } = useLocalSearchParams<{
    id: string;
    commentId: string;
  }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const timeline = useQuery(issueTimelineOptions(wsId, id));
  const edit = useEditComment(id);

  const entry = useMemo(
    () =>
      timeline.data?.find(
        (e) => e.type === "comment" && e.id === commentId,
      ),
    [timeline.data, commentId],
  );

  const comment = useMentionInput();
  const [contentBase, setContentBase] = useState("");
  const [seeded, setSeeded] = useState(false);
  // Stable setState identity pulled out of the hook return so the seeding
  // effect can list it explicitly without the whole `comment` object
  // (new every render) re-triggering the seed mid-edit. Same pattern as
  // issue/[id]/edit.tsx.
  const setCommentText = comment.setText;

  useEffect(() => {
    if (!entry || seeded) return;
    const initial = entry.content ?? "";
    setCommentText(stripChannelMediaMarkers(initial));
    setContentBase(initial);
    setSeeded(true);
  }, [entry, seeded, setCommentText]);

  // Comment vanished from the timeline while we were opening (deleted
  // from another client) — nothing to edit, bounce back.
  useEffect(() => {
    if (!timeline.data || entry) return;
    Alert.alert("Comment not found", "This comment may have been deleted.", [
      { text: "OK", onPress: () => router.back() },
    ]);
  }, [timeline.data, entry]);

  const currentContent = comment.serialize();

  const dirty = useMemo(() => {
    if (!entry || !seeded) return false;
    return (
      currentContent.trim() !==
      stripChannelMediaMarkers(contentBase).trim()
    );
  }, [entry, seeded, currentContent, contentBase]);

  // Server rejects empty content on PUT — gate Save on non-empty text,
  // same client-side guard as web's edit composer.
  const canSave =
    seeded && currentContent.trim().length > 0 && dirty && !edit.isPending;

  const onCancel = useCallback(() => {
    if (!dirty) {
      router.back();
      return;
    }
    Alert.alert(
      "Discard changes?",
      "Your edits to this comment will be lost.",
      [
        { text: "Keep editing", style: "cancel" },
        {
          text: "Discard",
          style: "destructive",
          onPress: () => router.back(),
        },
      ],
    );
  }, [dirty]);

  const onSave = useCallback(() => {
    if (!canSave) return;
    edit.mutate(
      { commentId, content: currentContent.trim() },
      {
        onSuccess: () => router.back(),
        onError: (err) => {
          Alert.alert(
            "Failed to save",
            err instanceof Error ? err.message : "Unknown error",
          );
        },
      },
    );
  }, [canSave, edit, commentId, currentContent]);

  const headerLeft = useCallback(
    () => (
      <Pressable onPress={onCancel} className="px-1 py-1">
        <Text className="text-base text-brand">Cancel</Text>
      </Pressable>
    ),
    [onCancel],
  );

  const headerRight = useCallback(
    () => (
      <Pressable
        onPress={onSave}
        disabled={!canSave}
        className={canSave ? "px-1 py-1" : "px-1 py-1 opacity-40"}
      >
        <Text className="text-base text-brand font-semibold">
          {edit.isPending ? "Saving…" : "Save"}
        </Text>
      </Pressable>
    ),
    [canSave, onSave, edit.isPending],
  );

  return (
    <>
      <Stack.Screen options={{ headerLeft, headerRight }} />
      <KeyboardAvoidingView
        className="flex-1 bg-background"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <ScrollView
          className="flex-1"
          contentContainerClassName="px-4 pt-4 pb-6 gap-4"
          keyboardShouldPersistTaps="handled"
        >
          {!entry ? (
            <Text className="text-sm text-muted-foreground">Loading…</Text>
          ) : (
            <DescriptionField
              description={comment}
              disabled={edit.isPending}
              placeholder="Edit comment… (type @ to mention)"
            />
          )}
        </ScrollView>
        {/* Mention suggestion bar floats above the keyboard while the user
            is mid-@. Outside the ScrollView so it doesn't scroll with the
            body. */}
        <MentionSuggestionBar {...comment.suggestionBar} />
      </KeyboardAvoidingView>
    </>
  );
}
