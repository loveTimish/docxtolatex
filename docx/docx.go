package docx

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zhexiao/mtef-go/eqn"
	"github.com/zhexiao/mtef-go/omml"
)

type Converter struct {
	Source      string
	Output      string
	Config      Config
	WriteReport bool
}

type relInfo struct {
	target  string
	relType string
}

var (
	badFracRe     = regexp.MustCompile(`\\frac\{[^}]*\}\{\s*[\+\-*/]\s*\}|\\frac\{\s*[\+\-*/]\s*\}\{[^}]*\}`)
	repeatedOpsRe = regexp.MustCompile(`[\+\-]{3,}`)
	tinyBraceRe   = regexp.MustCompile(`\{\s*\d+\s*\+\s*\}`)
)

// Convert walks the docx body in order and writes a UTF-8 LaTeX document
// with plain text, converted MathType OLE objects, OMML formulas, and extracted images.
func (c *Converter) Convert() (int, error) {
	cfg := c.Config.withDefaults()

	reader, err := zip.OpenReader(c.Source)
	if err != nil {
		return 0, fmt.Errorf("open docx: %w", err)
	}
	defer reader.Close()

	files := make(map[string]*zip.File)
	for _, f := range reader.File {
		files[f.Name] = f
	}

	rels, err := loadRels(files)
	if err != nil {
		return 0, err
	}
	numbering, err := loadNumbering(files)
	if err != nil {
		return 0, err
	}
	styleMap, err := loadStyleNames(files)
	if err != nil {
		return 0, err
	}

	docFile, ok := files["word/document.xml"]
	if !ok {
		return 0, fmt.Errorf("word/document.xml not found in %s", c.Source)
	}

	docReader, err := docFile.Open()
	if err != nil {
		return 0, fmt.Errorf("open document.xml: %w", err)
	}
	defer docReader.Close()

	outPath := c.Output
	if outPath == "" {
		outPath = strings.TrimSuffix(filepath.Base(c.Source), filepath.Ext(c.Source))
	}

	outDir := outPath
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return 0, fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	assetRoot := filepath.Join(outDir, "img")
	baseName := strings.TrimSuffix(filepath.Base(outDir), filepath.Ext(outDir))
	if baseName == "" {
		baseName = "output"
	}
	texPath := filepath.Join(outDir, baseName+".tex")

	report := newReport(c.Source)
	var buf bytes.Buffer
	buf.WriteString("% Auto-generated from ")
	buf.WriteString(filepath.Base(c.Source))
	buf.WriteString("\n")
	buf.WriteString("\\documentclass{")
	buf.WriteString(cfg.Document.Class)
	buf.WriteString("}\n")
	for _, pkg := range cfg.Document.Packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		buf.WriteString(renderUsePackage(pkg))
		buf.WriteString("\n")
	}
	buf.WriteString("\\begin{document}\n\n")

	dec := xml.NewDecoder(docReader)

	var para strings.Builder
	paragraphs := make([]paragraphBlock, 0, 256)
	currentParagraphStyle := ""
	currentNumID := ""
	currentListLevel := 0
	currentHasNumPr := false
	eqnCache := make(map[string]string)
	imgCache := make(map[string]string)
	equationCount := 0
	paragraphCount := 0
	var imgBuf []string
	inObject := false
	pendingOlePreviewRid := ""

	flushImages := func() {
		if len(imgBuf) == 0 {
			return
		}
		para.WriteString(renderImageRefs(imgBuf, cfg.Image))
		imgBuf = imgBuf[:0]
	}

	appendWarning := func(msg string) {
		report.Warnings = append(report.Warnings, msg)
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("parse document.xml: %w", err)
		}

		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "object":
				inObject = true
				pendingOlePreviewRid = ""
			case "p":
				para.Reset()
				currentParagraphStyle = ""
				currentNumID = ""
				currentListLevel = 0
				currentHasNumPr = false
			case "pStyle":
				currentParagraphStyle = resolveStyleName(getAttr(el.Attr, "val"), styleMap)
			case "numPr":
				currentHasNumPr = true
			case "numId":
				currentNumID = strings.TrimSpace(getAttr(el.Attr, "val"))
			case "ilvl":
				if level, convErr := strconv.Atoi(strings.TrimSpace(getAttr(el.Attr, "val"))); convErr == nil {
					currentListLevel = level
				}
			case "t":
				flushImages()
				var txt string
				if err := dec.DecodeElement(&txt, &el); err != nil {
					return 0, fmt.Errorf("decode text: %w", err)
				}
				para.WriteString(escapeLatex(txt))
			case "tab":
				flushImages()
				para.WriteString("\t")
			case "br", "cr":
				flushImages()
				para.WriteString("\\\\")
			case "OLEObject":
				flushImages()
				rid := getAttr(el.Attr, "id")
				if rid == "" {
					appendWarning("OLEObject without relationship id")
					continue
				}

				rel, hasRel := rels[rid]
				latex, err := resolveEquation(rid, rels, files, eqnCache, appendWarning)
				conversionErrorReason := ""
				if err != nil {
					appendWarning(fmt.Sprintf("OLE convert failed in paragraph %d (%s): %v", paragraphCount+1, rid, err))
					conversionErrorReason = "convert-error"
					latex = ""
				}

				entry := EquationReport{
					Index:     len(report.Equations) + 1,
					Kind:      "ole",
					Paragraph: paragraphCount + 1,
				}
				if hasRel {
					entry.Source = rel.target
				}

				if bad, reason := isBadLatex(latex); latex == "" || bad {
					entry.Status = "skipped"
					entry.Reason = reason
					if conversionErrorReason != "" {
						entry.Reason = conversionErrorReason
					} else if latex == "" {
						entry.Reason = "empty-output"
					}
					if pendingOlePreviewRid != "" && isImage(relType(rels, pendingOlePreviewRid)) {
						previewRel := rels[pendingOlePreviewRid]
						name, err := extractImage(assetRoot, previewRel.target, files, imgCache)
						if err != nil {
							return 0, err
						}
						if name != "" {
							para.WriteString(renderImageRefs([]string{name}, cfg.Image))
							entry.Status = "fallback-image"
							entry.Output = name
							equationCount++
							report.Summary.FallbackImages++
						}
					}
					report.Equations = append(report.Equations, entry)
					continue
				}

				para.WriteString(addCommandSpacing(latex))
				equationCount++
				entry.Status = "converted"
				entry.Output = latex
				report.Equations = append(report.Equations, entry)
				report.Summary.ConvertedOLE++
			case "oMath", "oMathPara":
				flushImages()
				latex, err := omml.ConvertElement(el, dec)
				if err != nil {
					return 0, fmt.Errorf("convert OMML: %w", err)
				}
				if latex == "" {
					appendWarning(fmt.Sprintf("empty OMML formula in paragraph %d", paragraphCount+1))
					continue
				}
				latex = addCommandSpacing(latex)
				display := el.Name.Local == "oMathPara"
				wrapped := wrapMath(latex, display)
				para.WriteString(wrapped)
				equationCount++
				report.Equations = append(report.Equations, EquationReport{
					Index:     len(report.Equations) + 1,
					Kind:      map[bool]string{true: "omml-display", false: "omml-inline"}[display],
					Paragraph: paragraphCount + 1,
					Status:    "converted",
					Output:    latex,
				})
				report.Summary.ConvertedOMML++
			case "imagedata", "blip":
				rid := getAttr(el.Attr, "embed")
				if rid == "" {
					rid = getAttr(el.Attr, "id")
				}
				if rid == "" {
					continue
				}
				if isOle(relType(rels, rid)) {
					latex, err := resolveEquation(rid, rels, files, eqnCache, appendWarning)
					if err != nil {
						appendWarning(fmt.Sprintf("inline OLE convert failed in paragraph %d (%s): %v", paragraphCount+1, rid, err))
						latex = ""
					}
					if latex != "" {
						para.WriteString(addCommandSpacing(latex))
						equationCount++
						report.Equations = append(report.Equations, EquationReport{
							Index:     len(report.Equations) + 1,
							Kind:      "ole-inline",
							Paragraph: paragraphCount + 1,
							Status:    "converted",
							Output:    latex,
						})
						report.Summary.ConvertedOLE++
					}
				} else if isImage(relType(rels, rid)) {
					if inObject {
						pendingOlePreviewRid = rid
					} else {
						rel := rels[rid]
						name, err := extractImage(assetRoot, rel.target, files, imgCache)
						if err != nil {
							return 0, err
						}
						if name != "" {
							imgBuf = append(imgBuf, name)
							report.Summary.ExtractedImages++
						}
					}
				}
			}
		case xml.EndElement:
			if el.Name.Local == "object" {
				inObject = false
				pendingOlePreviewRid = ""
			}
			if el.Name.Local == "p" {
				flushImages()
				paragraphCount++
				report.Summary.Paragraphs = paragraphCount
				block := paragraphBlock{Content: para.String(), Style: currentParagraphStyle}
				if currentHasNumPr && currentNumID != "" {
					if def, ok := numbering.resolve(currentNumID, currentListLevel); ok {
						block.List = &listRef{NumID: currentNumID, Level: currentListLevel, Def: def}
					} else if strings.TrimSpace(block.Content) != "" {
						appendWarning(fmt.Sprintf("list numbering unresolved in paragraph %d: numId=%s level=%d", paragraphCount, currentNumID, currentListLevel))
					}
				}
				paragraphs = append(paragraphs, block)
			}
		}
	}

	buf.WriteString(renderParagraphBlocks(paragraphs, cfg, &report))
	buf.WriteString("\\end{document}\n")

	outBytes := []byte(buf.String())
	if err := os.WriteFile(texPath, outBytes, 0644); err != nil {
		return 0, fmt.Errorf("write %s: %w", texPath, err)
	}

	c.Output = texPath
	report.Output = texPath
	report.Summary.Equations = equationCount
	if (c.WriteReport || cfg.Report.Enabled) && texPath != "" {
		reportPath := cfg.Report.File
		if strings.TrimSpace(reportPath) == "" {
			reportPath = filepath.Join(outDir, baseName+".report.json")
		}
		if err := writeReport(report, reportPath); err != nil {
			return 0, err
		}
	}
	return equationCount, nil
}

