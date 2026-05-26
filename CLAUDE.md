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

## 工具包开发流程

1. 阅读 `specs/` 下对应的设计文档
2. 在 `pkg/` 下创建工具包子目录
3. 实现类型定义、核心逻辑、测试
4. 运行 `go test ./pkg/...` 确保通过
5. 运行 `go vet ./pkg/...` 确保无警告

## 第一个工具包

- `pkg/resultcollected/` — 基于扫描日志文件的结果采集工具，支持增量和全量两种模式。
