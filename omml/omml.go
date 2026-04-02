package omml

import (
	"bytes"
	"encoding/xml"
	"github.com/zhexiao/mtef-go/latexmap"
	"strings"
	"unicode"
)

// ConvertElement converts an OMML math element (oMath/oMathPara) to KaTeX-friendly LaTeX.
func ConvertElement(start xml.StartElement, dec *xml.Decoder) (string, error) {
	var buf bytes.Buffer
	if err := walk(&buf, start, dec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// walk recursively parses OMML nodes into LaTeX.
func walk(buf *bytes.Buffer, start xml.StartElement, dec *xml.Decoder) error {
	switch start.Name.Local {
	case "t": // text
		var txt string
		if err := dec.DecodeElement(&txt, &start); err != nil {
			return err
		}
		buf.WriteString(escapeLatex(txt))
	case "r": // run
		for {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				if err := walk(buf, el, dec); err != nil {
					return err
				}
			case xml.EndElement:
				if el.Name.Local == "r" {
					return nil
				}
			}
		}
	case "oMathPara", "oMath":
		for {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				if err := walk(buf, el, dec); err != nil {
					return err
				}
			case xml.EndElement:
				if el.Name.Local == start.Name.Local {
					return nil
				}
			}
		}
	case "f": // fraction
		var num, den bytes.Buffer
		if err := consumeTo(&num, dec, "num"); err != nil {
			return err
		}
		if err := consumeTo(&den, dec, "den"); err != nil {
			return err
		}
		buf.WriteString(`\frac{` + num.String() + `}{` + den.String() + `}`)
	case "sSup", "sSub", "sSubSup": // superscript/subscript/both
		var base, sup, sub bytes.Buffer
		if err := consumeTo(&base, dec, "e"); err != nil {
			return err
		}
		if start.Name.Local == "sSup" || start.Name.Local == "sSubSup" {
			_ = consumeTo(&sup, dec, "sup")
		}
		if start.Name.Local == "sSub" || start.Name.Local == "sSubSup" {
			_ = consumeTo(&sub, dec, "sub")
		}
		buf.WriteString("{" + base.String() + "}")
		if sub.Len() > 0 {
			buf.WriteString("_{" + sub.String() + "}")
		}
		if sup.Len() > 0 {
			buf.WriteString("^{" + sup.String() + "}")
		}
	case "e": // generic container
		for {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				if err := walk(buf, el, dec); err != nil {
					return err
				}
			case xml.EndElement:
				if el.Name.Local == "e" {
					return nil
				}
			}
		}
	case "rad": // root
		var deg, e bytes.Buffer
		_ = consumeTo(&deg, dec, "deg")
		if err := consumeTo(&e, dec, "e"); err != nil {
			return err
		}
		if deg.Len() > 0 {
			buf.WriteString(`\sqrt[` + deg.String() + `]{` + e.String() + `}`)
		} else {
			buf.WriteString(`\sqrt{` + e.String() + `}`)
		}
	case "bar": // overline/underline
		dir := attrVal(start.Attr, "pos")
		var e bytes.Buffer
		if err := consumeTo(&e, dec, "e"); err != nil {
			return err
		}
		if dir == "top" || dir == "" {
			buf.WriteString(`\overline{` + e.String() + `}`)
		} else {
			buf.WriteString(`\underline{` + e.String() + `}`)
		}
	case "acc": // accent
		var e bytes.Buffer
		accent := ""
		for {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				switch el.Name.Local {
				case "accPr":
					accent = parseAccPr(dec)
				case "e":
					if err := walk(&e, el, dec); err != nil {
						return err
					}
				default:
					if err := walk(&e, el, dec); err != nil {
						return err
					}
				}
			case xml.EndElement:
				if el.Name.Local == "acc" {
					goto doneAcc
				}
			}
		}
	doneAcc:
		switch accent {
		case "^":
			buf.WriteString(`\hat{` + e.String() + `}`)
		case "\u2192", "\u20d7":
			buf.WriteString(`\vec{` + e.String() + `}`)
		default:
			buf.WriteString(`\bar{` + e.String() + `}`)
		}
	case "d": // delimiter
		// In Word OMML, delimiter chars are usually stored under <dPr><begChr/ endChr/ sepChr val="..."/></dPr>,
		// not as attributes of <d>. Parse them explicitly for correctness (e.g. absolute value bars).
		open, close, sep := "", "", ""
		var e bytes.Buffer
		for {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				switch el.Name.Local {
				case "dPr":
					open, close, sep = parseDelimPr(dec)
				case "e":
					if err := walk(&e, el, dec); err != nil {
						return err
					}
				default:
					// Consume/ignore other nodes for robustness.
					if err := walk(&e, el, dec); err != nil {
						return err
					}
				}
			case xml.EndElement:
				if el.Name.Local == "d" {
					goto doneDelim
				}
			}
		}
	doneDelim:
		if open == "" {
			open = "("
		}
		if close == "" {
			close = ")"
		}
		content := strings.TrimSpace(e.String())
		if strings.HasPrefix(content, `\begin{cases}`) {
			buf.WriteString(content)
			return nil
		}
		if sep != "" {
			parts := strings.Split(e.String(), string(sep))
			buf.WriteString(`\left` + open + strings.Join(parts, `\middle`+sep) + `\right` + close)
		} else {
			buf.WriteString(`\left` + open + content + `\right` + close)
		}
	case "nary": // summation/integral/product/...
		sym := mapNarySymbol(start)
		var sup, sub, e bytes.Buffer
		for {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				switch el.Name.Local {
				case "naryPr":
					if chr := parseNaryChr(dec); chr != "" {
						sym = mapNaryChr(chr)
					}
				case "sub":
					if err := walk(&sub, el, dec); err != nil {
						return err
					}
				case "sup":
					if err := walk(&sup, el, dec); err != nil {
						return err
					}
				case "e":
					if err := walk(&e, el, dec); err != nil {
						return err
					}
				default:
					if err := walk(&e, el, dec); err != nil {
						return err
					}
				}
			case xml.EndElement:
				if el.Name.Local == "nary" {
					goto doneNary
				}
			}
		}
	doneNary:
		if sym == "" {
			sym = `\sum`
		}
		buf.WriteString(sym)
		if sub.Len() > 0 {
			buf.WriteString("_{" + sub.String() + "}")
		}
		if sup.Len() > 0 {
			buf.WriteString("^{" + sup.String() + "}")
		}
		buf.WriteString("{" + e.String() + "}")
	case "limLow", "limUpp": // limits
		var base, lim bytes.Buffer
		if err := consumeTo(&base, dec, "e"); err != nil {
			return err
		}
		_ = consumeTo(&lim, dec, "lim")
		if start.Name.Local == "limLow" {
			buf.WriteString(`\lim_{` + lim.String() + `}` + base.String())
		} else {
			buf.WriteString(`\lim^{` + lim.String() + `}` + base.String())
		}
	case "func": // function name + argument
		var name, arg bytes.Buffer
		_ = consumeTo(&name, dec, "fName")
		if err := consumeTo(&arg, dec, "e"); err != nil {
			return err
		}
		buf.WriteString(formatFuncCall(name.String(), arg.String()))
	case "sPre": // prescripts
		var sub, sup, e bytes.Buffer
		_ = consumeTo(&sub, dec, "sub")
		_ = consumeTo(&sup, dec, "sup")
		if err := consumeTo(&e, dec, "e"); err != nil {
			return err
		}
		buf.WriteString(`_{` + sub.String() + `}^{` + sup.String() + `}` + e.String())
	case "groupChr": // overbrace/underbrace
		pos := attrVal(start.Attr, "pos")
		chr := attrVal(start.Attr, "chr")
		var e bytes.Buffer
		if err := consumeTo(&e, dec, "e"); err != nil {
			return err
		}
		if pos == "top" {
			if chr == "\u2322" {
				buf.WriteString(`\overbrace{` + e.String() + `}`)
			} else {
				buf.WriteString(`\overline{` + e.String() + `}`)
			}
		} else {
			if chr == "\u2322" {
				buf.WriteString(`\underbrace{` + e.String() + `}`)
			} else {
				buf.WriteString(`\underline{` + e.String() + `}`)
			}
		}
	case "m", "matrix": // matrix
		rows, brk := parseMatrix(dec)
		if shouldFlattenMatrix(rows) {
			for _, r := range rows {
				line := strings.TrimSpace(formatLineRow(r))
				if line == "" {
					continue
				}
				buf.WriteString("$")
				buf.WriteString(line)
				buf.WriteString("$\n")
			}
		} else {
			env := matrixEnv(brk)
			buf.WriteString(`\begin{` + env + `}`)
			for i, r := range rows {
				if i > 0 {
					buf.WriteString(`\\`)
				}
				buf.WriteString(strings.Join(r, " & "))
			}
			buf.WriteString(`\end{` + env + `}`)
		}
	case "eqArr": // aligned array
		rows := parseEqArray(dec)
		if len(rows) == 1 && len(rows[0]) == 2 {
			var rebuilt [][]string
			for _, cell := range rows[0] {
				lhs, rhs := splitCaseCell(cell)
				if lhs != "" {
					rebuilt = append(rebuilt, []string{lhs, rhs})
				}
			}
			if len(rebuilt) > 0 {
				rows = rebuilt
			}
		}
		if len(rows) == 0 {
			return nil
		}
		for i, r := range rows {
			if i > 0 {
				buf.WriteString(`\\`)
			}
			if len(r) == 2 {
				buf.WriteString(r[0] + " = " + r[1])
			} else {
				buf.WriteString(strings.Join(r, " "))
			}
		}
	default:
		// Skip unknown subtree for robustness.
		if err := dec.Skip(); err != nil {
			return err
		}
	}
	return nil
}

