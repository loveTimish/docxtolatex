package eqn

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// Convert is kept for compatibility; it loads a file and discards errors.
func Convert(filepath string) string {
	latex, err := ConvertFile(filepath)
	if err != nil {
		fmt.Print(err)
	}
	return latex
}

// ConvertFile converts a MathType OLE object stored on disk to LaTeX.
func ConvertFile(path string) (string, error) {
	buffer, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ConvertBytes(buffer)
}

// ConvertBytes converts a MathType OLE object from memory to LaTeX.
func ConvertBytes(buffer []byte) (string, error) {
	reader := bytes.NewReader(buffer)
	return ConvertReader(reader)
}

// ConvertReader converts a MathType OLE object provided by a reader/seekable stream to LaTeX.
func ConvertReader(reader io.ReadSeeker) (string, error) {
	mtef, err := Open(reader)
	if err != nil {
		return "", err
	}
	return mtef.Translate(), nil
}
