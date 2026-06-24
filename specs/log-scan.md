# log-scan

基于扫描日志文件的内容提取工具，提供两种扫描模式：

1. **Scan** — 一次性全文扫描，返回优先级最高的单条命中结果
2. **EmuScanner** — 仿真日志尾随扫描，持续追踪 emulation session 与 case 结果


## 采集规则定义

采集规则包含两种类型，分别用于常规匹配和 emulation 场景追踪。

### 常规规则（Rule）

每条 Rule 定义一个匹配模式，由 `Scan` 和 `EmuScanner` 共用：

- `Result` — 结果标识，由用户自行定义（例如 "PASS"、"FAIL"）
- `Keyword` — 关键字，用于快速判断结果的分类
- `Detail` — 匹配日志内容的正则表达式
- `Priority` — 优先级，数值越小优先级越高。当同一行命中多条规则时，以高优先级规则为准归类
- `MatchStartPoint` — 匹配起始点正则。命中后才开始采集，之前的内容忽略
- `ExtensionFields` — 扩展字段定义，从匹配行或上下文中提取额外字段

### 仿真规则（EmuRules）

用于 emulation 场景，定义 session 和 case 的生命周期追踪规则。仅由 `EmuScanner` 使用：

- `BeginRules` / `EndRules` — 匹配 emulation session 起止，EndRules 必填
- `CaseResultRules` — case 结果分类规则列表（`Result,Keyword,Priority,Pattern`），其中Pattern部分中必须包含一个捕获组，用于提取Case名称，必填

规则示例如下：
```yaml
- name: pattern1        # 以yaml格式为示例，每个规则的定义如下
  default: fail         # 默认结果，必填
  pass_rules:           # 结果为PASS的规则列表，规则中第一列为关键字，第二列为优先级（数值越小，优先级越高），第三列为正则表达式。pass_rules和fail_rules在采集结果时会合并到一起基于优先级排序后使用，必填
  - test_passed,1,TEST PASSED
  fail_rules:           # 结果为FAIL的规则列表，规则中第一列为关键字，第二列为优先级（数值越小，优先级越高），第三列为正则表达式，必填
  - test_failed,2,TEST FAILED
  exclude_rules:        # 采集结果是需要优先排除掉的规则列表，非必填
  - "Not Error"
  - "NOT ERROR"
  emulation:            # 用于硬件仿真（Emulation）场景的采集规则，最主要的目标是采集本次Emulation过程中每个case的结果，非必填
    begin_rules:        # 可以定义一条或者多条规则来匹配emulation是否开始，必填
    - Emulation Start!
    end_rules:          # 可以定义一条或者多条规则来匹配emulation是否结束，必填
    - Emulation End!
    case_pass_rules:  # 可以定义一条或者多条规则来匹配case执行pass的结果，并且可以分类，必填
    - case_passed,1,Case (.*) is completed  # 规则中第一列为关键字，第二列为优先级（数值越小，优先级越高），第三列为正则表达式。第三列正则表达式的捕获组表示捕获的case名称，需要提取出来作为case名称返回
    case_fail_rules:  # 可以定义一条或者多条规则来匹配case执行fail的结果，并且可以分类，必填
    - case_failed,1,Case (.*) is failed  # 规则中第一列为关键字，第二列为优先级（数值越小，优先级越高），第三列为正则表达式。第三列正则表达式的捕获组表示捕获的case名称，需要提取出来作为case名称返回
```


## Scan（全文扫描）

`Scan` 一次性读取完整日志文件，在所有行扫描完成后返回**唯一一个**最终结果。如果有多行命中，取优先级最高的那条作为最终结果。

### 函数签名

```go
func Scan(ctx context.Context, filePath string, rules []Rule, config *ScanConfig) (*ScanResult, error)
```

### 工作机制

- 逐行扫描日志文件，按优先级顺序依次匹配。
- 当某行命中多条规则时，优先级最高的规则胜出（数值最小的 Priority），该行按此规则归类。
- 所有行扫描完成后，从所有命中结果中取优先级最高的那条作为最终结果返回（优先级相同时取最先命中的）。
- 支持通过 `ScanConfig` 控制返回上下文行内容、行号等。
- 当定义扩展字段时，将扩展字段内容一并返回。

