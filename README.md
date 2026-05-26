# ff-session-manager

基于 AI 生成的 FusionFlex session-manager 工具代码库。生成的代码文件可直接被拷贝或引用到 session-manager 项目中使用。

## 项目结构

```
ff-session-manager/
├── pkg/              # 可被外部项目引用的工具包
├── docs/             # 项目文档
├── specs/            # 工具包的设计文档（实现机制等）
├── go.mod            # Go 模块定义
└── .gitignore
```

## 使用方式

作为 Go 模块引入：

```bash
go get github.com/wjliu/ff-session-manager
```

## 开发

```bash
# 克隆仓库
git clone https://github.com/wjliu/ff-session-manager.git

# 运行测试
go test ./pkg/...

# 验证模块
go mod verify
```
