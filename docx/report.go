package docx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type EquationStatus string

const (
	EquationStatusConverted     EquationStatus = "converted"
	EquationStatusSkipped       EquationStatus = "skipped"
	EquationStatusFallbackImage EquationStatus = "fallback-image"
)

type EquationReason string

const (
	EquationReasonUnknown           EquationReason = ""
	EquationReasonConvertError      EquationReason = "convert-error"
	EquationReasonInvalidOLE        EquationReason = "invalid-ole"
	EquationReasonMissingNative     EquationReason = "missing-equation-native"
	EquationReasonMTEFOpenPanic     EquationReason = "mtef-open-panic"
	EquationReasonEmptyOutput       EquationReason = "empty-output"
	EquationReasonEmptyMathBody     EquationReason = "empty-math-body"
	EquationReasonReplacementChar   EquationReason = "replacement-char"
	EquationReasonPlaceholderBox    EquationReason = "placeholder-box"
	EquationReasonNonPrintableRune  EquationReason = "non-printable-rune"
	EquationReasonTinyNonLatex      EquationReason = "tiny-nonlatex-fragment"
	EquationReasonBrokenFrac        EquationReason = "broken-frac"
	EquationReasonRepeatedOperators EquationReason = "repeated-operators"
	EquationReasonTruncatedBrace    EquationReason = "truncated-brace-group"
	EquationReasonArrowOnly         EquationReason = "arrow-only"
)

type EquationReasonDefinition struct {
	Reason      EquationReason `json:"reason"`
	Category    string         `json:"category"`
	Description string         `json:"description"`
}

var equationReasonCatalog = []EquationReasonDefinition{
	{Reason: EquationReasonConvertError, Category: "ole", Description: "OLE conversion failed but did not match a more specific classifier."},
	{Reason: EquationReasonInvalidOLE, Category: "ole", Description: "Input is not a readable OLE/MTEF payload."},
	{Reason: EquationReasonMissingNative, Category: "ole", Description: "OLE storage is readable, but it does not contain a MathType Equation Native stream."},
	{Reason: EquationReasonMTEFOpenPanic, Category: "ole", Description: "Underlying OLE/MTEF open/parse path panicked and was recovered."},
	{Reason: EquationReasonEmptyOutput, Category: "sanity", Description: "Converter returned an empty LaTeX string."},
	{Reason: EquationReasonEmptyMathBody, Category: "sanity", Description: "LaTeX wrapper exists but the math body is empty."},
	{Reason: EquationReasonReplacementChar, Category: "sanity", Description: "Output contains Unicode replacement characters, suggesting broken decoding."},
	{Reason: EquationReasonPlaceholderBox, Category: "sanity", Description: "Output still contains placeholder box glyphs."},
	{Reason: EquationReasonNonPrintableRune, Category: "sanity", Description: "Output contains non-printable runes."},
	{Reason: EquationReasonTinyNonLatex, Category: "sanity", Description: "Tiny non-LaTeX fragment that is usually safer to fall back as an image."},
	{Reason: EquationReasonBrokenFrac, Category: "sanity", Description: "Fraction body looks truncated or malformed."},
	{Reason: EquationReasonRepeatedOperators, Category: "sanity", Description: "Repeated operators suggest parser corruption."},
	{Reason: EquationReasonTruncatedBrace, Category: "sanity", Description: "Brace group looks truncated."},
	{Reason: EquationReasonArrowOnly, Category: "sanity", Description: "Output collapsed to an arrow-only fragment."},
}

type ConversionReport struct {
	Source      string           `json:"source"`
	Output      string           `json:"output,omitempty"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Summary     ReportSummary    `json:"summary"`
	Equations   []EquationReport `json:"equations,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type ReportSummary struct {
	Paragraphs        int `json:"paragraphs"`
	Equations         int `json:"equations"`
	ConvertedOLE      int `json:"convertedOle"`
	FallbackImages    int `json:"fallbackImages"`
	ConvertedOMML     int `json:"convertedOmml"`
	ExtractedImages   int `json:"extractedImages"`
	UnsupportedNodes  int `json:"unsupportedNodes"`
	ListItems         int `json:"listItems"`
	WorksheetSections int `json:"worksheetSections"`
}

type EquationReport struct {
	Index     int            `json:"index"`
	Kind      string         `json:"kind"`
	Source    string         `json:"source,omitempty"`
	Status    EquationStatus `json:"status"`
	Reason    EquationReason `json:"reason,omitempty"`
	Output    string         `json:"output,omitempty"`
	Paragraph int            `json:"paragraph,omitempty"`
}

func EquationReasonCatalog() []EquationReasonDefinition {
	out := make([]EquationReasonDefinition, len(equationReasonCatalog))
	copy(out, equationReasonCatalog)
	return out
}

func classifyOLEError(err error) EquationReason {
	if err == nil {
		return EquationReasonUnknown
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid ole/mtef payload"):
		return EquationReasonInvalidOLE
	case strings.Contains(msg, "equation native stream not found"):
		return EquationReasonMissingNative
	case strings.Contains(msg, "panic while parsing ole/mtef"):
		return EquationReasonMTEFOpenPanic
	default:
		return EquationReasonConvertError
	}
}

func newReport(source string) ConversionReport {
	return ConversionReport{
		Source:      source,
		GeneratedAt: time.Now().UTC(),
	}
}

func writeReport(report ConversionReport, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir report dir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	return nil
}
