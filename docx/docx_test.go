package docx

import (
	"strings"
	"testing"
)

func TestRenderParagraphUsesStyleMapping(t *testing.T) {
	cfg := DefaultConfig()
	got := renderParagraph("Heading content", "Heading 1", cfg)
	if !strings.Contains(got, `\section{Heading content}`) {
		t.Fatalf("expected heading mapping, got %q", got)
	}
}

func TestResolveStyleNameUsesStylesXMLMap(t *testing.T) {
	resolved := resolveStyleName("2", map[string]string{"2": "heading 1"})
	if resolved != "heading 1" {
		t.Fatalf("expected style id to resolve via styles.xml map, got %q", resolved)
	}
}

func TestRenderImageRefsIncludeGraphicsMode(t *testing.T) {
	got := renderImageRefs([]string{"img-a.png", "img-b.png"}, ImageConfig{Mode: "includegraphics"})
	if !strings.Contains(got, `\includegraphics{img-a.png}`) || !strings.Contains(got, `\includegraphics{img-b.png}`) {
		t.Fatalf("unexpected image rendering: %q", got)
	}
}

func TestUniqueAssetNameAvoidsCollisionsForDifferentTargets(t *testing.T) {
	data := []byte("same-bytes")
	a := uniqueAssetName("media/image1.png", data)
	b := uniqueAssetName("headers/image1.png", data)
	if a == b {
		t.Fatalf("expected unique names for different targets, got %q", a)
	}
}

func TestIsBadLatexReturnsReason(t *testing.T) {
	bad, reason := isBadLatex(`\frac{1}{+}`)
	if !bad {
		t.Fatal("expected latex to be flagged as bad")
	}
	if reason == "" {
		t.Fatal("expected a reason for bad latex")
	}
}

func TestRenderParagraphBlocksGroupsEnumerate(t *testing.T) {
	cfg := DefaultConfig()
	report := newReport("test")
	out := renderParagraphBlocks([]paragraphBlock{
		{Content: "计算题（每题4分，共40分）", Style: ""},
		{Content: "第一题", List: &listRef{NumID: "1", Level: 0, Def: numberingLevel{Environment: "enumerate"}}},
		{Content: "第二题", List: &listRef{NumID: "1", Level: 0, Def: numberingLevel{Environment: "enumerate"}}},
		{Content: "普通段落", Style: ""},
	}, cfg, &report)
	if !strings.Contains(out, `\subsection*{计算题（每题4分，共40分）}`) {
		t.Fatalf("expected worksheet section title, got %q", out)
	}
	if !strings.Contains(out, `\begin{enumerate}`) || !strings.Contains(out, `\item 第一题`) || !strings.Contains(out, `\item 第二题`) {
		t.Fatalf("expected enumerate structure, got %q", out)
	}
	if report.Summary.ListItems != 2 {
		t.Fatalf("expected 2 list items, got %d", report.Summary.ListItems)
	}
}

func TestListEnvironmentForBullet(t *testing.T) {
	if got := listEnvironmentFor("bullet"); got != "itemize" {
		t.Fatalf("expected itemize, got %q", got)
	}
	if got := listEnvironmentFor("decimal"); got != "enumerate" {
		t.Fatalf("expected enumerate, got %q", got)
	}
}

func TestParseTextualListMarker(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		out  string
		want bool
	}{
		{"1、第一题", 1, "第一题", true},
		{"（2）第二问", 2, "第二问", true},
		{"3. third item", 3, "third item", true},
		{"1.5 不是列表", 0, "", false},
	}
	for _, tc := range cases {
		n, out, ok := parseTextualListMarker(tc.in)
		if ok != tc.want || n != tc.n || out != tc.out {
			t.Fatalf("parseTextualListMarker(%q) = (%d, %q, %v), want (%d, %q, %v)", tc.in, n, out, ok, tc.n, tc.out, tc.want)
		}
	}
}

func TestPromoteTextualListsInWorksheetSection(t *testing.T) {
	cfg := DefaultConfig()
	paragraphs := []paragraphBlock{
		{Content: "计算题（每题4分，共40分）"},
		{Content: "1、第一题"},
		{Content: "2、第二题"},
		{Content: "普通段落"},
	}
	got := promoteTextualLists(paragraphs, cfg)
	if got[1].List == nil || got[2].List == nil {
		t.Fatalf("expected textual list promotion, got %#v %#v", got[1], got[2])
	}
	if got[1].Content != "第一题" || got[2].Content != "第二题" {
		t.Fatalf("expected stripped marker content, got %q and %q", got[1].Content, got[2].Content)
	}
}

func TestPromoteTextualListsAllowsContinuationBetweenItems(t *testing.T) {
	cfg := DefaultConfig()
	paragraphs := []paragraphBlock{
		{Content: "计算题（每题4分，共40分）"},
		{Content: "1、第一题"},
		{Content: "题目补充说明"},
		{Content: "2、第二题"},
		{Content: "第二题补充说明"},
	}
	got := promoteTextualLists(paragraphs, cfg)
	if got[1].List == nil || got[3].List == nil {
		t.Fatalf("expected list items to be promoted, got %#v %#v", got[1], got[3])
	}
	if !got[2].ListContinuation || !got[4].ListContinuation {
		t.Fatalf("expected continuation paragraphs, got %#v %#v", got[2], got[4])
	}
}

func TestPromoteTextualListsNeedsLongerRunOutsideWorksheet(t *testing.T) {
	cfg := DefaultConfig()
	paragraphs := []paragraphBlock{
		{Content: "1、第一项"},
		{Content: "2、第二项"},
	}
	got := promoteTextualLists(paragraphs, cfg)
	if got[0].List != nil || got[1].List != nil {
		t.Fatalf("expected no promotion outside worksheet for short run, got %#v %#v", got[0], got[1])
	}
}
