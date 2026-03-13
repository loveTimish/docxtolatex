# docxtolatex

将 Word `.docx` 中的数学内容按文档顺序转换为 LaTeX。

当前支持两类公式来源：

- MathType OLE 对象
- Word 原生公式 OMML

转换时会直接读取 `.docx` 压缩包内容，尽量在内存中完成解析；如果文档里包含图片，也会一并提取到输出目录。

## 功能概览

- 将单个 `oleObject*.bin` 转成 LaTeX
- 将整份 `.docx` 转成 `.tex`
- 按正文顺序输出普通文本、公式和图片
- 自动提取文档中的图片资源
- 对明显损坏的公式结果做图片回退，减少错误 LaTeX

## 环境要求

- Go 1.21+
- Windows / Linux / macOS 均可运行

可选：如果你在国内下载依赖较慢，可以先配置模块代理：

```powershell
setx GOPROXY https://goproxy.cn,direct
```

## 快速开始

转换单个 MathType OLE 对象：

```bash
go run main.go --filepath test/oleObject1.bin
```

转换整个 Word 文档：

```bash
go run main.go --wordDocx sample.docx
```

指定输出目录：

```bash
go run main.go --wordDocx sample.docx --output sample_out
```

## 输出说明

当使用 `--wordDocx` 时：

- 如果未指定 `--output`，默认输出目录名为源文档文件名去掉扩展名后的结果
- 程序会在该目录下生成：
  - `<目录名>.tex`
  - `img/` 图片目录

例如：

```bash
go run main.go --wordDocx xuekewang.docx
```

会生成类似结构：

```text
xuekewang/
  xuekewang.tex
  img/
    image1.png
    image2.wmf
    ...
```

## 命令行参数

- `--filepath, -f`
  - 转换单个 MathType OLE 对象文件
- `--wordDocx, -w`
  - 转换整份 Word `.docx`
- `--output, -o`
  - 指定输出目录

## 目录结构

- `main.go`
  - CLI 入口
- `docx/`
  - 整份 Word 文档解析、关系读取、图片提取、正文顺序遍历
- `eqn/`
  - MathType / MTEF 到 LaTeX 的核心转换逻辑
- `omml/`
  - OMML 到 LaTeX 的解析逻辑
- `latexmap/`
  - Unicode / 字符映射，辅助 OMML 和符号转换

## 当前行为说明

- OLE 公式按 `word/embeddings/` 关系解析
- OMML 公式按 `word/document.xml` 中出现顺序直接写入输出
- 普通图片会提取到 `img/` 并在文本中以占位形式保留位置
- 如果某个 OLE 公式转换结果明显异常，程序会优先回退到其预览图，而不是输出误导性的 LaTeX

## 常见问题

- 输出目录不是单个 `.tex` 文件
  - 这是当前设计行为。`--output` 指向的是输出目录，不是最终 `.tex` 文件路径。

- 某些公式没有转成 LaTeX，而是回退成图片
  - 这是保护性策略，用来避免输出明显错误的公式结果。

- Go 命令不可用
  - 先确认 `go version` 可以运行，并检查 `PATH` 中是否包含 Go 的 `bin` 目录。
