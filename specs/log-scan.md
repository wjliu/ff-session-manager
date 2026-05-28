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
- 内部缓存字段（小写非导出）：`detailRe`、`startRe`、`extFieldRe`，通过 `compile()` 方法延迟预编译。预编译后的正则对象在后续匹配中重复使用，避免每次匹配都重新编译。

**ScanResult** — 命中结果的载体：
- `Rule` 指针指向命中的规则，调用方可据此获取 `Result`、`Keyword` 等信息。
- `MatchedLine` 为命中的原始行内容；`MatchedLineNum` 为文件中绝对行号（从 1 开始）。
- `ContextLines` 包含命中行及其上下的内容；`ContextLineNum` 为命中行在 `ContextLines` 中的下标（从 0 开始），便于定位。
- `ExtensionFields` 为 `map[string]string`，存储从匹配行或上下文中提取的扩展字段。

**ScanConfig** — 控制输出行为：
- `ContextBefore`/`ContextAfter` 控制上下文行数。
- `IncludeLineNum`/`IncludeContextLineNum` 分别控制是否填充行号字段。开关分离，按需启用以减少不必要的数据填充。
- `SafeIO` 控制是否启用 NFS/IO 卡死防护。默认 false，走直接 I/O 快速路径；设为 true 时 I/O 操作通过 goroutine+channel 包装以响应 context 取消，但有额外调度开销。

### 共用扫描引擎 (`scanner.go`)

引擎按以下流水线处理日志：

1. **编译阶段**（`compileRules`）：遍历所有 Rule，调用 `compile()` 预编译 Detail、MatchStartPoint 和每个 ExtensionField 的正则表达式。编译失败时立即返回错误，包含具体规则名。

2. **排序阶段**（`sortRules`）：按 Priority 降序排列规则。排序返回新切片而不修改原切片。优先级高的规则排在前面，匹配时优先命中。

3. **I/O 阶段** — 双路径设计，通过 `safeIO bool` 参数切换：
   - **快速路径（默认，`safeIO=false`）**：直接调用 `os.Open`、`bufio.Scanner`、`f.Seek`，仅在入口处检查 `ctx.Err()`。零额外 goroutine，零 channel 分配。适用于本地文件或高性能场景。
   - **安全路径（`safeIO=true`）**：通过 goroutine + channel + `select` 模式包装 I/O 调用，context 取消时可立即中断阻塞。适用于 NFS 或网络文件系统等可能 hang 住的场景。
   - 共用逻辑：`openFile`、`readLines`、`seekFile` 三个函数均接受 `safeIO` 参数。`readLines` 快速路径通过 `bufio.Scanner` 逐行读取并记录每行起始字节偏移（`offset += len(line) + 1`）。
   - 安全路径中，若 context 在 `openFile` 返回前取消，泄漏的 goroutine 会在文件打开完成后将其关闭，防止句柄泄漏。

4. **扫描阶段**（`scanLines`）— 核心匹配循环，返回所有命中结果：
   - 逐行遍历所有行内容。
   - **起始点控制**：在尚未启动采集时，调用 `allStartPointsMatched` 检查当前行是否命中任一规则的 `MatchStartPoint`。若未命中则跳过该行。若所有规则都未定义起始点，则从第一行即启动。
   - **规则匹配**：调用 `matchLine` 对当前行按优先级顺序依次匹配 `detailRe`。返回第一个匹配的规则索引（即优先级最高的命中规则）。
   - **结果构造**：命中后通过 `extractContextLines` 提取上下文行，再通过 `buildResult` 构造 `ScanResult` 并追加到结果列表。

5. **上下文提取**（`extractContextLines`）：
   - 根据配置的 `ContextBefore`/`ContextAfter` 计算窗口范围 `[matchIdx-before, matchIdx+after+1)`，边界裁剪到 `[0, len(lines)]`。
   - 返回上下文行切片和命中行在其中的索引。

6. **扩展字段提取**（`extractExtensionFields`）：
   - 遍历规则的 ExtensionField 列表，用预编译的正则对文本执行 `FindStringSubmatch`，取第一个捕获组作为字段值。
   - 若正则未匹配到捕获组，则该字段不出现于结果中。
   - `buildResult` 中先在匹配行上提取，再在 `strings.Join(contextLines, "\n")` 合并文本上提取，两次结果合并，优先保留行级别的结果。