// loadRels parses word/_rels/document.xml.rels to build a map of rId -> relationship info.
func loadRels(files map[string]*zip.File) (map[string]relInfo, error) {
	rels := make(map[string]relInfo)
	relFile, ok := files["word/_rels/document.xml.rels"]
	if !ok {
		return rels, nil
	}

	rc, err := relFile.Open()
	if err != nil {
		return nil, fmt.Errorf("open relationships: %w", err)
	}
	defer rc.Close()

	var relDoc struct {
		Relationships []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}

	if err := xml.NewDecoder(rc).Decode(&relDoc); err != nil {
		return nil, fmt.Errorf("decode relationships: %w", err)
	}

	sort.Slice(relDoc.Relationships, func(i, j int) bool {
		return relDoc.Relationships[i].ID < relDoc.Relationships[j].ID
	})

	for _, r := range relDoc.Relationships {
		rels[r.ID] = relInfo{target: r.Target, relType: r.Type}
	}
	return rels, nil
}

func loadStyleNames(files map[string]*zip.File) (map[string]string, error) {
	styles := make(map[string]string)
	styleFile, ok := files["word/styles.xml"]
	if !ok {
		return styles, nil
	}

	rc, err := styleFile.Open()
	if err != nil {
		return nil, fmt.Errorf("open styles: %w", err)
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode styles: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "style" {
			continue
		}
		styleID := getAttr(start.Attr, "styleId")
		styleName, err := readStyleName(dec)
		if err != nil {
			return nil, err
		}
		if styleID != "" {
			styles[normalizeStyleName(styleID)] = normalizeStyleName(styleName)
		}
	}
	return styles, nil
}

