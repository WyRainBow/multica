package main

import (
	"strings"
	"testing"
)

func TestParseRoundKind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in     string
		number int
		phase  string
		ok     bool
	}{
		{"R1-方案评审", 1, "方案评审", true},
		{"R12-代码评审", 12, "代码评审", true},
		{"R0-代码评审", 0, "", false},
		{"rounds", 0, "", false},
		{"R2", 0, "", false},
	} {
		n, p, ok := ParseRoundKind(c.in)
		if ok != c.ok || n != c.number || p != c.phase {
			t.Errorf("ParseRoundKind(%q) = (%d, %q, %v), want (%d, %q, %v)",
				c.in, n, p, ok, c.number, c.phase, c.ok)
		}
	}
}

func TestNextRoundNumberComesFromTheDocuments(t *testing.T) {
	t.Parallel()
	if got := NextRoundNumber(nil, "代码评审"); got != 1 {
		t.Errorf("first round = %d, want 1", got)
	}
	// Counting from the documents rather than from a stored counter: a gap
	// stays visible, and a round cannot be closed twice under one number.
	rounds := []RoundDoc{{Number: 1, Phase: "代码评审"}, {Number: 3, Phase: "代码评审"}}
	if got := NextRoundNumber(rounds, "代码评审"); got != 4 {
		t.Errorf("after R1 and R3 = %d, want 4", got)
	}
}

func TestEachStationCountsItsOwnRounds(t *testing.T) {
	t.Parallel()
	// Counting across the whole card made a card that closed one round at
	// 方案评审 print its first 代码评审 round as R2 — which reads as a second
	// attempt and sends the next reader hunting for an R1 nobody held.
	closed := []RoundDoc{{Number: 1, Phase: "方案评审"}, {Number: 2, Phase: "方案评审"}}

	if got := NextRoundNumber(closed, "代码评审"); got != 1 {
		t.Errorf("first round at an untouched station = %d, want 1", got)
	}
	if got := NextRoundNumber(closed, "方案评审"); got != 3 {
		t.Errorf("next round at the station that ran twice = %d, want 3", got)
	}
	// Station names arrive from a flag and from a document kind; one of them
	// having picked up whitespace must not start a parallel sequence.
	if got := NextRoundNumber([]RoundDoc{{Number: 1, Phase: " 代码评审 "}}, "代码评审"); got != 2 {
		t.Errorf("whitespace around a station name split its sequence: got %d, want 2", got)
	}
}

func TestRoundSectionLeadsWithTheLatestRound(t *testing.T) {
	t.Parallel()
	section := RenderRoundSection([]RoundDoc{
		{Number: 1, Phase: "方案评审", Verdict: "approve", Summary: "定了三本账", DocID: "d1"},
		{Number: 2, Phase: "代码评审", Verdict: "request_changes", Summary: "撤回假绿", DocID: "d2"},
	}, "")
	r1 := strings.Index(section, "| R1 |")
	r2 := strings.Index(section, "| R2 |")
	if r2 < 0 || r1 < 0 || r2 > r1 {
		t.Errorf("rounds are not newest-first:\n%s", section)
	}
}

func TestASummaryCannotBreakItsRow(t *testing.T) {
	t.Parallel()
	section := RenderRoundSection([]RoundDoc{
		{Number: 1, Phase: "代码评审", Verdict: "approve", Summary: "a | b", DocID: "d1"},
	}, "")
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| R1 |") {
			// Counted against the header rather than a literal, so adding a
			// column does not turn this into a test of last week's table. An
			// unescaped pipe in the summary would silently add a column and
			// shift every value one to the right.
			want := strings.Count(headerRow(section), "|")
			if got := strings.Count(line, "|") - strings.Count(line, `\|`); got != want {
				t.Errorf("row has %d structural pipes, want %d: %s", got, want, line)
			}
		}
	}
}

// headerRow is the table's own column count, so a row is checked against the
// header it sits under rather than a number written down once.
func headerRow(section string) string {
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| 轮次 |") {
			return line
		}
	}
	return ""
}

func TestApplyRoundSectionLeavesTheRestOfTheSpecAlone(t *testing.T) {
	t.Parallel()
	spec := "# 目标\n\n把三本账分开。\n"
	once := ApplyRoundSection(spec, []RoundDoc{{Number: 1, Summary: "一"}}, "")
	if !strings.HasPrefix(once, spec) {
		t.Errorf("prose was disturbed:\n%s", once)
	}

	// Closing a second round rewrites the section, not the document — and does
	// not leave the first one behind.
	twice := ApplyRoundSection(once, []RoundDoc{{Number: 1, Summary: "一"}, {Number: 2, Summary: "二"}}, "")
	if !strings.HasPrefix(twice, spec) {
		t.Errorf("prose was disturbed on the second pass:\n%s", twice)
	}
	if n := strings.Count(twice, specSectionOpen); n != 1 {
		t.Errorf("%d sections after two closes, want 1", n)
	}
	if !strings.Contains(twice, "| R2 |") {
		t.Errorf("second round missing:\n%s", twice)
	}
}

