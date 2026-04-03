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

func TestRenderLatexForNaryMatrixEqArrayAndAccent(t *testing.T) {
	node := Group(
		Nary(`\sum`, Token("i=1"), Token("n"), Token("x_i")),
		Matrix("pmatrix", [][]*Node{{Token("a"), Token("b")}, {Token("c"), Token("d")}}),
		EqArray([][]*Node{{Token("x"), Token("1")}, {Token("y"), Token("2")}}),
		Accent(`\vec`, Token("v")),
	)

	got := RenderLatex(node)
	want := `\sum_{i=1}^{n}{x_i}\begin{pmatrix}a & b\\c & d\end{pmatrix}x = 1\\y = 2\vec{v}`
	if got != want {
		t.Fatalf("RenderLatex() = %q, want %q", got, want)
	}
}
