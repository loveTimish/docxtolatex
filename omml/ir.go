package omml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/zhexiao/mtef-go/mathir"
)

// ParseToIR parses a standalone OMML snippet into a minimal MathIR tree.
// Unsupported subtrees fall back to raw LaTeX nodes so the old string path can stay untouched.
func ParseToIR(data []byte) (*mathir.Node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("OMML snippet is empty")
			}
			return nil, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			return parseNodeFromStart(start, dec)
		}
	}
}

// ParseToIRString is a small test-friendly wrapper around ParseToIR.
func ParseToIRString(data string) (*mathir.Node, error) {
	return ParseToIR([]byte(data))
}

// ConvertElementToIR consumes the current OMML element from a streaming decoder and lowers it into MathIR.
func ConvertElementToIR(start xml.StartElement, dec *xml.Decoder) (*mathir.Node, error) {
	data, err := captureElement(start, dec)
	if err != nil {
		return nil, err
	}
	return ParseToIR(data)
}

func parseNodeFromStart(start xml.StartElement, dec *xml.Decoder) (*mathir.Node, error) {
	switch start.Name.Local {
	case "t":
		var txt string
		if err := dec.DecodeElement(&txt, &start); err != nil {
			return nil, err
		}
		return mathir.Token(escapeLatex(txt)), nil
	case "r", "oMath", "oMathPara", "e":
		return parseGroup(start.Name.Local, dec)
	case "f":
		return parseFraction(dec)
	case "sSup", "sSub", "sSubSup":
		return parseSubSup(start.Name.Local, dec)
	case "d":
		return parseFence(dec)
	case "rad":
		return parseRad(dec)
	default:
		return fallbackRawNode(start, dec)
	}
}

func parseGroup(local string, dec *xml.Decoder) (*mathir.Node, error) {
	children := make([]*mathir.Node, 0, 4)
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			child, err := parseNodeFromStart(el, dec)
			if err != nil {
				return nil, err
			}
			if child != nil {
				children = append(children, child)
			}
		case xml.EndElement:
			if el.Name.Local == local {
				return mathir.Group(children...), nil
			}
		}
	}
}

func parseContainer(local string, dec *xml.Decoder) (*mathir.Node, error) {
	children := make([]*mathir.Node, 0, 2)
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			child, err := parseNodeFromStart(el, dec)
			if err != nil {
				return nil, err
			}
			if child != nil {
				children = append(children, child)
			}
		case xml.EndElement:
			if el.Name.Local == local {
				return mathir.Group(children...), nil
			}
		}
	}
}

func parseFraction(dec *xml.Decoder) (*mathir.Node, error) {
	var num, den *mathir.Node
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "num":
				num, err = parseContainer("num", dec)
			case "den":
				den, err = parseContainer("den", dec)
			default:
				_, err = parseNodeFromStart(el, dec)
			}
			if err != nil {
				return nil, err
			}
		case xml.EndElement:
			if el.Name.Local == "f" {
				return mathir.Fraction(num, den), nil
			}
		}
	}
}

func parseSubSup(local string, dec *xml.Decoder) (*mathir.Node, error) {
	var base, sub, sup *mathir.Node
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "e":
				base, err = parseContainer("e", dec)
			case "sub":
				sub, err = parseContainer("sub", dec)
			case "sup":
				sup, err = parseContainer("sup", dec)
			default:
				_, err = parseNodeFromStart(el, dec)
			}
			if err != nil {
				return nil, err
			}
		case xml.EndElement:
			if el.Name.Local == local {
				return mathir.SubSup(base, sub, sup), nil
			}
		}
	}
}

func parseFence(dec *xml.Decoder) (*mathir.Node, error) {
	open, close := "", ""
	var inner *mathir.Node
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "dPr":
				open, close, _ = parseDelimPr(dec)
			case "e":
				inner, err = parseContainer("e", dec)
			default:
				_, err = parseNodeFromStart(el, dec)
			}
			if err != nil {
				return nil, err
			}
		case xml.EndElement:
			if el.Name.Local == "d" {
				return mathir.Fence(open, close, inner), nil
			}
		}
	}
}

func parseRad(dec *xml.Decoder) (*mathir.Node, error) {
	var deg, inner *mathir.Node
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "deg":
				deg, err = parseContainer("deg", dec)
			case "e":
				inner, err = parseContainer("e", dec)
			default:
				_, err = parseNodeFromStart(el, dec)
			}
			if err != nil {
				return nil, err
			}
		case xml.EndElement:
			if el.Name.Local == "rad" {
				if deg != nil && strings.TrimSpace(mathir.RenderLatex(deg)) != "" {
					return mathir.RawLatex(`\sqrt[` + mathir.RenderLatex(deg) + `]{` + mathir.RenderLatex(inner) + `}`), nil
				}
				return mathir.RawLatex(`\sqrt{` + mathir.RenderLatex(inner) + `}`), nil
			}
		}
	}
}

func fallbackRawNode(start xml.StartElement, dec *xml.Decoder) (*mathir.Node, error) {
	data, err := captureElement(start, dec)
	if err != nil {
		return nil, err
	}
	latex, err := convertCapturedToLatex(data)
	if err != nil {
		return nil, err
	}
	return mathir.RawLatex(strings.TrimSpace(latex)), nil
}

func captureElement(start xml.StartElement, dec *xml.Decoder) ([]byte, error) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	if err := enc.EncodeToken(start); err != nil {
		return nil, err
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
		if err := enc.EncodeToken(tok); err != nil {
			return nil, err
		}
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func convertCapturedToLatex(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		if start, ok := tok.(xml.StartElement); ok {
			return ConvertElement(start, dec)
		}
	}
}
