package resultcollected

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuleCompile(t *testing.T) {
	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
		{Result: "warn", Keyword: "WARN", Detail: `WARN:.*`, Priority: 5},
	}
	if err := compileRules(rules); err != nil {
		t.Fatal(err)
	}
	if rules[0].detailRe == nil {
		t.Error("expected detailRe to be compiled")
	}
}

func TestRuleCompileInvalidRegex(t *testing.T) {
	rules := []Rule{
		{Result: "bad", Keyword: "BAD", Detail: `[invalid`},
	}
	if err := compileRules(rules); err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestRuleCompileWithStartPoint(t *testing.T) {
	rules := []Rule{
		{
			Result:          "error",
			Keyword:         "ERROR",
			Detail:          `ERROR:.*`,
			Priority:        10,
			MatchStartPoint: `START`,
		},
	}
	if err := compileRules(rules); err != nil {
		t.Fatal(err)
	}
	if rules[0].startRe == nil {
		t.Error("expected startRe to be compiled")
	}
}

func TestRuleCompileWithExtensionFields(t *testing.T) {
	rules := []Rule{
		{
			Result:  "error",
			Keyword: "ERROR",
			Detail:  `ERROR:\s*(.*)`,
			ExtensionFields: []ExtensionField{
				{Name: "code", Pattern: `code=(\d+)`},
			},
		},
	}
	if err := compileRules(rules); err != nil {
		t.Fatal(err)
	}
	if rules[0].extFieldRe["code"] == nil {
		t.Error("expected extFieldRe to be compiled")
	}
}

func TestSortRules(t *testing.T) {
	rules := []Rule{
		{Result: "low", Priority: 1},
		{Result: "high", Priority: 10},
		{Result: "mid", Priority: 5},
	}
	sorted := sortRules(rules)
	if sorted[0].Priority != 10 || sorted[1].Priority != 5 || sorted[2].Priority != 1 {
		t.Errorf("rules not sorted by priority: %v", sorted)
	}
}

func TestMatchLine(t *testing.T) {
	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
		{Result: "warn", Keyword: "WARN", Detail: `WARN:.*`, Priority: 5},
	}
	if err := compileRules(rules); err != nil {
		t.Fatal(err)
	}
	rules = sortRules(rules)

	tests := []struct {
		line    string
		wantIdx int
	}{
		{"ERROR: something went wrong", 0},
		{"WARN: low memory", 1},
		{"INFO: all good", -1},
		{"", -1},
	}
	for _, tt := range tests {
		got := matchLine(tt.line, rules)
		if got != tt.wantIdx {
			t.Errorf("matchLine(%q) = %d, want %d", tt.line, got, tt.wantIdx)
		}
	}
}

func TestExtractExtensionFields(t *testing.T) {
	rule := &Rule{
		ExtensionFields: []ExtensionField{
			{Name: "code", Pattern: `code=(\d+)`},
			{Name: "user", Pattern: `user=(\w+)`},
		},
	}
	if err := rule.compile(); err != nil {
		t.Fatal(err)
	}
	fields := extractExtensionFields(rule, "ERROR: failed code=500 user=admin")
	if fields["code"] != "500" {
		t.Errorf("expected code=500, got %q", fields["code"])
	}
	if fields["user"] != "admin" {
		t.Errorf("expected user=admin, got %q", fields["user"])
	}
}

func TestExtractContextLines(t *testing.T) {
	lines := []string{"line0", "line1", "line2", "line3", "line4"}
	config := &ScanConfig{ContextBefore: 1, ContextAfter: 1}

	ctxLines, ctxLineNum := extractContextLines(lines, 2, config)
	if len(ctxLines) != 3 {
		t.Errorf("expected 3 context lines, got %d", len(ctxLines))
	}
	if ctxLineNum != 1 {
		t.Errorf("expected context line num 1, got %d", ctxLineNum)
	}
	if ctxLines[0] != "line1" || ctxLines[1] != "line2" || ctxLines[2] != "line3" {
		t.Errorf("unexpected context lines: %v", ctxLines)
	}
}

func TestExtractContextLinesAtBoundary(t *testing.T) {
	lines := []string{"line0", "line1", "line2"}
	config := &ScanConfig{ContextBefore: 5, ContextAfter: 5}

	ctxLines, ctxLineNum := extractContextLines(lines, 0, config)
	if len(ctxLines) != 3 {
		t.Errorf("expected 3 context lines, got %d", len(ctxLines))
	}
	if ctxLineNum != 0 {
		t.Errorf("expected context line num 0, got %d", ctxLineNum)
	}
}

func TestAllStartPointsMatched(t *testing.T) {
	rules := []Rule{
		{Result: "e1", Detail: `.*`, Priority: 10},
	}
	if err := compileRules(rules); err != nil {
		t.Fatal(err)
	}

	// 没有起始点规则，直接认为已启动
	if !allStartPointsMatched(rules, "anything") {
		t.Error("expected started when no start points defined")
	}

	// 有起始点规则
	rules2 := []Rule{
		{Result: "e1", Detail: `.*`, Priority: 10, MatchStartPoint: `START`},
	}
	if err := compileRules(rules2); err != nil {
		t.Fatal(err)
	}
	if allStartPointsMatched(rules2, "nothing here") {
		t.Error("expected not started before start point match")
	}
	if !allStartPointsMatched(rules2, "START processing") {
		t.Error("expected started after start point match")
	}
}

