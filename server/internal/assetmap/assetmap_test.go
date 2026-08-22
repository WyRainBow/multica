package assetmap

import (
	"strings"
	"testing"
)

// One answer, two deliveries. These tests pin the parts both surfaces depend
// on being identical, because the failure this package exists to prevent is
// the same asset described two ways.

func TestTheMapNamesWithoutQuoting(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	RenderGroups(&b, "intro line", []Group{{
		Label: "经验案例", When: "撞到类似问题时先看这里",
		Docs: []Doc{{ID: "c1", Title: "跳过的测试和通过的测试长得一样"}},
	}})
	out := b.String()
	for _, want := range []string{"intro line", "**经验案例**", "撞到类似问题时先看这里", "跳过的测试和通过的测试长得一样", "`c1`"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestAnEmptyGroupPrintsNoHeading(t *testing.T) {
	t.Parallel()
	// A heading with nothing under it claims the folder is empty rather than
	// absent, which is a different thing.
	var b strings.Builder
	RenderGroups(&b, "", []Group{{Label: "空的"}, {Label: "有的", Docs: []Doc{{ID: "x", Title: "一条"}}}})
	out := b.String()
	if strings.Contains(out, "**空的**") {
		t.Error("an empty group printed its heading")
	}
	if !strings.Contains(out, "**有的**") {
		t.Error("a non-empty group was dropped")
	}
}

func TestTruncationSaysWhatItLeftOut(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	RenderGroups(&b, "", []Group{{Label: "指南", Docs: []Doc{{ID: "g1", Title: "第一份"}}, Dropped: 9}})
	out := b.String()
	if !strings.Contains(out, "…9 more") {
		t.Errorf("truncation must state the count:\n%s", out)
	}
	if !strings.Contains(out, "multica wiki list --kind") {
		t.Error("a truncated list must say how to see the rest")
	}
}

func TestATitlelessDocumentIsStillListed(t *testing.T) {
	t.Parallel()
	// It remains findable by id, and dropping the row would silently shrink
	// the index.
	var b strings.Builder
	RenderGroups(&b, "", []Group{{Label: "L", Docs: []Doc{{ID: "x", Title: "  "}}}})
	if !strings.Contains(b.String(), "(untitled)") {
		t.Error("a titleless document lost its row")
	}
}

func TestHasAnyIgnoresEmptyGroups(t *testing.T) {
	t.Parallel()
	if HasAny(nil) || HasAny([]Group{{Label: "a"}, {Label: "b"}}) {
		t.Error("groups with no documents must not count as content")
	}
	if !HasAny([]Group{{Label: "a"}, {Label: "b", Docs: []Doc{{ID: "x"}}}}) {
		t.Error("one document anywhere is content")
	}
}

func TestEverySurfaceCarriesTheSourceOfTruthNotice(t *testing.T) {
	t.Parallel()
	// The map is titles and prose, and prose about code goes stale without
	// anyone editing it.
	if !strings.Contains(SourceOfTruthNotice, "以源码为准") {
		t.Errorf("the notice must point at the source: %q", SourceOfTruthNotice)
	}
}

func TestAnOversizedGroupWarnsBeforeItBecomesNoise(t *testing.T) {
	t.Parallel()
	// A list does not fail on a particular day. It gets gradually longer until
	// somebody notices it has become noise, and by then it has been noise for
	// a while — so the warning has to come from the list itself.
	docs := make([]Doc, ComfortableIndexSize+1)
	for i := range docs {
		docs[i] = Doc{ID: "d", Title: "t"}
	}
	var b strings.Builder
	RenderGroups(&b, "", []Group{{Label: "经验案例", Docs: docs}})
	out := b.String()
	if !strings.Contains(out, "超出 names-only 的舒适区") {
		t.Errorf("an oversized group must warn:\n%s", out)
	}
	if !strings.Contains(out, "挑选层") {
		t.Error("the warning must name what to do about it")
	}
}

func TestTheWarningCountsWhatWasDroppedToo(t *testing.T) {
	t.Parallel()
	// The cap hides entries; if only the shown ones counted, a folder of 200
	// truncated to 25 would report itself comfortable forever.
	var b strings.Builder
	RenderGroups(&b, "", []Group{{
		Label: "指南",
		Docs:  []Doc{{ID: "a", Title: "one"}},
		Dropped: ComfortableIndexSize,
	}})
	if !strings.Contains(b.String(), "超出 names-only 的舒适区") {
		t.Error("the threshold must count the full set, not the visible part")
	}
}

func TestAComfortableGroupSaysNothing(t *testing.T) {
	t.Parallel()
	// Twenty items need no selection layer, and a warning printed on every run
	// is one people stop reading before the day it matters.
	docs := make([]Doc, ComfortableIndexSize)
	for i := range docs {
		docs[i] = Doc{ID: "d", Title: "t"}
	}
	var b strings.Builder
	RenderGroups(&b, "", []Group{{Label: "指南", Docs: docs}})
	if strings.Contains(b.String(), "舒适区") {
		t.Error("a group at the threshold must not warn; only past it")
	}
}
