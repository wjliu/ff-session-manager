package logscan

import (
	"fmt"
	"regexp"
	"sort"
)

// compile 预编译 EmuRules 中的所有正则表达式。
// CaseResultRules 按 Priority 升序排列，数值越小优先级越高。
func (e *EmuRules) compile() error {
	if e.startRe != nil {
		return nil
	}

	for i, s := range e.StartRules {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("compile start_rules[%d]: %w", i, err)
		}
		e.startRe = append(e.startRe, re)
	}

	for i, s := range e.EndRules {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("compile end_rules[%d]: %w", i, err)
		}
		e.endRe = append(e.endRe, re)
	}

	for i, s := range e.CaseNameRules {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("compile case_name_rules[%d]: %w", i, err)
		}
		e.caseNameRe = append(e.caseNameRe, re)
	}

	for i, s := range e.CaseStartRules {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("compile case_start_rules[%d]: %w", i, err)
		}
		e.caseStartRe = append(e.caseStartRe, re)
	}

	for i, s := range e.CaseEndRules {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("compile case_end_rules[%d]: %w", i, err)
		}
		e.caseEndRe = append(e.caseEndRe, re)
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
// caseActive 为 true 时表示有 case 正在执行。
// currentCaseName 从 case_name_rules 的捕获组中提取。
type emuTracker struct {
	rules           *EmuRules
	emuActive       bool
	caseActive      bool
	currentCaseName string
	results         []EmuCaseResult
}

// processLine 处理一行日志，驱动状态机。
func (t *emuTracker) processLine(line string) {
	if !t.emuActive {
		for _, re := range t.rules.startRe {
			if re.MatchString(line) {
				t.emuActive = true
				t.caseActive = false
				t.currentCaseName = ""
				return
			}
		}
		return
	}

	// 检查 emulation 是否结束，优先级最高
	for _, re := range t.rules.endRe {
		if re.MatchString(line) {
			if t.caseActive {
				t.emitResult("", "")
			}
			t.emuActive = false
			return
		}
	}

	// 提取 case 名称
	nameUpdated := false
	for _, re := range t.rules.caseNameRe {
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			t.currentCaseName = matches[1]
			nameUpdated = true
			break
		}
	}

	// 检测 case 开始
	if !t.caseActive {
		if len(t.rules.caseStartRe) > 0 {
			for _, re := range t.rules.caseStartRe {
				if re.MatchString(line) {
					t.caseActive = true
					break
				}
			}
		} else if nameUpdated {
			t.caseActive = true
		}
	}

	// 检测 case 结束
	if t.caseActive {
		for _, re := range t.rules.caseEndRe {
			if re.MatchString(line) {
				t.classifyAndEmit(line)
				t.caseActive = false
				return
			}
		}
	}
}

// classifyAndEmit 在 case 结束时按优先级匹配结果规则并产出结果。
func (t *emuTracker) classifyAndEmit(line string) {
	for _, cr := range t.rules.sortedResultRules {
		if cr.detailRe.MatchString(line) {
			t.results = append(t.results, EmuCaseResult{
				CaseName: t.currentCaseName,
				Result:   cr.Result,
				Keyword:  cr.Keyword,
			})
			return
		}
	}
	t.results = append(t.results, EmuCaseResult{
		CaseName: t.currentCaseName,
	})
}

// emitResult 产出当前 case 的结果，用于 session 结束时 flush 未完成的 case。
func (t *emuTracker) emitResult(result, keyword string) {
	t.results = append(t.results, EmuCaseResult{
		CaseName: t.currentCaseName,
		Result:   result,
		Keyword:  keyword,
	})
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
	t.caseActive = false
	t.currentCaseName = ""
	t.results = nil
}
