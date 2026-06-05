# docxtolatex

`docxtolatex` converts Word `.docx` files into LaTeX-oriented text while preserving document order, MathType equations, Word OMML equations, and images as far as the source file allows.

这个项目的目标不是盲目把所有对象都硬转成 LaTeX，而是优先输出可信结果：能解析的公式转为 LaTeX，明显损坏、缺失 Native stream、或本质是二维图形/占位框的对象保留为图片，并在 report 里说明原因。

## Features

- Convert a single MathType OLE object (`oleObject*.bin`) to LaTeX.
- Convert a full `.docx` into a `.tex` file plus extracted image assets.
- Preserve the original order of paragraphs, inline equations, display equations, and images.
- Support common MathType / MTEF structures, including fractions, scripts, radicals, fences, matrices, piles, accents, and many symbols.
- Support Word OMML formulas, including inline/display formulas, fractions, scripts, fences, radicals, n-ary operators, matrices, equation arrays, and accents.
- Distinguish inline `$...$` and display `$$...$$` output, with corpus validation for odd or sticky dollar delimiters.
- Recover from broken OLE objects and fall back to preview images instead of emitting misleading LaTeX.
- Emit structured JSON reports with per-equation status and reason taxonomy.
- Provide corpus tools for batch conversion, validation, and manual review.

## Install

Requirements:

- Go 1.21+
- Windows, Linux, or macOS

Clone and test:

```bash
git clone git@github.com:loveTimish/docxtolatex.git
cd docxtolatex
go test ./...
```

If dependency downloads are slow in China:

```powershell
$env:GOPROXY = "https://goproxy.cn,direct"
```

## Quick Start

Convert one MathType OLE object:

```bash
go run main.go --filepath test/oleObject1.bin
```

Convert a Word document:

```bash
go run main.go --wordDocx sample.docx
```

Choose an output directory:

```bash
go run main.go --wordDocx sample.docx --output sample_out
```

Write a conversion report:

```bash
go run main.go --wordDocx sample.docx --output sample_out --report
```

Use a custom config:

```bash
go run main.go --wordDocx sample.docx --config config.example.json
```

## Output

For a document named `sample.docx`, the converter creates an output directory containing:

```text
sample_out/
  sample_out.tex
  sample_out.report.json
  img/
    image1-xxxxxxxx.png
    image2-xxxxxxxx.wmf
```

If `--output` is omitted, the output directory defaults to the input file name without `.docx`.

Images and fallback previews are emitted as `beginPic{...}endPic` placeholders by default. Use `config.example.json` to switch to `\includegraphics{...}` or a custom image template.

## CLI Options

- `--filepath, -f`: convert a single MathType OLE object.
- `--wordDocx, -w`: convert a full Word `.docx`.
- `--output, -o`: target output directory.
- `--config, -c`: JSON config for document class, packages, style mapping, image rendering, and report settings.
- `--report, -r`: write `<output>.report.json`.

## Configuration

See `config.example.json`:

```json
{
  "document": {
    "class": "article",
    "packages": ["[T1]{fontenc}", "[utf8]{inputenc}", "amsmath", "amssymb", "graphicx"]
  },
  "styles": {
    "Heading 1": "\\section{%s}",
    "Heading 2": "\\subsection{%s}",
    "Quote": "\\begin{quote}\n%s\n\\end{quote}"
  },
  "image": {
    "mode": "includegraphics"
  },
  "report": {
    "enabled": true,
    "file": ""
  }
}
```

Supported image modes:

- `placeholder`: emit `beginPic{...}endPic`.
- `includegraphics`: emit `\includegraphics{...}`.
- `template`: use `image.template` with `%s`.

## Reports

Reports are designed for debugging large corpora. Each equation entry includes:

- `kind`: `ole`, `ole-inline`, `omml-inline`, `omml-display`, etc.
- `status`: `converted`, `skipped`, or `fallback-image`.
- `reason`: why an equation fell back.
- `output`: LaTeX output or fallback image name.
- `paragraph`: paragraph index in the source traversal.

Current fallback reason taxonomy includes:

