package docx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type numberingInfo struct {
	levelsByNumID map[string]map[int]numberingLevel
}

type numberingLevel struct {
	NumFmt      string
	LvlText     string
	Environment string
}

type paragraphBlock struct {
	Content          string
	Style            string
	List             *listRef
	ListContinuation bool
}

type listRef struct {
	NumID string
	Level int
	Def   numberingLevel
}

type numberingDocument struct {
	AbstractNums []numberingAbstractNum `xml:"abstractNum"`
	Nums         []numberingNum         `xml:"num"`
}

type numberingAbstractNum struct {
	ID   string              `xml:"abstractNumId,attr"`
	Lvls []numberingLevelXML `xml:"lvl"`
}

type numberingLevelXML struct {
	ILvl    string         `xml:"ilvl,attr"`
	NumFmt  numberingValue `xml:"numFmt"`
	LvlText numberingValue `xml:"lvlText"`
}

type numberingNum struct {
	ID            string         `xml:"numId,attr"`
	AbstractNumID numberingValue `xml:"abstractNumId"`
}

type numberingValue struct {
	Val string `xml:"val,attr"`
}

func loadNumbering(files map[string]*zip.File) (numberingInfo, error) {
	info := numberingInfo{levelsByNumID: make(map[string]map[int]numberingLevel)}
	file, ok := files["word/numbering.xml"]
	if !ok {
		return info, nil
	}

	rc, err := file.Open()
	if err != nil {
		return info, fmt.Errorf("open numbering: %w", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return info, fmt.Errorf("read numbering: %w", err)
	}

	var doc numberingDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return info, fmt.Errorf("decode numbering: %w", err)
	}

	abstractLevels := make(map[string]map[int]numberingLevel)
	for _, abs := range doc.AbstractNums {
		if strings.TrimSpace(abs.ID) == "" {
			continue
		}
		levels := make(map[int]numberingLevel)
		for _, lvl := range abs.Lvls {
			index, err := strconv.Atoi(strings.TrimSpace(lvl.ILvl))
			if err != nil {
				continue
			}
			numFmt := strings.ToLower(strings.TrimSpace(lvl.NumFmt.Val))
			levels[index] = numberingLevel{
				NumFmt:      numFmt,
				LvlText:     strings.TrimSpace(lvl.LvlText.Val),
				Environment: listEnvironmentFor(numFmt),
			}
		}
		abstractLevels[abs.ID] = levels
	}

	for _, num := range doc.Nums {
		if strings.TrimSpace(num.ID) == "" {
			continue
		}
		levels := abstractLevels[strings.TrimSpace(num.AbstractNumID.Val)]
		if len(levels) == 0 {
			continue
		}
		clone := make(map[int]numberingLevel, len(levels))
		for k, v := range levels {
			clone[k] = v
		}
		info.levelsByNumID[num.ID] = clone
	}

	return info, nil
}

func (n numberingInfo) resolve(numID string, level int) (numberingLevel, bool) {
	if level < 0 {
		level = 0
	}
	levels, ok := n.levelsByNumID[strings.TrimSpace(numID)]
	if !ok {
		return numberingLevel{}, false
	}
	def, ok := levels[level]
	return def, ok
}

func listEnvironmentFor(numFmt string) string {
	switch strings.ToLower(strings.TrimSpace(numFmt)) {
	case "bullet":
		return "itemize"
	default:
		return "enumerate"
	}
}
