package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const commentEditTestID = "22222222-2222-4222-8222-222222222222"

// newCommentEditCaptureServer serves the GET a splice or append edit must do
// and captures what — if anything — gets PUT, so a test can prove that an
// error path read the comment but sent nothing.
func newCommentEditCaptureServer(t *testing.T, current string) (*[]string, *string) {
	t.Helper()
	var methods []string
	var putContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/comments/"+commentEditTestID:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": commentEditTestID, "content": current})
		case r.Method == http.MethodPut && r.URL.Path == "/api/comments/"+commentEditTestID:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			putContent, _ = body["content"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": commentEditTestID, "content": putContent})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	return &methods, &putContent
}

func TestRunIssueCommentEditReplacesOneShortPassage(t *testing.T) {
	// CJK before the anchor, so a byte offset would slice mid-character.
	current := "现状：第一批已经发车。\n\n风险：还没有灰度。"
	methods, put := newCommentEditCaptureServer(t, current)

	cmd := newIssueCommentEditTestCmd()
	_ = cmd.Flags().Set("replace", "第一批已经发车")
	_ = cmd.Flags().Set("with", "第一批已验收并发车")

	if err := runIssueCommentEdit(cmd, []string{commentEditTestID}); err != nil {
		t.Fatalf("edit comment: %v", err)
	}
	if want := "现状：第一批已验收并发车。\n\n风险：还没有灰度。"; *put != want {
		t.Fatalf("spliced body = %q, want %q", *put, want)
	}
	if len(*methods) != 2 || (*methods)[0] != "GET /api/comments/"+commentEditTestID ||
		(*methods)[1] != "PUT /api/comments/"+commentEditTestID {
		t.Fatalf("expected exactly GET then PUT, got %v", *methods)
	}
}

func TestRunIssueCommentEditReplacesASpanByItsEdges(t *testing.T) {
	current := "前缀段落。\n\n## 旧段落\n\n中间一大段，长到不值得整段粘贴。\n\n## 下一段\n\n后续内容。"
	methods, put := newCommentEditCaptureServer(t, current)

	cmd := newIssueCommentEditTestCmd()
	_ = cmd.Flags().Set("replace-start", "## 旧段落")
	_ = cmd.Flags().Set("replace-end", "中间一大段，长到不值得整段粘贴。")
	_ = cmd.Flags().Set("with", "新段落。")

	if err := runIssueCommentEdit(cmd, []string{commentEditTestID}); err != nil {
		t.Fatalf("edit comment: %v", err)
	}
	if want := "前缀段落。\n\n新段落。\n\n## 下一段\n\n后续内容。"; *put != want {
		t.Fatalf("spliced body = %q, want %q", *put, want)
	}
	if len(*methods) != 2 || (*methods)[0] != "GET /api/comments/"+commentEditTestID ||
		(*methods)[1] != "PUT /api/comments/"+commentEditTestID {
		t.Fatalf("expected exactly GET then PUT, got %v", *methods)
	}
}

func TestRunIssueCommentEditTakesTheReplacementFromStdin(t *testing.T) {
	current := "旧段落第一行。"
	methods, put := newCommentEditCaptureServer(t, current)

	pipeStdin(t, "新第一行\n新第二行\n", func() {
		cmd := newIssueCommentEditTestCmd()
		_ = cmd.Flags().Set("replace", "旧段落第一行")
		_ = cmd.Flags().Set("with-stdin", "true")

		if err := runIssueCommentEdit(cmd, []string{commentEditTestID}); err != nil {
			t.Fatalf("edit comment: %v", err)
		}
	})
	// The anchor stopped before the sentence's "。", so that character is
	// body, not anchor, and survives the splice.
	if want := "新第一行\n新第二行。"; *put != want {
		t.Fatalf("spliced body = %q, want %q", *put, want)
	}
	if len(*methods) != 2 {
		t.Fatalf("expected GET then PUT, got %v", *methods)
	}
}