func readStyleName(dec *xml.Decoder) (string, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return "", nil
			}
			return "", fmt.Errorf("read style name: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "name" {
				return getAttr(el.Attr, "val"), nil
			}
		case xml.EndElement:
			if el.Name.Local == "style" {
				return "", nil
			}
		}
	}
}

func relType(rels map[string]relInfo, rid string) string {
	if r, ok := rels[rid]; ok {
		return r.relType
	}
	return ""
}

func isOle(relType string) bool {
	return strings.Contains(relType, "oleObject")
}

func isImage(relType string) bool {
	return strings.Contains(relType, "image")
}

func resolveEquation(rid string, rels map[string]relInfo, files map[string]*zip.File, cache map[string]string, appendWarning func(string)) (string, error) {
	rel, ok := rels[rid]
	if !ok {
		return "", nil
	}

	if cached, ok := cache[rel.target]; ok {
		return cached, nil
	}

	path := filepath.ToSlash(filepath.Join("word", rel.target))
	f, ok := files[path]
	if !ok {
		return "", fmt.Errorf("OLE object %s not found", path)
	}

	rc, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("open OLE %s: %w", path, err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read OLE %s: %w", path, err)
	}

	latex, err := eqn.ConvertBytesWithContext(data, rel.target)
	if err != nil {
		return "", fmt.Errorf("convert OLE %s: %w", path, err)
	}

	cache[rel.target] = latex
	return latex, nil
}