### 输入

- 日志文件路径。
- 采集规则列表 `[]Rule`，应包含一条或多条规则。
- 可选 `*ScanConfig`，设置 `SafeIO=true` 可启用 NFS/IO 卡死防护。

### 输出

- 优先级最高的命中结果（单条），可能为 nil（无匹配）。
- 当包含扩展字段时，也返回扩展字段内容。


## EmuScanner（仿真扫描）

`EmuScanner` 是有状态的扫描器，跟随日志文件增长持续追踪 emulation 过程。主要应用场景是硬件仿真日志：检测 session 起止，追踪每个 case 的执行结果。

与 `Scan` 的核心区别：**有状态、可多次调用**，每次仅扫描新增内容；内部维护 emulation 状态机以产出结构化的 `EmuCaseResult`。

### 构造函数

```go
func NewEmuScanner(filePath string) *EmuScanner           // 直接 I/O 快速路径
func NewSafeEmuScanner(filePath string) *EmuScanner        // NFS/IO 卡死防护
```

### Scan 方法

```go
func (s *EmuScanner) Scan(ctx context.Context, emuRules *EmuRules) ([]EmuCaseResult, EmuScanState, error)
```

- `emuRules` — 仿真追踪规则，首次调用时初始化内部状态机；后续调用沿用已有追踪器，可传 `nil`。

### 工作机制

- 每次调用从上次结束的字节偏移继续，无需手动指定，确保日志不遗漏。
- 文件截断时自动检测并重置偏移与追踪器状态。
- 新行逐条送入 emulation 状态机：检测 session 起止（BeginRules → EndRules）→ 分类 case 结果（CaseResultRules）。
- 优先级用于 case 结果分类决胜（同一行命中多条 CaseResultRule 时按 Priority 升序匹配，数值越小优先级越高）。

### 状态管理

- `Reset()` — 重置扫描偏移和 emulation 追踪器状态
- `Offset() int64` — 返回当前字节偏移
- 并发安全：`Scan` 不可并发调用，`Reset` 和 `Offset` 可在任意时刻调用

### 输出

- `[]EmuCaseResult` — 本次新完成的 case 结果列表（可能为空）。每条包含 `CaseName`、`Result`、`Keyword`
- `EmuScanState` — 本轮扫描中状态机的状态变化，包含三个字段：
  - `BeginMatched` — 本轮是否匹配到了 BeginRules
  - `EndMatched` — 本轮是否匹配到了 EndRules
  - `InSession` — 扫描结束后是否处于 emulation session 中


---

## 实现原理

### 类型体系 (`types.go`)

**Rule** — 采集规则的核心数据结构：
- 对外字段：`Result`、`Keyword`、`Detail`、`Priority`、`MatchStartPoint`、`ExtensionFields`，与上文规则定义一一对应。
- 内部缓存字段（小写非导出）：`detailRe`、`startRe`、`extFieldRe`，通过 `compile()` 方法延迟预编译。

**CaseRule** — case 结果分类规则，用于 emulation 场景：
- 对外字段：`Result`、`Keyword`、`Priority`、`Pattern`。`Pattern` 必须包含一个捕获组用于提取 case 名称。
- 内部缓存：`detailRe`（预编译正则）。

**EmuRules** — 仿真采集规则容器：
- 对外字段：`BeginRules`、`EndRules`、`CaseResultRules`。
- 内部缓存：`beginRe`/`endRe` 预编译正则切片，`sortedResultRules` 按 Priority 升序排列。

**EmuCaseResult** — 仿真 case 结果载体：`CaseName`、`Result`、`Keyword`。

**ScanResult** — 命中结果的载体：
- `Rule` 指针指向命中的规则，调用方可据此获取 `Result`、`Keyword` 等信息。
- `MatchedLine` 为命中的原始行内容；`MatchedLineNum` 为文件中绝对行号（从 1 开始）。
- `ContextLines` 包含命中行及其上下的内容；`ContextLineNum` 为命中行在 `ContextLines` 中的下标（从 0 开始）。
- `ExtensionFields` 为 `map[string]string`，存储从匹配行或上下文中提取的扩展字段。