func TestApplyRoundSectionRepairsHandEditedMarkers(t *testing.T) {
	t.Parallel()
	// Someone deleted the closing marker. Appending a fresh section would
	// leave the spec claiming two sets of current conclusions.
	damaged := "# 目标\n\n" + specSectionOpen + "\n\n旧内容\n"
	fixed := ApplyRoundSection(damaged, []RoundDoc{{Number: 1, Summary: "一"}}, "")
	if n := strings.Count(fixed, specSectionOpen); n != 1 {
		t.Errorf("%d opening markers, want 1:\n%s", n, fixed)
	}
	if strings.Contains(fixed, "旧内容") {
		t.Errorf("stale content survived inside the managed section:\n%s", fixed)
	}
}

func TestAnEmptySpecStillGetsItsSection(t *testing.T) {
	t.Parallel()
	out := ApplyRoundSection("", nil, "")
	if !strings.Contains(out, specSectionOpen) || !strings.Contains(out, "尚无收口轮次") {
		t.Errorf("empty spec did not get a section:\n%s", out)
	}
}

func TestTheSpecCarriesWhatWasActuallyChecked(t *testing.T) {
	t.Parallel()
	section := RenderRoundSection([]RoundDoc{{
		Number: 1, Phase: "测试验收", Verdict: "approve",
		Summary:     "闭环跑通",
		VerifiedSHA: "66dc40e79aa1bb2c3d4e5f60718293a4b5c6d7e8",
		Evidence:    "views 4044/4044, typecheck 6/6",
		DocID:       "d1",
	}}, "")
	// An approval says someone decided; these two say what they decided on.
	// Without them a verdict is an opinion, which is the gap this closes.
	for _, want := range []string{"66dc40e7", "views 4044/4044"} {
		if !strings.Contains(section, want) {
			t.Errorf("section does not carry %q:\n%s", want, section)
		}
	}
	// The full 40 characters belong in the round document; the table is read
	// across, so it carries the short form.
	if strings.Contains(section, "66dc40e79aa1bb2c3d4e5f60718293a4b5c6d7e8") {
		t.Error("the table printed the full SHA instead of the short form")
	}
}

func TestAnUncheckedRoundSaysSoRatherThanLookingChecked(t *testing.T) {
	t.Parallel()
	section := RenderRoundSection([]RoundDoc{{
		Number: 1, Phase: "代码评审", Verdict: "approve", Summary: "没测就批", DocID: "d1",
	}}, "")
	// Empty cells read as em dashes. A blank would look like a rendering
	// glitch; this reads as "nobody recorded one", which is the fact.
	if !strings.Contains(section, "| — | — |") {
		t.Errorf("an unverified round does not show as unverified:\n%s", section)
	}
}

func TestTheWatermarkSaysWhatTheConclusionsAccountFor(t *testing.T) {
	t.Parallel()
	// Without it a reader sees the conclusions but not whether anything was
	// argued after them, so the only safe move is re-reading every comment —
	// the cost this section exists to remove.
	section := RenderRoundSection([]RoundDoc{{Number: 1, Phase: "方案评审", Summary: "定了"}},
		"2026-08-21T09:00:00Z")

	if !strings.Contains(section, "2026-08-21T09:00:00Z") {
		t.Errorf("the watermark did not travel:\n%s", section)
	}
	// It has to say what to do with it, or it reads as decoration.
	if !strings.Contains(section, "只读那之后的") {
		t.Error("the watermark must say the comments after it are the ones to read")
	}
}

func TestNoWatermarkIsBetterThanAWrongOne(t *testing.T) {
	t.Parallel()
	// A section rebuilt without a close time must not imply one. Claiming
	// conclusions are current as of some moment they are not is worse than
	// saying nothing, because the reader then skips the comments.
	section := RenderRoundSection([]RoundDoc{{Number: 1, Summary: "定了"}}, "   ")
	if strings.Contains(section, "结论计入截至") {
		t.Errorf("a blank watermark still rendered a claim:\n%s", section)
	}
}

func TestAnEmptySectionCarriesNoWatermarkEither(t *testing.T) {
	t.Parallel()
	// Nothing has been concluded, so nothing has been accounted for.
	section := RenderRoundSection(nil, "2026-08-21T09:00:00Z")
	if strings.Contains(section, "结论计入截至") {
		t.Error("a section with no rounds claimed to account for comments")
	}
	if !strings.Contains(section, "尚无收口轮次") {
		t.Error("an empty section must say it is empty")
	}
}

// The ledger records progress, not struggle. Every entry a worktree carried
// into COC-315 read "…完成", while the two detours that session actually took
// were nowhere — and of 18 closed rounds exactly one mentioned a wrong turn,
// unprompted. These two fields are where that goes, and the section below the
// table is what makes it readable across rounds.

