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
