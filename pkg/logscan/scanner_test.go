package logscan

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
	if sorted[0].Priority != 1 || sorted[1].Priority != 5 || sorted[2].Priority != 10 {
		t.Errorf("rules not sorted by priority asc: %v", sorted)
	}
}

func TestMatchLine(t *testing.T) {
	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
		{Result: "warn", Keyword: "WARN", Detail: `WARN:.*`, Priority: 2},
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

	if !allStartPointsMatched(rules, "anything") {
		t.Error("expected started when no start points defined")
	}

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

func createTempLogFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---------- Scan (full) ----------

func TestScan(t *testing.T) {
	content := "INFO: starting\nERROR: something failed code=500\nINFO: processing\nWARN: low memory code=300\nINFO: done\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{
			Result:  "error_result",
			Keyword: "ERROR",
			Detail:  `ERROR:.*`,
			Priority: 1,
			ExtensionFields: []ExtensionField{
				{Name: "code", Pattern: `code=(\d+)`},
			},
		},
		{
			Result:  "warn_result",
			Keyword: "WARN",
			Detail:  `WARN:.*`,
			Priority: 2,
		},
	}

	ctx := context.Background()
	result, err := Scan(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Rule.Result != "error_result" {
		t.Errorf("expected error_result (priority 1 < warn priority 2), got %s", result.Rule.Result)
	}
	if result.ExtensionFields["code"] != "500" {
		t.Errorf("expected code=500, got %s", result.ExtensionFields["code"])
	}
}

func TestScanWithContext(t *testing.T) {
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
	result, err := Scan(ctx, path, rules, config)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.MatchedLineNum != 3 {
		t.Errorf("expected line num 3, got %d", result.MatchedLineNum)
	}
	if result.ContextLineNum != 1 {
		t.Errorf("expected context line num 1, got %d", result.ContextLineNum)
	}
	if len(result.ContextLines) != 3 {
		t.Errorf("expected 3 context lines, got %d", len(result.ContextLines))
	}
	if result.ContextLines[0] != "line2" || result.ContextLines[1] != "ERROR: fail" || result.ContextLines[2] != "line4" {
		t.Errorf("unexpected context: %v", result.ContextLines)
	}
}

func TestScanWithStartPoint(t *testing.T) {
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
	result, err := Scan(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(result.MatchedLine, "captured") {
		t.Errorf("expected captured error, got %q", result.MatchedLine)
	}
}

func TestScanPriority(t *testing.T) {
	content := "ERROR: something\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "low", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
		{Result: "high", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}

	ctx := context.Background()
	result, err := Scan(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Rule.Result != "high" {
		t.Errorf("expected high priority rule, got %s", result.Rule.Result)
	}
}

func TestScanNoMatch(t *testing.T) {
	content := "INFO: all good\nDEBUG: nothing\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	result, err := Scan(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestScanContextCanceled(t *testing.T) {
	content := "ERROR: test\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Scan(ctx, path, rules, nil)
	if err == nil {
		t.Error("expected context canceled error")
	}
}

func TestScanEmptyFile(t *testing.T) {
	path := createTempLogFile(t, "")

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	result, err := Scan(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestScanWithMultipleExtensionFields(t *testing.T) {
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
	result, err := Scan(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExtensionFields["code"] != "500" {
		t.Errorf("expected code=500, got %q", result.ExtensionFields["code"])
	}
	if result.ExtensionFields["user"] != "admin" {
		t.Errorf("expected user=admin, got %q", result.ExtensionFields["user"])
	}
}

func TestScanSafeIO(t *testing.T) {
	content := "ERROR: something failed\nWARN: warning\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
		{Result: "warn", Keyword: "WARN", Detail: `WARN:.*`, Priority: 2},
	}

	ctx := context.Background()
	config := &ScanConfig{SafeIO: true}
	result, err := Scan(ctx, path, rules, config)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result with SafeIO")
	}
	if result.Rule.Result != "error" {
		t.Errorf("expected error result, got %s", result.Rule.Result)
	}
}

func TestScanSafeIOContextCanceled(t *testing.T) {
	content := "ERROR: test\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	config := &ScanConfig{SafeIO: true}

	_, err := Scan(ctx, path, rules, config)
	if err == nil {
		t.Error("expected context canceled error with SafeIO")
	}
}

// ---------- EmuScanner ----------

// testEmuRules 返回一个永不会匹配日志内容的仿真规则，用于测试中仅验证常规扫描结果。
func testEmuRules() *EmuRules {
	return &EmuRules{
		EndRules: []string{`___NEVER___`},
		CaseResultRules: []CaseRule{
			{Result: "x", Keyword: "x", Priority: 1, Pattern: `___NEVER___`},
		},
	}
}

func TestEmuScanner(t *testing.T) {
	content := "INFO: start\nERROR: first error\nINFO: middle\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	_, results, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
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

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("ERROR: second error\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, results, err = scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from second scan, got %d", len(results))
	}
	if results[0].MatchedLine != "ERROR: second error" {
		t.Errorf("unexpected match: %q", results[0].MatchedLine)
	}
	if scanner.Offset() <= firstOffset {
		t.Error("offset should have advanced")
	}
}

func TestEmuScannerNoNewContent(t *testing.T) {
	content := "ERROR: only error\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	_, results, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	_, results, err = scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestEmuScannerReset(t *testing.T) {
	content := "ERROR: first\nERROR: second\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	_, results, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
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

	_, results, err = scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results after reset, got %d", len(results))
	}
}

func TestEmuScannerFileTruncation(t *testing.T) {
	content := "ERROR: first\nERROR: second\nERROR: third\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	_, results, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if err := os.WriteFile(path, []byte("ERROR: new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, results, err = scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after truncation, got %d", len(results))
	}
}

