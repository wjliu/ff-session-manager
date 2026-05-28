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

func TestScanFull(t *testing.T) {
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
	result, err := ScanFull(ctx, path, rules, nil)
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
	result, err := ScanFull(ctx, path, rules, config)
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
	result, err := ScanFull(ctx, path, rules, nil)
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

func TestScanFullPriority(t *testing.T) {
	content := "ERROR: something\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "low", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
		{Result: "high", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}

	ctx := context.Background()
	result, err := ScanFull(ctx, path, rules, nil)
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

func TestScanFullNoMatch(t *testing.T) {
	content := "INFO: all good\nDEBUG: nothing\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx := context.Background()
	result, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestScanFullContextCanceled(t *testing.T) {
	content := "ERROR: test\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

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
	result, err := ScanFull(ctx, path, rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
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

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("ERROR: second error\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

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

	if err := os.WriteFile(path, []byte("ERROR: new\n"), 0644); err != nil {
		t.Fatal(err)
	}

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
	result, err := ScanFull(ctx, path, rules, nil)
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

func TestScanFullSafeIO(t *testing.T) {
	content := "ERROR: something failed\nWARN: warning\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
		{Result: "warn", Keyword: "WARN", Detail: `WARN:.*`, Priority: 2},
	}

	ctx := context.Background()
	config := &ScanConfig{SafeIO: true}
	result, err := ScanFull(ctx, path, rules, config)
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

func TestIncrementalScannerSafeIO(t *testing.T) {
	content := "ERROR: first\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	scanner := NewSafeIncrementalScanner(path)
	ctx := context.Background()

	results, err := scanner.Scan(ctx, rules, nil)
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

func TestScanFullSafeIOContextCanceled(t *testing.T) {
	content := "ERROR: test\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 10},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	config := &ScanConfig{SafeIO: true}

	_, err := ScanFull(ctx, path, rules, config)
	if err == nil {
		t.Error("expected context canceled error with SafeIO")
	}
}

// ========== Emulation tests ==========

func TestEmuRulesCompileSuccess(t *testing.T) {
	rules := &EmuRules{
		StartRules:     []string{`Emulation Start!`},
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`, `Case .* is failed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case .* is completed`},
			{Result: "fail", Keyword: "case_failed", Priority: 2, Pattern: `Case .* is failed`},
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
		StartRules: []string{`[invalid`},
	}
	if err := rules.compile(); err == nil {
		t.Error("expected error for invalid regex in start_rules")
	}
}

func TestEmuRulesCompileEmptyResultRules(t *testing.T) {
	rules := &EmuRules{
		EndRules:      []string{`End`},
		CaseNameRules: []string{`Case (\w+)`},
		CaseEndRules:  []string{`done`},
	}
	if err := rules.compile(); err == nil {
		t.Error("expected error for empty case_result_rules")
	}
}

func TestEmuRulesCompileIdempotent(t *testing.T) {
	rules := &EmuRules{
		EndRules:       []string{`End`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `pass`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}
	// second call should be no-op
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}
}

func TestEmuTrackerSessionStart(t *testing.T) {
	rules := &EmuRules{
		StartRules:     []string{`Emulation Start!`},
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
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
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules, emuActive: true, caseActive: true, currentCaseName: "test1"}
	tracker.processLine("Emulation End!")

	if tracker.emuActive {
		t.Error("expected emu not active after end line")
	}
	if len(tracker.results) != 1 {
		t.Fatalf("expected 1 flushed result, got %d", len(tracker.results))
	}
	if tracker.results[0].CaseName != "test1" {
		t.Errorf("expected case name test1, got %s", tracker.results[0].CaseName)
	}
}

func TestEmuTrackerCaseNameExtraction(t *testing.T) {
	rules := &EmuRules{
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules, emuActive: true}
	tracker.processLine("Case is test_001")

	if tracker.currentCaseName != "test_001" {
		t.Errorf("expected case name test_001, got %s", tracker.currentCaseName)
	}
}

func TestEmuTrackerCaseResultClassification(t *testing.T) {
	rules := &EmuRules{
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case .* is completed`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{
		rules:           rules,
		emuActive:       true,
		caseActive:      true,
		currentCaseName: "test_001",
	}
	tracker.processLine("Case test_001 is completed")

	if tracker.caseActive {
		t.Error("expected case not active after end")
	}
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
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`Case .* is done`},
		CaseResultRules: []CaseRule{
			{Result: "low_priority", Keyword: "low", Priority: 10, Pattern: `Case .* is done`},
			{Result: "high_priority", Keyword: "high", Priority: 1, Pattern: `Case .* is done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{
		rules:           rules,
		emuActive:       true,
		caseActive:      true,
		currentCaseName: "test_001",
	}
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
		StartRules:     []string{`Emulation Start!`},
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseStartRules: []string{`Case .*, start to run`},
		CaseEndRules:   []string{`Case .* is completed`, `Case .* is failed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case .* is completed`},
			{Result: "fail", Keyword: "case_failed", Priority: 1, Pattern: `Case .* is failed`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}
	lines := []string{
		"Emulation Start!",
		"Case is test_001",
		"Case test_001, start to run",
		"Case test_001 is completed",
		"Case is test_002",
		"Case test_002, start to run",
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

func TestEmuTrackerImplicitCaseStart(t *testing.T) {
	rules := &EmuRules{
		EndRules:       []string{`Emulation End!`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case .* is completed`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules, emuActive: true}
	tracker.processLine("Case is test_001")

	if !tracker.caseActive {
		t.Error("expected case to start implicitly when no case_start_rules defined")
	}
}

func TestEmuTrackerIdleLinesSkipped(t *testing.T) {
	rules := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}
	tracker.processLine("some idle line")
	tracker.processLine("Case is test_001")
	tracker.processLine("done")

	if tracker.emuActive {
		t.Error("expected idle lines to be skipped before start")
	}
}

func TestEmuTrackerMultipleSessions(t *testing.T) {
	rules := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}

	// session 1
	tracker.processLine("START")
	tracker.processLine("Case is case1")
	tracker.processLine("done")
	tracker.processLine("END")

	results1 := tracker.flushResults()
	if len(results1) != 1 || results1[0].CaseName != "case1" {
		t.Errorf("unexpected session 1 results: %v", results1)
	}

	// session 2
	tracker.processLine("START")
	tracker.processLine("Case is case2")
	tracker.processLine("done")
	tracker.processLine("END")

	results2 := tracker.flushResults()
	if len(results2) != 1 || results2[0].CaseName != "case2" {
		t.Errorf("unexpected session 2 results: %v", results2)
	}
}

func TestEmuTrackerReset(t *testing.T) {
	rules := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
		},
	}
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}

	tracker := &emuTracker{rules: rules}
	tracker.processLine("START")
	tracker.processLine("Case is test1")
	tracker.caseActive = true

	tracker.reset()
	if tracker.emuActive {
		t.Error("expected emuActive false after reset")
	}
	if tracker.caseActive {
		t.Error("expected caseActive false after reset")
	}
	if tracker.currentCaseName != "" {
		t.Errorf("expected empty name after reset, got %s", tracker.currentCaseName)
	}
}

func TestIncrementalScannerScanEmu(t *testing.T) {
	content := "START\nCase is test_001\nCase test_001 is completed\nCase is test_002\nCase test_002 is failed\nEND\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`, `Case .* is failed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "case_passed", Priority: 1, Pattern: `Case .* is completed`},
			{Result: "fail", Keyword: "case_failed", Priority: 1, Pattern: `Case .* is failed`},
		},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	emuResults, scanResults, err := scanner.ScanEmu(ctx, rules, emuRules, nil)
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

func TestIncrementalScannerScanEmuStatePersistence(t *testing.T) {
	content := "START\nCase is test_001\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case .* is completed`},
		},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	emuResults, _, err := scanner.ScanEmu(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 0 {
		t.Fatalf("expected 0 emu results (case not yet ended), got %d", len(emuResults))
	}

	// append more content
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("Case test_001 is completed\nEND\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	emuResults, _, err = scanner.ScanEmu(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 1 {
		t.Fatalf("expected 1 emu result after case ended, got %d", len(emuResults))
	}
	if emuResults[0].CaseName != "test_001" || emuResults[0].Result != "pass" {
		t.Errorf("unexpected emu result: %+v", emuResults[0])
	}
}

func TestIncrementalScannerScanEmuTruncationReset(t *testing.T) {
	content := "START\nCase is test_001\nCase test_001 is completed\nCase is test_002\nCase test_002 is completed\nEND\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case .* is completed`},
		},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	emuResults, _, err := scanner.ScanEmu(ctx, rules, emuRules, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(emuResults) != 2 {
		t.Fatalf("expected 2 emu results, got %d", len(emuResults))
	}

	// truncate to shorter content
	if err := os.WriteFile(path, []byte("START\nCase is new_case\nCase new_case is completed\nEND\n"), 0644); err != nil {
		t.Fatal(err)
	}

	emuResults, _, err = scanner.ScanEmu(ctx, rules, emuRules, nil)
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

func TestIncrementalScannerScanEmuFileNotFound(t *testing.T) {
	scanner := NewIncrementalScanner("/nonexistent/path/emu.log")
	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
		},
	}

	ctx := context.Background()
	emuResults, scanResults, err := scanner.ScanEmu(ctx, rules, emuRules, nil)
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

func TestIncrementalScannerScanEmuRulesChanged(t *testing.T) {
	content := "START\nCase is test_001\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules1 := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case .* is completed`},
		},
	}
	emuRules2 := &EmuRules{
		StartRules:     []string{`START_V2`},
		EndRules:       []string{`END_V2`},
		CaseNameRules:  []string{`Case (\w+)`},
		CaseEndRules:   []string{`done`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `done`},
		},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	_, _, err := scanner.ScanEmu(ctx, rules, emuRules1, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = scanner.ScanEmu(ctx, rules, emuRules2, nil)
	if err == nil {
		t.Error("expected error when emu rules changed between calls")
	}
}

func TestIncrementalScannerScanEmuWithScanResults(t *testing.T) {
	content := "START\nERROR: something broke\nCase is test_001\nCase test_001 is completed\nEND\n"
	path := createTempLogFile(t, content)

	rules := []Rule{
		{Result: "error", Keyword: "ERROR", Detail: `ERROR:.*`, Priority: 1},
	}
	emuRules := &EmuRules{
		StartRules:     []string{`START`},
		EndRules:       []string{`END`},
		CaseNameRules:  []string{`Case is (\w+)`},
		CaseEndRules:   []string{`Case .* is completed`},
		CaseResultRules: []CaseRule{
			{Result: "pass", Keyword: "ok", Priority: 1, Pattern: `Case .* is completed`},
		},
	}

	scanner := NewIncrementalScanner(path)
	ctx := context.Background()

	emuResults, scanResults, err := scanner.ScanEmu(ctx, rules, emuRules, nil)
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