func extractImage(assetRoot, target string, files map[string]*zip.File, cache map[string]string) (string, error) {
	if cached, ok := cache[target]; ok {
		return cached, nil
	}

	srcPath := filepath.ToSlash(filepath.Join("word", target))
	f, ok := files[srcPath]
	if !ok {
		return "", fmt.Errorf("image %s not found", srcPath)
	}

	rc, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("open image %s: %w", srcPath, err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read image %s: %w", srcPath, err)
	}

	filename := uniqueAssetName(target, data)
	ext := strings.ToLower(filepath.Ext(filename))
	if (ext == ".wmf" || ext == ".emf") && len(data) > 0 && len(data) < 512 {
		cache[target] = ""
		return "", nil
	}

	dstPath := filepath.Join(assetRoot, filename)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(dstPath), err)
	}
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return "", fmt.Errorf("write image %s: %w", dstPath, err)
	}

	cache[target] = filename
	return filename, nil
}

func getAttr(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func escapeLatex(s string) string {
	replacer := strings.NewReplacer(
		"\\", `\textbackslash{}`,
		"&", `\&`,
		"%", `\%`,
		"$", `\$`,
		"#", `\#`,
		"_", `\_`,
		"{", `\{`,
		"}", `\}`,
		"~", `\textasciitilde{}`,
		"^", `\^{}`,
	)
	return replacer.Replace(s)
}

func isBadLatex(latex string) (bool, string) {
	s := strings.TrimSpace(latex)
	if s == "" {
		return true, "empty-output"
	}
	inner := s
	if strings.HasPrefix(inner, "$$") && strings.HasSuffix(inner, "$$") && len(inner) >= 4 {
		inner = strings.TrimSpace(inner[2 : len(inner)-2])
	} else if strings.HasPrefix(inner, "$") && strings.HasSuffix(inner, "$") && len(inner) >= 2 {
		inner = strings.TrimSpace(inner[1 : len(inner)-1])
	}
	if inner == "" {
		return true, "empty-math-body"
	}
	if strings.ContainsRune(inner, '\uFFFD') {
		return true, "replacement-char"
	}
	if strings.ContainsRune(inner, '□') {
		return true, "placeholder-box"
	}
	for _, r := range inner {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return true, "non-printable-rune"
		}
	}
	if !strings.Contains(inner, "\\") && utf8.RuneCountInString(inner) <= 2 {
		for _, r := range inner {
			if r > 127 && (unicode.IsLetter(r) || unicode.IsSymbol(r) || unicode.IsMark(r)) {
				return true, "tiny-nonlatex-fragment"
			}
		}
	}
	if badFracRe.MatchString(inner) {
		return true, "broken-frac"
	}
	if repeatedOpsRe.MatchString(inner) {
		return true, "repeated-operators"
	}
	if tinyBraceRe.MatchString(inner) {
		return true, "truncated-brace-group"
	}
	arrowOnly := strings.TrimSpace(strings.ReplaceAll(inner, `\rightarrow`, ""))
	if arrowOnly == "" && strings.Contains(inner, `\rightarrow`) {
		return true, "arrow-only"
	}
	return false, ""
}