func TestRunIssueCommentEditRefusesAnAmbiguousReplace(t *testing.T) {
	current := "第一次提到 路由决策链路。\n\n第二次提到 路由决策链路。"
	methods, put := newCommentEditCaptureServer(t, current)

	cmd := newIssueCommentEditTestCmd()
	_ = cmd.Flags().Set("replace", "路由决策链路")
	_ = cmd.Flags().Set("with", "改这一处")

	err := runIssueCommentEdit(cmd, []string{commentEditTestID})
	if err == nil || !strings.Contains(err.Error(), "--replace") || !strings.Contains(err.Error(), "2 spans") {
		t.Fatalf("expected an ambiguity error naming --replace and the count, got %v", err)
	}
	// The read is fine; the write is not. One GET, zero PUTs.
	if len(*methods) != 1 || (*methods)[0] != "GET /api/comments/"+commentEditTestID {
		t.Fatalf("expected exactly one GET, got %v", *methods)
	}
	if *put != "" {
		t.Fatalf("ambiguous anchor must not PUT anything, sent %q", *put)
	}
}

func TestRunIssueCommentEditRefusesAMissingAnchor(t *testing.T) {
	methods, put := newCommentEditCaptureServer(t, "正文只有一段。")

	cmd := newIssueCommentEditTestCmd()
	_ = cmd.Flags().Set("replace", "这段不存在")
	_ = cmd.Flags().Set("with", "改这一处")

	err := runIssueCommentEdit(cmd, []string{commentEditTestID})
	if err == nil || !strings.Contains(err.Error(), "--replace text does not appear in the comment") {
		t.Fatalf("expected a missing-anchor error naming --replace, got %v", err)
	}
	if len(*methods) != 1 || *put != "" {
		t.Fatalf("expected one GET and no PUT, got methods=%v put=%q", *methods, *put)
	}
}

// An end that only appears before the start is the reversed-anchor mistake:
// the span cannot run backwards, and guessing a different end would splice
// the wrong passage into a real PUT.
func TestRunIssueCommentEditRefusesAnEndThatPrecedesItsStart(t *testing.T) {
	methods, put := newCommentEditCaptureServer(t, "开头句。\n\n中间。\n\n结尾句。")

	cmd := newIssueCommentEditTestCmd()
	_ = cmd.Flags().Set("replace-start", "结尾句")
	_ = cmd.Flags().Set("replace-end", "开头句")
	_ = cmd.Flags().Set("with", "x")

	err := runIssueCommentEdit(cmd, []string{commentEditTestID})
	if err == nil || !strings.Contains(err.Error(), "--replace-end") || !strings.Contains(err.Error(), "after") {
		t.Fatalf("expected an error naming --replace-end and the order problem, got %v", err)
	}
	if len(*methods) != 1 || *put != "" {
		t.Fatalf("expected one GET and no PUT, got methods=%v put=%q", *methods, *put)
	}
}

func TestRunIssueCommentEditAppendsAParagraph(t *testing.T) {
	methods, put := newCommentEditCaptureServer(t, "第一段。")

	cmd := newIssueCommentEditTestCmd()
	_ = cmd.Flags().Set("append", "补充：验收证据在第二条评论。")

	if err := runIssueCommentEdit(cmd, []string{commentEditTestID}); err != nil {
		t.Fatalf("edit comment: %v", err)
	}
	if want := "第一段。\n\n补充：验收证据在第二条评论。"; *put != want {
		t.Fatalf("appended body = %q, want %q", *put, want)
	}
	if len(*methods) != 2 || (*methods)[0] != "GET /api/comments/"+commentEditTestID ||
		(*methods)[1] != "PUT /api/comments/"+commentEditTestID {
		t.Fatalf("expected exactly GET then PUT, got %v", *methods)
	}
}

func TestRunIssueCommentEditAppendCollapsesTrailingBlankLines(t *testing.T) {
	_, put := newCommentEditCaptureServer(t, "第一段。\n\n\n")

	cmd := newIssueCommentEditTestCmd()
	_ = cmd.Flags().Set("append-stdin", "true")
	pipeStdin(t, "补充。\n", func() {
		if err := runIssueCommentEdit(cmd, []string{commentEditTestID}); err != nil {
			t.Fatalf("edit comment: %v", err)
		}
	})
	if want := "第一段。\n\n补充。"; *put != want {
		t.Fatalf("appended body = %q, want %q", *put, want)
	}
}

