package mathir

import "strings"

type NodeKind string

const (
	NodeToken    NodeKind = "token"
	NodeGroup    NodeKind = "group"
	NodeFrac     NodeKind = "frac"
	NodeSubSup   NodeKind = "subsup"
	NodeFence    NodeKind = "fence"
	NodeRawLatex NodeKind = "raw-latex"
)

type Node struct {
	Kind        NodeKind
	Text        string
	Children    []*Node
	Numerator   *Node
	Denominator *Node
	Base        *Node
	Subscript   *Node
	Superscript *Node
	Open        string
	Close       string
	Inner       *Node
}

func Token(text string) *Node {
	return &Node{Kind: NodeToken, Text: text}
}

func Group(children ...*Node) *Node {
	filtered := make([]*Node, 0, len(children))
	for _, child := range children {
		if child != nil {
			filtered = append(filtered, child)
		}
	}
	return &Node{Kind: NodeGroup, Children: filtered}
}

func Fraction(num, den *Node) *Node {
	return &Node{Kind: NodeFrac, Numerator: num, Denominator: den}
}

func SubSup(base, sub, sup *Node) *Node {
	return &Node{Kind: NodeSubSup, Base: base, Subscript: sub, Superscript: sup}
}

func Fence(open, close string, inner *Node) *Node {
	return &Node{Kind: NodeFence, Open: open, Close: close, Inner: inner}
}

func RawLatex(latex string) *Node {
	return &Node{Kind: NodeRawLatex, Text: latex}
}

func RenderLatex(node *Node) string {
	if node == nil {
		return ""
	}

	switch node.Kind {
	case NodeToken, NodeRawLatex:
		return node.Text
	case NodeGroup:
		parts := make([]string, 0, len(node.Children))
		for _, child := range node.Children {
			parts = append(parts, RenderLatex(child))
		}
		return strings.Join(parts, "")
	case NodeFrac:
		return `\frac{` + RenderLatex(node.Numerator) + `}{` + RenderLatex(node.Denominator) + `}`
	case NodeSubSup:
		var b strings.Builder
		b.WriteString("{")
		b.WriteString(RenderLatex(node.Base))
		b.WriteString("}")
		if node.Subscript != nil {
			b.WriteString("_{")
			b.WriteString(RenderLatex(node.Subscript))
			b.WriteString("}")
		}
		if node.Superscript != nil {
			b.WriteString("^{")
			b.WriteString(RenderLatex(node.Superscript))
			b.WriteString("}")
		}
		return b.String()
	case NodeFence:
		open := node.Open
		close := node.Close
		if open == "" {
			open = "("
		}
		if close == "" {
			close = ")"
		}
		return `\left` + open + RenderLatex(node.Inner) + `\right` + close
	default:
		return node.Text
	}
}
