# mtef-go / docxtolatex

将 `.docx` 中的 MathType OLE 公式直接转换为 KaTeX/LaTeX，解压和解析全在内存完成，不落地中间文件。

## 快速开始

- 依赖：Go 1.21+（Windows/Linux/macOS 均可）。若需加速依赖下载，可执行：
  ```powershell
  setx GOPROXY https://goproxy.cn,direct
  ```

- 转换单个 OLE 对象文件：
  ```bash
  go run main.go --filepath test/oleObject1.bin
  ```

- 转换整个 `.docx` 并生成 LaTeX 文档（默认输出 `<docx>.tex`，可用 `--output` 指定）：
  ```bash
  go run main.go --wordDocx sample.docx --output sample.tex
  ```

生成的 `.tex` 以陈列公式依次写出每个数学对象，便于查看或再编辑。

## 目录提示
- `omml/`：OMML → KaTeX/LaTeX 解析与符号映射。
- `eqn/`：MathType MTEF 转换。
- `docx/`：Word 文档解析入口。

## 常见问题
- 输出缺少分隔符：如果原始节点未提供分隔符，转换器会默认补 `()`，以保证 `\left(` / `\right)` 配对。
- Go 未找到：确认 `go version` 可用，并检查 PATH 是否包含 Go 安装目录。
