package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/extrame/ole2"
	"github.com/zhexiao/mtef-go/eqn"
)

const oleCbHdr = uint16(28)

func main() {
	if len(os.Args) != 3 {
		log.Fatalf("usage: inspect_equation <docx> <ole-entry>")
	}
	data := readZipEntry(os.Args[1], os.Args[2])
	fmt.Printf("ole entry=%s size=%d\n", os.Args[2], len(data))
	fmt.Printf("convert: %q\n", mustConvert(data))

	ole, err := ole2.Open(bytes.NewReader(data), "")
	if err != nil {
		log.Fatal(err)
	}
	dir, err := ole.ListDir()
	if err != nil {
		log.Fatal(err)
	}
	for i, file := range dir {
		fmt.Printf("dir[%d]=%q size=%d start=%d type=%d\n", i, file.Name(), file.Size, file.Sstart, file.Type)
		if file.Name() == "Equation Native" {
			root := dir[0]
			r := ole.OpenFile(file, root)
			native, err := io.ReadAll(r)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("native size=%d\n", len(native))
			dump(native[:min(len(native), 96)])
			if len(native) >= int(oleCbHdr) {
				var cbHdr uint16
				var cbSize uint32
				hr := bytes.NewReader(native[:oleCbHdr])
				_ = binary.Read(hr, binary.LittleEndian, &cbHdr)
				_, _ = hr.Seek(4+2, io.SeekCurrent)
				_ = binary.Read(hr, binary.LittleEndian, &cbSize)
				fmt.Printf("cbHdr=%d cbSize=%d bodyAvailable=%d\n", cbHdr, cbSize, len(native)-int(cbHdr))
				body := native[cbHdr:min(len(native), int(cbHdr)+int(cbSize))]
				fmt.Printf("body size=%d\n", len(body))
				dump(body[:min(len(body), 96)])
				m, err := eqn.Open(bytes.NewReader(data))
				if err != nil {
					fmt.Printf("open eqn error: %v\n", err)
				} else if m != nil {
					fmt.Print(m.DebugSummary(120))
				}
			}
		}
	}
}

func readZipEntry(path, name string) []byte {
	zr, err := zip.OpenReader(path)
	if err != nil {
		log.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				log.Fatal(err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				log.Fatal(err)
			}
			return data
		}
	}
	log.Fatalf("entry not found: %s", name)
	return nil
}

func mustConvert(data []byte) string {
	latex, err := eqn.ConvertBytesWithContext(data, "inspect")
	if err != nil {
		return "ERR: " + err.Error()
	}
	return latex
}

func dump(data []byte) {
	for i := 0; i < len(data); i += 16 {
		end := min(i+16, len(data))
		fmt.Printf("%04x  ", i)
		for _, b := range data[i:end] {
			fmt.Printf("%02x ", b)
		}
		fmt.Print("\n")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
