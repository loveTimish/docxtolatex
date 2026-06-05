package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zhexiao/mtef-go/docx"
)

var (
	displayMathRe = regexp.MustCompile(`\$\$[\s\S]*?\$\$`)
	inlineMathRe  = regexp.MustCompile(`(?s)(^|[^$])\$([^$].*?)\$([^$]|$)`)
	reviewRes     = []reviewRule{
		{Name: "image-placeholder", Detail: "fallback or extracted image placeholder remains in text", Re: regexp.MustCompile(`beginPic\{[^}]+\}endPic`)},
		{Name: "split-parenthesized-math", Detail: "math delimiters split a parenthesized phrase with surrounding prose", Re: regexp.MustCompile(`\$[^$]*\([^$]*\$[\p{Han}]|[\p{Han}][^$]*\$[^$]*\)\$`)},
		{Name: "escaped-dollar-text", Detail: "literal escaped dollar text suggests LaTeX was escaped as plain text", Re: regexp.MustCompile(`\\\$[^$\n]*(?:\\textbackslash\{\}|\\[{}])[^$\n]*\\\$`)},
		{Name: "duplicate-plus-before-dots", Detail: "duplicate plus before ellipsis", Re: regexp.MustCompile(`\+{2,}\s*\\(?:l|c)dots`)},
		{Name: "long-subscript", Detail: "very long subscript may indicate swallowed prose", Re: regexp.MustCompile(`_\s*\{\s*[^}]{40,}\}`)},
	}
)

func main() {
	corpus := flag.String("corpus", `F:\26刷题卷`, "directory containing .docx files")
	out := flag.String("out", filepath.Join(os.TempDir(), "docxtolatex-corpus"), "output directory")
	limit := flag.Int("limit", 0, "maximum number of .docx files to convert; 0 means all")
	match := flag.String("match", "", "optional substring filter for .docx base name or full path")
	exact := flag.Bool("exact", false, "make -match compare against the exact .docx base name")
	strict := flag.Bool("strict", false, "exit non-zero when generated output validation finds issues")
	flag.Parse()

	files, err := listDocx(*corpus)
	if err != nil {
		fatal(err)
	}
	if *limit > 0 && *limit < len(files) {
		files = files[:*limit]
	}
	if *match != "" {
		filtered := files[:0]
		needle := strings.ToLower(*match)
		for _, file := range files {
			base := strings.ToLower(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
			full := strings.ToLower(file)
			if (*exact && base == needle) || (!*exact && (strings.Contains(base, needle) || strings.Contains(full, needle))) {
				filtered = append(filtered, file)
			}
		}
		files = filtered
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		fatal(err)
	}

	summaryPath := filepath.Join(*out, "summary.csv")
	summary, err := os.Create(summaryPath)
	if err != nil {
		fatal(err)
	}
	defer summary.Close()

	w := csv.NewWriter(summary)
	defer w.Flush()
	_ = w.Write([]string{"file", "status", "equations", "convertedOmml", "convertedOle", "fallbackImages", "inlineDollar", "displayDollar", "oddDollarLines", "stickyDollarLines", "warnings", "output", "error"})

	validationPath := filepath.Join(*out, "validation.csv")
	validation, err := os.Create(validationPath)
	if err != nil {
		fatal(err)
	}
	defer validation.Close()

	vw := csv.NewWriter(validation)
	defer vw.Flush()
	_ = vw.Write([]string{"file", "line", "issue", "detail", "text"})

	reviewPath := filepath.Join(*out, "manual_review.csv")
	review, err := os.Create(reviewPath)
	if err != nil {
		fatal(err)
	}
	defer review.Close()

	rw := csv.NewWriter(review)
	defer rw.Flush()
	_ = rw.Write([]string{"file", "line", "issue", "detail", "text"})
	issues := 0

	for _, source := range files {
		name := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
		target := filepath.Join(*out, sanitizeName(name))
		converter := docx.Converter{
			Source:      source,
			Output:      target,
			WriteReport: true,
		}
		count, err := converter.Convert()
		if err != nil {
			issues++
			_ = w.Write([]string{source, "error", fmt.Sprint(count), "", "", "", "", "", "", "", "", "", err.Error()})
			_ = vw.Write([]string{source, "", "convert-error", err.Error(), ""})
			w.Flush()
			vw.Flush()
			continue
		}

		texBytes, err := os.ReadFile(converter.Output)
		if err != nil {
			issues++
			_ = w.Write([]string{source, "error", fmt.Sprint(count), "", "", "", "", "", "", "", "", converter.Output, err.Error()})
			_ = vw.Write([]string{source, "", "read-output-error", err.Error(), ""})
			w.Flush()
			vw.Flush()
			continue
		}
		report, _ := readReport(target)
		tex := string(texBytes)
		displayCount := len(displayMathRe.FindAllString(tex, -1))
		inlineCount := len(inlineMathRe.FindAllString(tex, -1))
		lineIssues := validateTexLines(source, tex, vw)
		issues += lineIssues.Total
		writeManualReviewCandidates(source, tex, rw)
		_ = w.Write([]string{
			source,
			"ok",
			fmt.Sprint(count),
			fmt.Sprint(report.Summary.ConvertedOMML),
			fmt.Sprint(report.Summary.ConvertedOLE),
			fmt.Sprint(report.Summary.FallbackImages),
			fmt.Sprint(inlineCount),
			fmt.Sprint(displayCount),
			fmt.Sprint(lineIssues.OddDollarLines),
			fmt.Sprint(lineIssues.StickyDollarLines),
			fmt.Sprint(len(report.Warnings)),
			converter.Output,
			"",
		})
		w.Flush()
		vw.Flush()
		rw.Flush()
	}

	fmt.Println(summaryPath)
	fmt.Println(validationPath)
	fmt.Println(reviewPath)
	if *strict && issues > 0 {
		fatal(fmt.Errorf("validation found %d issue(s)", issues))
	}
}

type reviewRule struct {
	Name   string
	Detail string
	Re     *regexp.Regexp
}

type validationCounts struct {
	Total             int
	OddDollarLines    int
	StickyDollarLines int
}

func validateTexLines(file string, tex string, w *csv.Writer) validationCounts {
	var counts validationCounts
	lines := strings.Split(tex, "\n")
	for i, line := range lines {
		lineNo := fmt.Sprint(i + 1)
		dollarCount := countUnescapedDollars(line)
		if dollarCount%2 != 0 {
			counts.Total++
			counts.OddDollarLines++
			_ = w.Write([]string{file, lineNo, "odd-dollar-count", fmt.Sprintf("%d unescaped dollars", dollarCount), trimForCSV(line)})
		}
		if hasStickyDollars(line) {
			counts.Total++
			counts.StickyDollarLines++
			_ = w.Write([]string{file, lineNo, "sticky-dollar", "adjacent or empty math delimiters", trimForCSV(line)})
		}
	}
	return counts
}

func writeManualReviewCandidates(file string, tex string, w *csv.Writer) {
	lines := strings.Split(tex, "\n")
	for i, line := range lines {
		for _, rule := range reviewRes {
			if rule.Re.MatchString(line) {
				_ = w.Write([]string{file, fmt.Sprint(i + 1), rule.Name, rule.Detail, trimForCSV(line)})
				break
			}
		}
	}
}

func countUnescapedDollars(s string) int {
	count := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '$' && !isEscapedAt(s, i) {
			count++
		}
		i += size
	}
	return count
}

