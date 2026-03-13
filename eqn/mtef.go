package eqn

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"fmt"
	"github.com/extrame/ole2"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
)

const oleCbHdr = uint16(28)

// [MTEFv5](https://docs.wiris.com/en/mathtype/mathtype_desktop/mathtype-sdk/mtef5)
type MTEFv5 struct {
	mMtefVer     uint8
	mPlatform    uint8
	mProduct     uint8
	mVersion     uint8
	mVersionSub  uint8
	mApplication string
	mInline      uint8

	reader io.ReadSeeker

	ast   *MtAST
	nodes []*MtAST

	//是否合法，顺利解析
	Valid bool

	// DebugContext is an optional label (e.g., OLE target path) used in debug output.
	DebugContext string
}

func envBool(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false
	}
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func tagName(t RecordType) string {
	switch t {
	case ROOT:
		return "ROOT"
	case END:
		return "END"
	case LINE:
		return "LINE"
	case CHAR:
		return "CHAR"
	case TMPL:
		return "TMPL"
	case PILE:
		return "PILE"
	case MATRIX:
		return "MATRIX"
	case EMBELL:
		return "EMBELL"
	default:
		return fmt.Sprintf("TAG_%d", int(t))
	}
}

func (m *MTEFv5) dumpAST(ast *MtAST, depth int, maxDepth int, maxNodes int, nodes *int, b *strings.Builder) {
	if ast == nil || *nodes >= maxNodes {
		return
	}
	*nodes++
	indent := strings.Repeat("  ", depth)
	b.WriteString(indent)
	b.WriteString(tagName(ast.tag))

	switch ast.tag {
	case CHAR:
		if ast.value != nil {
			ch := ast.value.(*MtChar)
			b.WriteString(fmt.Sprintf(" mtcode=0x%04x typeface=%d", ch.mtcode, ch.typeface))
		}
	case TMPL:
		if ast.value != nil {
			t := ast.value.(*MtTmpl)
			b.WriteString(fmt.Sprintf(" selector=%d variation=0x%04x options=0x%02x nudge=(%d,%d) children=%d",
				t.selector, t.variation, t.options, t.nudgeX, t.nudgeY, len(ast.children)))
		}
	case MATRIX:
		if ast.value != nil {
			mt := ast.value.(*MtMatrix)
			b.WriteString(fmt.Sprintf(" rows=%d cols=%d children=%d", mt.rows, mt.cols, len(ast.children)))
		}
	default:
		if len(ast.children) > 0 {
			b.WriteString(fmt.Sprintf(" children=%d", len(ast.children)))
		}
	}
	b.WriteString("\n")

	if depth >= maxDepth {
		return
	}
	for _, c := range ast.children {
		if *nodes >= maxNodes {
			return
		}
		m.dumpAST(c, depth+1, maxDepth, maxNodes, nodes, b)
	}
}

func (m *MTEFv5) readRecord() (err error) {
	/**
	读取body的每一行数据并保存到数组里
	*/
	//默认设置为合法的，除非遇到不可解析数据
	m.Valid = true

	//Header
	_ = binary.Read(m.reader, binary.LittleEndian, &m.mMtefVer)
	_ = binary.Read(m.reader, binary.LittleEndian, &m.mPlatform)
	_ = binary.Read(m.reader, binary.LittleEndian, &m.mProduct)
	_ = binary.Read(m.reader, binary.LittleEndian, &m.mVersion)
	_ = binary.Read(m.reader, binary.LittleEndian, &m.mVersionSub)

	// MTEF v5+ has an Application key field (null-terminated string)
	// MTEF v3 and earlier do NOT have this field
	if m.mMtefVer >= 5 {
		m.mApplication, _ = m.readNullTerminatedString()
	}

	_ = binary.Read(m.reader, binary.LittleEndian, &m.mInline)

	// Body
	// MTEF v3 uses a packed tag byte: high 4 bits = options, low 4 bits = record type.
	// Do NOT use the v5/v6 record loop for v3, otherwise we will misinterpret tag bytes
	// (often >= 0x80) as FUTURE records and desync the stream.
	if m.mMtefVer == 3 {
		return m.readBodyV3()
	}

	// Body (v4/v5+)
	for {
		record := RecordType(0)
		err = binary.Read(m.reader, binary.LittleEndian, &record)

		// FUTURE records (type >= 100): payload length follows as an unsigned integer.
		// Standard encoding uses a single uint8. Some MathType producers (e.g. DSMT6)
		// use 0xFF as a sentinel meaning "extended length: next 2 bytes are uint16 LE".
		if record >= FUTURE {
			var skipLen uint8
			_ = binary.Read(m.reader, binary.LittleEndian, &skipLen)

			var skip int64
			if skipLen == 0xFF {
				var extLen uint16
				_ = binary.Read(m.reader, binary.LittleEndian, &extLen)
				skip = int64(extLen)
			} else {
				skip = int64(skipLen)
			}

			_, _ = m.reader.Seek(skip, io.SeekCurrent)
			continue
		}

		//debug 使用
		//fmt.Println(record)

		if err != nil {
			break
		}
		switch record {
		case END:
			m.nodes = append(m.nodes, &MtAST{END, nil, nil})
		case LINE:
			// LINE carries layout/stack information for slots and expressions.
			// Some legacy streams may differ, but treating LINE as marker-only can corrupt the AST.
			line := new(MtLine)
			_ = m.readLine(line)
			m.nodes = append(m.nodes, &MtAST{LINE, line, nil})
		case CHAR:
			char := new(MtChar)
			_ = m.readChar(char)

			m.nodes = append(m.nodes, &MtAST{CHAR, char, nil})
		case TMPL:
			tmpl := new(MtTmpl)
			_ = m.readTMPL(tmpl)

			m.nodes = append(m.nodes, &MtAST{TMPL, tmpl, nil})
		case PILE:
			pile := new(MtPile)
			_ = m.readPile(pile)

			m.nodes = append(m.nodes, &MtAST{PILE, pile, nil})
		case MATRIX:
			matrix := new(MtMatrix)
			_ = m.readMatrix(matrix)

			m.nodes = append(m.nodes, &MtAST{MATRIX, matrix, nil})

			//匹配矩阵数据下面的2个nil
			m.nodes = append(m.nodes, &MtAST{LINE, new(MtLine), nil})
			m.nodes = append(m.nodes, &MtAST{LINE, new(MtLine), nil})
		case EMBELL:
			embell := new(MtEmbellRd)
			_ = m.readEmbell(embell)

			m.nodes = append(m.nodes, &MtAST{tag: EMBELL, value: embell, children: nil})
		case RULER:
			// RULER record: read nStops and skip the tab stop data
			var nStops uint8
			_ = binary.Read(m.reader, binary.LittleEndian, &nStops)
			// Each tab stop has: type (1 byte) + offset (2 bytes) = 3 bytes
			for i := uint8(0); i < nStops; i++ {
				var stopType uint8
				var stopOffset uint16
				_ = binary.Read(m.reader, binary.LittleEndian, &stopType)
				_ = binary.Read(m.reader, binary.LittleEndian, &stopOffset)
			}
			// RULER is metadata, no need to add to nodes
		case FONT_STYLE_DEF:
			fsDef := new(MtfontStyleDef)
			_ = binary.Read(m.reader, binary.LittleEndian, &fsDef.fontDefIndex)
			fsDef.name, _ = m.readNullTerminatedString()

			//读取字节，但是不关心数据，注释
			//m.nodes = append(m.nodes, &MtAST{FONT_STYLE_DEF, fsDef, nil})
		case SIZE:
			mtSize := new(MtSize)
			_ = binary.Read(m.reader, binary.LittleEndian, &mtSize.lsize)
			_ = binary.Read(m.reader, binary.LittleEndian, &mtSize.dsize)
		case SUB:
			m.nodes = append(m.nodes, &MtAST{SUB, nil, nil})
		case SUB2:
			m.nodes = append(m.nodes, &MtAST{SUB2, nil, nil})
		case SYM:
			m.nodes = append(m.nodes, &MtAST{SYM, nil, nil})
		case SUBSYM:
			m.nodes = append(m.nodes, &MtAST{SUBSYM, nil, nil})
		case FONT_DEF:
			fdef := new(MtfontDef)
			_ = binary.Read(m.reader, binary.LittleEndian, &fdef.encDefIndex)
			fdef.name, _ = m.readNullTerminatedString()

			m.nodes = append(m.nodes, &MtAST{FONT_DEF, fdef, nil})
		case COLOR:
			cIndex := new(MtColorDefIndex)
			_ = binary.Read(m.reader, binary.LittleEndian, &cIndex.index)

			//读取字节，但是不关心数据，注释
			//m.nodes = append(m.nodes, &MtAST{tag: COLOR, value: cIndex, children: nil})
		case COLOR_DEF:
			cDef := new(MtColorDef)
			_ = m.readColorDef(cDef)

			//读取字节，但是不关心数据，注释
			//m.nodes = append(m.nodes, &MtAST{tag: COLOR_DEF, value: cDef, children: nil})
		case FULL:
			m.nodes = append(m.nodes, &MtAST{FULL, nil, nil})
		case EQN_PREFS:
			prefs := new(MtEqnPrefs)
			_ = m.readEqnPrefs(prefs)

			m.nodes = append(m.nodes, &MtAST{EQN_PREFS, prefs, nil})
		case ENCODING_DEF:
			enc, _ := m.readNullTerminatedString()

			m.nodes = append(m.nodes, &MtAST{ENCODING_DEF, enc, nil})
		default:
			// Unknown record type - log for debugging but don't invalidate the entire equation
			// This allows partial recovery from unknown records
			log.Printf("Unknown record type %d at current position, skipping", record)
			// Don't set m.Valid = false here - allow the equation to continue parsing
			// The equation may still be partially usable
		}
	}

	return nil
}

