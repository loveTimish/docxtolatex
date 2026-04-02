package docx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ConversionReport struct {
	Source      string           `json:"source"`
	Output      string           `json:"output,omitempty"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Summary     ReportSummary    `json:"summary"`
	Equations   []EquationReport `json:"equations,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type ReportSummary struct {
	Paragraphs       int `json:"paragraphs"`
	Equations        int `json:"equations"`
	ConvertedOLE     int `json:"convertedOle"`
	FallbackImages   int `json:"fallbackImages"`
	ConvertedOMML    int `json:"convertedOmml"`
	ExtractedImages  int `json:"extractedImages"`
	UnsupportedNodes int `json:"unsupportedNodes"`
}

type EquationReport struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"`
	Source    string `json:"source,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Output    string `json:"output,omitempty"`
	Paragraph int    `json:"paragraph,omitempty"`
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
