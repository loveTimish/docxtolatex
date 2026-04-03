# Regression corpus

本文件说明 `docxtolatex` 当前已经纳入仓库、可直接复查的回归语料，以及它们分别用来盯什么问题。

目标不是把所有样本都塞进仓库，而是让后续维护者知道：

- 每个 `compare-*` 目录为什么存在
- `.report.json` 该怎么读
- 哪些是真实文档输出快照，哪些只是当前回归基线
- 现在的样本覆盖了什么，还没覆盖什么

## 目录约定

当前仓库根目录下与回归直接相关的目录：

- `compare-0403-regress/`
- `compare-0407-regress/`
- `compare-0403周六定时训练A教师版---bb901eec-b4d3-4303-afd4-5c1f6eb42570/`
- `compare-0407定时训练B教师版---84682b2f-4b27-4d3a-af60-04bb826bf605/`

每个目录通常包含：

- `*.tex`：转换输出
- `*.report.json`：转换报告
- `img/`：提取出的图片与 fallback 预览图

## 语料说明

### 1. `compare-0407-regress/`

**用途：OLE 正向回归基线**

- 文档类型：真实试卷样本输出快照
- 主要验证：
  - 正常 MathType OLE 主链路是否仍然可转
  - 标题/段落/列表等文档结构恢复是否回归
  - 新改动是否引入无谓 fallback
- 当前观察：
  - `convertedOle=261`
  - `fallbackImages=0`
  - `convertedOmml=0`

**适合拿它盯的回归：**

- OLE 主链路稳定性
- 列表/题型结构输出是否被打乱
- report summary 计数是否明显漂移

### 2. `compare-0403-regress/`

**用途：OLE 容错 / fallback 回归基线**

- 文档类型：真实试卷样本输出快照
- 主要验证：
  - 坏 OLE 是否仍然“不崩”
  - fallback 图片是否还能稳定落盘并保留位置
  - report reason / warning 是否仍能解释失败
- 当前观察：
  - `convertedOle=278`
  - `fallbackImages=99`
  - `convertedOmml=0`

**适合拿它盯的回归：**

- `recover()` 是否还生效
- fallback-image 计数是否异常变化
- report taxonomy 是否把失败原因越写越糊

### 3. `compare-0403周六定时训练A教师版---.../`

**用途：真实样本全量输出留档**

- 更像一次具体文档转换的结果目录，而不是“精简后的回归快照”
- 适合人工抽查：
  - 输出长什么样
  - 图片资源是否齐
  - 与 `*-regress` 基线相比是否存在人工观察差异

### 4. `compare-0407定时训练B教师版---.../`

**用途：真实样本全量输出留档**

- 与上面对应，保留另一份试卷型文档的完整输出
- 更适合人工 spot check，不适合作为精细 feature matrix 的唯一依据

## `report.json` 怎么看

当前最关键的字段：

- `summary.paragraphs`：总段落数
- `summary.equations`：总公式数
- `summary.convertedOle`：成功转出的 OLE 数
- `summary.fallbackImages`：走图片回退的数量
- `summary.convertedOmml`：成功转出的 OMML 数
- `summary.listItems`：恢复出的列表项数量
- `summary.worksheetSections`：识别出的试卷大题段落数量
- `equations[]`：逐公式明细
- `warnings[]`：转换期警告

逐公式明细里最值得盯的是：

- `kind`：`ole` / `ole-inline` / `omml-inline` / `omml-display`
- `status`：`converted` / `skipped` / `fallback-image`
- `reason`：失败或降级原因
- `output`：LaTeX 或 fallback 图片名
- `paragraph`：对应段落位置

## 当前语料覆盖范围

### 已覆盖

- 真实试卷型 DOCX
- MathType OLE 正常链路
- MathType OLE 大量损坏时的保底链路
- 段落样式、列表、题型结构的真实文档行为

### 明显未覆盖

- **人工导出的真实 OMML 文档**
- OLE 的特性级最小样本（当前 OLE 仍主要依赖真实试卷回归）
- OMML 更复杂的特性级样本（当前已补最小 XML 单测，覆盖 frac/subsup/fence/rad fallback）
- 表格、脚注、复杂版式
- 同一能力的“正例 + 反例 + 边界例”三件套

## 新增的最小 OMML 文档级夹具

当前仓库已经补上一条**程序化生成 DOCX → `docx.Converter` → `.tex` / `.report.json`** 的 OMML 回归路径：

- 测试入口：`docx/TestConvertGeneratedOMMLRegressionDocx`
- 夹具形式：测试里直接组装最小 `.docx` zip 包
- 当前覆盖：inline script、display frac、matrix、report 中 `convertedOmml` / `kind` 计数

这条基线的意义是：

- 已经不再只靠源码审阅或 XML 片段单测看 OMML
- 可以直接回归“文档顺序 + 包装输出 + report summary/entries”

但它也有明确限制：

- **它不是人工从 Word 导出的真实样本**
- 还不能覆盖不同 Word/编辑器生产的命名空间、冗余节点、兼容性噪音
- 目前更适合作为**最小可重复工程基线**，而不是 producer-compat 全量证据

## 当前建议的回归使用方式

### 做文档结构改动时

至少检查：

- `compare-0407-regress/*.tex`
- `compare-0403-regress/*.tex`
- 两份 `*.report.json` 的 summary 是否出现可解释的变化

### 做 OLE 容错或 report 分类改动时

优先检查：

- `compare-0403-regress/*.report.json`
- `fallback-image` 数量
- `reason` 枚举是否变得更清晰而不是更随意

### 做 OMML 改动时

当前光看现有 `compare-*` 不够，因为它们几乎不覆盖 OMML。现在仓库里已经有：

1. 最小 XML / IR 单测（`omml/ir_test.go`）
2. 一份程序化生成的最小 DOCX 文档级回归（`docx/TestConvertGeneratedOMMLRegressionDocx`）

接下来仍应继续补：

1. 一份**真实 Word 导出的** OMML DOCX 回归样本
2. 更复杂结构的 XML 单测（如 nary、matrix、eqArr、accent）
3. 然后再把样本输出沉成新的 `compare-*` 目录

## 下一步最值得补的语料

1. **一份真实 OMML 为主的 DOCX**
   - 至少覆盖 inline/display、frac、scripts、fence、matrix
2. **feature 级最小回归集**
   - 建议用最小 XML / 最小 OLE 片段配合单测，而不是都依赖整份试卷
3. **报告分类回归样本**
   - 明确验证 `invalid-ole`、`mtef-open-panic`、`empty-output`、`tiny-nonlatex-fragment` 等 reason

当这些补齐之后，`compare-*` 才不只是“历史输出目录”，而会真正变成可维护的 regression corpus。