// TagTypeV3 option bits (stored in the high 4 bits of the tag byte)
const (
	v3XfNULL   uint8 = 0x1 // LINE placeholder only
	v3XfRULER  uint8 = 0x2 // RULER record follows LINE/PILE
	v3XfLSPACE uint8 = 0x4 // LINE spacing value follows tag
	v3XfLMOVE  uint8 = 0x8 // nudge values follow tag
)

func (m *MTEFv5) readBodyV3() error {
	// v3: tag byte packs options+type. END has no payload. Some record readers
	// re-read the tag byte, so we seek back for container-like records.
	for {
		var tagByte uint8
		if err := binary.Read(m.reader, binary.LittleEndian, &tagByte); err != nil {
			// EOF
			break
		}
		recType := RecordType(tagByte & 0x0F)

		if recType == END {
			m.nodes = append(m.nodes, &MtAST{END, nil, nil})
			continue
		}

		// These readers expect to read the tag byte themselves (to get options).
		switch recType {
		case LINE, CHAR, TMPL, PILE, MATRIX, EMBELL:
			_, _ = m.reader.Seek(-1, io.SeekCurrent)
		}

		switch recType {
		case LINE:
			line := new(MtLine)
			if err := m.readLineV3(line); err != nil {
				m.Valid = false
				return err
			}
			m.nodes = append(m.nodes, &MtAST{LINE, line, nil})
		case CHAR:
			char := new(MtChar)
			if err := m.readCharV3(char); err != nil {
				m.Valid = false
				return err
			}
			m.nodes = append(m.nodes, &MtAST{CHAR, char, nil})
		case TMPL:
			tmpl := new(MtTmpl)
			if err := m.readTMPLV3(tmpl); err != nil {
				m.Valid = false
				return err
			}
			m.nodes = append(m.nodes, &MtAST{TMPL, tmpl, nil})
		case PILE:
			pile := new(MtPile)
			if err := m.readPileV3(pile); err != nil {
				m.Valid = false
				return err
			}
			m.nodes = append(m.nodes, &MtAST{PILE, pile, nil})
		case MATRIX:
			matrix := new(MtMatrix)
			if err := m.readMatrixV3(matrix); err != nil {
				m.Valid = false
				return err
			}
			m.nodes = append(m.nodes, &MtAST{MATRIX, matrix, nil})
			// Match matrix slot terminators like v5 path.
			m.nodes = append(m.nodes, &MtAST{LINE, new(MtLine), nil})
			m.nodes = append(m.nodes, &MtAST{LINE, new(MtLine), nil})
		case EMBELL:
			embell := new(MtEmbellRd)
			if err := m.readEmbellV3(embell); err != nil {
				m.Valid = false
				return err
			}
			m.nodes = append(m.nodes, &MtAST{tag: EMBELL, value: embell, children: nil})
		case SIZE:
			// v3 SIZE: 2 bytes (lsize,dsize)
			var b [2]byte
			_, _ = io.ReadFull(m.reader, b[:])
		case FULL:
			m.nodes = append(m.nodes, &MtAST{FULL, nil, nil})
		case SUB:
			m.nodes = append(m.nodes, &MtAST{SUB, nil, nil})
		case SUB2:
			m.nodes = append(m.nodes, &MtAST{SUB2, nil, nil})
		case SYM:
			m.nodes = append(m.nodes, &MtAST{SYM, nil, nil})
		case SUBSYM:
			m.nodes = append(m.nodes, &MtAST{SUBSYM, nil, nil})
		default:
			// Unknown v3 record type: mark invalid and stop.
			m.Valid = false
			return fmt.Errorf("unknown v3 record type %d (tag=0x%02x)", recType, tagByte)
		}
	}
	return nil
}

func (m *MTEFv5) readNudgeV3() (nudgeX int16, nudgeY int16, err error) {
	var b1, b2 uint8
	if err = binary.Read(m.reader, binary.LittleEndian, &b1); err != nil {
		return 0, 0, err
	}
	if err = binary.Read(m.reader, binary.LittleEndian, &b2); err != nil {
		return 0, 0, err
	}
	if b1 == 128 && b2 == 128 {
		_ = binary.Read(m.reader, binary.LittleEndian, &nudgeX)
		_ = binary.Read(m.reader, binary.LittleEndian, &nudgeY)
		return nudgeX, nudgeY, nil
	}
	return int16(int(b1) - 128), int16(int(b2) - 128), nil
}

func (m *MTEFv5) readLineV3(line *MtLine) error {
	var tag uint8
	if err := binary.Read(m.reader, binary.LittleEndian, &tag); err != nil {
		return err
	}
	recType := tag & 0x0F
	options := (tag & 0xF0) >> 4
	if recType != uint8(LINE) {
		return fmt.Errorf("readLineV3: unexpected recType=%d", recType)
	}
	if options&v3XfLMOVE != 0 {
		line.nudgeX, line.nudgeY, _ = m.readNudgeV3()
	}
	if options&v3XfLSPACE != 0 {
		_ = binary.Read(m.reader, binary.LittleEndian, &line.lineSpace)
	}
	// v3 RULER payload is not fully specified in our sources; match mtef-latex behavior:
	// consume the following RULER tag (and optional nudge), but ignore further payload.
	if options&v3XfRULER != 0 {
		var rulerTag uint8
		if err := binary.Read(m.reader, binary.LittleEndian, &rulerTag); err != nil {
			return err
		}
		rulerType := rulerTag & 0x0F
		rulerOpts := (rulerTag & 0xF0) >> 4
		if rulerType != uint8(RULER) {
			return fmt.Errorf("readLineV3: expected RULER, got type=%d", rulerType)
		}
		if rulerOpts&v3XfLMOVE != 0 {
			_, _, _ = m.readNudgeV3()
		}
	}
	if options&v3XfNULL != 0 {
		line.null = true
	}
	return nil
}

func (m *MTEFv5) readCharV3(char *MtChar) error {
	var tag uint8
	if err := binary.Read(m.reader, binary.LittleEndian, &tag); err != nil {
		return err
	}
	recType := tag & 0x0F
	options := (tag & 0xF0) >> 4
	if recType != uint8(CHAR) {
		return fmt.Errorf("readCharV3: unexpected recType=%d", recType)
	}
	char.options = options
	if options&v3XfLMOVE != 0 {
		char.nudgeX, char.nudgeY, _ = m.readNudgeV3()
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &char.typeface)
	_ = binary.Read(m.reader, binary.LittleEndian, &char.mtcode)
	return nil
}

func (m *MTEFv5) readTMPLV3(tmpl *MtTmpl) error {
	var tag uint8
	if err := binary.Read(m.reader, binary.LittleEndian, &tag); err != nil {
		return err
	}
	recType := tag & 0x0F
	options := (tag & 0xF0) >> 4
	if recType != uint8(TMPL) {
		return fmt.Errorf("readTMPLV3: unexpected recType=%d", recType)
	}
	if options&v3XfLMOVE != 0 {
		tmpl.nudgeX, tmpl.nudgeY, _ = m.readNudgeV3()
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &tmpl.selector)
	// variation, 1 or 2 bytes (same encoding as v5)
	byte1 := uint8(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &byte1)
	if 0x80 == byte1&0x80 {
		byte2 := uint8(0)
		_ = binary.Read(m.reader, binary.LittleEndian, &byte2)
		tmpl.variation = (uint16(byte1) & 0x7F) | (uint16(byte2) << 8)
	} else {
		tmpl.variation = uint16(byte1)
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &tmpl.options)
	return nil
}

func (m *MTEFv5) readPileV3(pile *MtPile) error {
	var tag uint8
	if err := binary.Read(m.reader, binary.LittleEndian, &tag); err != nil {
		return err
	}
	recType := tag & 0x0F
	options := (tag & 0xF0) >> 4
	if recType != uint8(PILE) {
		return fmt.Errorf("readPileV3: unexpected recType=%d", recType)
	}
	if options&v3XfLMOVE != 0 {
		pile.nudgeX, pile.nudgeY, _ = m.readNudgeV3()
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &pile.halign)
	_ = binary.Read(m.reader, binary.LittleEndian, &pile.valign)
	if options&v3XfRULER != 0 {
		// same minimal ruler skip as in readLineV3
		var rulerTag uint8
		if err := binary.Read(m.reader, binary.LittleEndian, &rulerTag); err != nil {
			return err
		}
		rulerType := rulerTag & 0x0F
		rulerOpts := (rulerTag & 0xF0) >> 4
		if rulerType != uint8(RULER) {
			return fmt.Errorf("readPileV3: expected RULER, got type=%d", rulerType)
		}
		if rulerOpts&v3XfLMOVE != 0 {
			_, _, _ = m.readNudgeV3()
		}
	}
	return nil
}

