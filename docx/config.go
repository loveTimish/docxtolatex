package docx

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Document DocumentConfig    `json:"document"`
	Styles   map[string]string `json:"styles"`
	Image    ImageConfig       `json:"image"`
	Report   ReportConfig      `json:"report"`
}

type DocumentConfig struct {
	Class    string   `json:"class"`
	Packages []string `json:"packages"`
}

type ImageConfig struct {
	Mode     string `json:"mode"`
	Template string `json:"template"`
}

type ReportConfig struct {
	Enabled bool   `json:"enabled"`
	File    string `json:"file"`
}

type rawConfig struct {
	Document *DocumentConfig   `json:"document"`
	Styles   map[string]string `json:"styles"`
	Image    *ImageConfig      `json:"image"`
	Report   *ReportConfig     `json:"report"`
}

func DefaultConfig() Config {
	return Config{
		Document: DocumentConfig{
			Class: "article",
			Packages: []string{
				"[T1]{fontenc}",
				"[utf8]{inputenc}",
				"amsmath",
				"amssymb",
				"graphicx",
			},
		},
		Styles: normalizeStyleMap(map[string]string{
			"Title":     `\section*{%s}`,
			"Subtitle":  `\subsection*{%s}`,
			"Heading 1": `\section{%s}`,
			"Heading1":  `\section{%s}`,
			"Heading 2": `\subsection{%s}`,
			"Heading2":  `\subsection{%s}`,
			"Heading 3": `\subsubsection{%s}`,
			"Heading3":  `\subsubsection{%s}`,
			"Quote": `\begin{quote}
%s
\end{quote}`,
			"Intense Quote": `\begin{quote}
%s
\end{quote}`,
		}),
		Image: ImageConfig{
			Mode:     "placeholder",
			Template: `beginPic{%s}endPic`,
		},
		Report: ReportConfig{
			Enabled: false,
		},
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	if raw.Document != nil {
		if strings.TrimSpace(raw.Document.Class) != "" {
			cfg.Document.Class = strings.TrimSpace(raw.Document.Class)
		}
		if len(raw.Document.Packages) > 0 {
			cfg.Document.Packages = append([]string(nil), raw.Document.Packages...)
		}
	}
	if raw.Styles != nil {
		merged := cfg.Styles
		for k, v := range normalizeStyleMap(raw.Styles) {
			merged[k] = v
		}
		cfg.Styles = merged
	}
	if raw.Image != nil {
		if strings.TrimSpace(raw.Image.Mode) != "" {
			cfg.Image.Mode = strings.ToLower(strings.TrimSpace(raw.Image.Mode))
		}
		if raw.Image.Template != "" {
			cfg.Image.Template = raw.Image.Template
		}
	}
	if raw.Report != nil {
		cfg.Report.Enabled = raw.Report.Enabled
		if strings.TrimSpace(raw.Report.File) != "" {
			cfg.Report.File = strings.TrimSpace(raw.Report.File)
		}
	}

	return cfg.withDefaults(), nil
}

func (c Config) withDefaults() Config {
	defaults := DefaultConfig()

	if strings.TrimSpace(c.Document.Class) == "" {
		c.Document.Class = defaults.Document.Class
	}
	if len(c.Document.Packages) == 0 {
		c.Document.Packages = append([]string(nil), defaults.Document.Packages...)
	}
	if c.Styles == nil {
		c.Styles = defaults.Styles
	} else {
		c.Styles = normalizeStyleMap(c.Styles)
	}
	if strings.TrimSpace(c.Image.Mode) == "" {
		c.Image.Mode = defaults.Image.Mode
	}
	if c.Image.Template == "" {
		c.Image.Template = defaults.Image.Template
	}

	return c
}

func normalizeStyleMap(styles map[string]string) map[string]string {
	normalized := make(map[string]string, len(styles))
	for k, v := range styles {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		normalized[key] = v
	}
	return normalized
}