// consumeTo reads until the specified local name is closed, writing its content to out.
func consumeTo(out *bytes.Buffer, dec *xml.Decoder, local string) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == local {
				for {
					tok2, err := dec.Token()
					if err != nil {
						return err
					}
					switch el2 := tok2.(type) {
					case xml.StartElement:
						if err := walk(out, el2, dec); err != nil {
							return err
						}
					case xml.EndElement:
						if el2.Name.Local == local {
							return nil
						}
					}
				}
			} else {
				if err := walk(out, el, dec); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if el.Name.Local == local {
				return nil
			}
		}
	}
}

func attrVal(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func escapeLatex(s string) string {
	// 1) Escape TeX special characters from raw text (but keep '&' for alignment/cases).
	replacer := strings.NewReplacer(
		"\\", `\textbackslash{}`,
		// In OMML math, '&' is often used for alignment (cases/arrays). Escaping it breaks KaTeX.
		"&", "&",
		"%", `\%`,
		"$", `\$`,
		"#", `\#`,
		"_", `\_`,
		"{", `\{`,
		"}", `\}`,
		"~", `\textasciitilde{}`,
		"^", `\^{}`,
	)

	s = replacer.Replace(s)

	// 2) Map Unicode symbols to KaTeX-friendly commands using eqn's mapping (filtered + overrides).
	s = latexmap.ReplaceEqnChars(s)

	// 3) Handle a few common Chinese math connectives/quantifier words.
	s = strings.NewReplacer(
		"对任意", `\forall `,
		"任意", `\forall `,
		"存在", `\exists `,
		"因为", `\because `,
		"所以", `\therefore `,
	).Replace(s)
	return s
}

func mapNarySymbol(start xml.StartElement) string {
	if chr := attrVal(start.Attr, "chr"); chr != "" {
		if mapped, ok := bigOpMap[chr]; ok {
			return mapped
		}
	}
	val := ""
	for _, a := range start.Attr {
		if a.Name.Local == "val" {
			val = a.Value
			break
		}
	}
	switch val {
	case "\u2211": // sum
		return `\sum`
	case "\u222b": // integral
		return `\int`
	case "\u222d": // triple integral
		return `\iiint`
	case "\u222f": // surface integral double contour
		// Degrade for KaTeX compatibility.
		return `\iint`
	case "\u2a0c": // quadruple integral
		return `\iiiint`
	case "\u220f": // product
		return `\prod`
	case "\u213f": // script small pi sometimes used as product
		return `\prod`
	case "\u22c2": // big cap
		return `\bigcap`
	case "\u22c3": // big cup
		return `\bigcup`
	default:
		return `\int`
	}
}

func mapNaryChr(chr string) string {
	if mapped, ok := bigOpMap[chr]; ok {
		return mapped
	}
	switch chr {
	case "\u2211":
		return `\sum`
	case "\u222b":
		return `\int`
	case "\u222d":
		return `\iiint`
	case "\u222f":
		return `\iint`
	case "\u2a0c":
		return `\iiiint`
	case "\u220f", "\u213f":
		return `\prod`
	default:
		return `\int`
	}
}

func formatFuncCall(name string, arg string) string {
	cmd := normalizeFuncCommand(name)
	if cmd != "" {
		// cmd includes leading backslash, e.g. \sin, \log, \Gamma
		return cmd + " " + arg
	}
	op := sanitizeIdentifier(name)
	if op == "" {
		op = "f"
	}
	// KaTeX supports \operatorname{...}
	return `\operatorname{` + escapeLatex(op) + `} ` + arg
}

func normalizeFuncCommand(name string) string {
	n := strings.TrimSpace(name)
	n = strings.Trim(n, "\\")
	if n == "" {
		return ""
	}
	// Heuristic: some OMML producers serialize limits as a single function-name token.
	// Example: "lim_ninftylim" (or similar) should become \lim_{n\to\infty}.
	ln := strings.ToLower(n)
	ln = strings.ReplaceAll(ln, " ", "")
	if strings.Contains(ln, "lim") && strings.Contains(ln, "infty") && strings.Contains(ln, "n") {
		return `\lim_{n\to\infty}`
	}
	// Greek letters sometimes appear as function name nodes in OMML.
	switch n {
	case "Γ":
		return `\Gamma`
	case "γ":
		return `\gamma`
	case "Π":
		return `\Pi`
	case "π":
		return `\pi`
	}
	// Common math functions KaTeX supports as built-in commands.
	switch strings.ToLower(n) {
	case "sin", "cos", "tan", "cot", "sec", "csc",
		"arcsin", "arccos", "arctan",
		"sinh", "cosh", "tanh",
		"log", "ln", "exp",
		"lim", "max", "min", "sup", "inf",
		"det", "dim", "ker",
		"gcd", "lcm",
		"mod", "bmod", "pmod":
		return `\` + strings.ToLower(n)
	}
	// For other identifiers, prefer \operatorname{...} rather than generating unknown commands.
	return ""
}

func sanitizeIdentifier(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		// Allow letters/digits and a small set of ASCII marks commonly used in function names.
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func matrixEnv(brk string) string {
	switch brk {
	case "[":
		return "bmatrix"
	case "{":
		return "Bmatrix"
	case "(":
		return "pmatrix"
	case "|":
		return "vmatrix"
	case "||":
		return "Vmatrix"
	default:
		return "matrix"
	}
}

// parseAccPr reads accPr to find nested chr value.
func parseAccPr(dec *xml.Decoder) string {
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "chr" {
				return attrVal(el.Attr, "val")
			}
		case xml.EndElement:
			if el.Name.Local == "accPr" {
				return ""
			}
		}
	}
}

// parseDelimPr reads dPr to find begChr/endChr/sepChr values.
func parseDelimPr(dec *xml.Decoder) (open string, close string, sep string) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return open, close, sep
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "begChr":
				open = attrVal(el.Attr, "val")
			case "endChr":
				close = attrVal(el.Attr, "val")
			case "sepChr":
				sep = attrVal(el.Attr, "val")
			default:
				_ = dec.Skip()
			}
		case xml.EndElement:
			if el.Name.Local == "dPr" {
				return open, close, sep
			}
		}
	}
}

// parseNaryChr reads naryPr for chr val.
func parseNaryChr(dec *xml.Decoder) string {
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "chr" {
				return attrVal(el.Attr, "val")
			}
		case xml.EndElement:
			if el.Name.Local == "naryPr" {
				return ""
			}
		}
	}
}

var bigOpMap = map[string]string{
	// KaTeX does not reliably support \Bbbsum; degrade to \sum for compatibility.
	"\u2140": "\\sum",
	"\u220f": "\\prod",
	"\u2210": "\\coprod",
	"\u2211": "\\sum",
	"\u222b": "\\int",
	"\u222c": "\\iint",
	"\u222d": "\\iiint",
	"\u222e": "\\oint",
	"\u22c0": "\\bigwedge",
	"\u22c1": "\\bigvee",
	"\u22c2": "\\bigcap",
	"\u22c3": "\\bigcup",
	"\u2a00": "\\bigodot",
	"\u2a01": "\\bigoplus",
	"\u2a02": "\\bigotimes",
}

func parseMatrix(dec *xml.Decoder) ([][]string, string) {
	var rows [][]string
	brk := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			return rows, brk
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "mPr" {
				for {
					tok2, err := dec.Token()
					if err != nil {
						return rows, brk
					}
					switch el2 := tok2.(type) {
					case xml.StartElement:
						if el2.Name.Local == "brk" {
							brk = attrVal(el2.Attr, "val")
						} else {
							_ = dec.Skip()
						}
					case xml.EndElement:
						if el2.Name.Local == "mPr" {
							goto next
						}
					}
				}
			}
			if el.Name.Local == "mr" {
				var row []string
				for {
					tok2, err := dec.Token()
					if err != nil {
						return rows, brk
					}
					switch el2 := tok2.(type) {
					case xml.StartElement:
						var cell bytes.Buffer
						if err := walk(&cell, el2, dec); err != nil {
							return rows, brk
						}
						row = append(row, cell.String())
					case xml.EndElement:
						if el2.Name.Local == "mr" {
							rows = append(rows, row)
							goto next
						}
					}
				}
			}
		case xml.EndElement:
			if el.Name.Local == "m" || el.Name.Local == "matrix" {
				return rows, brk
			}
		}
	next:
	}
}

func parseEqArray(dec *xml.Decoder) [][]string {
	var rows [][]string
	var loose []string
	for {
		tok, err := dec.Token()
		if err != nil {
			if len(loose) > 0 {
				rows = append(rows, loose)
			}
			return rows
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "mr" {
				var row []string
				for {
					tok2, err := dec.Token()
					if err != nil {
						return rows
					}
					switch el2 := tok2.(type) {
					case xml.StartElement:
						var part bytes.Buffer
						if err := walk(&part, el2, dec); err != nil {
							return rows
						}
						row = append(row, part.String())
					case xml.EndElement:
						if el2.Name.Local == "mr" {
							rows = append(rows, row)
							goto nextRow
						}
					}
				}
			} else if el.Name.Local == "e" {
				var part bytes.Buffer
				if err := walk(&part, el, dec); err != nil {
					return rows
				}
				loose = append(loose, part.String())
			}
		case xml.EndElement:
			if el.Name.Local == "eqArr" {
				if len(loose) > 0 {
					rows = append(rows, loose)
				}
				return rows
			}
		}
	nextRow:
	}
}

func splitCaseCell(s string) (string, string) {
	firstComma := strings.Index(s, ",")
	firstCnComma := strings.Index(s, "\uFF0C")
	pos := firstComma
	if pos == -1 || (firstCnComma != -1 && firstCnComma < pos) {
		pos = firstCnComma
	}
	if pos == -1 {
		// Some producers include leading alignment markers inside the cell text.
		// Strip leading '&' (and legacy '\&') so we don't emit "&&" in cases.
		t := strings.TrimSpace(strings.TrimPrefix(s, `\&`))
		t = strings.TrimSpace(strings.TrimPrefix(t, "&"))
		return t, ""
	}
	lhs := strings.TrimSpace(s[:pos])
	lhs = strings.TrimSpace(strings.TrimPrefix(lhs, `\&`))
	lhs = strings.TrimSpace(strings.TrimPrefix(lhs, "&"))

	rhs := strings.TrimSpace(strings.TrimPrefix(s[pos+1:], `\&`))
	rhs = strings.TrimSpace(strings.TrimPrefix(rhs, "&"))
	return lhs, rhs
}

func shouldFlattenMatrix(rows [][]string) bool {
	if len(rows) == 0 {
		return false
	}
	for _, r := range rows {
		if len(r) == 0 || len(r) > 3 {
			return false
		}
	}
	for _, r := range rows {
		for _, cell := range r {
			if strings.TrimSpace(cell) == "=" {
				return true
			}
		}
	}
	return false
}

func formatLineRow(row []string) string {
	parts := make([]string, 0, len(row))
	for _, cell := range row {
		if strings.TrimSpace(cell) == "" {
			continue
		}
		parts = append(parts, cell)
	}
	return strings.Join(parts, " ")
}