func (m *MTEFv5) readMatrixV3(matrix *MtMatrix) error {
	var tag uint8
	if err := binary.Read(m.reader, binary.LittleEndian, &tag); err != nil {
		return err
	}
	recType := tag & 0x0F
	options := (tag & 0xF0) >> 4
	if recType != uint8(MATRIX) {
		return fmt.Errorf("readMatrixV3: unexpected recType=%d", recType)
	}
	if options&v3XfLMOVE != 0 {
		matrix.nudgeX, matrix.nudgeY, _ = m.readNudgeV3()
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.valign)
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.h_just)
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.v_just)
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.rows)
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.cols)
	return nil
}

func (m *MTEFv5) readEmbellV3(embell *MtEmbellRd) error {
	var tag uint8
	if err := binary.Read(m.reader, binary.LittleEndian, &tag); err != nil {
		return err
	}
	recType := tag & 0x0F
	options := (tag & 0xF0) >> 4
	if recType != uint8(EMBELL) {
		return fmt.Errorf("readEmbellV3: unexpected recType=%d", recType)
	}
	if options&v3XfLMOVE != 0 {
		embell.nudgeX, embell.nudgeY, _ = m.readNudgeV3()
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &embell.embellType)
	return nil
}

func (m *MTEFv5) readNullTerminatedString() (s string, err error) {
	buf, p := bytes.Buffer{}, []byte{0}
	for {
		_, err = m.reader.Read(p)
		if p[0] == 0 {
			break
		}
		buf.WriteByte(p[0])
	}
	return buf.String(), err
}

// readApplicationKey reads the MTEF application key.
// Some producers serialize this field as UTF-16LE null-terminated text, while
// others use single-byte null-terminated text. Detect and decode both forms.
func (m *MTEFv5) readLine(line *MtLine) (err error) {
	// MTEF v3: LINE payload differs; do not consume bytes here.
	if m.mMtefVer <= 3 {
		return nil
	}

	// MTEF v5+ format
	options := OptionType(0)
	err = binary.Read(m.reader, binary.LittleEndian, &options)

	if MtefOptNudge == MtefOptNudge&options {
		line.nudgeX, line.nudgeY, _ = m.readNudge()
	}
	if MtefOptLineLspace == MtefOptLineLspace&options {
		_ = binary.Read(m.reader, binary.LittleEndian, &line.lineSpace)
	}

	//RULER解析
	if mtefOPT_LP_RULER == mtefOPT_LP_RULER&options {
		var nStops uint8
		_ = binary.Read(m.reader, binary.LittleEndian, &nStops)

		var tabList []uint8
		for i := uint8(0); i < nStops; i++ {
			var stopVal uint8
			_ = binary.Read(m.reader, binary.LittleEndian, &stopVal)
			tabList = append(tabList, stopVal)

			var tabOffset uint16
			_ = binary.Read(m.reader, binary.LittleEndian, &tabOffset)
		}
	}

	if MtefOptLineNull == MtefOptLineNull&options {
		line.null = true
	}

	return err
}

func (m *MTEFv5) readDimensionArrays(size int64) (array []string, err error) {
	var flag = true
	var tmpStr = new(bytes.Buffer)
	var count = int64(0)

	var fx = func(x uint8) {
		if flag {
			// Expecting unit type at the start of a dimension value
			switch x {
			case 0x00:
				flag = false
				tmpStr.WriteString("in")
			case 0x01:
				flag = false
				tmpStr.WriteString("cm")
			case 0x02:
				flag = false
				tmpStr.WriteString("pt")
			case 0x03:
				flag = false
				tmpStr.WriteString("pc")
			case 0x04:
				flag = false
				tmpStr.WriteString("%")
			case 0x0f:
				// Separator encountered while expecting unit - empty value
				flag = true
				count += 1
				array = append(array, tmpStr.String())
				tmpStr.Reset()
			default:
				// Unknown unit type - treat as unitless and continue
				flag = false
			}
		} else {
			// Expecting numeric value
			switch x {
			case 0x00:
				tmpStr.WriteByte('0')
			case 0x01:
				tmpStr.WriteByte('1')
			case 0x02:
				tmpStr.WriteByte('2')
			case 0x03:
				tmpStr.WriteByte('3')
			case 0x04:
				tmpStr.WriteByte('4')
			case 0x05:
				tmpStr.WriteByte('5')
			case 0x06:
				tmpStr.WriteByte('6')
			case 0x07:
				tmpStr.WriteByte('7')
			case 0x08:
				tmpStr.WriteByte('8')
			case 0x09:
				tmpStr.WriteByte('9')
			case 0x0a:
				tmpStr.WriteByte('.')
			case 0x0b:
				tmpStr.WriteByte('-')
			case 0x0c:
				// Extended character - skip silently
			case 0x0d:
				// Extended character - skip silently
			case 0x0e:
				// Extended character - skip silently
			case 0x0f:
				// End of current dimension value
				flag = true
				count += 1
				array = append(array, tmpStr.String())
				tmpStr.Reset()
			default:
				// Unknown nibble value - skip silently
			}
		}
	}

	// Safety limit to prevent infinite loops on corrupted data
	maxIterations := size * 10
	iterations := int64(0)

	for {
		if count >= size {
			break
		}
		iterations++
		if iterations > maxIterations {
			// Prevent infinite loop on corrupted data
			break
		}

		ch := uint8(0)
		err = binary.Read(m.reader, binary.LittleEndian, &ch)
		if err != nil {
			// EOF or read error - stop parsing
			break
		}

		hi := (ch & 0xf0) / 16
		lo := ch & 0x0f
		fx(hi)
		fx(lo)
	}
	return array, nil
}

func (m *MTEFv5) readEqnPrefs(eqnPrefs *MtEqnPrefs) (err error) {
	options := uint8(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &options)

	//sizes
	size := uint8(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &size)
	eqnPrefs.sizes, _ = m.readDimensionArrays(int64(size))

	//spaces
	size = 0
	_ = binary.Read(m.reader, binary.LittleEndian, &size)
	eqnPrefs.spaces, _ = m.readDimensionArrays(int64(size))

	//styles
	size = 0
	_ = binary.Read(m.reader, binary.LittleEndian, &size)
	styles := make([]byte, size)
	for i := uint8(0); i < size; i++ {
		c := uint8(0)
		_ = binary.Read(m.reader, binary.LittleEndian, &c)
		if c == 0 {
			styles = append(styles, 0)
		} else {
			_ = binary.Read(m.reader, binary.LittleEndian, &c)
			styles = append(styles, c)
		}
	}
	eqnPrefs.styles = styles
	return nil
}

func (m *MTEFv5) readChar(char *MtChar) (err error) {
	// MTEF v3: No options byte. Format is typeface(1) + mtcode(2)
	// MTEF v5: options(1) + [nudge] + typeface(1) + [mtcode(2)] + ...
	if m.mMtefVer <= 3 {
		_ = binary.Read(m.reader, binary.LittleEndian, &char.typeface)
		_ = binary.Read(m.reader, binary.LittleEndian, &char.mtcode)
		return nil
	}

	// MTEF v5+ format
	options := OptionType(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &options)

	if MtefOptNudge == MtefOptNudge&options {
		char.nudgeX, char.nudgeY, _ = m.readNudge()
	}

	_ = binary.Read(m.reader, binary.LittleEndian, &char.typeface)
	if MtefOptCharEncNoMtcode != MtefOptCharEncNoMtcode&options {
		_ = binary.Read(m.reader, binary.LittleEndian, &char.mtcode)
	}

	if MtefOptCharEncChar8 == MtefOptCharEncChar8&options {
		_ = binary.Read(m.reader, binary.LittleEndian, &char.bits8)
	}
	if MtefOptCharEncChar16 == MtefOptCharEncChar16&options {
		_ = binary.Read(m.reader, binary.LittleEndian, &char.bits16)
	}

	//fmt.Println(char)

	return nil
}

func (m *MTEFv5) readNudge() (nudgeX int16, nudgeY int16, err error) {
	b1 := 0
	b2 := 0
	_ = binary.Read(m.reader, binary.LittleEndian, &b1)
	_ = binary.Read(m.reader, binary.LittleEndian, &b2)

	if b1 == 128 || b2 == 128 {
		_ = binary.Read(m.reader, binary.LittleEndian, &nudgeX)
		_ = binary.Read(m.reader, binary.LittleEndian, &nudgeY)
		return nudgeX, nudgeY, err
	} else {
		nudgeX = int16(b1)
		nudgeY = int16(b2)
		return nudgeX, nudgeY, err
	}
}

func (m *MTEFv5) readTMPL(tmpl *MtTmpl) (err error) {
	// MTEF v3 has a different TMPL format:
	//   v3: selector (1) + variation (1) + options (1)
	//   v5: options (1) + [nudge] + selector (1) + variation (1-2) + options (1)
	if m.mMtefVer <= 3 {
		// MTEF v3 format: no leading options byte, no nudge
		_ = binary.Read(m.reader, binary.LittleEndian, &tmpl.selector)
		var variation uint8
		_ = binary.Read(m.reader, binary.LittleEndian, &variation)
		tmpl.variation = uint16(variation)
		_ = binary.Read(m.reader, binary.LittleEndian, &tmpl.options)
		return nil
	}

	// MTEF v5+ format
	options := OptionType(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &options)

	if MtefOptNudge == MtefOptNudge&options {
		tmpl.nudgeX, tmpl.nudgeY, _ = m.readNudge()
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &tmpl.selector)

	// variation, 1 or 2 bytes
	byte1 := uint8(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &byte1)
	if 0x80 == byte1&0x80 {
		byte2 := uint8(0)
		_ = binary.Read(m.reader, binary.LittleEndian, &byte2)
		tmpl.variation = (uint16(byte1) & 0x7F) | (uint16(byte2) << 8)
	} else {
		tmpl.variation = uint16(byte1)
	}
	_ = binary.Read(m.reader, binary.LittleEndian, &tmpl.options)
	return nil
}

