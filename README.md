# docxtolatex

将 Word `.docx` 中的数学内容按文档顺序转换为 LaTeX。

当前支持两类公式来源：

- MathType OLE 对象
- Word 原生公式 OMML

转换时会直接读取 `.docx` 压缩包内容，尽量在内存中完成解析；如果文档里包含图片，也会一并提取到输出目录。

> 仓库已开始引入最小 `MathIR` 方向：当前先让 OMML 能产出基础语义节点（`token/group/frac/subsup/fence/nary/matrix/eq-array/accent/raw-latex`），但默认文档转换仍继续走现有字符串输出路径，避免一次性大重构。

## 功能概览

- 将单个 `oleObject*.bin` 转成 LaTeX
- 将整份 `.docx` 转成 `.tex`
- 按正文顺序输出普通文本、公式和图片
- 自动提取文档中的图片资源
- 对明显损坏的公式结果做图片回退，减少错误 LaTeX
- 基于段落样式做基础结构映射（如标题、引用）
- 支持 JSON 配置文档类、包、样式映射和图片渲染方式
- 可选输出 JSON 转换报告，便于排查失败公式和回退原因
- report `reason` 已开始收敛为稳定 taxonomy（如 `invalid-ole`、`mtef-open-panic`、`empty-output`）

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

输出 JSON 转换报告：

```bash
go run main.go --wordDocx sample.docx --report
```

使用自定义配置：

```bash
go run main.go --wordDocx sample.docx --config config.example.json
```

## 输出说明

当使用 `--wordDocx` 时：

- 如果未指定 `--output`，默认输出目录名为源文档文件名去掉扩展名后的结果
- 程序会在该目录下生成：
  - `<目录名>.tex`
  - `img/` 图片目录
  - 可选的 `<目录名>.report.json` 转换报告（启用 `--report` 或配置文件中的 `report.enabled` 时）

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
- `--config, -c`
  - 指定 JSON 配置文件，用于自定义样式映射、文档类、包和图片输出策略
- `--report, -r`
  - 输出 JSON 转换报告

## 目录结构

- `main.go`
  - CLI 入口
- `docx/`
  - 整份 Word 文档解析、关系读取、图片提取、正文顺序遍历
- `eqn/`
  - MathType / MTEF 到 LaTeX 的核心转换逻辑
- `omml/`
  - OMML 到 LaTeX 的解析逻辑，以及最小 OMML → MathIR 入口
- `mathir/`
  - 公式统一语义层的最小节点定义与 LaTeX 回渲染
- `latexmap/`
  - Unicode / 字符映射，辅助 OMML 和符号转换
- `docs/math-support-matrix.md`
  - 当前 MathType OLE / OMML 支持矩阵与样本结论
- `docs/regression-corpus.md`
  - `compare-*` 样本、报告与真实文档输出快照说明

## 当前行为说明

- OLE 公式按 `word/embeddings/` 关系解析
- OMML 公式按 `word/document.xml` 中出现顺序直接写入输出
- 常见标题样式（如 `Heading 1/2/3`）会映射为基础 LaTeX 结构
- 普通图片默认提取到 `img/` 并以占位形式保留位置，也可通过配置改成 `\includegraphics{}`
- 图片文件名会附加哈希，避免同名资源相互覆盖
- 如果某个 OLE 公式转换结果明显异常，程序会优先回退到其预览图，而不是输出误导性的 LaTeX

## 配置文件说明

可参考仓库中的 `config.example.json`。当前支持：

- `document.class`
  - 输出文档类，如 `article`、`report`
- `document.packages`
  - 需要注入的 LaTeX 包列表
- `styles`
  - 段落样式名到 LaTeX 模板的映射，模板必须包含 `%s`
- `image.mode`
  - `placeholder` / `includegraphics` / `template`
- `image.template`
  - 当 `image.mode=template` 时使用的模板
- `report.enabled`
  - 是否默认输出转换报告
- `report.file`
  - 自定义报告路径

## 常见问题

- 输出目录不是单个 `.tex` 文件
  - 这是当前设计行为。`--output` 指向的是输出目录，不是最终 `.tex` 文件路径。

- 某些公式没有转成 LaTeX，而是回退成图片
  - 这是保护性策略，用来避免输出明显错误的公式结果。

- Go 命令不可用
  - 先确认 `go version` 可以运行，并检查 `PATH` 中是否包含 Go 的 `bin` 目录。