func TestEmuScannerNonExistentFile(t *testing.T) {
	scanner := NewEmuScanner("/nonexistent/path/test.log")
	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	emuResults, scanResults, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 0 {
		t.Errorf("expected 0 emu results, got %d", len(emuResults))
	}
	if len(scanResults) != 0 {
		t.Errorf("expected 0 scan results, got %d", len(scanResults))
	}
}

func TestEmuScannerSafeIO(t *testing.T) {
	content := "ERROR: first\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewSafeEmuScanner(path)
	ctx := context.Background()

	_, results, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].MatchedLine != "ERROR: first" {
		t.Errorf("unexpected match: %q", results[0].MatchedLine)
	}
}

func TestEmuScannerResumeFromOffset(t *testing.T) {
	content := "ERROR: first\nERROR: second\nERROR: third\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	_, results, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	savedOffset := scanner.Offset()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("ERROR: fourth\nERROR: fifth\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate restart: create new scanner at saved offset
	scanner2 := NewEmuScannerAtOffset(path, savedOffset)
	_, results, err = scanner2.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 new results, got %d", len(results))
	}
	if results[0].MatchedLine != "ERROR: fourth" {
		t.Errorf("unexpected match: %q", results[0].MatchedLine)
	}
	if results[1].MatchedLine != "ERROR: fifth" {
		t.Errorf("unexpected match: %q", results[1].MatchedLine)
	}
}