func (m *MTEFv5) readPile(pile *MtPile) (err error) {
	// MTEF v3: halign (1) + valign (1)
	// MTEF v5: options (1) + [nudge] + halign (1) + valign (1)
	if m.mMtefVer <= 3 {
		_ = binary.Read(m.reader, binary.LittleEndian, &pile.halign)
		_ = binary.Read(m.reader, binary.LittleEndian, &pile.valign)
		return nil
	}

	options := OptionType(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &options)

	if MtefOptNudge == MtefOptNudge&options {
		pile.nudgeX, pile.nudgeY, _ = m.readNudge()
	}

	//读取halign和valign
	_ = binary.Read(m.reader, binary.LittleEndian, &pile.halign)
	_ = binary.Read(m.reader, binary.LittleEndian, &pile.valign)

	return nil
}

func (m *MTEFv5) readMatrix(matrix *MtMatrix) (err error) {
	// MTEF v3: valign (1) + h_just (1) + v_just (1) + rows (1) + cols (1)
	// MTEF v5: options (1) + [nudge] + valign (1) + h_just (1) + v_just (1) + rows (1) + cols (1)
	if m.mMtefVer <= 3 {
		_ = binary.Read(m.reader, binary.LittleEndian, &matrix.valign)
		_ = binary.Read(m.reader, binary.LittleEndian, &matrix.h_just)
		_ = binary.Read(m.reader, binary.LittleEndian, &matrix.v_just)
		_ = binary.Read(m.reader, binary.LittleEndian, &matrix.rows)
		_ = binary.Read(m.reader, binary.LittleEndian, &matrix.cols)
		return nil
	}

	options := OptionType(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &options)

	if MtefOptNudge == MtefOptNudge&options {
		matrix.nudgeX, matrix.nudgeY, _ = m.readNudge()
	}

	//读取valign和h_just、v_just
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.valign)
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.h_just)
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.v_just)

	//读取rows和cols
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.rows)
	_ = binary.Read(m.reader, binary.LittleEndian, &matrix.cols)

	return nil
}

func (m *MTEFv5) readEmbell(embell *MtEmbellRd) (err error) {
	// MTEF v3: embellType (1) only
	// MTEF v5: options (1) + [nudge] + embellType (1)
	if m.mMtefVer <= 3 {
		_ = binary.Read(m.reader, binary.LittleEndian, &embell.embellType)
		return nil
	}

	options := OptionType(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &options)

	if MtefOptNudge == MtefOptNudge&options {
		embell.nudgeX, embell.nudgeY, _ = m.readNudge()
	}

	//读取embellishment type
	_ = binary.Read(m.reader, binary.LittleEndian, &embell.embellType)
	return nil
}

func (m *MTEFv5) readColorDef(colorDef *MtColorDef) (err error) {
	options := OptionType(0)
	_ = binary.Read(m.reader, binary.LittleEndian, &options)

	var color uint16
	if mtefCOLOR_CMYK == mtefCOLOR_CMYK&options {
		//CMYK，读4个值
		for i := 0; i < 4; i++ {
			_ = binary.Read(m.reader, binary.LittleEndian, &color)
			colorDef.values = append(colorDef.values, uint8(color))
		}
	} else {
		//	RGB，读3个值
		for i := 0; i < 3; i++ {
			_ = binary.Read(m.reader, binary.LittleEndian, &color)
			colorDef.values = append(colorDef.values, uint8(color))
		}
	}

	if mtefCOLOR_NAME == mtefCOLOR_NAME&options {
		colorDef.name, _ = m.readNullTerminatedString()
	}

	return nil
}

func (m *MTEFv5) Translate() string {
	latexStr, err := m.makeLatex(m.ast)
	if err != nil {
		fmt.Println(err)
	}

	if m.Valid {
		// Some sources include Chinese connective words inside MathType text runs.
		// Normalize them to KaTeX-friendly logic symbols when they appear.
		latexStr = strings.NewReplacer(
			"因为", `\because `,
			"所以", `\therefore `,
		).Replace(latexStr)
		// Normalize fullwidth punctuation that appears inside equations (e.g. factorial '！').
		latexStr = strings.ReplaceAll(latexStr, "！", "!")

		// Filter out empty formulas - if the content between $$ and $$ is just whitespace,
		// return empty string instead of "$$  $$"
		content := strings.TrimSpace(latexStr)
		if content == "$$  $$" || content == "$$$$" {
			return ""
		}
		// Also check if the inner content is empty
		inner := strings.TrimPrefix(content, "$$")
		inner = strings.TrimSuffix(inner, "$$")
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return ""
		}

		// Optional AST debug dump (stderr via log), controlled by env var.
		// - DOCXTOLATEX_DEBUG_AST=1 enables.
		// - DOCXTOLATEX_DEBUG_AST_FILTER=<substr> prints only when latex contains substr.
		// - DOCXTOLATEX_DEBUG_AST_ON_ISSUE=1 prints when latex looks suspicious (e.g. factorials),
		//   even if no filter is provided.
		if envBool("DOCXTOLATEX_DEBUG_AST") {
			filter := os.Getenv("DOCXTOLATEX_DEBUG_AST_FILTER")
			onIssue := envBool("DOCXTOLATEX_DEBUG_AST_ON_ISSUE")
			looksSuspicious := strings.Contains(latexStr, "!") || strings.Contains(latexStr, "！")
			shouldDump := false
			if filter != "" {
				shouldDump = strings.Contains(latexStr, filter)
			} else if onIssue {
				shouldDump = looksSuspicious
			} else {
				shouldDump = true
			}

			if shouldDump {
				maxDepth := envInt("DOCXTOLATEX_DEBUG_AST_MAX_DEPTH", 6)
				maxNodes := envInt("DOCXTOLATEX_DEBUG_AST_MAX_NODES", 400)
				var sb strings.Builder
				sb.WriteString("=== DOCXTOLATEX_DEBUG_AST BEGIN ===\n")
				if m.DebugContext != "" {
					sb.WriteString("context: " + m.DebugContext + "\n")
				}
				sb.WriteString(fmt.Sprintf("mtef: ver=%d platform=%d product=%d version=%d sub=%d\n",
					m.mMtefVer, m.mPlatform, m.mProduct, m.mVersion, m.mVersionSub))
				sb.WriteString("latex: " + strings.TrimSpace(latexStr) + "\n")
				nodes := 0
				m.dumpAST(m.ast, 0, maxDepth, maxNodes, &nodes, &sb)
				sb.WriteString("=== DOCXTOLATEX_DEBUG_AST END ===\n")
				// Use stdout instead of stderr to avoid PowerShell NativeCommandError
				// when running with debug enabled.
				_, _ = fmt.Fprint(os.Stdout, sb.String())
			}
		}

		return latexStr
	} else {
		return ""
	}

}

func (m *MTEFv5) makeAST() (err error) {
	/**
	根据数组生成出栈入栈结构
	*/
	ast := new(MtAST)
	ast.tag = 0xff
	ast.value = nil
	m.ast = ast

	stack := list.New()
	stack.PushBack(ast)

	for _, node := range m.nodes {
		//debug 可用
		//fmt.Printf("%+v %+v \n", node.tag, node.value)

		switch node.tag {
		case LINE:
			if stack.Len() > 0 {
				ele := stack.Back()

				//将对象强制转为MtAST类型
				parent := ele.Value.(*MtAST)

				parent.children = append(parent.children, node)
			}
			if !node.value.(*MtLine).null {
				//如果与0 <nil> 匹配，则需要入栈
				stack.PushBack(node)
			}
		case TMPL:
			if stack.Len() > 0 {
				ele := stack.Back()

				//将对象强制转为MtAST类型
				parent := ele.Value.(*MtAST)

				parent.children = append(parent.children, node)
			}

			//如果与0 <nil> 匹配，则需要入栈
			stack.PushBack(node)
		case PILE:
			if stack.Len() > 0 {
				ele := stack.Back()

				//将对象强制转为MtAST类型
				parent := ele.Value.(*MtAST)

				parent.children = append(parent.children, node)
			}

			//如果与0 <nil> 匹配，则需要入栈
			stack.PushBack(node)
		case MATRIX:
			if stack.Len() > 0 {
				ele := stack.Back()

				//将对象强制转为MtAST类型
				parent := ele.Value.(*MtAST)

				parent.children = append(parent.children, node)
			}

			//如果与0 <nil> 匹配，则需要入栈
			stack.PushBack(node)
		case END:
			if stack.Len() > 0 {
				ele := stack.Back()
				stack.Remove(ele)
			}
		case CHAR:
			if stack.Len() > 0 {
				ele := stack.Back()

				//将对象强制转为MtAST类型
				parent := ele.Value.(*MtAST)

				parent.children = append(parent.children, node)
			} else if stack.Len() == 0 {
				//never go there
				ast.children = append(ast.children, node)
			}
		case EMBELL:
			if stack.Len() > 0 {
				//读取父节点
				ele := stack.Back()

				//并将对象强制转为MtAST类型
				parent := ele.Value.(*MtAST)
				parent.children = append(parent.children, node)

				switch EmbellType(node.value.(*MtEmbellRd).embellType) {
				//数据结构中，这些数据是在字符后面，但是在latex展示中某些字符需要在字符前面
				//比如： $$ \hat y $$
				//所以我们需要交换最后2位
				case emb1DOT, embHAT, embOBAR:
					if len(parent.children) >= 2 {
						embellData := parent.children[len(parent.children)-1]
						charData := parent.children[len(parent.children)-2]
						parent.children = parent.children[:len(parent.children)-2]

						parent.children = append(parent.children, embellData, charData)
					}
				}
			}

			//如果与0 <nil> 匹配，则需要入栈
			stack.PushBack(node)

			//case COLOR_DEF:
			//	/*
			//	这个数据结构有3或4个（RGB或者CMYK）对应的nil，所以需要循环把每个值都push到栈里面
			//
			//	16 &{values:[0 0 0] name:}
			//	0 <nil>
			//	0 <nil>
			//	0 <nil>
			//	 */
			//
			//	colorList := node.value.(*MtColorDef).values
			//	if len(colorList) > 0 {
			//		//读取每个color的值，然后入栈
			//		for _, val := range colorList {
			//			//如果与0 <nil> 匹配，则需要入栈
			//			stack.PushBack(val)
			//		}
			//	}
			//case FONT_STYLE_DEF:
			//	/*
			//	这个数据结构如下，所以需要配对6个入栈
			//	8 &{fontDefIndex:1 name:}
			//	0 <nil>
			//	0 <nil>
			//	0 <nil>
			//	0 <nil>
			//	0 <nil>
			//	0 <nil>
			//	*/
			//
			//	fontIndex := node.value.(*MtfontStyleDef).fontDefIndex
			//	if fontIndex == 1 {
			//		for i := 0; i < 6; i++ {
			//			//如果与0 <nil> 匹配，则需要入栈
			//			stack.PushBack(0)
			//		}
			//	}
		}
	}

	//m.ast.debug(0)
	return nil
}