**ScanConfig** — 控制输出行为：
- `ContextBefore`/`ContextAfter` 控制上下文行数。
- `IncludeLineNum`/`IncludeContextLineNum` 分别控制是否填充行号字段。
- `SafeIO` 控制是否启用 NFS/IO 卡死防护。默认 false，走直接 I/O 快速路径。

### 共用扫描引擎 (`scanner.go`)

引擎按以下流水线处理日志：

1. **编译阶段**（`compileRules`）：遍历所有 Rule，调用 `compile()` 预编译 Detail、MatchStartPoint 和每个 ExtensionField 的正则表达式。

2. **排序阶段**（`sortRules`）：按 Priority 升序排列（数值越小优先级越高），返回新切片不修改原切片。

3. **I/O 阶段** — 双路径设计，通过 `safeIO bool` 参数切换：
   - **快速路径（默认）**：直接调用 `os.Open`、`bufio.Scanner`、`f.Seek`，零额外 goroutine。
   - **安全路径（`safeIO=true`）**：通过 goroutine + channel + `select` 包装 I/O，context 取消时可立即中断。

4. **扫描阶段**（`scanLines`）— 核心匹配循环：
   - 逐行遍历，起始点控制（`allStartPointsMatched`），按优先级匹配（`matchLine`），提取上下文（`extractContextLines`），构造结果（`buildResult`）。

5. **扩展字段提取**（`extractExtensionFields`）：先在匹配行提取，再在合并上下文文本上提取，行级别结果优先。

### 全文扫描 (`scan.go`)

`Scan` 函数式入口：一次性读取完整文件，所有命中结果中取优先级最高的单条返回。无匹配返回 nil。每次从文件开头完整处理，不记录状态。

### 仿真扫描 (`emuscanner.go`)

`EmuScanner` 是有状态的扫描器，跟随日志文件增长进行 emulation 追踪。提供 `NewEmuScanner`（快速 I/O）和 `NewSafeEmuScanner`（NFS 防护）两种构造。

`Scan(ctx, emuRules)` 方法：
1. 首次调用以 emuRules 初始化追踪器，后续调用沿用已有追踪器（emuRules 可传 nil）。
2. 打开文件、检测截断（截断时同步重置追踪器）、Seek 到 offset、读取新行。
3. 新行逐条送入 `emuTracker.processLine` 驱动状态机。
4. 返回 `([]EmuCaseResult, EmuScanState, error)`。`EmuScanState` 包含 `BeginMatched`、`EndMatched`、`InSession` 三个字段，表示本轮扫描中的状态变化。

**状态管理**：`path`、`offset`（字节偏移）、`sync.Mutex` 保护。`Reset` 重置 offset 和追踪器；`Offset` 返回当前偏移。

### 仿真状态机 (`emu.go`)

**EmuRules 编译**（`compile`）：预编译所有正则切片，CaseResultRules 按 Priority 升序排列。

**emuTracker 状态机**（`processLine`）：
1. 未激活时检查 `beginRe` → 激活 session（`beginRe` 为空时从第一行即激活）
2. 激活后优先检查 `endRe` → 结束 session
3. 激活后按优先级匹配 `sortedResultRules` → 命中后从 Pattern 捕获组提取 case 名称，产出 `EmuCaseResult`（`Result`/`Keyword` 分别取自 `CaseRule.Result`/`CaseRule.Keyword`）
4. `flushState()` 返回并清空本轮的状态变化标志

### 测试覆盖 (`scanner_test.go`)

共 46 个测试用例，覆盖：
- 规则编译、排序、匹配、扩展字段/上下文提取、起始点控制
- 全文扫描（基本功能、上下文、起始点过滤、优先级、无匹配、空文件、context 取消、SafeIO）
- EmuScanner（基本扫描、增量追踪、无新内容、Reset、截断检测、文件不存在、SafeIO、offset 恢复、恢复后截断重置）
- EmuRules 编译（正常、无效正则、空 result_rules、幂等）
- emuTracker 状态机（session 起止、case 结果匹配、优先级决胜、多 case、idle 跳过、多 session、reset）
- EmuScanner 集成（case 匹配、状态跨调用保持、截断重置、非 case 行干扰测试）
