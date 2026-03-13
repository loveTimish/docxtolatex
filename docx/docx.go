package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zhexiao/mtef-go/eqn"
	"github.com/zhexiao/mtef-go/omml"
)

type Converter struct {
	Source string
	Output string
}

type relInfo struct {
	target  string
	relType string
}

// Convert walks the docx body in order and writes a minimal UTF-8 LaTeX file
// with plain text, converted MathType OLE objects, and extracted images.
func (c *Converter) Convert() (int, error) {
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

	var buf bytes.Buffer
	buf.WriteString("% Auto-generated from ")
	buf.WriteString(filepath.Base(c.Source))
	buf.WriteString("\n\\documentclass{article}\n")
	buf.WriteString("\\usepackage[T1]{fontenc}\n\\usepackage[utf8]{inputenc}\n")
	buf.WriteString("\\usepackage{amsmath}\n\\usepackage{amssymb}\n\\usepackage{graphicx}\n")
	buf.WriteString("\\begin{document}\n\n")

	dec := xml.NewDecoder(docReader)

	var para strings.Builder
	eqnCache := make(map[string]string)
	imgCache := make(map[string]string)
	equationCount := 0
	var imgBuf []string
	inObject := false
	pendingOlePreviewRid := ""

	isBadLatex := func(latex string) bool {
		s := strings.TrimSpace(latex)
		if s == "" {
			return true
		}
		inner := s
		if strings.HasPrefix(inner, "$$") && strings.HasSuffix(inner, "$$") && len(inner) >= 4 {
			inner = strings.TrimSpace(inner[2 : len(inner)-2])
		} else if strings.HasPrefix(inner, "$") && strings.HasSuffix(inner, "$") && len(inner) >= 2 {
			inner = strings.TrimSpace(inner[1 : len(inner)-1])
		}
		if inner == "" {
			return true
		}
		if strings.ContainsRune(inner, '\uFFFD') {
			return true
		}
		// Treat common "unknown glyph" placeholders as failure as well.
		if strings.ContainsRune(inner, '□') {
			return true
		}
		// If there are non-printable runes in the math content, it's usually garbage and
		// will render as tofu/boxes in editors. Prefer image fallback.
		for _, r := range inner {
			if r == '\n' || r == '\r' || r == '\t' {
				continue
			}
			if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
				return true
			}
		}
		// If the result is just a tiny non-LaTeX blob (e.g. "$$ Ȁ $$"), treat it as failed.
		if !strings.Contains(inner, "\\") && utf8.RuneCountInString(inner) <= 2 {
			for _, r := range inner {
				if r > 127 && (unicode.IsLetter(r) || unicode.IsSymbol(r) || unicode.IsMark(r)) {
					return true
				}
			}
		}
		// Heuristics for "obviously broken" conversions that would lose meaning.
		// Prefer image fallback instead of outputting misleading TeX.
		// - \frac{1}{+} or \frac{+}{1}
		badFrac := regexp.MustCompile(`\\frac\{[^}]*\}\{\s*[\+\-*/]\s*\}|\\frac\{\s*[\+\-*/]\s*\}\{[^}]*\}`)
		if badFrac.MatchString(inner) {
			return true
		}
		// - Repeated operators like "+++" or "---" (not common in valid TeX here)
		if regexp.MustCompile(`[\+\-]{3,}`).MatchString(inner) {
			return true
		}
		// - Tiny brace groups that end with '+' like "{ 1+ }" (often truncated parse)
		if regexp.MustCompile(`\{\s*\d+\s*\+\s*\}`).MatchString(inner) {
			return true
		}
		// - Arrow-only formulas (usually a broken template expansion)
		arrowOnly := strings.TrimSpace(strings.ReplaceAll(inner, `\rightarrow`, ""))
		if arrowOnly == "" && strings.Contains(inner, `\rightarrow`) {
			return true
		}
		return false
	}

	flushImages := func() {
		if len(imgBuf) == 0 {
			return
		}
		para.WriteString("beginPic{" + strings.Join(imgBuf, ",") + "}endPic")
		imgBuf = imgBuf[:0]
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
				// A Word OLE object (e.g., MathType) often has a preview image (imagedata/blip)
				// plus the OLEObject relationship. We defer emitting the preview image until we
				// know whether the OLE->LaTeX conversion succeeded.
				inObject = true
				pendingOlePreviewRid = ""
			case "p":
				para.Reset()
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
					continue
				}
				latex, err := resolveEquation(rid, rels, files, eqnCache)
				if err != nil {
					return 0, err
				}
				// Fallback: if LaTeX conversion failed/looks broken, use the preview image
				// captured inside the same w:object (if any).
				if latex == "" || isBadLatex(latex) {
					if pendingOlePreviewRid != "" && isImage(relType(rels, pendingOlePreviewRid)) {
						rel := rels[pendingOlePreviewRid]
						name, err := extractImage(assetRoot, rel.target, files, imgCache)
						if err != nil {
							return 0, err
						}
						if name != "" {
							para.WriteString("beginPic{" + name + "}endPic")
							equationCount++
						}
					}
					// either way, don't emit the (bad) latex
					continue
				}
				if latex != "" {
					para.WriteString(addCommandSpacing(latex))
					equationCount++
				}
			case "oMath", "oMathPara":
				flushImages()
				latex, err := omml.ConvertElement(el, dec)
				if err != nil {
					return 0, fmt.Errorf("convert OMML: %w", err)
				}
				if latex != "" {
					latex = addCommandSpacing(latex)
					display := el.Name.Local == "oMathPara"
					para.WriteString(wrapMath(latex, display))
					equationCount++
				}
			case "imagedata", "blip":
				// a:blip uses r:embed; v:imagedata uses r:id
				rid := getAttr(el.Attr, "embed")
				if rid == "" {
					rid = getAttr(el.Attr, "id")
				}
				if rid == "" {
					continue
				}
				if isOle(relType(rels, rid)) {
					latex, err := resolveEquation(rid, rels, files, eqnCache)
					if err != nil {
						return 0, err
					}
					if latex != "" {
						para.WriteString(latex)
						equationCount++
					}
				} else if isImage(relType(rels, rid)) {
					// If we're inside an OLE object, this image is usually just a preview for the OLE equation.
					// Defer emitting it until we see whether OLE conversion works.
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
				content := para.String()
				if strings.Contains(content, "==end") {
					// 视为分隔符行：只保留分隔符本身
					buf.WriteString("==end\n\n")
				} else {
					para.WriteString("\n\n")
					buf.WriteString(para.String())
				}
			}
		}
	}

	buf.WriteString("\\end{document}\n")

	outBytes := []byte(addCommandSpacing(buf.String()))
	if err := os.WriteFile(texPath, outBytes, 0644); err != nil {
		return 0, fmt.Errorf("write %s: %w", texPath, err)
	}

	c.Output = texPath
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

	// Stable order not required, but sort for determinism in tests.
	sort.Slice(relDoc.Relationships, func(i, j int) bool {
		return relDoc.Relationships[i].ID < relDoc.Relationships[j].ID
	})

	for _, r := range relDoc.Relationships {
		rels[r.ID] = relInfo{target: r.Target, relType: r.Type}
	}
	return rels, nil
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