// createTempLogFile 创建临时日志文件用于测试。
func createTempLogFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanFull(t *testing.T) {
	content := "INFO: starting\nERROR: something failed code=500\nINFO: processing\nWARN: low memory code=300\nINFO: done\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{
			Result:  "error_result",
			Keyword: "ERROR",
			Detail:  `ERROR:.*`,
			Priority: 10,
			ExtensionFields: []ExtensionField{
				{Name: "code", Pattern: `code=(\d+)`},
			},
		},
		{
			Result:  "warn_result",
			Keyword: "WARN",
			Detail:  `WARN:.*`,
			Priority: 5,
		},
	}

	ctx := context.Background()
	results, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Rule.Result != "error_result" {
		t.Errorf("first result should be error_result, got %s", results[0].Rule.Result)
	}
	if results[0].ExtensionFields["code"] != "500" {
		t.Errorf("expected code=500, got %s", results[0].ExtensionFields["code"])
	}
	if results[1].Rule.Result != "warn_result" {
		t.Errorf("second result should be warn_result, got %s", results[1].Rule.Result)
	}
}

func TestScanFullWithContext(t *testing.T) {
	content := "line1\nline2\nERROR: fail\nline4\nline5\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}
	config := &ScanConfig{
		ContextBefore:         1,
		ContextAfter:          1,
		IncludeLineNum:        true,
		IncludeContextLineNum: true,
	}

	ctx := context.Background()
	results, err := ScanFull(ctx, path, rules, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.MatchedLineNum != 3 {
		t.Errorf("expected line num 3, got %d", r.MatchedLineNum)
	}
	if r.ContextLineNum != 1 {
		t.Errorf("expected context line num 1, got %d", r.ContextLineNum)
	}
	if len(r.ContextLines) != 3 {
		t.Errorf("expected 3 context lines, got %d", len(r.ContextLines))
	}
	if r.ContextLines[0] != "line2" || r.ContextLines[1] != "ERROR: fail" || r.ContextLines[2] != "line4" {
		t.Errorf("unexpected context: %v", r.ContextLines)
	}
}

func TestScanFullWithStartPoint(t *testing.T) {
	content := "pre-processing\nERROR: ignored\nSTART\nERROR: captured\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{
			Result:          "error",
			Keyword:         "ERROR",
			Detail:          `ERROR:.*`,
			Priority:        10,
			MatchStartPoint: `START`,
		},
	}

	ctx := context.Background()
	results, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].MatchedLine, "captured") {
		t.Errorf("expected captured error, got %q", results[0].MatchedLine)
	}
}

func TestScanFullPriority(t *testing.T) {
	content := "ERROR: something\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "low", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
		{Result: "high", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	results, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Rule.Result != "high" {
		t.Errorf("expected high priority rule, got %s", results[0].Rule.Result)
	}
}

func TestScanFullNoMatch(t *testing.T) {
	content := "INFO: all good\nDEBUG: nothing\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	results, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestScanFullContextCanceled(t *testing.T) {
	content := "ERROR: test\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := ScanFull(ctx, path, rules, nil)
	if err == nil {
		t.Error("expected context canceled error")
	}
}

func TestScanFullEmptyFile(t *testing.T) {
	path := createTempLogFile(t, "")

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	results, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestIncrementalScanner(t *testing.T) {
	content := "INFO: start\nERROR: first error\nINFO: middle\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	results, err := scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].MatchedLine != "ERROR: first error" {
		t.Errorf("unexpected match: %q", results[0].MatchedLine)
	}

	firstOffset := scanner.Offset()
	if firstOffset <= 0 {
		t.Errorf("expected positive offset, got %d", firstOffset)
	}

	// 追加新内容
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("ERROR: second error\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	// 增量扫描应只返回新内容
	results, err = scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from incremental scan, got %d", len(results))
	}
	if results[0].MatchedLine != "ERROR: second error" {
		t.Errorf("unexpected match: %q", results[0].MatchedLine)
	}
	if scanner.Offset() <= firstOffset {
		t.Error("offset should have advanced")
	}
}

func TestIncrementalScannerNoNewContent(t *testing.T) {
	content := "ERROR: only error\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	results, err := scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// 没有新内容时，第二次扫描应返回空
	results, err = scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestIncrementalScannerReset(t *testing.T) {
	content := "ERROR: first\nERROR: second\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	results, err := scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	scanner.Reset()
	if scanner.Offset() != 0 {
		t.Errorf("expected offset 0 after reset, got %d", scanner.Offset())
	}

	// 重置后再次扫描应返回所有结果
	results, err = scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results after reset, got %d", len(results))
	}
}

func TestIncrementalScannerFileTruncation(t *testing.T) {
	content := "ERROR: first\nERROR: second\nERROR: third\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	results, err := scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// 模拟文件被截断（重写更短的内容）
	if err := os.WriteFile(path, []byte("ERROR: new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 扫描应检测到截断并从头开始
	results, err = scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after truncation, got %d", len(results))
	}
}

func TestIncrementalScannerNonExistentFile(t *testing.T) {
	scanner := NewIncrementalScanner("/nonexistent/path/test.log")
	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	results, err := scanner.Scan(ctx, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent file, got %d", len(results))
	}
}

func TestScanFullWithMultipleExtensionFields(t *testing.T) {
	content := "ERROR: service failed code=500 user=admin\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{
			Result:  "error",
			Keyword: "ERROR",
			Detail:  `ERROR:.*`,
			Priority: 10,
			ExtensionFields: []ExtensionField{
				{Name: "code", Pattern: `code=(\d+)`},
				{Name: "user", Pattern: `user=(\w+)`},
			},
		},
	}

	ctx := context.Background()
	results, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExtensionFields["code"] != "500" {
		t.Errorf("expected code=500, got %q", results[0].ExtensionFields["code"])
	}
	if results[0].ExtensionFields["user"] != "admin" {
		t.Errorf("expected user=admin, got %q", results[0].ExtensionFields["user"])
	}
}
