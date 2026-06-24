package logscan

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

// NewEmuRules 根据规则字符串切片构造 EmuRules。
// beginRules 和 endRules 为原始正则字符串，可以为 nil 或空切片（beginRules 为空时从第一行激活 session）。
// casePassRules 和 caseFailRules 中每条字符串格式为 "keyword,priority,pattern"，
// 其中 priority 为整数（数值越小优先级越高），pattern 为正则表达式且必须包含一个捕获组用于提取 case 名称。
// pass 规则的 Result 固定为 "pass"，fail 规则的 Result 固定为 "fail"。
func NewEmuRules(beginRules, endRules, casePassRules, caseFailRules []string) (*EmuRules, error) {
	rules := &EmuRules{
		BeginRules: beginRules,
		EndRules:   endRules,
	}

	parseCaseRule := func(s string, result string) (CaseRule, error) {
		parts := strings.SplitN(s, ",", 3)
		if len(parts) != 3 {
			return CaseRule{}, fmt.Errorf("invalid case rule %q: expected format \"keyword,priority,pattern\"", s)
		}
		priority, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return CaseRule{}, fmt.Errorf("invalid priority in case rule %q: %w", s, err)
		}
		return CaseRule{
			Result:   result,
			Keyword:  strings.TrimSpace(parts[0]),
			Priority: priority,
			Pattern:  strings.TrimSpace(parts[2]),
		}, nil
	}

	for _, s := range casePassRules {
		cr, err := parseCaseRule(s, "pass")
		if err != nil {
			return nil, err
		}
		rules.CaseResultRules = append(rules.CaseResultRules, cr)
	}
	for _, s := range caseFailRules {
		cr, err := parseCaseRule(s, "fail")
		if err != nil {
			return nil, err
		}
		rules.CaseResultRules = append(rules.CaseResultRules, cr)
	}

	return rules, nil
}

// emuTracker 管理仿真扫描的状态机。
// emuActive 为 true 时表示处于 emulation session 中。
type emuTracker struct {
	rules       *EmuRules
	emuActive   bool
	beginSeen   bool
	endSeen     bool
	results     []EmuCaseResult
}

// processLine 处理一行日志，驱动状态机。
func (t *emuTracker) processLine(line string) {
	if !t.emuActive {
		// 1. 未激活时检查 beginRe → 激活 session
		if len(t.rules.beginRe) == 0 {
			// BeginRules 为空时，从第一行即视为 session 开始
			t.emuActive = true
			t.beginSeen = true
			// 继续往下，在同一条行检查 endRe 和 CaseResultRules
		} else {
			for _, re := range t.rules.beginRe {
				if re.MatchString(line) {
					t.emuActive = true
					t.beginSeen = true
					return
				}
			}
			return
		}
	}

	// 2. 激活后优先检查 endRe → 结束 session
	for _, re := range t.rules.endRe {
		if re.MatchString(line) {
			t.emuActive = false
			t.endSeen = true
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

// flushState 返回本轮扫描中状态机的状态变化，并重置 beginSeen/endSeen。
func (t *emuTracker) flushState() EmuScanState {
	s := EmuScanState{
		BeginMatched: t.beginSeen,
		EndMatched:   t.endSeen,
		InSession:    t.emuActive,
	}
	t.beginSeen = false
	t.endSeen = false
	return s
}

// reset 重置状态机。
func (t *emuTracker) reset() {
	t.emuActive = false
	t.beginSeen = false
	t.endSeen = false
	t.results = nil
}
