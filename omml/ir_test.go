package omml

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/zhexiao/mtef-go/mathir"
)

func TestParseToIRFraction(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:f><m:num><m:r><m:t>a</m:t></m:r></m:num><m:den><m:r><m:t>b</m:t></m:r></m:den></m:f></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `\frac{a}{b}` {
		t.Fatalf("expected fraction latex, got %q", got)
	}
}

func TestParseToIRSubSup(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:sSubSup><m:e><m:r><m:t>x</m:t></m:r></m:e><m:sub><m:r><m:t>i</m:t></m:r></m:sub><m:sup><m:r><m:t>2</m:t></m:r></m:sup></m:sSubSup></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `{x}_{i}^{2}` {
		t.Fatalf("expected subsup latex, got %q", got)
	}
}

func TestParseToIRFence(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:d><m:dPr><m:begChr m:val="("/><m:endChr m:val=")"/></m:dPr><m:e><m:r><m:t>x</m:t></m:r></m:e></m:d></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `\left(x\right)` {
		t.Fatalf("expected fence latex, got %q", got)
	}
}

func TestParseToIRFallsBackToRawLatexForUnsupportedNode(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:rad><m:e><m:r><m:t>x</m:t></m:r></m:e></m:rad></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `\sqrt{x}` {
		t.Fatalf("expected raw fallback latex, got %q", got)
	}
}

func TestConvertElementToIRWorksWithStreamingDecoder(t *testing.T) {
	input := `<root xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:oMath><m:f><m:num><m:r><m:t>1</m:t></m:r></m:num><m:den><m:r><m:t>2</m:t></m:r></m:den></m:f></m:oMath><tail>ok</tail></root>`
	dec := xml.NewDecoder(strings.NewReader(input))
	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("unexpected decoder error before oMath: %v", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "oMath" {
			continue
		}
		node, err := ConvertElementToIR(start, dec)
		if err != nil {
			t.Fatalf("ConvertElementToIR returned error: %v", err)
		}
		if got := mathir.RenderLatex(node); got != `\frac{1}{2}` {
			t.Fatalf("expected streaming fraction latex, got %q", got)
		}
		break
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("unexpected decoder error after oMath: %v", err)
		}
		start, ok := tok.(xml.StartElement)
		if ok && start.Name.Local == "tail" {
			var tail string
			if err := dec.DecodeElement(&tail, &start); err != nil {
				t.Fatalf("DecodeElement tail returned error: %v", err)
			}
			if tail != "ok" {
				t.Fatalf("expected decoder to continue after oMath, got %q", tail)
			}
			return
		}
	}
}

func TestParseToIRNary(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:nary><m:naryPr><m:chr m:val="∑"/></m:naryPr><m:sub><m:r><m:t>i=1</m:t></m:r></m:sub><m:sup><m:r><m:t>n</m:t></m:r></m:sup><m:e><m:r><m:t>x</m:t></m:r></m:e></m:nary></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `\sum_{i=1}^{n}{x}` {
		t.Fatalf("expected nary latex, got %q", got)
	}
}

func TestParseToIRMatrix(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:m><m:mPr><m:brk m:val="["/></m:mPr><m:mr><m:e><m:r><m:t>a</m:t></m:r></m:e><m:e><m:r><m:t>b</m:t></m:r></m:e></m:mr><m:mr><m:e><m:r><m:t>c</m:t></m:r></m:e><m:e><m:r><m:t>d</m:t></m:r></m:e></m:mr></m:m></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `\begin{bmatrix}a & b\\c & d\end{bmatrix}` {
		t.Fatalf("expected matrix latex, got %q", got)
	}
}

func TestParseToIREqArray(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:eqArr><m:mr><m:e><m:r><m:t>x</m:t></m:r></m:e><m:e><m:r><m:t>1</m:t></m:r></m:e></m:mr><m:mr><m:e><m:r><m:t>y</m:t></m:r></m:e><m:e><m:r><m:t>2</m:t></m:r></m:e></m:mr></m:eqArr></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `x = 1\\y = 2` {
		t.Fatalf("expected eqArr latex, got %q", got)
	}
}

func TestParseToIRAccent(t *testing.T) {
	input := `<m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:acc><m:accPr><m:chr m:val="→"/></m:accPr><m:e><m:r><m:t>AB</m:t></m:r></m:e></m:acc></m:oMath>`
	node, err := ParseToIRString(input)
	if err != nil {
		t.Fatalf("ParseToIRString returned error: %v", err)
	}
	if got := mathir.RenderLatex(node); got != `\vec{AB}` {
		t.Fatalf("expected accent latex, got %q", got)
	}
}