func (m *MTEFv5) makeLatex(ast *MtAST) (latex string, err error) {
	/**
	根据出栈入栈结构生成latex字符串
	*/

	buf := new(bytes.Buffer)

	switch ast.tag {
	case ROOT:
		// Use an index loop so we can do small, safe normalizations (e.g., collapse "+++")
		// and recognize certain legacy patterns across sibling nodes.
		for i := 0; i < len(ast.children); i++ {
			cur := ast.children[i]

			// Collapse repeated + / - tokens that sometimes appear due to slot parsing drift.
			// This helps avoid generating "+++" in output and keeps KaTeX happy.
			if cur.tag == CHAR && i+1 < len(ast.children) {
				ch := cur.value.(*MtChar).mtcode
				if ch == '+' || ch == '-' {
					j := i + 1
					for j < len(ast.children) {
						n := ast.children[j]
						if n.tag == CHAR && n.value.(*MtChar).mtcode == ch {
							j++
							continue
						}
						break
					}
					_latex, _ := m.makeLatex(cur)
					buf.WriteString(_latex)
					i = j - 1
					continue
				}
			}

			// Legacy pattern: some v3 streams encode a "fraction-like" construct as:
			//   LINE( TMPL tmARROW {...numer-ish...} ) + next sibling LINE(denom)
			// followed by "+ + + \cdots +" placeholder pieces.
			// If we detect this, stitch it into a proper \frac{...}{...} so we can keep LaTeX
			// instead of falling back to images.
			if cur.tag == LINE && i+1 < len(ast.children) && ast.children[i+1].tag == LINE {
				line0 := cur
				line1 := ast.children[i+1]
				if len(line0.children) == 1 && line0.children[0].tag == TMPL {
					tmplAst := line0.children[0]
					tmpl := tmplAst.value.(*MtTmpl)
					if SelectorType(tmpl.selector) == tmARROW && tmpl.variation == 0 {
						// Build numerator from all template children, skipping empty tmINTEG markers.
						var numBuf strings.Builder
						for _, c := range tmplAst.children {
							if c.tag == TMPL {
								t := c.value.(*MtTmpl)
								if SelectorType(t.selector) == tmINTEG && len(c.children) == 0 && t.variation == 0 {
									continue
								}
							}
							s, _ := m.makeLatex(c)
							numBuf.WriteString(s)
						}
						numer := strings.TrimSpace(numBuf.String())
						denom, _ := m.makeLatex(line1)
						denom = strings.TrimSpace(denom)
						// Heuristic: only apply when both sides are non-empty and denominator looks "mathy".
						if numer != "" && denom != "" && (strings.Contains(denom, `\times`) || strings.ContainsAny(denom, "0123456789")) {
							buf.WriteString(fmt.Sprintf("\\frac{%s}{%s}", numer, denom))
							i++ // consume the denominator LINE as well
							continue
						}
					}
				}
			}

			_latex, _ := m.makeLatex(cur)
			buf.WriteString(_latex)
		}
		content := strings.TrimSpace(buf.String())
		return wrapMathRoot(content), nil
	case CHAR:
		mtcode := ast.value.(*MtChar).mtcode
		typeface := ast.value.(*MtChar).typeface
		// mtcode is a character code; convert explicitly to a rune to avoid Go vet warning.
		char := string(rune(mtcode))

		//生成char的一些特殊集
		hexExtend := ""
		typefaceFmt := ""
		isSymbolFont := false
		switch typeface - 128 {
		case fnMTEXTRA:
			hexExtend = "/mathmode"
		case fnSPACE:
			hexExtend = "/mathmode"
		case fnTEXT:
			typefaceFmt = "{ \\rm{ %v } }"
		case fnSYMBOL:
			// Symbol font uses different character mapping
			// Convert to Symbol PUA range (0xF0xx) for lookup
			isSymbolFont = true
			hexExtend = "/mathmode"
		}

		// For Symbol font, try PUA mapping first (0xF0xx range)
		effectiveMtcode := mtcode
		if isSymbolFont && mtcode < 0x100 {
			effectiveMtcode = 0xF000 | mtcode
		}

		//生成扩展字符的key
		hexCode := fmt.Sprintf("%04x", effectiveMtcode)
		hexKey := fmt.Sprintf("char/0x%v%v", hexCode, hexExtend)

		//首先去找扩展字符 (使用统一的LookupChar查找所有映射表)
		sChar, ok := LookupChar(hexKey)
		if ok {
			char = sChar
		} else {
			// For Symbol font, also try the original mtcode with symbol mapping
			if isSymbolFont {
				hexKeyOrig := fmt.Sprintf("char/0x%04x%v", mtcode, hexExtend)
				sChar, ok = LookupChar(hexKeyOrig)
				if ok {
					char = sChar
				}
			}
			if !ok {
				// Try without mode suffix as fallback
				hexKeyNoMode := fmt.Sprintf("char/0x%v", hexCode)
				sChar, ok = LookupChar(hexKeyNoMode)
				if ok {
					char = sChar
				} else {
					// Try original mtcode without mode suffix
					hexKeyOrigNoMode := fmt.Sprintf("char/0x%04x", mtcode)
					sChar, ok = LookupChar(hexKeyOrigNoMode)
					if ok {
						char = sChar
					} else {
						//如果char是特殊symbol，需要转义
						sChar, ok := SpecialChar[char]
						if ok {
							char = sChar
						}
					}
				}
			}
		}

		//确定字符是否为文本，如果是文本，则需要包一层
		if typefaceFmt != "" {
			char = fmt.Sprintf(typefaceFmt, char)
		}

		buf.WriteString(char)
		return buf.String(), nil
	case TMPL:
		//强制类型转换为MtTmpl
		tmpl := ast.value.(*MtTmpl)

		// MTEF v3 template selector values differ from v5+.
		// In particular:
		// - v3 selector=14 is FRACT (not arrow)
		// - v3 selector=15 is SCRIPT (not integral)
		// If we interpret them using the v5 selector table, combinations/permutations
		// like A_6^6 will turn into integrals (\iint), and some superscripts disappear.
		if m.mMtefVer == 3 {
			switch tmpl.selector {
			case 14: // v3 tmFRACT
				if len(ast.children) >= 2 {
					num, _ := m.makeLatex(ast.children[0])
					den, _ := m.makeLatex(ast.children[1])
					buf.WriteString(fmt.Sprintf("\\frac{%v}{%v}", num, den))
					return buf.String(), nil
				}
			case 15: // v3 tmSCRIPT
				variation := tmpl.variation
				// 0: superscript, 1: subscript, 2: both
				if variation == 0 || variation == 1 {
					content := ""
					for _, c := range ast.children {
						s, _ := m.makeLatex(c)
						if strings.TrimSpace(s) != "" {
							content = s
							break
						}
					}
					if variation == 0 {
						buf.WriteString(fmt.Sprintf("^{%v}", content))
					} else {
						buf.WriteString(fmt.Sprintf("_{%v}", content))
					}
					return buf.String(), nil
				}
				if variation == 2 {
					sub := ""
					sup := ""
					if len(ast.children) > 0 {
						sub, _ = m.makeLatex(ast.children[0])
					}
					if len(ast.children) > 1 {
						sup, _ = m.makeLatex(ast.children[1])
					}
					buf.WriteString(fmt.Sprintf("_{%v}^{%v}", sub, sup))
					return buf.String(), nil
				}
			}
		}

		switch SelectorType(tmpl.selector) {
		case tmANGLE:
			var mainSlot, leftSlot, rightSlot string
			if len(ast.children) > 0 {
				mainSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				leftSlot, _ = m.makeLatex(ast.children[1])
			}
			if len(ast.children) > 2 {
				rightSlot, _ = m.makeLatex(ast.children[2])
			}

			//转成latex代码
			var mainStr, leftStr, rightStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}
			if leftSlot != "" {
				leftStr = fmt.Sprintf("\\left %v", leftSlot)
			}
			if rightSlot != "" {
				rightStr = fmt.Sprintf("\\right %v", rightSlot)
			}

			buf.WriteString(fmt.Sprintf("%v %v %v", leftStr, mainStr, rightStr))
			return buf.String(), nil

		case tmPAREN:
			var mainSlot, leftSlot, rightSlot string
			if len(ast.children) > 0 {
				mainSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				leftSlot, _ = m.makeLatex(ast.children[1])
			}
			if len(ast.children) > 2 {
				rightSlot, _ = m.makeLatex(ast.children[2])
			}

			//转成latex代码
			var mainStr, leftStr, rightStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}
			if leftSlot != "" {
				leftStr = fmt.Sprintf("\\left %v", leftSlot)
			}
			if rightSlot != "" {
				rightStr = fmt.Sprintf("\\right %v", rightSlot)
			}

			buf.WriteString(fmt.Sprintf("%v %v %v", leftStr, mainStr, rightStr))
			return buf.String(), nil
		case tmBRACE:
			var mainSlot, leftSlot, rightSlot string
			for idx, astData := range ast.children {
				if idx == 0 {
					mainSlot, _ = m.makeLatex(astData)
				} else if idx == 1 {
					leftSlot, _ = m.makeLatex(astData)
				} else {
					rightSlot, _ = m.makeLatex(astData)
				}
			}

			if rightSlot == "" {
				rightSlot = "."
			} else {
				rightSlot = " " + rightSlot
			}

			// Avoid nested array when the content is already an array-like block.
			trimmedMain := strings.TrimSpace(mainSlot)
			if strings.HasPrefix(trimmedMain, `\begin{array}{`) && strings.HasSuffix(trimmedMain, `\end{array}`) {
				buf.WriteString(fmt.Sprintf("\\left %v %v \\right%v", leftSlot, mainSlot, rightSlot))
				return buf.String(), nil
			}

			//组装公式
			buf.WriteString(fmt.Sprintf("\\left %v \\begin{array}{l} %v \\end{array} \\right%v", leftSlot, mainSlot, rightSlot))

			return buf.String(), nil
		case tmBRACK:
			var mainSlot, leftSlot, rightSlot string
			if len(ast.children) > 0 {
				mainSlot, _ = m.makeLatex(ast.children[0])
			}
			if mainSlot == "" {
				mainSlot = "\\space"
			}
			if len(ast.children) > 1 {
				leftSlot, _ = m.makeLatex(ast.children[1])
			}
			if len(ast.children) > 2 {
				rightSlot, _ = m.makeLatex(ast.children[2])
			}
			buf.WriteString(fmt.Sprintf("\\left%v %v \\right%v", leftSlot, mainSlot, rightSlot))
			return buf.String(), nil
		case tmBAR:
			//读取数据 ParBoxClass
			var mainSlot, leftSlot, rightSlot string
			for idx, astData := range ast.children {
				if idx == 0 {
					mainSlot, _ = m.makeLatex(astData)
				} else if idx == 1 {
					leftSlot, _ = m.makeLatex(astData)
				} else {
					rightSlot, _ = m.makeLatex(astData)
				}
			}

			if rightSlot == "" {
				rightSlot = "."
			} else {
				rightSlot = " " + rightSlot
			}

			//转成latex代码
			var mainStr, leftStr, rightStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}
			if leftSlot != "" {
				leftStr = fmt.Sprintf("\\left %v", leftSlot)
			}
			if rightSlot != "" {
				rightStr = fmt.Sprintf("\\right %v", rightSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v %v %v", leftStr, mainStr, rightStr)
			buf.WriteString(tmplStr)

			return buf.String(), nil
		case tmINTERVAL:
			//读取数据 ParBoxClass
			var mainSlot, leftSlot, rightSlot string
			if len(ast.children) > 0 {
				mainSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				leftSlot, _ = m.makeLatex(ast.children[1])
			}
			if len(ast.children) > 2 {
				rightSlot, _ = m.makeLatex(ast.children[2])
			}

			//转成latex代码
			var mainStr, leftStr, rightStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}
			if leftSlot != "" {
				leftStr = fmt.Sprintf("\\left %v", leftSlot)
			}
			if rightSlot != "" {
				rightStr = fmt.Sprintf("\\right %v", rightSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v %v %v", leftStr, mainStr, rightStr)
			buf.WriteString(tmplStr)

			return buf.String(), nil
		case tmROOT:
			var mainSlot, radiSlot string
			if len(ast.children) > 0 {
				mainSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				radiSlot, _ = m.makeLatex(ast.children[1])
			}
			if radiSlot != "" {
				buf.WriteString(fmt.Sprintf("\\sqrt[%v] { %v }", radiSlot, mainSlot))
			} else {
				buf.WriteString(fmt.Sprintf("\\sqrt { %v }", mainSlot))
			}
			return buf.String(), nil
		case tmFRACT:
			var numSlot, denSlot string
			if len(ast.children) > 0 {
				numSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				denSlot, _ = m.makeLatex(ast.children[1])
			}
			buf.WriteString(fmt.Sprintf("\\frac { %v } { %v }", numSlot, denSlot))
			return buf.String(), nil
		case tmARROW:
			/*
				variation	symbol	description
				0×0000	tvAR_SINGLE	single arrow
				0×0001	tvAR_DOUBLE	double arrow
				0×0002	tvAR_HARPOON	harpoon
				0×0004	tvAR_TOP	top slot is present
				0×0008	tvAR_BOTTOM	bottom slot is present
				0×0010	tvAR_LEFT	if single, arrow points left
				0×0020	tvAR_RIGHT	if single, arrow points right
				0×0010	tvAR_LOS	if double or harpoon, large over small
				0×0020	tvAR_SOL	if double or harpoon, small over large
			*/
			var topSlot, bottomSlot string
			if len(ast.children) > 0 {
				topSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				bottomSlot, _ = m.makeLatex(ast.children[1])
			}

			/*
				variation转码
			*/
			variationsMap := make(map[uint16]string)
			variationsMap[0x0000] = "single"
			variationsMap[0x0001] = "double"
			variationsMap[0x0002] = "harpoon"
			variationsMap[0x0004] = "topSlotPresent"
			variationsMap[0x0008] = "bottomSlotPresent"
			variationsMap[0x0010] = "pointLeft"
			variationsMap[0x0020] = "pointRight"

			//有序循环
			variationsCode := []uint16{0x0000, 0x0001, 0x0002, 0x0004, 0x0008, 0x0010, 0x0020}

			arrowStyle := "single"
			pointLeft := false
			pointRight := false
			for _, vCode := range variationsCode {
				if vCode&uint16(tmpl.variation) != 0 {
					if variationsMap[vCode] == "double" {
						arrowStyle = "double"
					} else if variationsMap[vCode] == "harpoon" {
						arrowStyle = "harpoon"
					} else if variationsMap[vCode] == "pointLeft" {
						pointLeft = true
					} else if variationsMap[vCode] == "pointRight" {
						pointRight = true
					}
				}
			}

			// Heuristic compatibility:
			// Some legacy MTEF streams (esp. v3) encode fractions using selector=14 (tmARROW),
			// with numerator/denominator stored in the top/bottom slots and variation lacking
			// explicit arrow direction/style bits. Preserve this behavior when it clearly
			// looks like a fraction.
			if (uint16(tmpl.variation)&(0x0001|0x0002|0x0010|0x0020) == 0) && (topSlot != "" || bottomSlot != "") {
				buf.WriteString(fmt.Sprintf("\\frac{%v}{%v}", topSlot, bottomSlot))
				return buf.String(), nil
			}

			// Build base arrow command
			base := "\\rightarrow"
			if arrowStyle == "double" {
				base = "\\leftrightarrow"
			} else if arrowStyle == "harpoon" {
				// KaTeX supports common harpoons; default to right harpoon.
				base = "\\rightharpoonup"
			}
			if pointLeft && !pointRight {
				if arrowStyle == "double" {
					base = "\\leftrightarrow"
				} else if arrowStyle == "harpoon" {
					base = "\\leftharpoonup"
				} else {
					base = "\\leftarrow"
				}
			}

			// Apply optional top/bottom labels.
			// Use \overset/\underset for KaTeX compatibility.
			arrow := base
			if topSlot != "" {
				arrow = fmt.Sprintf("\\overset{%v}{%v}", topSlot, arrow)
			}
			if bottomSlot != "" {
				arrow = fmt.Sprintf("\\underset{%v}{%v}", bottomSlot, arrow)
			}
			buf.WriteString(arrow)

			return buf.String(), nil
		case tmUBAR:
			//读取数据
			mainAST := ast.children[0]

			//读取latex数据
			mainSlot, _ := m.makeLatex(mainAST)

			//转成latex代码
			var mainStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf(" {\\underline{ %v }} ", mainSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf(" %v ", mainStr)
			buf.WriteString(tmplStr)

			//返回数据
			return buf.String(), nil
		case tmSUM:
			//读取数据 BigOpBoxClass
			var mainSlot, upperSlot, lowerSlot, operatorSlot string
			for idx, astData := range ast.children {
				if idx == 0 {
					mainSlot, _ = m.makeLatex(astData)
				} else if idx == 1 {
					lowerSlot, _ = m.makeLatex(astData)
				} else if idx == 2 {
					upperSlot, _ = m.makeLatex(astData)
				} else {
					operatorSlot, _ = m.makeLatex(astData)
				}
			}

			//转成latex代码
			var mainStr, lowerStr, upperStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}
			if lowerSlot != "" {
				lowerStr = fmt.Sprintf("\\limits_{ %v }", lowerSlot)
			}
			if upperSlot != "" {
				upperStr = fmt.Sprintf("^ %v", upperSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v %v %v %v", operatorSlot, lowerStr, upperStr, mainStr)
			buf.WriteString(tmplStr)

			return buf.String(), nil
		case tmLIM:
			//读取数据 LimBoxClass
			var mainSlot, lowerSlot, upperSlot string
			for idx, astData := range ast.children {
				if idx == 0 {
					mainSlot, _ = m.makeLatex(astData)
				} else if idx == 1 {
					lowerSlot, _ = m.makeLatex(astData)
				} else {
					upperSlot, _ = m.makeLatex(astData)
				}
			}

			//转成latex代码
			var mainStr, lowerStr, upperStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("\\mathop { %v }", mainSlot)
			}
			if lowerSlot != "" {
				lowerStr = fmt.Sprintf("\\limits_{ %v }", lowerSlot)
			}
			if upperSlot != "" {
				upperStr = ""
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v %v %v", mainStr, lowerStr, upperStr)
			buf.WriteString(tmplStr)

			return buf.String(), nil
		case tmSUP:
			var subSlot, supSlot string
			if len(ast.children) > 0 {
				subSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				supSlot, _ = m.makeLatex(ast.children[1])
			}

			buf.WriteString(" ^ { ")
			buf.WriteString(supSlot)
			buf.WriteString(" } ")
			if subSlot != "" {
				buf.WriteString(" { ")
				buf.WriteString(subSlot)
				buf.WriteString(" } ")
			}
			return buf.String(), nil
		case tmSUB:
			//读取下标和上标
			var subSlot, supSlot string
			if len(ast.children) > 0 {
				subSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				supSlot, _ = m.makeLatex(ast.children[1])
			}

			//转成latex代码
			var subFmt, supFmt string
			if subSlot != "" {
				subFmt = fmt.Sprintf("_{ %v }", subSlot)
			}
			if supSlot != "" {
				supFmt = fmt.Sprintf("^{ %v }", supSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v  %v", subFmt, supFmt)
			buf.WriteString(tmplStr)

			//返回数据
			return buf.String(), nil
		case tmSUBSUP:
			//读取下标和上标
			var subSlot, supSlot string
			if len(ast.children) > 0 {
				subSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				supSlot, _ = m.makeLatex(ast.children[1])
			}

			//转成latex代码
			var subFmt, supFmt string
			if subSlot != "" {
				subFmt = fmt.Sprintf("_{ %v }", subSlot)
			}
			if supSlot != "" {
				supFmt = fmt.Sprintf("^{ %v }", supSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v  %v", subFmt, supFmt)
			buf.WriteString(tmplStr)

			//返回数据
			return buf.String(), nil
		case tmVEC:
			/*
				variations：
				variation	symbol	description
				0×0001	tvVE_LEFT	arrow points left
				0×0002	tvVE_RIGHT	arrow points right
				0×0004	tvVE_UNDER	arrow under slot, else over slot
				0×0008	tvVE_HARPOON	harpoon

				这个转换是通过掩码计算的：
				比如variation的值是3，即0000 0000 0000 0011

				对应的是0×0001和0×0002：
				0000 0000 0000 0001
				0000 0000 0000 0010
			*/

			//读取数据 HatBoxClass
			mainAST := ast.children[0]

			//读取latex数据
			mainSlot, _ := m.makeLatex(mainAST)

			//转成latex代码
			var mainStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}

			/*
				variation转码
			*/
			variationsMap := make(map[uint16]string)
			variationsMap[0x0001] = "left"
			variationsMap[0x0002] = "right"
			variationsMap[0x0004] = "tvVE_UNDER"
			variationsMap[0x0008] = "harpoonup"

			//有序循环
			variationsCode := []uint16{0x0001, 0x0002, 0x0004, 0x0008}

			topStr := "\\overset\\"
			for _, vCode := range variationsCode {
				if vCode&uint16(tmpl.variation) != 0 {
					topStr = topStr + variationsMap[vCode]
				}
			}

			//如果variationCode小于8，则一定不是harpoon,那么默认就使用arrow
			if tmpl.variation < 8 {
				topStr = topStr + "arrow"
			}
			/*
				variation转码 END
			*/

			//组成整体公式
			tmplStr := fmt.Sprintf("%v %v", topStr, mainStr)
			buf.WriteString(tmplStr)

			return buf.String(), nil
		case tmHAT:
			//读取数据 HatBoxClass
			var mainSlot, topSlot string
			if len(ast.children) > 0 {
				mainSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				topSlot, _ = m.makeLatex(ast.children[1])
			}

			//转成latex代码
			var mainStr, topStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}
			if topSlot != "" {
				topStr = fmt.Sprintf(" %v ", topSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v %v", topStr, mainStr)
			buf.WriteString(tmplStr)

			return buf.String(), nil
		case tmARC:
			//读取数据 HatBoxClass
			var mainSlot, topSlot string
			if len(ast.children) > 0 {
				mainSlot, _ = m.makeLatex(ast.children[0])
			}
			if len(ast.children) > 1 {
				topSlot, _ = m.makeLatex(ast.children[1])
			}

			//转成latex代码
			var mainStr, topStr string
			if mainSlot != "" {
				mainStr = fmt.Sprintf("{ %v }", mainSlot)
			}
			if topSlot != "" {
				topStr = fmt.Sprintf("\\overset %v", topSlot)
			}

			//组成整体公式
			tmplStr := fmt.Sprintf("%v %v", topStr, mainStr)
			buf.WriteString(tmplStr)

			return buf.String(), nil
		case tmINTEG:
			// Integral template - BigOpBoxClass structure:
			// variation bits determine the integral type:
			//   0x0001 = tvINT_1 (single integral)
			//   0x0002 = tvINT_2 (double integral)
			//   0x0003 = tvINT_3 (triple integral)
			//   0x0004 = tvINT_LOOP (contour integral)
			//   0x0000 = no integral symbol (used for subscript-like notation)
			//
			// However, MathType sometimes uses tmINTEG with variation=0x0001 for
			// subscript notation like (5350)₇ (base notation), not actual integrals.
			// We detect this by checking if the structure looks like a subscript:
			// - If mainSlot is a simple number and lower/upper are single digits/numbers,
			//   it's likely base notation, not an integral.
			//
			// Child structure (like other BigOpBoxClass):
			// child[0]: main body (may be empty LINE)
			// child[1]: lower limit
			// child[2]: upper limit
			var mainSlot, lowerSlot, upperSlot string
			for idx, astData := range ast.children {
				if idx == 0 {
					mainSlot, _ = m.makeLatex(astData)
				} else if idx == 1 {
					lowerSlot, _ = m.makeLatex(astData)
				} else if idx == 2 {
					upperSlot, _ = m.makeLatex(astData)
				}
			}

			// Determine the integral symbol based on variation
			intType := tmpl.variation & 0x000F

			// Check if this looks like subscript notation (e.g., base notation)
			// Heuristics: if the "integral" has no main body, or if the limits look like
			// simple numbers/bases (not typical integral bounds), treat as subscript
			isSubscriptNotation := false
			if intType == 0x0000 {
				isSubscriptNotation = true
			} else if intType == 0x0001 {
				// Check for patterns that suggest base notation:
				// - Empty main slot with numeric lower slot (like _7 for base 7)
				// - Main slot that's just a number (like "10" for base 10)
				// - Upper slot that's just a number (exponent-like)
				mainTrimmed := strings.TrimSpace(mainSlot)
				lowerTrimmed := strings.TrimSpace(lowerSlot)
				upperTrimmed := strings.TrimSpace(upperSlot)

				// Check if main is purely numeric
				isMainNumeric := true
				for _, r := range mainTrimmed {
					if !((r >= '0' && r <= '9') || r == ' ') {
						isMainNumeric = false
						break
					}
				}

				// If main slot is just a small number (like "10", "7", "3"), treat as subscript base
				if mainTrimmed != "" && isMainNumeric && len(mainTrimmed) <= 3 {
					// This is likely base notation like _10 (base 10)
					isSubscriptNotation = true
				}

				// Also check: if both lower and upper are empty but main has content
				if mainTrimmed != "" && lowerTrimmed == "" && upperTrimmed == "" {
					// Just a single element in what looks like integral template
					// Treat as subscript notation
					isSubscriptNotation = true
				}
			}

			if isSubscriptNotation {
				// Output as subscript notation (for base notation like (5350)_7)
				// When tmINTEG is used for subscript:
				// - The expression being subscripted (e.g., "(5350)") is BEFORE the template
				// - mainSlot = the subscript value (e.g., "7" for base 7)
				// - lowerSlot/upperSlot may contain following expressions
				// So we output: _{mainSlot} followed by continuation
				if mainSlot != "" {
					buf.WriteString(fmt.Sprintf("_{%v}", mainSlot))
				}
				// Append any continuation (lower/upper slots contain sibling expressions)
				if lowerSlot != "" {
					buf.WriteString(lowerSlot)
				}
				if upperSlot != "" {
					buf.WriteString(upperSlot)
				}
				return buf.String(), nil
			}

			// Real integral - determine symbol
			var intSymbol string
			switch intType {
			case 0x0001:
				intSymbol = "\\int"
			case 0x0002:
				intSymbol = "\\iint"
			case 0x0003:
				intSymbol = "\\iiint"
			case 0x0004, 0x0008, 0x000C:
				intSymbol = "\\oint"
			default:
				intSymbol = "\\int"
			}

			// Build integral with limits and body
			if lowerSlot != "" || upperSlot != "" {
				buf.WriteString(fmt.Sprintf("%v_{%v}^{%v}", intSymbol, lowerSlot, upperSlot))
			} else {
				buf.WriteString(intSymbol)
			}
			if mainSlot != "" {
				buf.WriteString(fmt.Sprintf("{%v}", mainSlot))
			}
			return buf.String(), nil
		case tmLSCRIPT:
			// Left scripts (left super/sub scripts). Used in some legacy streams.
			// Variations (common):
			// 0: left superscript only
			// 1: left subscript only
			// 2: left subscript and superscript
			variation := tmpl.variation
			if variation == 0 {
				// Find first non-empty child as superscript content.
				sup := ""
				for _, c := range ast.children {
					s, _ := m.makeLatex(c)
					if strings.TrimSpace(s) != "" {
						sup = s
						break
					}
				}
				buf.WriteString(fmt.Sprintf("{}^{%v}", sup))
				return buf.String(), nil
			} else if variation == 1 {
				sub := ""
				for _, c := range ast.children {
					s, _ := m.makeLatex(c)
					if strings.TrimSpace(s) != "" {
						sub = s
						break
					}
				}
				buf.WriteString(fmt.Sprintf("{}_{%v}", sub))
				return buf.String(), nil
			} else if variation == 2 {
				sub := ""
				sup := ""
				if len(ast.children) > 0 {
					sub, _ = m.makeLatex(ast.children[0])
				}
				if len(ast.children) > 1 {
					sup, _ = m.makeLatex(ast.children[1])
				}
				buf.WriteString(fmt.Sprintf("{}_{%v}^{%v}", sub, sup))
				return buf.String(), nil
			}
			// Fallback: concatenate children
			for _, c := range ast.children {
				s, _ := m.makeLatex(c)
				buf.WriteString(s)
			}
			return buf.String(), nil
		default:
			m.Valid = false
			log.Println("TMPL NOT IMPLEMENT", tmpl.selector, tmpl.variation)
		}
		for _, _ast := range ast.children {
			_latex, _ := m.makeLatex(_ast)
			buf.WriteString(_latex)
		}
		return buf.String(), nil
	case PILE:
		for idx, _ast := range ast.children {
			_latex, _ := m.makeLatex(_ast)

			//多个line字符串数据以 \\ 分割
			if idx > 0 {
				buf.WriteString(" \\\\ ")
			}

			buf.WriteString(_latex)
		}
		return buf.String(), nil
	case MATRIX:
		matrixCols := int(ast.value.(*MtMatrix).cols)
		matrixRows := int(ast.value.(*MtMatrix).rows)
		if matrixCols <= 0 {
			matrixCols = 1
		}
		// Build a valid array column spec (KaTeX requires it to match the number of columns).
		colSpec := strings.Repeat("c", matrixCols)
		buf.WriteString(" \\begin{array}{")
		buf.WriteString(colSpec)
		buf.WriteString("} ")

		expected := matrixRows * matrixCols
		cells := ast.children
		if expected > 0 && expected < len(cells) {
			// Some MathType exports prepend phantom/empty cells before real content.
			// If we truncate blindly, we may drop valid trailing equations.
			// Prefer dropping leading empty cells first, then apply length cap.
			extra := len(cells) - expected
			start := 0
			for start < len(cells) && extra > 0 {
				peek, _ := m.makeLatex(cells[start])
				trimmed := strings.TrimSpace(peek)
				if trimmed == "" || trimmed == "{}" {
					start++
					extra--
					continue
				}
				break
			}
			if start > 0 {
				cells = cells[start:]
			}
			if expected < len(cells) {
				cells = cells[:expected]
			}
		}
		for i, cellAst := range cells {
			cell, _ := m.makeLatex(cellAst)
			cell = strings.TrimSpace(cell)
			if cell == "" {
				cell = "{}"
			}
			buf.WriteString(cell)
			if (i+1)%matrixCols == 0 {
				// New row, unless it's the last cell.
				if i != len(cells)-1 {
					buf.WriteString(" \\\\ ")
				}
			} else {
				buf.WriteString(" & ")
			}
		}

		buf.WriteString(" \\end{array} ")
		return buf.String(), nil
	case LINE:
		// Some Word/MathType content uses an "empty matrix/array row" as an indent marker,
		// followed by real content on the same line, e.g.:
		//   4S=  \begin{array}{ccc} {}&{}&{} \end{array}  1\times 4^2 + ...
		// v3h behavior: keep it simple and only wrap the shifted tail into a single-row array
		// (remaining columns left empty). This is more compatible with some downstream renderers.
		for i := 0; i < len(ast.children); i++ {
			c := ast.children[i]
			if c.tag == MATRIX && i+1 < len(ast.children) {
				matAst := c
				mat := matAst.value.(*MtMatrix)
				cols := int(mat.cols)
				if cols <= 0 {
					continue
				}
				allEmpty := true
				for _, cellAst := range matAst.children {
					s, _ := m.makeLatex(cellAst)
					if strings.TrimSpace(s) != "" && strings.TrimSpace(s) != "{}" {
						allEmpty = false
						break
					}
				}
				if !allEmpty {
					continue
				}
				var rest strings.Builder
				for _, r := range ast.children[i+1:] {
					s, _ := m.makeLatex(r)
					rest.WriteString(s)
				}
				restStr := strings.TrimSpace(rest.String())
				if restStr == "" {
					continue
				}

				// Emit the prefix (before the matrix)
				for _, p := range ast.children[:i] {
					s, _ := m.makeLatex(p)
					buf.WriteString(s)
				}

				colSpec := strings.Repeat("l", cols)
				buf.WriteString("\\begin{array}{")
				buf.WriteString(colSpec)
				buf.WriteString("} ")
				buf.WriteString("{ ")
				buf.WriteString(restStr)
				buf.WriteString(" }")
				for k := 1; k < cols; k++ {
					buf.WriteString(" & {}")
				}
				buf.WriteString(" \\end{array} ")
				return buf.String(), nil
			}
		}

		for _, _ast := range ast.children {
			_latex, _ := m.makeLatex(_ast)
			buf.WriteString(_latex)
		}
		return buf.String(), nil
	case EMBELL:
		embellType := EmbellType(ast.value.(*MtEmbellRd).embellType)
		var embellStr string

		switch embellType {
		case emb1DOT:
			embellStr = " \\dot "
		case emb1PRIME:
			embellStr = "'"
		case emb2PRIME:
			embellStr = "''"
		case emb3PRIME:
			embellStr = "'''"
		case embHAT:
			embellStr = " \\hat "
		case embOBAR:
			embellStr = " \\bar "
		default:
			log.Println("not implement embell:", embellType)
		}

		buf.WriteString(embellStr)
		return buf.String(), nil
	}

	return "", nil
}

func wrapMathRoot(content string) string {
	if content == "" {
		return ""
	}
	if isWrappedMath(content) {
		return content
	}
	return "$$ " + content + " $$"
}

func isWrappedMath(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$") && len(trimmed) >= 4 {
		return true
	}
	if strings.HasPrefix(trimmed, "$") && strings.HasSuffix(trimmed, "$") && len(trimmed) >= 2 {
		return true
	}
	return false
}

// [MTEF Storage](https://docs.wiris.com/en/mathtype/mathtype_desktop/mathtype-sdk/mtefstorage)
func Open(reader io.ReadSeeker) (eqn *MTEFv5, err error) {
	//parse `mtef` stream from `ole` object
	ole, err := ole2.Open(reader, "")
	if err != nil {
		fmt.Println(err)
	}

	dir, err := ole.ListDir()
	if err != nil {
		fmt.Println(err)
	}

	for _, file := range dir {
		if "Equation Native" == file.Name() {
			root := dir[0]
			reader := ole.OpenFile(file, root)

			hdrBuffer := make([]byte, oleCbHdr)
			if _, err := reader.Read(hdrBuffer); err == nil {
				hdrReader := bytes.NewReader(hdrBuffer)
				var cbHdr = uint16(0)
				var cbSize = uint32(0)

				_ = binary.Read(hdrReader, binary.LittleEndian, &cbHdr)
				if cbHdr != oleCbHdr {
					return nil, err
				}

				//ignore `version: u32` and `cf: u16`
				_, _ = hdrReader.Seek(4+2, io.SeekCurrent)
				_ = binary.Read(hdrReader, binary.LittleEndian, &cbSize)

				//body from `cbHdr` to `cbHdr + cbSize`
				eqnBody := make([]byte, cbSize)
				_, _ = reader.Seek(int64(cbHdr), io.SeekStart)
				_, _ = reader.Read(eqnBody)

				eqn = new(MTEFv5)
				eqn.reader = bytes.NewReader(eqnBody)
				_ = eqn.readRecord()
				_ = eqn.makeAST()
				return eqn, nil
			}

			return nil, err
		}
	}
	return nil, err
}