7. **起始点判断**（`allStartPointsMatched`）：
   - 遍历排序后的规则，若任一规则的 `startRe` 匹配当前行，则返回 true（启动采集）。
   - 若没有任何规则定义了 `MatchStartPoint`，直接返回 true，即从文件开头开始采集。

### 结果采集 / 全量扫描 (`full.go`)

`ScanFull` 函数式入口：
1. 编译规则（`compileRules`）。
2. 通过 `openFile` 打开文件（根据 `config.SafeIO` 选择快速/安全路径）。
3. 通过 `readLines` 读取全部行内容和字节偏移（根据 `config.SafeIO` 选择路径）。
4. 调用 `scanLines` 执行扫描，得到所有命中结果。
5. 通过 `pickHighestPriority` 从所有命中结果中筛选举最高优先级的单条结果返回。若优先级相同，取最先命中的那条。

全量扫描每次从文件开头完整处理，不记录状态。

### 内容采集 / 增量扫描 (`incremental.go`)

`IncrementalScanner` 是有状态的扫描器，设计目标是**持续收集所有匹配内容**。

每次 `Scan` 调用将新增内容中所有命中行一并返回，不做优先级过滤。调用方自行决定如何使用这些结果（例如累积统计、阈值告警等）。

**构造函数**：
- `NewIncrementalScanner(filePath)` — 默认构造，走直接 I/O 快速路径。适合本地文件或多 scanner 并行的高性能场景。
- `NewSafeIncrementalScanner(filePath)` — 安全构造，内部设置 `safeIO=true`。所有 I/O 通过 goroutine+channel 包装，适合 NFS 等可能 hang 住的场景。

**状态管理**：
- `path` 记录日志文件路径。
- `offset` 记录上次读取结束的字节位置。每次 `Scan` 结束后更新为最后读取行的末尾偏移（`offsets[last] + len(lastLine) + 1`）。
- `sync.Mutex` 保护 `offset` 的读写，`Scan` 持有锁全程，`Reset` 和 `Offset` 单独加锁。

**增量扫描流程**（`Scan` 方法）：
1. 加锁，编译规则。
2. 通过 `openFile` 打开文件（根据 `s.safeIO` 选择路径；文件不存在时返回空结果而非报错）。
3. `f.Stat()` 获取文件大小，与当前 `offset` 比较：
   - 若文件大小 < offset，说明文件被截断（如日志轮转），将 offset 重置为 0，从头开始扫描。
4. 通过 `seekFile` 定位到 offset（根据 `s.safeIO` 选择路径）。
5. 通过 `readLines` 从 offset 起读取新写入的行（根据 `s.safeIO` 选择路径）。
6. `scanLines` 扫描新行，返回全部命中结果（不过滤）。
7. 更新 offset。

**并发安全**：`Scan` 不可并发调用（内部状态变更）。`Reset` 和 `Offset` 可在任意时刻调用，各自加锁保护。三者之间通过互斥锁保证无数据竞争。

**截断检测**：通过 `f.Stat()` 获取当前文件大小并与记录的 offset 比较。若文件大小更小，判定文件已被外部截断（如日志 rotate），将 offset 重置为 0 重新扫描。若新文件大小仍小于 offset，下次 Scan 时会再次触发重置，确保最终收敛。

### 测试覆盖 (`scanner_test.go`)

共 26 个测试用例，覆盖以下场景：
- 规则编译（正常、无效正则、含起始点、含扩展字段）
- 规则排序（优先级降序）
- 行匹配（命中/未命中/空行）
- 扩展字段提取（单个、多个字段）
- 上下文行提取（正常、边界裁剪）
- 起始点控制（有/无起始点规则）
- 全量扫描（基本功能、上下文行数、起始点过滤、优先级语义、无匹配、空文件、context 取消）
- 增量扫描（基本功能、无新内容返回空、Reset 重置、文件截断检测、文件不存在容错、SafeIO 构造）
- 全量扫描 SafeIO 路径（正常扫描、context 取消）
- 多扩展字段提取
