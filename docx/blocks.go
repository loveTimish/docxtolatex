package docx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	arabicListMarkerRe = regexp.MustCompile(`^\s*(\d{1,2})[、]\s*(.+)$`)
	parenListMarkerRe  = regexp.MustCompile(`^\s*[（(](\d{1,2})[）)]\s*(.+)$`)
	dotListMarkerRe    = regexp.MustCompile(`^\s*(\d{1,2})[\.．]\s+(.+)$`)
)

func renderParagraphBlocks(paragraphs []paragraphBlock, cfg Config, report *ConversionReport) string {
	paragraphs = promoteTextualLists(paragraphs, cfg)

	var buf strings.Builder
	var stack []numberingLevel

	closeToDepth := func(depth int) {
		for len(stack) > depth {
			env := stack[len(stack)-1].Environment
			if env == "" {
				env = "enumerate"
			}
			buf.WriteString(`\end{` + env + `}`)
			buf.WriteString("\n")
			stack = stack[:len(stack)-1]
		}
	}

	openToDepth := func(depth int, def numberingLevel) {
		env := def.Environment
		if env == "" {
			env = "enumerate"
		}
		for len(stack) < depth {
			buf.WriteString(`\begin{` + env + `}`)
			buf.WriteString("\n")
			stack = append(stack, numberingLevel{Environment: env, NumFmt: def.NumFmt, LvlText: def.LvlText})
		}
	}

	for _, p := range paragraphs {
		trimmed := strings.TrimSpace(p.Content)
		if isWorksheetSectionTitle(trimmed) && !isConfiguredHeadingStyle(p.Style, cfg) {
			closeToDepth(0)
			buf.WriteString(fmt.Sprintf(`\subsection*{%s}`, trimmed))
			buf.WriteString("\n\n")
			if report != nil {
				report.Summary.WorksheetSections++
			}
			continue
		}
		if p.List != nil {
			depth := p.List.Level + 1
			if depth < 1 {
				depth = 1
			}
			if len(stack) > depth {
				closeToDepth(depth)
			}
			if len(stack) == depth && depth > 0 {
				currentEnv := stack[depth-1].Environment
				nextEnv := p.List.Def.Environment
				if nextEnv == "" {
					nextEnv = "enumerate"
				}
				if currentEnv != nextEnv {
					closeToDepth(depth - 1)
				}
			}
			openToDepth(depth, p.List.Def)
			if p.ListContinuation {
				if trimmed == "" {
					buf.WriteString("\n")
				} else {
					buf.WriteString(trimmed)
					buf.WriteString("\n\n")
				}
			} else if trimmed != "" {
				buf.WriteString(`\item `)
				buf.WriteString(trimmed)
				buf.WriteString("\n")
				if report != nil {
					report.Summary.ListItems++
				}
			}
			continue
		}

		closeToDepth(0)
		if trimmed == "" {
			buf.WriteString("\n")
			continue
		}
		buf.WriteString(renderParagraph(p.Content, p.Style, cfg))
	}

	closeToDepth(0)
	return buf.String()
}

func promoteTextualLists(paragraphs []paragraphBlock, cfg Config) []paragraphBlock {
	if len(paragraphs) == 0 {
		return paragraphs
	}

	out := append([]paragraphBlock(nil), paragraphs...)
	inWorksheetSection := false

	for i := 0; i < len(out); {
		trimmed := strings.TrimSpace(out[i].Content)
		if isWorksheetSectionTitle(trimmed) {
			inWorksheetSection = true
			i++
			continue
		}
		if trimmed == "" {
			i++
			continue
		}
		if isConfiguredHeadingStyle(out[i].Style, cfg) && !isWorksheetSectionTitle(trimmed) {
			inWorksheetSection = false
		}
		if out[i].List != nil || out[i].ListContinuation {
			i++
			continue
		}

		firstNum, firstBody, ok := parseTextualListMarker(trimmed)
		if !ok {
			i++
			continue
		}

		minRun := 3
		if inWorksheetSection {
			minRun = 2
		}

		itemStarts := []int{i}
		itemBodies := []string{firstBody}
		lastNum := firstNum
		boundary := len(out)
		cursor := i + 1
		for cursor < len(out) {
			candidate := strings.TrimSpace(out[cursor].Content)
			if isWorksheetSectionTitle(candidate) || isConfiguredHeadingStyle(out[cursor].Style, cfg) || out[cursor].List != nil {
				boundary = cursor
				break
			}
			nextNum, nextBody, nextOK := parseTextualListMarker(candidate)
			if nextOK {
				if nextNum == lastNum+1 {
					itemStarts = append(itemStarts, cursor)
					itemBodies = append(itemBodies, nextBody)
					lastNum = nextNum
					cursor++
					continue
				}
				if nextNum <= lastNum {
					boundary = cursor
					break
				}
			}
			cursor++
		}

		if len(itemStarts) < minRun {
			i++
			continue
		}

		listDef := &listRef{NumID: "textual-decimal", Level: 0, Def: numberingLevel{NumFmt: "decimal", Environment: "enumerate"}}
		for idx, paraIdx := range itemStarts {
			out[paraIdx].Content = itemBodies[idx]
			out[paraIdx].List = listDef
			out[paraIdx].ListContinuation = false

			end := boundary
			if idx+1 < len(itemStarts) {
				end = itemStarts[idx+1]
			}
			for k := paraIdx + 1; k < end; k++ {
				out[k].List = listDef
				out[k].ListContinuation = true
			}
		}
		i = boundary
	}

	return out
}

func parseTextualListMarker(content string) (int, string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return 0, "", false
	}
	for _, re := range []*regexp.Regexp{arabicListMarkerRe, parenListMarkerRe, dotListMarkerRe} {
		m := re.FindStringSubmatch(trimmed)
		if len(m) != 3 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		body := strings.TrimSpace(m[2])
		if body == "" {
			continue
		}
		return n, body, true
	}
	return 0, "", false
}

func isWorksheetSectionTitle(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || strings.Contains(trimmed, "\n") {
		return false
	}
	if strings.Contains(trimmed, "（每题") || strings.Contains(trimmed, "(每题") {
		return strings.Contains(trimmed, "题")
	}
	return false
}

func isConfiguredHeadingStyle(style string, cfg Config) bool {
	normalized := normalizeStyleName(style)
	if normalized == "" {
		return false
	}
	format, ok := cfg.Styles[normalized]
	if !ok {
		return false
	}
	format = strings.ToLower(strings.TrimSpace(format))
	return strings.Contains(format, `\section`) || strings.Contains(format, `\subsection`) || strings.Contains(format, `\subsubsection`)
}
