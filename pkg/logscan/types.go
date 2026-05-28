// Package logscan 基于扫描日志文件的内容提取工具。
// 包含结果采集（全量扫描，返回最高优先级单条结果）和内容采集（增量扫描，返回全部命中内容）两种模式。
package logscan

import "regexp"

// Rule 定义一条采集规则。
type Rule struct {
	// Result 结果标识，由用户自行定义。
	Result string
	// Keyword 关键字，用于快速判断结果的分类。
	Keyword string
	// Detail 从日志中匹配内容的正则表达式。
	Detail string
	// Priority 优先级，数值越小优先级越高。采集时以高优先级规则命中的结果为准。
	Priority int
	// MatchStartPoint 匹配起始点正则。当日志行命中此模式后才开始采集，之前的内容忽略。
	MatchStartPoint string
	// ExtensionFields 扩展字段定义，从匹配行或上下文中提取额外字段。
	ExtensionFields []ExtensionField

	detailRe   *regexp.Regexp
	startRe    *regexp.Regexp
	extFieldRe map[string]*regexp.Regexp
}

// ExtensionField 定义从日志中提取的扩展字段。
type ExtensionField struct {
	// Name 扩展字段名。
	Name string
	// Pattern 用于提取字段值的正则表达式，必须包含一个捕获组。
	Pattern string
}

// ScanResult 表示一条采集命中的结果。
type ScanResult struct {
	// Rule 命中的采集规则。
	Rule *Rule
	// MatchedLine 命中的行内容。
	MatchedLine string
	// MatchedLineNum 命中的行在文件中的行号（从 1 开始）。
	MatchedLineNum int64
	// ContextLineNum 命中的行在返回的 ContextLines 中的行号（从 0 开始）。
	ContextLineNum int
	// ContextLines 命中行及其上下文行内容。
	ContextLines []string
	// ExtensionFields 从匹配行或上下文中提取的扩展字段。
	ExtensionFields map[string]string
}

// ScanConfig 扫描配置。
type ScanConfig struct {
	// ContextBefore 返回命中行之前的上下文行数。
	ContextBefore int
	// ContextAfter 返回命中行之后的上下文行数。
	ContextAfter int
	// IncludeLineNum 是否在结果中包含行号。
	IncludeLineNum bool
	// IncludeContextLineNum 是否包含命中行在上下文中的行号。
	IncludeContextLineNum bool
	// SafeIO 是否启用 NFS/IO 卡死防护。启用后所有 I/O 操作通过 goroutine+channel
	// 包装以响应 context 取消，但会有额外调度开销。默认关闭，走直接 I/O 快速路径。
	SafeIO bool
}

// compile 预编译规则中的正则表达式，避免每次匹配时重复编译。
func (r *Rule) compile() error {
	if r.detailRe != nil {
		return nil
	}
	re, err := regexp.Compile(r.Detail)
	if err != nil {
		return err
	}
	r.detailRe = re

	if r.MatchStartPoint != "" {
		re, err := regexp.Compile(r.MatchStartPoint)
		if err != nil {
			return err
		}
		r.startRe = re
	}

	r.extFieldRe = make(map[string]*regexp.Regexp, len(r.ExtensionFields))
	for _, ef := range r.ExtensionFields {
		re, err := regexp.Compile(ef.Pattern)
		if err != nil {
			return err
		}
		r.extFieldRe[ef.Name] = re
	}
	return nil
}

// CaseRule 定义一条 case 结果分类规则，用于 emulation 场景中判定 case 执行结果。
// 一条 case 结束时，按优先级匹配 case_result_rules 以确定结果。
type CaseRule struct {
	// Result 结果标识，由用户自行定义（例如 "pass"、"fail"、"unknown"）。
	Result string
	// Keyword 关键字，用于快速判断结果的分类。
	Keyword string
	// Priority 优先级，数值越小优先级越高。
	Priority int
	// Pattern 匹配 case 结果的正则表达式。
	Pattern string

	detailRe *regexp.Regexp
}

// EmuRules 定义硬件仿真（Emulation）场景的采集规则。
// 用于增量扫描中追踪 emulation session 生命周期和每个 case 的执行结果。
type EmuRules struct {
	// StartRules 匹配 emulation 开始的规则列表。非必填。
	StartRules []string
	// EndRules 匹配 emulation 结束的规则列表。必填。
	EndRules []string
	// CaseNameRules 提取 case 名称的规则列表，每个规则必须包含一个捕获组。必填。
	CaseNameRules []string
	// CaseStartRules 匹配 case 开始执行的规则列表。非必填。
	CaseStartRules []string
	// CaseEndRules 匹配 case 执行结束的规则列表。必填。
	CaseEndRules []string
	// CaseResultRules case 结果分类规则列表。必填。
	CaseResultRules []CaseRule

	startRe         []*regexp.Regexp
	endRe           []*regexp.Regexp
	caseNameRe      []*regexp.Regexp
	caseStartRe     []*regexp.Regexp
	caseEndRe       []*regexp.Regexp
	sortedResultRules []CaseRule
}

// EmuCaseResult 表示一个收集到的 case 执行结果。
type EmuCaseResult struct {
	// CaseName case 名称，从 case_name_rules 的捕获组提取。
	CaseName string
	// Result 结果标识，由 case_result_rules 中匹配的 result 字段定义。
	Result string
	// Keyword 关键字标识，由 case_result_rules 中匹配的 keyword 字段定义。
	Keyword string
}
