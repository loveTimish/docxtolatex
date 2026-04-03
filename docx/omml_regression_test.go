package docx

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertGeneratedOMMLRegressionDocx(t *testing.T) {
	tempDir := t.TempDir()
	source := filepath.Join(tempDir, "generated-omml.docx")
	outputDir := filepath.Join(tempDir, "generated-omml-out")

	writeMinimalDocxFixture(t, source, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math">
  <w:body>
    <w:p>
      <w:r><w:t>inline </w:t></w:r>
      <m:oMath>
        <m:sSub>
          <m:e><m:r><m:t>x</m:t></m:r></m:e>
          <m:sub><m:r><m:t>i</m:t></m:r></m:sub>
        </m:sSub>
      </m:oMath>
      <w:r><w:t> tail</w:t></w:r>
    </w:p>
    <w:p>
      <m:oMathPara>
        <m:oMath>
          <m:f>
            <m:num><m:r><m:t>a</m:t></m:r></m:num>
            <m:den><m:r><m:t>b</m:t></m:r></m:den>
          </m:f>
        </m:oMath>
      </m:oMathPara>
    </w:p>
    <w:p>
      <m:oMath>
        <m:m>
          <m:mPr><m:brk m:val="["/></m:mPr>
          <m:mr>
            <m:e><m:r><m:t>a</m:t></m:r></m:e>
            <m:e><m:r><m:t>b</m:t></m:r></m:e>
          </m:mr>
          <m:mr>
            <m:e><m:r><m:t>c</m:t></m:r></m:e>
            <m:e><m:r><m:t>d</m:t></m:r></m:e>
          </m:mr>
        </m:m>
      </m:oMath>
    </w:p>
  </w:body>
</w:document>`)

	converter := &Converter{Source: source, Output: outputDir, WriteReport: true}
	count, err := converter.Convert()
	if err != nil {
		t.Fatalf("Convert() returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("Convert() equation count = %d, want 3", count)
	}

	tex, err := os.ReadFile(converter.Output)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", converter.Output, err)
	}
	texStr := string(tex)
	for _, want := range []string{
		`inline ${x}_{i}$ tail`,
		`$$ \frac{a}{b} $$`,
		`$\begin{bmatrix}a & b\\c & d\end{bmatrix}$`,
	} {
		if !strings.Contains(texStr, want) {
			t.Fatalf("expected converted tex to contain %q, got:\n%s", want, texStr)
		}
	}

	reportPath := filepath.Join(outputDir, "generated-omml-out.report.json")
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", reportPath, err)
	}

	var report ConversionReport
	if err := json.Unmarshal(reportData, &report); err != nil {
		t.Fatalf("json.Unmarshal(report) returned error: %v", err)
	}
	if report.Summary.ConvertedOMML != 3 || report.Summary.ConvertedOLE != 0 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
	if len(report.Equations) != 3 {
		t.Fatalf("expected 3 equation entries, got %d", len(report.Equations))
	}
	if got := []string{report.Equations[0].Kind, report.Equations[1].Kind, report.Equations[2].Kind}; strings.Join(got, ",") != "omml-inline,omml-display,omml-inline" {
		t.Fatalf("unexpected equation kinds: %v", got)
	}
	for i, eq := range report.Equations {
		if eq.Status != EquationStatusConverted {
			t.Fatalf("equation %d status = %q, want %q", i+1, eq.Status, EquationStatusConverted)
		}
	}
}

func writeMinimalDocxFixture(t *testing.T, path string, documentXML string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%s) returned error: %v", path, err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	writeZipEntry := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip.Create(%s) returned error: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%s) returned error: %v", name, err)
		}
	}

	writeZipEntry("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	writeZipEntry("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)
	writeZipEntry("word/document.xml", documentXML)

	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close() returned error: %v", err)
	}
}
