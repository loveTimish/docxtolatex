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

	answerContextMarkers = []string{
		"【答案】", "【详解】", "【解析】", "【解答】", "【证明】",
		"答案：", "详解：", "解析：", "解答：", "证明：", "解：",
	}
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

		firstMarker, ok := parseTextualListMarker(trimmed)
		if !ok || !allowTextualListMarker(out, i, firstMarker, inWorksheetSection) {
			i++
			continue
		}

		minRun := 3
		if inWorksheetSection {
			minRun = 2
		}

		itemStarts := []int{i}
		itemBodies := []string{firstMarker.Body}
		lastNum := firstMarker.Number
		boundary := len(out)
		cursor := i + 1
		for cursor < len(out) {
			candidate := strings.TrimSpace(out[cursor].Content)
			if isWorksheetSectionTitle(candidate) || isConfiguredHeadingStyle(out[cursor].Style, cfg) || out[cursor].List != nil {
				boundary = cursor
				break
			}
			nextMarker, nextOK := parseTextualListMarker(candidate)
			if nextOK && allowTextualListMarker(out, cursor, nextMarker, inWorksheetSection) {
				if nextMarker.Number == lastNum+1 {
					itemStarts = append(itemStarts, cursor)
					itemBodies = append(itemBodies, nextMarker.Body)
					lastNum = nextMarker.Number
					cursor++
					continue
				}
				if nextMarker.Number <= lastNum {
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

		listDef := &listRef{NumID: "textual-" + firstMarker.Kind, Level: 0, Def: numberingLevel{NumFmt: "decimal", Environment: "enumerate"}}
		for idx, paraIdx := range itemStarts {
			out[paraIdx].Content = itemBodies[idx]
			out[paraIdx].List = listDef
			out[paraIdx].ListContinuation = false

			end := boundary
			if idx+1 < len(itemStarts) {
				end = itemStarts[idx+1]
			}
			for k := paraIdx + 1; k < end; k++ {
				contTrimmed := strings.TrimSpace(out[k].Content)
				if listDef.NumID != "textual-arabic-comma" && hasAnswerPrefix(contTrimmed) {
					break
				}
				out[k].List = listDef
				out[k].ListContinuation = true
			}
		}
		i = boundary
	}

	return out
}

type textualListMarker struct {
	Kind   string
	Number int
	Body   string
}

func parseTextualListMarker(content string) (textualListMarker, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return textualListMarker{}, false
	}
	cases := []struct {
		kind string
		re   *regexp.Regexp
	}{
		{kind: "arabic-comma", re: arabicListMarkerRe},
		{kind: "paren", re: parenListMarkerRe},
		{kind: "dot", re: dotListMarkerRe},
	}
	for _, c := range cases {
		m := c.re.FindStringSubmatch(trimmed)
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
		return textualListMarker{Kind: c.kind, Number: n, Body: body}, true
	}
	return textualListMarker{}, false
}

func allowTextualListMarker(paragraphs []paragraphBlock, idx int, marker textualListMarker, inWorksheetSection bool) bool {
	if hasAnswerPrefix(marker.Body) {
		return false
	}
	if marker.Kind == "arabic-comma" {
		return true
	}
	if !inWorksheetSection {
		return true
	}
	return !hasRecentAnswerContext(paragraphs, idx)
}

func hasAnswerPrefix(content string) bool {
	trimmed := strings.TrimSpace(content)
	for _, marker := range answerContextMarkers {
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	return false
}

func hasRecentAnswerContext(paragraphs []paragraphBlock, idx int) bool {
	seen := 0
	for i := idx; i >= 0 && seen < 4; i-- {
		trimmed := strings.TrimSpace(paragraphs[i].Content)
		if trimmed == "" {
			continue
		}
		if isWorksheetSectionTitle(trimmed) {
			break
		}
		if hasAnswerPrefix(trimmed) {
			return true
		}
		seen++
	}
	return false
}

func isWorksheetSectionTitle(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || strings.Contains(trimmed, "\n") {
		return false
	}
	if strings.Contains(trimmed, "（每题") || strings.Contains(trimmed, "(每题") {
		return strings.Contains(trimmed, "题")
	}
	if strings.Contains(trimmed, "（") || strings.Contains(trimmed, "(") {
		for _, keyword := range []string{"计算题", "填空题", "解答题", "综合题", "选择题", "证明题", "应用题", "实验题"} {
			if strings.Contains(trimmed, keyword) {
				return true
			}
		}
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
