# result-collected-based-on-scan-logs

基于扫描日志文件的结果采集工具，主要分为两种：
1. 增量扫描日志文件内容并从中提取结果
2. 全量扫描日志文件内容并从中提取结果


## 采集规则定义

采集规则包含如下关键定义：
- 结果：必选，可以用户自行定义
- 关键字：必选，用于快速判断结果的分类关键字
- 详细内容：必选，从日志中匹配到的完整的内容
- 优先级：可选，针对采集规则排序，在采集时以高优先级规则命中的结果为准
- 匹配起始点：可选，当匹配起始点命中时再开始进行采集，否则前面的日志行内容可以直接忽略
- 扩展字段：可选，除了匹配结果外，可以从该行或者临近的上下文中提取字段，规则中定义出提取的字段名和提取的正则表达式，提取出来的内容作为扩展字段值

## 采集工作机制

除了上述基础的采集规则定义外，在实际工作时应注意以下：
- 采集时应逐行扫描日志文件，并按照优先级顺序依次匹配
- 在所有行内容全部扫描完成后，将优先级最高的规则命中的结果作为最终结果返回
- 命中结果并返回时可以根据参数控制返回上下文行内容、命中的行内容、命中的行在文件中的行数、命中的行在返回的上下文中的行数等
- 当定义扩展字段时，应将扩展字段内容一并返回


## 增量采集工具

增量采集工具具备以下功能：
- 每次调用时，可以从上次匹配完成的位置继续匹配，无需手动指定偏移，并且确保日志内容不会遗漏
- 采集时能应对NFS hang住或者IO卡死场景

### 输入

- 日志文件，通常只需要一个确定的文件路径
- 采集规则，基于上述采集规则定义的结构的对象，应包含一条或者多条规则供工具使用

### 输出

- 采集到的结果内容
- 当包含扩展字段时，也返回扩展字段内容

## 全量采集工具

全量采集工具具备以下功能：
- 依次完整匹配所有文件内容，根据上述采集工作机制执行
- 采集时能应对NFS hang住或者IO卡死场景

### 输入

- 日志文件，通常只需要一个确定的文件路径
- 采集规则，基于上述采集规则定义的结构的对象，应包含一条或者多条规则供工具使用

### 输出

- 采集到的结果内容
- 当包含扩展字段时，也返回扩展字段内容

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

### 共用扫描引擎 (`scanner.go`)

引擎按以下流水线处理日志：

1. **编译阶段**（`compileRules`）：遍历所有 Rule，调用 `compile()` 预编译 Detail、MatchStartPoint 和每个 ExtensionField 的正则表达式。编译失败时立即返回错误，包含具体规则名。

2. **排序阶段**（`sortRules`）：按 Priority 降序排列规则。排序返回新切片而不修改原切片。优先级高的规则排在前面，匹配时优先命中。

3. **I/O 阶段** — 所有文件操作通过 goroutine + channel + `select` 模式实现 context 取消：
   - `openFile`：在 goroutine 中调用 `os.Open`，主 goroutine 通过 `select` 同时监听 context 取消和打开结果。若 context 已取消，在另一个 goroutine 中等待文件打开完成后关闭它，防止文件句柄泄漏。
   - `readLines`：以 `bufio.Scanner` 逐行读取，同时记录每行的起始字节偏移量（`offset += len(line) + 1`，+1 为换行符）。上下文取消时立即返回 `ctx.Err()`。
   - `seekFile`：在 goroutine 中执行 `f.Seek`，支持 context 超时控制。
   - 此模式确保 NFS hang 住或 IO 卡死时，调用方能通过 context 超时或取消来中断阻塞。

4. **扫描阶段**（`scanLines`）— 核心匹配循环：
   - 逐行遍历所有行内容。
   - **起始点控制**：在尚未启动采集时，调用 `allStartPointsMatched` 检查当前行是否命中任一规则的 `MatchStartPoint`。若未命中则跳过该行。若所有规则都未定义起始点，则从第一行即启动。
   - **规则匹配**：调用 `matchLine` 对当前行按优先级顺序依次匹配 `detailRe`。返回第一个匹配的规则索引（即优先级最高的命中规则）。
   - **结果构造**：命中后通过 `extractContextLines` 提取上下文行，再通过 `buildResult` 构造 `ScanResult`。

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

### 全量采集 (`full.go`)

`ScanFull` 函数式入口，流程简洁：
1. 编译规则（`compileRules`）。
2. 通过 `openFile` 打开文件（支持 context 取消）。
3. 通过 `readLines` 读取全部行内容和字节偏移（支持 context 取消）。
4. 调用 `scanLines` 执行扫描并返回全部命中结果。

全量扫描每次从文件开头完整处理，不记录状态。

### 增量采集 (`incremental.go`)

`IncrementalScanner` 是有状态的扫描器，核心设计：

**状态管理**：
- `path` 记录日志文件路径。
- `offset` 记录上次读取结束的字节位置。每次 `Scan` 结束后更新为最后读取行的末尾偏移（`offsets[last] + len(lastLine) + 1`）。
- `sync.Mutex` 保护 `offset` 的读写，`Scan` 持有锁全程，`Reset` 和 `Offset` 单独加锁。

**增量扫描流程**（`Scan` 方法）：
1. 加锁，编译规则。
2. 打开文件（文件不存在时返回空结果而非报错，容忍日志文件延迟创建）。
3. `f.Stat()` 获取文件大小，与当前 `offset` 比较：
   - 若文件大小 < offset，说明文件被截断（如日志轮转），将 offset 重置为 0，从头开始扫描。
4. `seekFile` 定位到 offset。
5. `readLines` 从 offset 起读取新写入的行。
6. `scanLines` 扫描新行并返回结果。
7. 更新 offset。

**并发安全**：`Scan` 不可并发调用（内部状态变更）。`Reset` 和 `Offset` 可在任意时刻调用，各自加锁保护。三者之间通过互斥锁保证无数据竞争。

**截断检测**：通过 `f.Stat()` 获取当前文件大小并与记录的 offset 比较。若文件大小更小，判定文件已被外部截断（如日志 rotate），将 offset 重置为 0 重新扫描。若新文件大小仍小于 offset，下次 Scan 时会再次触发重置，确保最终收敛。

### 测试覆盖 (`scanner_test.go`)

共 23 个测试用例，覆盖以下场景：
- 规则编译（正常、无效正则、含起始点、含扩展字段）
- 规则排序（优先级降序）
- 行匹配（命中/未命中/空行）
- 扩展字段提取（单个、多个字段）
- 上下文行提取（正常、边界裁剪）
- 起始点控制（有/无起始点规则）
- 全量扫描（基本功能、上下文行数、起始点过滤、优先级语义、无匹配、空文件、context 取消）
- 增量扫描（基本功能、无新内容返回空、Reset 重置、文件截断检测、文件不存在容错）
- 多扩展字段提取
