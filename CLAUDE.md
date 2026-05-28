# ff-session-manager

基于 AI 生成的 FusionFlex session-manager 工具代码库。生成的代码文件可直接被拷贝或引用到 session-manager 项目中使用。

## 项目结构

```
ff-session-manager/
├── pkg/              # 可被外部项目引用的 Go 工具包
├── docs/             # 项目架构与组织文档
├── specs/            # 工具包设计文档（实现机制、算法描述等）
├── go.mod            # Go 模块定义
├── go.sum            # 依赖校验和
└── .gitignore
```

- `pkg/` — 每个工具包一个子目录，包名即目录名。所有公开 API 必须有 godoc 注释。
- `docs/` — 项目级文档（架构、约定等），不涉及具体实现机制。
- `specs/` — 工具包设计文档，描述实现机制、算法、数据结构等，是编码的主要参考。

## 开发约定

- Go 版本: 1.21+
- 模块路径: `github.com/wjliu/ff-session-manager`
- 测试: `go test ./pkg/...`
- 错误处理: 使用 `fmt.Errorf` 包装错误，不引入第三方错误库。
- 日志: 不引入日志库，通过返回值传递错误信息。
- 并发安全: 公开的类型和方法需标注并发安全语义。
- 超时控制: 涉及 I/O 的操作必须支持 `context.Context` 取消和超时。
- 文档和规范使用中文编写。代码注释和提交信息可使用中文或英文。

## 工具包开发流程

1. 阅读 `specs/` 下对应的设计文档
2. 在 `pkg/` 下创建工具包子目录
3. 实现类型定义、核心逻辑、测试
4. 运行 `go test ./pkg/...` 确保通过
5. 运行 `go vet ./pkg/...` 确保无警告

## 开发进度

### 已完成

- 项目骨架初始化：go.mod (Go 1.21)、.gitignore、CLAUDE.md
- `pkg/logscan/` — 基于扫描日志文件的内容提取工具（对应 `specs/log-scan.md`）
  - 全文扫描：`Scan` 一次性全量扫描，返回最高优先级单条结果
  - 仿真追踪：`EmuScanner` 跟随日志增长进行 emulation case 追踪，返回 case 结果 + 常规命中
  - `types.go` — Rule、ExtensionField、ScanResult、ScanConfig、CaseRule、EmuRules、EmuCaseResult 类型定义
  - `scanner.go` — 共用扫描引擎（规则编译排序、双路径 I/O、逐行匹配、扩展字段/上下文提取）
  - `emu.go` — EmuRules 预编译 + emuTracker 状态机（session/case 起止检测、结果分类）
  - `incremental.go` — EmuScanner（偏移追踪、截断检测、Reset），提供 NewEmuScanner（快速路径）和 NewSafeEmuScanner（NFS 防护）两种构造
  - `full.go` — Scan 全量扫描 + pickHighestPriority 结果筛选
  - I/O 默认走直接调用快速路径；通过 ScanConfig.SafeIO 或 NewSafeEmuScanner 按需启用 goroutine+channel 防护
  - `scanner_test.go` — 46 个测试用例（含 SafeIO 双路径、context 取消、emu 状态机），全部通过
- Git 仓库已初始化，remote origin 指向 `https://github.com/wjliu/ff-session-manager.git`，分支 main

### 待完成

- 其他工具包（待 specs/ 下新增设计文档后开发）