func TestTheStruggleHistoryRendersBelowTheTable(t *testing.T) {
	t.Parallel()
	section := RenderRoundSection([]RoundDoc{
		{Number: 1, Phase: "代码评审", Verdict: "approve", Summary: "第一刀", DocID: "d1"},
		{
			Number: 2, Phase: "代码评审", Verdict: "approve", Summary: "第二刀", DocID: "d2",
			Rework: "评审发现身份端点写错，已当场修复",
			Detour: "先去 grep 了 handler，实际在 cli 那侧",
		},
	}, "")

	for _, want := range []string{
		"过程记录",
		"评审发现身份端点写错",
		"先去 grep 了 handler",
		// Which round it belongs to is the whole point: a detour with no
		// round attached cannot be counted against a station.
		"R2 代码评审",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section does not carry %q:\n%s", want, section)
		}
	}

	// The table stays seven columns. Widening it for two fields that are
	// empty on most rounds would cost every reader to serve a few rows.
	header := "| 轮次 | 节点 | 结论 | 要点 | 验收版本 | 验证证据 | 正身 |"
	if !strings.Contains(section, header) {
		t.Errorf("the round table changed shape:\n%s", section)
	}
}

func TestRoundsWithNoStruggleRenderExactlyAsBefore(t *testing.T) {
	t.Parallel()
	// Most rounds have neither field. An empty "过程记录" heading would read
	// as "nothing went wrong and someone checked", which nobody did.
	plain := []RoundDoc{{Number: 1, Phase: "方案评审", Verdict: "approve", Summary: "定了", DocID: "d1"}}
	section := RenderRoundSection(plain, "")
	if strings.Contains(section, "过程记录") {
		t.Errorf("a round with no detour still got the heading:\n%s", section)
	}
}

func TestABlankStruggleFieldIsTreatedAsAbsent(t *testing.T) {
	t.Parallel()
	// Whitespace arrives from shell quoting more often than anyone expects,
	// and a bullet reading "R1 · 返工：" is worse than no bullet.
	section := RenderRoundSection([]RoundDoc{{
		Number: 1, Phase: "代码评审", Verdict: "approve", Summary: "定了", DocID: "d1",
		Rework: "   ", Detour: "\n",
	}}, "")
	if strings.Contains(section, "过程记录") {
		t.Errorf("blank fields produced a struggle section:\n%s", section)
	}
}

func TestTheStruggleFieldsSurviveTheRoundTrip(t *testing.T) {
	t.Parallel()
	// The document is write-once and the spec is rebuilt from it, so a field
	// the writer emits but the reader does not parse is silently lost on the
	// next close — the failure mode that made this a round-trip test rather
	// than two separate ones.
	written := renderRoundBody(RoundDoc{
		Number: 3, Phase: "代码评审", Verdict: "approve", Summary: "第三刀",
		Rework: "评审发现越界，已当场修复",
		Detour: "以为是渲染层，实际在解析",
	}, "", "")

	// Read it back the way the next close does: through roundsFromDocs, the
	// only path that actually rebuilds the spec.
	parsed := roundsFromDocs("COC-315", []docRow{{
		ID:      "d3",
		Title:   "COC-315 R3 代码评审：第三刀",
		Kind:    "COC-315/rounds/R3-代码评审",
		Content: written,
	}})
	if len(parsed) != 1 {
		t.Fatalf("expected one round back, got %d", len(parsed))
	}
	back := parsed[0]
	if back.Rework != "评审发现越界，已当场修复" {
		t.Errorf("rework did not survive: %q\n%s", back.Rework, written)
	}
	if back.Detour != "以为是渲染层，实际在解析" {
		t.Errorf("detour did not survive: %q\n%s", back.Detour, written)
	}
	if !strings.Contains(RenderRoundSection([]RoundDoc{back}, ""), "评审发现越界") {
		t.Error("a parsed round did not reach the spec's struggle section")
	}
}

func TestTheRoundBeingClosedReachesTheSpecWithEveryField(t *testing.T) {
	t.Parallel()
	// The spec used to get a hand-assembled copy of the round being closed
	// while the document got the real one. A field added to the document was
	// then missing from the table until some LATER close happened to re-read
	// it from disk — which is how the first two struggle entries written on
	// this card appeared one round late.
	//
	// Constructing the value once is the fix. This asserts the property that
	// makes it a fix: whatever renderRoundBody was given is what the spec
	// renders, with no field dropped in between.
	closing := RoundDoc{
		Number: 1, Phase: "代码评审", Verdict: "approve", Summary: "收口",
		VerifiedSHA: "57dd42b30b72de6638b27f2409d81f85eb2e505e",
		Evidence:    "全绿",
		Rework:      "评论端点返回裸数组，我按信封解",
		Detour:      "已是最新的分支提前 return，没登记",
		DocID:       "doc-1",
	}
	body := renderRoundBody(closing, "", "")
	section := RenderRoundSection([]RoundDoc{closing}, "")

	for _, want := range []string{"评论端点返回裸数组", "已是最新的分支提前 return"} {
		if !strings.Contains(body, want) {
			t.Errorf("the round document lost %q:\n%s", want, body)
		}
		if !strings.Contains(section, want) {
			t.Errorf("the spec lost %q on the close that wrote it:\n%s", want, section)
		}
	}
}
