# log-scan

基于扫描日志文件的内容提取工具，提供两种扫描模式：

1. **Scan** — 一次性全文扫描，返回优先级最高的单条命中结果
2. **EmuScanner** — 仿真日志尾随扫描，持续追踪 emulation session 与 case 结果，同时返回常规规则命中


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

- `StartRules` / `EndRules` — 匹配 emulation session 起止，EndRules 必填
- `CaseNameRules` — 提取 case 名称，必须包含一个捕获组，必填
- `CaseStartRules` / `CaseEndRules` — 匹配 case 起止，CaseEndRules 必填
- `CaseResultRules` — case 结果分类规则列表（`Result,Keyword,Priority,Pattern`），必填

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
  emu_rules:            # 用于硬件仿真（Emulation）场景的采集规则，最主要的目标是采集本次Emulation过程中每个case的结果，非必填
    start_rules:        # 可以定义一条或者多条规则来匹配emulation是否开始，非必填
    - Emulation Start!
    end_rules:          # 可以定义一条或者多条规则来匹配emulation是否结束，必填
    - Emulation End!
    case_name_rules:    # 可以定义一条或者多条规则来匹配case的名称，必填
    - Case is (\w+)     # 匹配后提取捕获组即()中的内容作为case名称
    - Case (\w+)
    case_start_rules:   # 可以定义一条或者多条规则来匹配case是否开始执行，非必填
    - Case .*, start to run
    case_end_rules:     # 可以定义一条或者多条规则来匹配case是否执行结束，必填
    - Case .* is completed
    case_result_rules:  # 可以定义一条或者多条规则来匹配case执行结果，并且可以分类，必填
    - paas,case_passed,1,Case .* is completed  # 需要注意，第一列的结果字段不在固定为PASS和FAIL，支持用户自行定义，FusionFlex仅要求该列是表示结果的定义。其他三个部分定义参考pass_rules和fail_rules
    - fail,case_failed,1,Case .* is failed
    - unknown,case_unknown,1,Case .* is unknown
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
func (s *EmuScanner) Scan(ctx context.Context, rules []Rule, emuRules *EmuRules, config *ScanConfig) ([]EmuCaseResult, []ScanResult, error)
```

- `rules` — 常规匹配规则，命中后返回 `[]ScanResult`
- `emuRules` — 仿真追踪规则，驱动状态机产出 `[]EmuCaseResult`
- 首次调用以 emuRules 初始化内部追踪器，后续调用沿用已有追踪器

### 工作机制

- 每次调用从上次结束的字节偏移继续，无需手动指定，确保日志不遗漏。
- 文件截断时自动检测并重置偏移与追踪器状态。
- 新行逐条送入 emulation 状态机：检测 session 起止 → 提取 case 名称 → 检测 case 起止 → 分类 case 结果。
- 同时通过常规规则匹配，返回命中的 `ScanResult` 列表。
- 优先级仅用于单行决胜（同一行命中多条规则时归类）和 case 结果分类决胜。

### 状态管理

- `Reset()` — 重置扫描偏移和 emulation 追踪器状态
- `Offset() int64` — 返回当前字节偏移
- 并发安全：`Scan` 不可并发调用，`Reset` 和 `Offset` 可在任意时刻调用

### 输出

- `[]EmuCaseResult` — 本次新完成的 case 结果列表（可能为空）。每条包含 `CaseName`、`Result`、`Keyword`
- `[]ScanResult` — 本次新命中的常规规则结果列表（可能为空）
- 每条 ScanResult 包含命中的规则信息、匹配行内容、上下文行内容及扩展字段内容


---

## 实现原理

### 类型体系 (`types.go`)

**Rule** — 采集规则的核心数据结构：
- 对外字段：`Result`、`Keyword`、`Detail`、`Priority`、`MatchStartPoint`、`ExtensionFields`，与上文规则定义一一对应。
- 内部缓存字段（小写非导出）：`detailRe`、`startRe`、`extFieldRe`，通过 `compile()` 方法延迟预编译。

**CaseRule** — case 结果分类规则，用于 emulation 场景：
- 对外字段：`Result`、`Keyword`、`Priority`、`Pattern`。
- 内部缓存：`detailRe`（预编译正则）。

**EmuRules** — 仿真采集规则容器：
- 对外字段：`StartRules`、`EndRules`、`CaseNameRules`、`CaseStartRules`、`CaseEndRules`、`CaseResultRules`。
- 内部缓存：各组预编译正则切片，`sortedResultRules` 按 Priority 升序排列。

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

`Scan(ctx, rules, emuRules, config)` 方法：
1. 首次调用以 emuRules 初始化追踪器，后续调用沿用已有追踪器。
2. 打开文件、检测截断（截断时同步重置追踪器）、Seek 到 offset、读取新行。
3. 新行逐条送入 `emuTracker.processLine` 驱动状态机。
4. 同时通过 `scanLines` 进行常规规则匹配。
5. 返回 `([]EmuCaseResult, []ScanResult, error)`。

**状态管理**：`path`、`offset`（字节偏移）、`sync.Mutex` 保护。`Reset` 重置 offset 和追踪器；`Offset` 返回当前偏移。

### 仿真状态机 (`emu.go`)

**EmuRules 编译**（`compile`）：预编译所有正则切片，CaseResultRules 按 Priority 升序排列。

**emuTracker 状态机**（`processLine`）：
1. 未激活时检查 `startRe` → 激活 session
2. 激活后优先检查 `endRe` → 结束 session（如有活跃 case 则 flush）
3. 检查 `caseNameRe` → 提取 case 名称（捕获组）
4. 检查 `caseStartRe` → 标记 case 开始；若无 start 规则则隐式开始
5. 检查 `caseEndRe` → 按优先级匹配 `caseResultRe` 分类，产出 `EmuCaseResult`

### 测试覆盖 (`scanner_test.go`)

共 45 个测试用例，覆盖：
- 规则编译、排序、匹配、扩展字段/上下文提取、起始点控制
- 全文扫描（基本功能、上下文、起始点过滤、优先级、无匹配、空文件、context 取消、SafeIO）
- EmuScanner（基本扫描、无新内容、Reset、截断检测、文件不存在、SafeIO）
- EmuRules 编译（正常、无效正则、空 result_rules、幂等）
- emuTracker 状态机（session 起止、case 名称提取、结果分类、优先级决胜、多 case、隐式开始、idle 跳过、多 session、reset）
- EmuScanner 集成（emu+scan 结果、状态跨调用保持、截断重置）
