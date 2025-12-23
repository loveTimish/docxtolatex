package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhexiao/mtef-go/eqn"
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
				if latex != "" {
					para.WriteString(latex)
					equationCount++
				}
			case "imagedata", "blip":
				rid := getAttr(el.Attr, "embed")
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
		case xml.EndElement:
			if el.Name.Local == "p" {
				flushImages()
				para.WriteString("\n\n")
				buf.WriteString(para.String())
			}
		}
	}

	buf.WriteString("\\end{document}\n")

	if err := os.WriteFile(texPath, buf.Bytes(), 0644); err != nil {
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

	latex, err := eqn.ConvertBytes(data)
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
