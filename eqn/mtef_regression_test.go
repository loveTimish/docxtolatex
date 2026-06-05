package eqn

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertDSMT6EquationWithExtendedSizeRecord(t *testing.T) {
	docx := filepath.Join(`F:\资料\xsc资料\word_files`, "5.docx")
	zr, err := zip.OpenReader(docx)
	if err != nil {
		t.Skipf("regression fixture not available: %v", err)
	}
	defer zr.Close()

	var data []byte
	for _, f := range zr.File {
		if f.Name != "word/embeddings/oleObject14.bin" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if len(data) == 0 {
		t.Skip("regression OLE object not found")
	}

	got, err := ConvertBytesWithContext(data, "5.docx/oleObject14.bin")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a_{ n }", "a_{ 1 }", "\\times d"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}