// addCommandSpacing inserts a space after a backslash-led command when it is immediately
// followed by a letter/number/backslash so symbols do not stick to operands (e.g. \cdotB -> \cdot B).
func addCommandSpacing(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		r := runes[i]
		if r == '\\' {
			b.WriteRune(r)
			i++
			cmdStart := i
			for i < len(runes) && unicode.IsLetter(runes[i]) {
				b.WriteRune(runes[i])
				i++
			}
			if i >= len(runes) {
				b.WriteRune(' ')
			} else {
				next := runes[i]
				cmdLen := i - cmdStart
				if cmdLen == 0 && next == '\\' {
					// keep LaTeX line breaks as-is
				} else if next != ' ' && (unicode.IsLetter(next) || unicode.IsDigit(next) || next == '\\' || next == '×') {
					b.WriteRune(' ')
				}
			}
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

func wrapMath(latex string, display bool) string {
	latex = normalizeMathSpaces(latex)
	trimmed := strings.TrimSpace(latex)
	if strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$") && len(trimmed) >= 4 {
		return latex
	}
	if strings.HasPrefix(trimmed, "$") && strings.HasSuffix(trimmed, "$") && len(trimmed) >= 2 {
		return latex
	}
	if display {
		return "$$ " + latex + " $$"
	}
	return "$" + latex + "$"
}

func normalizeMathSpaces(latex string) string {
	return strings.ReplaceAll(latex, "\u3000", " ")
}

func renderParagraph(content string, style string, cfg Config) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "\n"
	}
	if strings.Contains(trimmed, "==end") {
		return "==end\n\n"
	}
	if format, ok := cfg.Styles[normalizeStyleName(style)]; ok && strings.Contains(format, "%s") {
		return fmt.Sprintf(format, trimmed) + "\n\n"
	}
	return trimmed + "\n\n"
}

func normalizeStyleName(style string) string {
	return strings.ToLower(strings.TrimSpace(style))
}

func resolveStyleName(style string, styleMap map[string]string) string {
	normalized := normalizeStyleName(style)
	if normalized == "" {
		return ""
	}
	if resolved, ok := styleMap[normalized]; ok && resolved != "" {
		return resolved
	}
	return normalized
}

func renderUsePackage(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	if strings.HasPrefix(pkg, "[") || strings.HasPrefix(pkg, "{") {
		return `\usepackage` + pkg
	}
	return `\usepackage{` + pkg + `}`
}

func renderImageRefs(names []string, cfg ImageConfig) string {
	if len(names) == 0 {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "placeholder"
	}

	parts := make([]string, 0, len(names))
	for _, name := range names {
		switch mode {
		case "includegraphics":
			parts = append(parts, fmt.Sprintf(`\includegraphics{%s}`, name))
		case "template":
			tpl := cfg.Template
			if tpl == "" {
				tpl = `beginPic{%s}endPic`
			}
			parts = append(parts, fmt.Sprintf(tpl, name))
		default:
			parts = append(parts, fmt.Sprintf(`beginPic{%s}endPic`, name))
		}
	}

	if mode == "includegraphics" {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts, "")
}

func uniqueAssetName(target string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(target))
	base := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	base = sanitizeFileStem(base)
	if base == "" {
		base = "asset"
	}
	sum := sha1.Sum(append([]byte(target), data...))
	return fmt.Sprintf("%s-%x%s", base, sum[:4], ext)
}

func sanitizeFileStem(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