- `invalid-ole`
- `missing-equation-native`
- `mtef-open-panic`
- `convert-error`
- `empty-output`
- `empty-math-body`
- `replacement-char`
- `placeholder-box`
- `non-printable-rune`
- `tiny-nonlatex-fragment`
- `broken-frac`
- `repeated-operators`
- `truncated-brace-group`
- `arrow-only`

## Corpus Workflow

For production-like regression checks, use `tools/corpus_check.go`.

Sample first:

```powershell
go run .\tools\corpus_check.go `
  -corpus "F:\资料\xsc资料\word_files" `
  -out "D:\docxtolatex\corpus_out_xsc_sample" `
  -limit 20 `
  -strict
```

Then run the full corpus:

```powershell
go test ./...

go run .\tools\corpus_check.go `
  -corpus "F:\资料\xsc资料\word_files" `
  -out "D:\docxtolatex\corpus_out_xsc" `
  -strict
```

Important outputs:

- `summary.csv`: one row per document, with equation counts, fallback counts, dollar delimiter checks, warnings, and output path.
- `validation.csv`: strict validation failures. A file with only the header row is the expected passing state.
- `manual_review.csv`: high-risk output snippets for human review.
- `*.report.json`: per-document equation-level conversion details.

Current full-corpus baseline on `F:\资料\xsc资料\word_files`:

- Documents: 155
- Equations: 40996
- Converted OLE equations: 40790
- OLE fallback images: 206
- `validation.csv`: no data rows

Remaining fallbacks are mostly non-formula objects, corrupt/empty OLE objects, objects without `Equation Native`, placeholder boxes, or full two-dimensional diagrams that are safer as images.

## Debug Tools

Inspect one equation inside a `.docx`:

```powershell
go run .\tools\inspect_equation "F:\资料\xsc资料\word_files\14.docx" "word/embeddings/oleObject864.bin"
```

Dump OLE directory metadata:

```powershell
go run .\tools\inspect_ole_dir "F:\资料\xsc资料\word_files\5.docx" "word/embeddings/oleObject71.bin"
```

Enable MTEF record tracing:

```powershell
$env:DOCXTOLATEX_TRACE_RECORDS = "1"
go run .\tools\inspect_equation "sample.docx" "word/embeddings/oleObject1.bin"
```

Enable AST debug output:

```powershell
$env:DOCXTOLATEX_DEBUG_AST = "1"
$env:DOCXTOLATEX_DEBUG_AST_MAX_DEPTH = "8"
go run main.go --wordDocx sample.docx --report
```

## Project Layout

- `main.go`: CLI entry point.
- `docx/`: `.docx` traversal, relationships, image extraction, style mapping, reports, and fallback decisions.
- `eqn/`: MathType OLE / MTEF parser and LaTeX renderer.
- `omml/`: Word OMML parser and LaTeX/MathIR conversion.
- `mathir/`: minimal formula IR and LaTeX renderer used by OMML work.
- `latexmap/`: symbol and Unicode mapping helpers.
- `tools/corpus_check.go`: batch conversion and validation.
- `tools/inspect_equation/`: inspect a specific OLE equation in a document.
- `tools/inspect_ole_dir/`: inspect OLE storage entries.
- `docs/math-support-matrix.md`: current math support matrix.
- `docs/regression-corpus.md`: regression corpus notes.

## Design Notes

MathType OLE objects often contain both a preview image and an `Equation Native` stream. The converter uses `Equation Native` when it is present and readable. If the stream is missing, invalid, empty, or produces suspicious LaTeX, the converter keeps the preview image and records the reason.

This conservative behavior is intentional. In real worksheet documents, some OLE-looking objects are actually diagrams, answer boxes, layout placeholders, or broken zero-byte embeddings. Preserving those as images is usually better than manufacturing incorrect LaTeX.

## Known Limitations

- Complex two-dimensional diagrams embedded as MathType are usually kept as images.
- Some broken OLE objects have no recoverable `Equation Native` stream.
- OMML fallback to preview images is not equivalent to the OLE fallback path.
- The MathIR layer is still minimal and is not yet the default path for all equation sources.
- Tables, footnotes, headers/footers, and complex page layout are not the main focus yet.

## License

This project is licensed under the terms in `LICENSE`.