// The three ways to build the next body are exclusive, and the conflict is
// detected before stdin is read — a conflicted run must fail rather than
// swallow the pipe and then, worse, act on one interpretation.
func TestRunIssueCommentEditRejectsMixingBodySources(t *testing.T) {
	for _, tt := range []struct {
		name string
		set  func(*cobra.Command)
	}{
		{"replace with content", func(c *cobra.Command) {
			_ = c.Flags().Set("replace", "a")
			_ = c.Flags().Set("with", "b")
			_ = c.Flags().Set("content", "c")
		}},
		{"replace with content-stdin", func(c *cobra.Command) {
			_ = c.Flags().Set("replace", "a")
			_ = c.Flags().Set("with", "b")
			_ = c.Flags().Set("content-stdin", "true")
		}},
		{"append with content-file", func(c *cobra.Command) {
			_ = c.Flags().Set("append", "a")
			_ = c.Flags().Set("content-file", "b.txt")
		}},
		{"append with replace", func(c *cobra.Command) {
			_ = c.Flags().Set("append", "a")
			_ = c.Flags().Set("replace", "b")
			_ = c.Flags().Set("with", "c")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newIssueCommentEditTestCmd()
			tt.set(cmd)
			err := runIssueCommentEdit(cmd, []string{commentEditTestID})
			if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("expected a mutual-exclusion error, got %v", err)
			}
		})
	}
}

// Half an anchor pair or a --with with nothing to replace is a usage error,
// and must fail before any request is built.
func TestRunIssueCommentEditRejectsIncompleteSpanFlags(t *testing.T) {
	for _, tt := range []struct {
		name string
		set  func(*cobra.Command)
		want string
	}{
		{"start without end", func(c *cobra.Command) {
			_ = c.Flags().Set("replace-start", "a")
		}, "--replace-start needs --replace-end"},
		{"end without start", func(c *cobra.Command) {
			_ = c.Flags().Set("replace-end", "a")
		}, "--replace-end needs --replace-start"},
		{"replace alongside the pair", func(c *cobra.Command) {
			_ = c.Flags().Set("replace", "a")
			_ = c.Flags().Set("replace-start", "b")
			_ = c.Flags().Set("replace-end", "c")
		}, "--replace and --replace-start/--replace-end are mutually exclusive"},
		{"with without an anchor", func(c *cobra.Command) {
			_ = c.Flags().Set("with", "a")
		}, "--with needs an anchor"},
		{"anchor without with", func(c *cobra.Command) {
			_ = c.Flags().Set("replace", "a")
		}, "--with or --with-stdin is required"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newIssueCommentEditTestCmd()
			tt.set(cmd)
			err := runIssueCommentEdit(cmd, []string{commentEditTestID})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error mentioning %q, got %v", tt.want, err)
			}
		})
	}
}

// Removing a sentence is an ordinary surgical edit — the one right after
// fixing a typo — and it was the single edit the command could not express,
// because an empty --with was indistinguishable from no --with at all.
func TestCommentEdit_EmptyWithDeletesThePassage(t *testing.T) {
	target := commentEditTarget{
		kind:  commentEditReplaceSpan,
		spec:  quoteSpec{Start: "删掉甲。"},
		names: spanFlagNames{Noun: "comment", Start: "replace"},
		with:  "",
	}
	got, err := target.apply("保留一。\n\n删掉甲。\n\n保留二。")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// One blank line, not three: deleting a paragraph takes the blank line
	// above it and the one below, so the gap would grow on every delete.
	if want := "保留一。\n\n保留二。"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// The collapse belongs to delete alone. A replacement that deliberately
// contains a blank line must survive verbatim.
func TestCommentEdit_NonEmptyReplacementKeepsItsBlankLines(t *testing.T) {
	target := commentEditTarget{
		kind:  commentEditReplaceSpan,
		spec:  quoteSpec{Start: "换我。"},
		names: spanFlagNames{Noun: "comment", Start: "replace"},
		with:  "甲。\n\n乙。",
	}
	got, err := target.apply("换我。\n\n尾。")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if want := "甲。\n\n乙。\n\n尾。"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestCollapseBlankRun(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a\n\n\n\nb", "a\n\nb"},
		{"a\n\nb", "a\n\nb"},
		{"a\nb", "a\nb"},
		{"a\n\n\n", "a"},
	} {
		if got := collapseBlankRun(c.in); got != c.want {
			t.Errorf("collapseBlankRun(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
