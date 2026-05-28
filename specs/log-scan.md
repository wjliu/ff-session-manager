# log-scan

基于扫描日志文件的内容提取工具，分为两种独立工具：

1. **结果采集工具**（全量扫描） — 读取完整日志文件，返回唯一一个最终结果
2. **内容采集工具**（增量扫描） — 持续收集日志文件中所有能匹配上规则的内容


## 采集规则定义

采集规则包含如下关键定义，由两个工具共用：
- 结果：必选，可以用户自行定义
- 关键字：必选，用于快速判断结果的分类关键字
- 详细内容：必选，从日志中匹配到的完整的内容
- 优先级：可选，当同一行内容命中多条规则时，以高优先级规则为准进行归类
- 匹配起始点：可选，当匹配起始点命中时再开始进行采集，否则前面的日志行内容可以直接忽略
- 扩展字段：可选，除了匹配结果外，可以从该行或者临近的上下文中提取字段，规则中定义出提取的字段名和提取的正则表达式，提取出来的内容作为扩展字段值

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


## 结果采集工具（全量扫描）

结果采集工具读取完整的日志文件，在所有行扫描完成后返回**唯一一个**最终结果。如果有多行命中，取优先级最高的那条作为最终结果。

### 工作机制

- 逐行扫描日志文件，按照优先级顺序依次匹配。
- 当某行命中多条规则时，优先级最高的规则胜出，该行按此规则归类。
- 所有行扫描完成后，从所有命中结果中取优先级最高的那条作为最终结果返回（优先级相同时取最先命中的）。
- 支持通过参数控制返回上下文行内容、命中的行号、命中行在上下文中的行数等。
- 当定义扩展字段时，将扩展字段内容一并返回。

### 输入

- 日志文件，通常只需要一个确定的文件路径。
- 采集规则，应包含一条或者多条规则供工具使用。

### 输出

- 优先级最高的命中结果（单条），可能为空（无匹配）。
- 当包含扩展字段时，也返回扩展字段内容。

### 其他

- 采集时能应对 NFS hang 住或者 IO 卡死场景。


## 内容采集工具（增量扫描）

内容采集工具的定位是**内容收集**：通过多次调用，持续收集日志文件中所有能匹配上规则的内容。每次调用都会将本次新扫描到的全部匹配结果返回，调用方自行决定如何使用。

与结果采集工具的核心区别：**不按优先级过滤结果**，所有命中行一律返回；优先级仅用于同一行命中多条规则时决定该行归类到哪条规则。

### 工作机制

- 每次调用时，从上次匹配完成的位置继续匹配，无需手动指定偏移，确保日志内容不会遗漏。
- 本次新扫描到的所有命中行全部返回，不做优先级过滤。可能返回零条、一条或多条结果。
- 优先级仅用于单行决胜：同一行命中多条规则时，选优先级最高的规则对该行进行归类。

### 输入

- 日志文件，通常只需要一个确定的文件路径。
- 采集规则，应包含一条或者多条规则供工具使用。规则中的优先级字段在内容采集中仅用于单行决胜，非必选。

### 输出

- 本次新扫描到的全部命中结果列表（可能为空）。
- 每条结果包含命中的规则信息、匹配行内容、上下文行内容及扩展字段内容。

### 其他

- 采集时能应对 NFS hang 住或者 IO 卡死场景。


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
