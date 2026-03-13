package latexmap

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/zhexiao/mtef-go/eqn"
)

var (
	once    sync.Once
	runeMap map[rune]string
)

var keyRe = regexp.MustCompile(`^char/0x([0-9a-fA-F]{4,5})(/(?:mathmode|textmode))?$`)

// Overrides for KaTeX compatibility or OMML-specific expectations.
var overrides = map[rune]string{
	// KaTeX often doesn't support \oiint reliably; use \iint.
	0x222f: `\iint `,
	// KaTeX does not reliably support \Bbbsum.
	0x2140: `\sum `,
}

func buildRuneMap() {
	runeMap = make(map[rune]string, 8192)

	// process adds mappings from a charMap. If allowOverwrite is false, existing
	// entries are preserved; if true, new entries overwrite old ones.
	process := func(charMap map[string]string, wantMathMode bool, allowOverwrite bool) {
		for k, v := range charMap {
			m := keyRe.FindStringSubmatch(k)
			if m == nil {
				continue
			}
			isMath := m[2] != ""
			if isMath != wantMathMode {
				continue
			}
			code64, err := strconv.ParseInt(m[1], 16, 32)
			if err != nil {
				continue
			}
			r := rune(code64)

			// Never map '&' from eqn (OMML uses it for alignment).
			if r == '&' {
				continue
			}
			// Skip most ASCII to avoid changing normal text behavior; keep < and > for HTML safety.
			if r < 0x00A0 && r != '<' && r != '>' {
				continue
			}
			// Skip obviously broken mappings.
			if strings.Contains(v, "\uFFFD") {
				continue
			}
			// If already mapped (from mathmode pass), keep it unless we're allowing overwrite.
			if _, exists := runeMap[r]; exists && !allowOverwrite {
				continue
			}

			runeMap[r] = ensureSafeSpacing(v)
		}
	}

	// 1. Process base Chars map (lowest priority)
	process(eqn.Chars, true, false)
	process(eqn.Chars, false, false)

	// 2. Process extended chars (can add new mappings but not overwrite)
	process(eqn.ExtendedChars, true, false)
	process(eqn.ExtendedChars, false, false)

	// 3. Process docx2tex KaTeX chars (higher priority - can overwrite previous)
	// These are extracted from docx2tex's comprehensive katexmap.xml
	process(eqn.DocxTexKatexChars, true, true)
	process(eqn.DocxTexKatexChars, false, true)

	// 4. Apply hardcoded overrides last (highest priority).
	for r, v := range overrides {
		runeMap[r] = v
	}
}

func ensureSafeSpacing(v string) string {
	// Many eqn mappings already include trailing spaces. For KaTeX robustness, ensure
	// command-like replacements don't glue to following letters/digits.
	v = strings.TrimSuffix(v, "\t")
	if strings.HasPrefix(v, "\\") && !strings.HasSuffix(v, " ") {
		rs := []rune(v)
		if len(rs) > 0 {
			last := rs[len(rs)-1]
			if unicode.IsLetter(last) || last == '}' {
				return v + " "
			}
		}
	}
	return v
}

// ReplaceEqnChars maps Unicode symbols in a string using (a safe subset of) eqn.Chars.
// It is designed for OMML math runs where many symbols appear as Unicode characters.
func ReplaceEqnChars(s string) string {
	once.Do(buildRuneMap)
	var b strings.Builder
	b.Grow(len(s) + 16)
	for _, r := range s {
		if repl, ok := runeMap[r]; ok {
			b.WriteString(repl)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}


