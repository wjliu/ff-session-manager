package logscan

import (
	"fmt"
	"regexp"
	"sort"
)

// compile 预编译 EmuRules 中的所有正则表达式。
// CaseResultRules 按 Priority 升序排列，数值越小优先级越高。
func (e *EmuRules) compile() error {
	if e.beginRe != nil {
		return nil
	}

	for i, s := range e.BeginRules {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("compile begin_rules[%d]: %w", i, err)
		}
		e.beginRe = append(e.beginRe, re)
	}

	for i, s := range e.EndRules {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("compile end_rules[%d]: %w", i, err)
		}
		e.endRe = append(e.endRe, re)
	}

	if len(e.CaseResultRules) == 0 {
		return fmt.Errorf("case_result_rules must not be empty")
	}

	e.sortedResultRules = make([]CaseRule, len(e.CaseResultRules))
	copy(e.sortedResultRules, e.CaseResultRules)
	for i := range e.sortedResultRules {
		re, err := regexp.Compile(e.sortedResultRules[i].Pattern)
		if err != nil {
			return fmt.Errorf("compile case_result_rules[%d]: %w", i, err)
		}
		e.sortedResultRules[i].detailRe = re
	}
	sort.SliceStable(e.sortedResultRules, func(i, j int) bool {
		return e.sortedResultRules[i].Priority < e.sortedResultRules[j].Priority
	})

	return nil
}

// emuTracker 管理仿真扫描的状态机。
// emuActive 为 true 时表示处于 emulation session 中。
type emuTracker struct {
	rules     *EmuRules
	emuActive bool
	results   []EmuCaseResult
}

// processLine 处理一行日志，驱动状态机。
func (t *emuTracker) processLine(line string) {
	if !t.emuActive {
		// 1. 未激活时检查 beginRe → 激活 session
		for _, re := range t.rules.beginRe {
			if re.MatchString(line) {
				t.emuActive = true
				return
			}
		}
		return
	}

	// 2. 激活后优先检查 endRe → 结束 session
	for _, re := range t.rules.endRe {
		if re.MatchString(line) {
			t.emuActive = false
			return
		}
	}

	// 3. 按优先级匹配 CaseResultRules，命中后提取 case 名并产出结果
	for _, cr := range t.rules.sortedResultRules {
		matches := cr.detailRe.FindStringSubmatch(line)
		if len(matches) > 1 {
			t.results = append(t.results, EmuCaseResult{
				CaseName: matches[1],
				Result:   cr.Result,
				Keyword:  cr.Keyword,
			})
			return
		}
	}
}

// flushResults 返回并清空累积的结果。
func (t *emuTracker) flushResults() []EmuCaseResult {
	r := t.results
	t.results = nil
	return r
}

// reset 重置状态机。
func (t *emuTracker) reset() {
	t.emuActive = false
	t.results = nil
}