func isEscapedAt(s string, idx int) bool {
	backslashes := 0
	for i := idx - 1; i >= 0 && s[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func hasStickyDollars(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != '$' || isEscapedAt(line, i) {
			continue
		}
		runLen := dollarRunLength(line, i)
		if runLen > 2 {
			return true
		}
		if runLen == 2 {
			i += runLen - 1
			continue
		}
		next := nextUnescapedDollarRun(line, i+1)
		if next.Start == -1 {
			return false
		}
		if next.Length == 1 && strings.TrimSpace(line[i+1:next.Start]) == "" {
			return true
		}
		i = next.Start + next.Length - 1
	}
	return false
}

type dollarRun struct {
	Start  int
	Length int
}

func nextUnescapedDollarRun(s string, start int) dollarRun {
	for i := start; i < len(s); i++ {
		if s[i] == '$' && !isEscapedAt(s, i) {
			return dollarRun{Start: i, Length: dollarRunLength(s, i)}
		}
	}
	return dollarRun{Start: -1}
}

func dollarRunLength(s string, start int) int {
	n := 0
	for i := start; i < len(s) && s[i] == '$' && !isEscapedAt(s, i); i++ {
		n++
	}
	return n
}

func trimForCSV(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 240 {
		return s
	}
	return s[:240]
}

func listDocx(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "~$") || !strings.EqualFold(filepath.Ext(name), ".docx") {
			continue
		}
		files = append(files, filepath.Join(root, name))
	}
	sort.Strings(files)
	return files, nil
}

func readReport(outputDir string) (docx.ConversionReport, error) {
	var report docx.ConversionReport
	name := filepath.Base(outputDir) + ".report.json"
	data, err := os.ReadFile(filepath.Join(outputDir, name))
	if err != nil {
		return report, err
	}
	err = json.Unmarshal(data, &report)
	return report, err
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