func resolveEquation(rid string, rels map[string]relInfo, files map[string]*zip.File, cache map[string]string) (string, error) {
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

	dstPath := filepath.Join(assetRoot, target)
	filename := filepath.Base(target)
	dstPath = filepath.Join(assetRoot, filename)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(dstPath), err)
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

	// Some MathType OLE preview images can be essentially "empty" placeholders.
	// If such a tiny WMF/EMF is used for equation fallback, it should be treated as
	// an empty formula (emit nothing) rather than a confusing beginPic marker.
	ext := strings.ToLower(filepath.Ext(filename))
	if (ext == ".wmf" || ext == ".emf") && len(data) > 0 && len(data) < 512 {
		cache[target] = ""
		return "", nil
	}

	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return "", fmt.Errorf("write image %s: %w", dstPath, err)
	}

	rel := filename
	cache[target] = rel
	return rel, nil
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
				// Don't break TeX linebreak command: "\\" should stay as-is (no inserted space).
				cmdLen := i - cmdStart
				if cmdLen == 0 && next == '\\' {
					// no-op
				} else if next != ' ' && (unicode.IsLetter(next) || unicode.IsDigit(next) || next == '\\' || next == '×') {
					b.WriteRune(' ')
				}
			}
			continue
		}
		b.WriteRune(r)
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

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
