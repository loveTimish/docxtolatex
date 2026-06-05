package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/extrame/ole2"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("usage: inspect_ole_dir <docx> <entry> [entry...]")
	}
	docx := os.Args[1]
	zr, err := zip.OpenReader(docx)
	if err != nil {
		log.Fatal(err)
	}
	defer zr.Close()

	entries := map[string]bool{}
	for _, arg := range os.Args[2:] {
		entries[arg] = true
	}
	for _, zf := range zr.File {
		if !entries[zf.Name] {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			log.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("== %s size=%d ==\n", zf.Name, len(data))
		ole, err := ole2.Open(bytes.NewReader(data), "")
		if err != nil {
			fmt.Printf("open error: %v\n", err)
			continue
		}
		dir, err := ole.ListDir()
		if err != nil {
			fmt.Printf("list error: %v\n", err)
			continue
		}
		for i, f := range dir {
			fmt.Printf("%02d %q\n", i, f.Name())
		}
	}
}