func TestEmuScannerResumeTruncationReset(t *testing.T) {
	content := "ERROR: first\nERROR: second\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	_, results, err := scanner.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	savedOffset := scanner.Offset()

	// File truncated to smaller size
	if err := os.WriteFile(path, []byte("ERROR: new only\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Resume with stale offset > new file size — should auto-reset
	scanner2 := NewEmuScannerAtOffset(path, savedOffset)
	_, results, err = scanner2.Scan(ctx, rules, testEmuRules(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after truncation reset, got %d", len(results))
	}
	if results[0].MatchedLine != "ERROR: new only" {
		t.Errorf("unexpected match: %q", results[0].MatchedLine)
	}
}


// ---------- Emulation tests ----------

func TestEmuRulesCompileSuccess(t *testing.T) {
	rules := &EmuRules{
		BeginRules: []string{`Emulation Start!`},
		EndRules:   []string{`Emulation End!`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case (\w+) is completed`},
			{Result: "fail", Keyword: "case_failed", Priority: 2, Pattern: `Case (\w+) is failed`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}
	if len(rules.sortedResultRules) != 2 {
		t.Fatalf("expected 2 sorted result rules, got %d", len(rules.sortedResultRules))
	}
	if rules.sortedResultRules[0].Priority != 1 || rules.sortedResultRules[1].Priority != 2 {
		t.Errorf("result rules not sorted by priority asc")
	}
}

func TestEmuRulesCompileInvalidRegex(t *testing.T) {
	rules := &EmuRules{
		BeginRules: []string{`[invalid`},
	}
	if err := rules.compile(); err == nil {
		t.Error("expected error for invalid regex in begin_rules")
	}
}

func TestEmuRulesCompileEmptyResultRules(t *testing.T) {
	rules := &EmuRules{
		EndRules: []string{`End`},
	}
	if err := rules.compile(); err == nil {
		t.Error("expected error for empty case_result_rules")
	}
}

func TestEmuRulesCompileIdempotent(t *testing.T) {
	rules := &EmuRules{
		EndRules: []string{`End`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `(\w+)`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}
}

func TestEmuTrackerSessionStart(t *testing.T) {
	rules := &EmuRules{
		BeginRules: []string{`Emulation Start!`},
		EndRules:   []string{`Emulation End!`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}
	tracker.processLine("some random line")
	if tracker.emuActive {
		t.Error("expected emu not active before start")
	}

	tracker.processLine("Emulation Start!")
	if !tracker.emuActive {
		t.Error("expected emu active after start line")
	}
}

func TestEmuTrackerSessionEnd(t *testing.T) {
	rules := &EmuRules{
		EndRules: []string{`Emulation End!`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules, emuActive: true}
	tracker.processLine("Case test1 done") // should produce result while active
	tracker.processLine("Emulation End!")  // ends session

	if tracker.emuActive {
		t.Error("expected emu not active after end line")
	}
	// Result produced before end should still be there
	results := tracker.flushResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].CaseName != "test1" {
		t.Errorf("expected case name test1, got %s", results[0].CaseName)
	}
}

func TestEmuTrackerCaseResultMatching(t *testing.T) {
	rules := &EmuRules{
		EndRules: []string{`Emulation End!`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case (\w+) is completed`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules, emuActive: true}
	tracker.processLine("Case test_001 is completed")

	if len(tracker.results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(tracker.results))
	}
	r := tracker.results[0]
	if r.CaseName != "test_001" || r.Result != "pass" || r.Keyword != "case_passed" {
		t.Errorf("unexpected result: name=%s result=%s keyword=%s", r.CaseName, r.Result, r.Keyword)
	}
}

func TestEmuTrackerCaseResultPriority(t *testing.T) {
	rules := &EmuRules{
		EndRules: []string{`Emulation End!`},
		CaseResultRules: []CaseRule{
			{Result: "low_priority", Keyword: "low", Priority: 10, Pattern: `Case (\w+) is done`},
			{Result: "high_priority", Keyword: "high", Priority: 1, Pattern: `Case (\w+) is done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules, emuActive: true}
	tracker.processLine("Case test_001 is done")

	if len(tracker.results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(tracker.results))
	}
	if tracker.results[0].Result != "high_priority" {
		t.Errorf("expected high_priority (priority 1 < 10), got %s", tracker.results[0].Result)
	}
}

func TestEmuTrackerMultipleCases(t *testing.T) {
	rules := &EmuRules{
		BeginRules: []string{`Emulation Start!`},
		EndRules:   []string{`Emulation End!`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case (\w+) is completed`},
			{Result: "fail", Keyword: "case_failed", Priority: 1, Pattern: `Case (\w+) is failed`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}
	lines := []string{
		"Emulation Start!",
		"Case test_001 is completed",
		"Case test_002 is failed",
		"Emulation End!",
	}
	for _, line := range lines {
		tracker.processLine(line)
	}

	results := tracker.flushResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].CaseName != "test_001" || results[0].Result != "pass" {
		t.Errorf("unexpected result 0: name=%s result=%s", results[0].CaseName, results[0].Result)
	}
	if results[1].CaseName != "test_002" || results[1].Result != "fail" {
		t.Errorf("unexpected result 1: name=%s result=%s", results[1].CaseName, results[1].Result)
	}
}

func TestEmuTrackerIdleLinesSkipped(t *testing.T) {
	rules := &EmuRules{
		BeginRules: []string{`START`},
		EndRules:   []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}
	tracker.processLine("some idle line")
	tracker.processLine("Case test_001 done") // before session start, should be ignored

	if tracker.emuActive {
		t.Error("expected idle lines to be skipped before session start")
	}
	if len(tracker.results) != 0 {
		t.Error("expected no results before session start")
	}
}

func TestEmuTrackerMultipleSessions(t *testing.T) {
	rules := &EmuRules{
		BeginRules: []string{`START`},
		EndRules:   []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}

	// session 1
	tracker.processLine("START")
	tracker.processLine("Case case1 done")
	tracker.processLine("END")

	results1 := tracker.flushResults()
	if len(results1) != 1 || results1[0].CaseName != "case1" {
		t.Errorf("unexpected session 1 results: %v", results1)
	}

	// session 2
	tracker.processLine("START")
	tracker.processLine("Case case2 done")
	tracker.processLine("END")

	results2 := tracker.flushResults()
	if len(results2) != 1 || results2[0].CaseName != "case2" {
		t.Errorf("unexpected session 2 results: %v", results2)
	}
}

func TestEmuTrackerReset(t *testing.T) {
	rules := &EmuRules{
		BeginRules: []string{`START`},
		EndRules:   []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}
	tracker.processLine("START")
	tracker.processLine("Case test1 done")

	tracker.reset()
	if tracker.emuActive {
		t.Error("expected emuActive false after reset")
	}
	if len(tracker.results) != 0 {
		t.Error("expected empty results after reset")
	}
}

func TestEmuScannerScan(t *testing.T) {
	content := "START\nCase test_001 is completed\nCase test_002 is failed\nEND\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		BeginRules: []string{`START`},
		EndRules:   []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case (\w+) is completed`},
			{Result: "fail", Keyword: "case_failed", Priority: 1, Pattern: `Case (\w+) is failed`},
		},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	emuResults, scanResults, err := scanner.Scan(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 2 {
		t.Fatalf("expected 2 emu results, got %d", len(emuResults))
	}
	if emuResults[0].CaseName != "test_001" || emuResults[0].Result != "pass" {
		t.Errorf("unexpected emu result 0: %+v", emuResults[0])
	}
	if emuResults[1].CaseName != "test_002" || emuResults[1].Result != "fail" {
		t.Errorf("unexpected emu result 1: %+v", emuResults[1])
	}
	if len(scanResults) != 0 {
		t.Errorf("expected 0 scan results, got %d", len(scanResults))
	}
}

func TestEmuScannerScanStatePersistence(t *testing.T) {
	content := "START\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		BeginRules: []string{`START`},
		EndRules:   []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) is completed`},
		},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	emuResults, _, err := scanner.Scan(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 0 {
		t.Fatalf("expected 0 emu results (no case matched yet), got %d", len(emuResults))
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("Case test_001 is completed\nEND\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	emuResults, _, err = scanner.Scan(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 1 {
		t.Fatalf("expected 1 emu result after case matched, got %d", len(emuResults))
	}
	if emuResults[0].CaseName != "test_001" || emuResults[0].Result != "pass" {
		t.Errorf("unexpected emu result: %+v", emuResults[0])
	}
}

func TestEmuScannerScanTruncationReset(t *testing.T) {
	content := "START\nCase test_001 is completed\nCase test_002 is completed\nEND\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		BeginRules: []string{`START`},
		EndRules:   []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) is completed`},
		},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	emuResults, _, err := scanner.Scan(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 2 {
		t.Fatalf("expected 2 emu results, got %d", len(emuResults))
	}

	if err := os.WriteFile(path, []byte("START\nCase new_case is completed\nEND\n"), 0644); err != nil {
		t.Fatal(err)
	}

	emuResults, _, err = scanner.Scan(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 1 {
		t.Fatalf("expected 1 emu result after truncation, got %d", len(emuResults))
	}
	if emuResults[0].CaseName != "new_case" {
		t.Errorf("expected case new_case after truncation, got %s", emuResults[0].CaseName)
	}
}

func TestEmuScannerScanFileNotFound(t *testing.T) {
	scanner := NewEmuScanner("/nonexistent/path/emu.log")
	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		EndRules: []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) done`},
		},
	}

	ctx := context.Background()
	emuResults, scanResults, err := scanner.Scan(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 0 {
		t.Errorf("expected 0 emu results, got %d", len(emuResults))
	}
	if len(scanResults) != 0 {
		t.Errorf("expected 0 scan results, got %d", len(scanResults))
	}
}

func TestEmuScannerScanWithScanResults(t *testing.T) {
	content := "START\nERROR: something broke\nCase test_001 is completed\nEND\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		BeginRules: []string{`START`},
		EndRules:   []string{`END`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case (\w+) is completed`},
		},
	}

	scanner := NewEmuScanner(path)
	ctx := context.Background()

	emuResults, scanResults, err := scanner.Scan(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 1 {
		t.Fatalf("expected 1 emu result, got %d", len(emuResults))
	}
	if len(scanResults) != 1 {
		t.Fatalf("expected 1 scan result, got %d", len(scanResults))
	}
	if scanResults[0].MatchedLine != "ERROR: something broke" {
		t.Errorf("unexpected scan match: %q", scanResults[0].MatchedLine)
	}
}
