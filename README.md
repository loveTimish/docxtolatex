# mtef-go

高效将 `.docx` 内的 MathType OLE 对象直接转换为 LaTeX（内存解压，不落地文件）。

## 使用

- 转换单个 OLE 对象文件：
```
go run main.go --filepath test/oleObject1.bin
```

- 转换 `.docx` 并生成 LaTeX 文档（默认输出 `<docx>.tex`，可用 `--output` 指定）：
```
go run main.go --wordDocx sample.docx --output sample.tex
```

生成的 `.tex` 含简洁导言，每个方程按 `word/embeddings/` 顺序输出为陈列公式。

## 安装 Go 环境（Windows）
1. 从官网下载安装包：https://go.dev/dl/ ，选择适合的 Windows msi 安装器并双击安装（保持默认路径即可）。
2. 安装后重新打开终端，运行 `go version` 确认可用；若未识别，检查环境变量 `PATH` 中是否包含 Go 的 `bin` 目录（通常为 `C:\Program Files\Go\bin`）。
3. 配置模块代理（可选，加速依赖下载）：
   ```
   setx GOPROXY https://goproxy.cn,direct
   ```
4. 在本项目目录运行示例命令（如上）进行转换。
