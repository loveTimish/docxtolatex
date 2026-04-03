package mathir

import "strings"

type NodeKind string

const (
	NodeToken    NodeKind = "token"
	NodeGroup    NodeKind = "group"
	NodeFrac     NodeKind = "frac"
	NodeSubSup   NodeKind = "subsup"
	NodeFence    NodeKind = "fence"
	NodeNary     NodeKind = "nary"
	NodeMatrix   NodeKind = "matrix"
	NodeEqArray  NodeKind = "eq-array"
	NodeAccent   NodeKind = "accent"
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
	Operator    string
	Lower       *Node
	Upper       *Node
	Rows        [][]*Node
	Environment string
	Command     string
	Operand     *Node
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

func Nary(operator string, lower, upper, body *Node) *Node {
	return &Node{Kind: NodeNary, Operator: operator, Lower: lower, Upper: upper, Operand: body}
}

func Matrix(environment string, rows [][]*Node) *Node {
	return &Node{Kind: NodeMatrix, Environment: environment, Rows: rows}
}

func EqArray(rows [][]*Node) *Node {
	return &Node{Kind: NodeEqArray, Rows: rows}
}

func Accent(command string, operand *Node) *Node {
	return &Node{Kind: NodeAccent, Command: command, Operand: operand}
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
	case NodeNary:
		op := node.Operator
		if op == "" {
			op = `\sum`
		}
		var b strings.Builder
		b.WriteString(op)
		if node.Lower != nil {
			b.WriteString("_{")
			b.WriteString(RenderLatex(node.Lower))
			b.WriteString("}")
		}
		if node.Upper != nil {
			b.WriteString("^{")
			b.WriteString(RenderLatex(node.Upper))
			b.WriteString("}")
		}
		b.WriteString("{")
		b.WriteString(RenderLatex(node.Operand))
		b.WriteString("}")
		return b.String()
	case NodeMatrix:
		env := node.Environment
		if env == "" {
			env = "matrix"
		}
		var b strings.Builder
		b.WriteString(`\begin{`)
		b.WriteString(env)
		b.WriteString("}")
		for i, row := range node.Rows {
			if i > 0 {
				b.WriteString(`\\`)
			}
			for j, cell := range row {
				if j > 0 {
					b.WriteString(" & ")
				}
				b.WriteString(RenderLatex(cell))
			}
		}
		b.WriteString(`\end{`)
		b.WriteString(env)
		b.WriteString("}")
		return b.String()
	case NodeEqArray:
		var b strings.Builder
		for i, row := range node.Rows {
			if i > 0 {
				b.WriteString(`\\`)
			}
			if len(row) == 2 {
				b.WriteString(RenderLatex(row[0]))
				b.WriteString(" = ")
				b.WriteString(RenderLatex(row[1]))
				continue
			}
			parts := make([]string, 0, len(row))
			for _, cell := range row {
				parts = append(parts, RenderLatex(cell))
			}
			b.WriteString(strings.Join(parts, " "))
		}
		return b.String()
	case NodeAccent:
		cmd := node.Command
		if cmd == "" {
			cmd = `\bar`
		}
		return cmd + `{` + RenderLatex(node.Operand) + `}`
	default:
		return node.Text
	}
}
