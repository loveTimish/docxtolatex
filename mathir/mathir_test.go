package mathir

import "testing"

func TestRenderLatexForStructuredNodes(t *testing.T) {
	node := Group(
		Token("f"),
		Fraction(Token("a"), Token("b")),
		SubSup(Token("x"), Token("i"), Token("2")),
		Fence("(", ")", Token("y")),
	)

	got := RenderLatex(node)
	want := `f\frac{a}{b}{x}_{i}^{2}\left(y\right)`
	if got != want {
		t.Fatalf("RenderLatex() = %q, want %q", got, want)
	}
}

func TestRenderLatexForRawNode(t *testing.T) {
	node := RawLatex(`\sqrt{x}`)
	if got := RenderLatex(node); got != `\sqrt{x}` {
		t.Fatalf("RenderLatex(raw) = %q, want %q", got, `\sqrt{x}`)
	}
}
