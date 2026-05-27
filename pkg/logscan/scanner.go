package logscan

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// sortRules 按优先级降序排列规则，优先级高的在前。
func sortRules(rules []Rule) []Rule {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})
	return sorted
}

// compileRules 预编译所有规则的正则表达式。
func compileRules(rules []Rule) error {
	for i := range rules {
		if err := rules[i].compile(); err != nil {
			return fmt.Errorf("compile rule %q: %w", rules[i].Result, err)
		}
	}
	return nil
}

// openFile 打开文件。
// safeIO=false 时走直接 I/O 快速路径（默认）；safeIO=true 时通过 goroutine+channel 响应 context 取消。
func openFile(ctx context.Context, filePath string, safeIO bool) (*os.File, error) {
	if !safeIO {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return os.Open(filePath)
	}

	type result struct {
		f   *os.File
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := os.Open(filePath)
		ch <- result{f, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			r := <-ch
			if r.f != nil {
				r.f.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		return r.f, r.err
	}
}

// readLines 从 reader 逐行读取，返回行内容及每行的起始字节偏移。
// safeIO=false 时走直接 I/O 快速路径（默认），仅在入口检查 context；safeIO=true 时每行读取可被 context 取消中断。
func readLines(ctx context.Context, reader io.Reader, startOffset int64, safeIO bool) ([]string, []int64, error) {
	if !safeIO {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var lines []string
		var offsets []int64
		scanner := bufio.NewScanner(reader)
		offset := startOffset
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			offsets = append(offsets, offset)
			offset += int64(len(scanner.Bytes()) + 1) // +1 for newline
		}
		return lines, offsets, scanner.Err()
	}

	type lineResult struct {
		lines   []string
		offsets []int64
		err     error
	}
	ch := make(chan lineResult, 1)
	go func() {
		var lines []string
		var offsets []int64
		scanner := bufio.NewScanner(reader)
		offset := startOffset
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			offsets = append(offsets, offset)
			offset += int64(len(scanner.Bytes()) + 1) // +1 for newline
		}
		ch <- lineResult{lines, offsets, scanner.Err()}
	}()
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case r := <-ch:
		return r.lines, r.offsets, r.err
	}
}

// matchLine 对单行内容按优先级顺序匹配规则。
// 返回第一个匹配的规则索引，未匹配返回 -1。
func matchLine(line string, rules []Rule) int {
	for i := range rules {
		if rules[i].detailRe.MatchString(line) {
			return i
		}
	}
	return -1
}

// extractExtensionFields 从给定文本中提取扩展字段。
func extractExtensionFields(rule *Rule, text string) map[string]string {
	if len(rule.extFieldRe) == 0 {
		return nil
	}
	fields := make(map[string]string, len(rule.extFieldRe))
	for _, ef := range rule.ExtensionFields {
		if re, ok := rule.extFieldRe[ef.Name]; ok {
			matches := re.FindStringSubmatch(text)
			if len(matches) > 1 {
				fields[ef.Name] = matches[1]
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// extractContextLines 提取命中行及其上下文行。
func extractContextLines(lines []string, matchIdx int, config *ScanConfig) ([]string, int) {
	if config == nil {
		return []string{lines[matchIdx]}, 0
	}
	before := config.ContextBefore
	after := config.ContextAfter
	start := max(matchIdx-before, 0)
	end := min(matchIdx+after+1, len(lines))
	ctxLines := make([]string, end-start)
	copy(ctxLines, lines[start:end])
	ctxLineNum := matchIdx - start
	return ctxLines, ctxLineNum
}

// buildResult 构造 ScanResult。
func buildResult(rule *Rule, matchedLine string, lineNum int64, contextLineNum int, contextLines []string, config *ScanConfig) ScanResult {
	r := ScanResult{
		Rule:            rule,
		MatchedLine:     matchedLine,
		ContextLines:    contextLines,
		ExtensionFields: extractExtensionFields(rule, matchedLine),
	}

	if rule.extFieldRe == nil || len(rule.extFieldRe) > 0 {
		// Also try extracting from context lines
		contextText := strings.Join(contextLines, "\n")
		extFields := extractExtensionFields(rule, contextText)
		if extFields != nil {
			if r.ExtensionFields == nil {
				r.ExtensionFields = extFields
			} else {
				for k, v := range extFields {
					r.ExtensionFields[k] = v
				}
			}
		}
	}

	if config != nil {
		if config.IncludeLineNum {
			r.MatchedLineNum = lineNum
		}
		if config.IncludeContextLineNum {
			r.ContextLineNum = contextLineNum
		}
	}
	return r
}

// scanLines 对已读取的行进行扫描，返回采集结果。
func scanLines(lines []string, lineOffsets []int64, rules []Rule, config *ScanConfig) []ScanResult {
	sortedRules := sortRules(rules)
	var results []ScanResult
	started := false

	for i, line := range lines {
		if !started {
			// 检查是否所有规则都有 startRe，如果有未命中的起始点则跳过
			started = allStartPointsMatched(sortedRules, line)
			if !started {
				continue
			}
		}

		ruleIdx := matchLine(line, sortedRules)
		if ruleIdx < 0 {
			continue
		}

		rule := &sortedRules[ruleIdx]
		lineNum := int64(i + 1)
		if len(lineOffsets) > 0 {
			lineNum = int64(i + 1)
		}

		ctxLines, ctxLineNum := extractContextLines(lines, i, config)
		result := buildResult(rule, line, lineNum, ctxLineNum, ctxLines, config)
		results = append(results, result)
	}

	return results
}

// allStartPointsMatched 检查是否所有规则的起始点都已匹配或无需起始点。
// 一旦某行命中任一起始点，则认为扫描已启动。
func allStartPointsMatched(rules []Rule, line string) bool {
	hasStartPoint := false
	for i := range rules {
		if rules[i].startRe != nil {
			hasStartPoint = true
			if rules[i].startRe.MatchString(line) {
				return true
			}
		}
	}
	return !hasStartPoint // 如果没有规则定义起始点，直接认为已启动
}

// seekFile 在文件中定位。
// safeIO=false 时走直接 I/O 快速路径（默认）；safeIO=true 时通过 goroutine+channel 响应 context 取消。
func seekFile(ctx context.Context, f *os.File, offset int64, safeIO bool) error {
	if !safeIO {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := f.Seek(offset, io.SeekStart)
		return err
	}

	type result struct {
		pos int64
		err error
	}
	ch := make(chan result, 1)
	go func() {
		pos, err := f.Seek(offset, io.SeekStart)
		ch <- result{pos, err}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		_ = r.pos
		return nil
	}
}
