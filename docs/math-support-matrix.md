# Math support matrix

本文件是 `docxtolatex` 当前数学能力的**工程基线**，重点不是宣传“支持了多少”，而是把：

- 哪条链路已经能稳定工作
- 哪些场景只是部分可用
- 哪些场景主要依赖 fallback
- 哪些场景还没有正式支持

写清楚，方便后续回归与分阶段推进。

## 状态定义

- **implemented**：主链路已有明确实现，且当前代码/样本能证明可工作。
- **partial**：已有实现，但覆盖不完整、行为启发式强，或尚缺系统回归。
- **fallback**：当前主要依赖图片回退/保底逻辑，而不是稳定的 LaTeX 语义输出。
- **unsupported**：当前没有正式实现，或只有零散片段，不能视为能力。

## 样本证据

当前仓库内可直接复查的回归样本：

1. `compare-0407-regress/compare-0407-regress.report.json`
   - 典型“正常 OLE 试卷”
   - `convertedOle=261`，`fallbackImages=0`
2. `compare-0403-regress/compare-0403-regress.report.json`
   - 典型“混合大量坏 OLE 的试卷”
   - `convertedOle=278`，`fallbackImages=99`

说明：这两份样本几乎都走 **MathType OLE**，`convertedOmml=0`。因此 **OMML 基线仍明显弱于 OLE**。当前除了源码审阅与 XML/IR 单测外，仓库里已补上一条程序化生成的最小 DOCX 文档级回归（`docx/TestConvertGeneratedOMMLRegressionDocx`）；但仍然缺一份人工从 Word 导出的真实 OMML 文档样本。

## 合并支持矩阵

| 能力 | MathType OLE | OMML | 证据 / 备注 |
| --- | --- | --- | --- |
| 文档顺序内嵌公式抽取 | implemented | implemented | `docx/docx.go` 已同时识别 `OLEObject` 与 `oMath/oMathPara` |
| 单公式转换入口 | implemented | implemented | `eqn.ConvertBytes*` 与 `omml.ConvertElement` |
| 损坏输入不崩溃 | implemented | partial | OLE 已有 `recover()`；OMML 主链路依赖 XML 解析返回错误，但缺坏样本回归 |
| 公式失败图片回退 | fallback | unsupported | 回退逻辑只对 OLE 预览图生效 |
| 分式 | implemented | implemented | OLE 在 0407/0403 中大量出现；OMML 有 `f` |
| 上下标 | implemented | implemented | OLE 在样本中高频；OMML 有 `sSup/sSub/sSubSup` |
| 定界符 / 括号栅栏 | partial | partial | OLE 有基础输出但仍常见符号粘连/语义不稳；OMML 有 `d` 但复杂分隔符仍靠启发式 |
| 根式 | partial | implemented | OMML 有 `rad`；OLE 样本中能见到部分输出，但缺系统矩阵 |
| n-ary 运算（求和/积分/连乘） | partial | partial | OMML 有 `nary/limLow/limUpp`；OLE 真实样本可转部分常见表达式，但未建立特性级回归 |
| 矩阵 / cases / 对齐 | partial | partial | OMML 有 `m/eqArr`，但 cases/aligned 仍偏启发式；OLE 侧缺明确能力拆分 |
| 重音 / bar / accent | partial | partial | OMML 已实现 `bar/acc/groupChr`，但未系统回归；OLE 侧现状未量化 |
| 纯文本/几何符号短片段 | partial | partial | 现有链路会输出，但质量不稳定，且 OLE 有大量“碎片公式”现象 |
| 语义层 IR | unsupported | partial | OLE 仍无统一 IR；OMML 已有最小 `MathIR`（`token/group/frac/subsup/fence/raw-latex`），但默认转换仍走旧字符串链路 |
| report reason taxonomy | partial | partial | 本轮已开始统一 reason 常量和分类，但还未覆盖所有转换阶段 |

## 两份真实样本的结论

### 0407 定时训练 B（正常 OLE 主链路）

- 状态：**可作为 OLE 正向回归基线**
- 结果：261 个公式全部走 OLE 转换成功，无图片回退
- 含义：
  - 现有 OLE 主链路对“规则、完整、试卷型”文档已具备可用性
  - 但这不等于“MathType 全量支持”，只是说明常见基础表达式稳定度已达基线

### 0403 定时训练 A（坏 OLE / fallback 基线）

- 状态：**可作为 OLE 容错回归基线**
- 结果：377 个公式中，278 个转为 LaTeX，99 个走图片回退
- 含义：
  - 当前系统已经具备“坏 OLE 不崩 + 尽量交付”的保底能力
  - 但大量失败仍集中在 `convert-error` / `empty-output` 路径，说明 OLE 深层解析仍远未完善

## 当前最明显的缺口

1. **仍缺真实 Word 导出的 OMML 回归文档**
   - 现在 OMML 已经有最小 `MathIR`、XML 单测，以及程序化生成的最小 DOCX 文档级夹具。
   - 但这还不能替代真实生产者样本，只能算“最小可重复工程基线”。
2. **MathType 能力还没有 feature-by-feature 拆账**
   - 目前只能从真实试卷整体可用性反推，缺少“分式/矩阵/限/符号”级别的精确矩阵。
3. **fallback 只覆盖 OLE**
   - OMML 解析失败时，当前没有对等的图片保底策略。
4. **report taxonomy 仍在起步阶段**
   - 已有统一 reason 常量，但还没有把所有失败来源完全收敛到稳定枚举。

## 建议的下一步更新方式

后续每推进一刀，优先把矩阵补到以下粒度：

- 先按 **公式来源**：OLE / OMML
- 再按 **结构族**：frac、scripts、fence、radical、nary、matrix、accent、function、cases/aligned
- 最后给每一项绑定：
  - 代码入口
  - 单测
  - 回归样本（真实文档或最小 XML / OLE 片段）

这样 support matrix 才会从“仓库说明”慢慢变成“可执行的工程清单”。
