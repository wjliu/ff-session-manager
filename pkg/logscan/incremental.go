package logscan

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// IncrementalScanner 增量扫描器，每次调用从上次结束位置继续扫描。
// 确保日志内容不会遗漏。默认走直接 I/O 快速路径；如需 NFS/IO 卡死防护，
// 使用 NewSafeIncrementalScanner 创建。
//
// 并发安全: Scan 方法不可并发调用，但 Reset 和 Offset 可在任意时候调用。
type IncrementalScanner struct {
	mu         sync.Mutex
	path       string
	offset     int64
	safeIO     bool
	emuTracker *emuTracker
}

// NewIncrementalScanner 创建一个增量扫描器，走直接 I/O 快速路径。
// 初始偏移为 0，即从文件开头开始扫描。
func NewIncrementalScanner(filePath string) *IncrementalScanner {
	return &IncrementalScanner{path: filePath}
}

// NewSafeIncrementalScanner 创建一个启用 NFS/IO 卡死防护的增量扫描器。
// 所有 I/O 操作通过 goroutine+channel 包装以响应 context 取消，但有额外调度开销。
func NewSafeIncrementalScanner(filePath string) *IncrementalScanner {
	return &IncrementalScanner{path: filePath, safeIO: true}
}

// Scan 从上次扫描结束位置继续扫描文件。
// 每次调用返回新出现的匹配结果，并更新内部偏移。
// 如果文件未发生变化或没有新匹配，返回空切片。
func (s *IncrementalScanner) Scan(ctx context.Context, rules []Rule, config *ScanConfig) ([]ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := compileRules(rules); err != nil {
		return nil, err
	}

	f, err := openFile(ctx, s.path, s.safeIO)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open file %s: %w", s.path, err)
	}
	defer f.Close()

	// 检查文件是否被截断
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat file %s: %w", s.path, err)
	}
	if fi.Size() < s.offset {
		s.offset = 0
	}

	if err := seekFile(ctx, f, s.offset, s.safeIO); err != nil {
		return nil, fmt.Errorf("seek file %s: %w", s.path, err)
	}

	lines, offsets, err := readLines(ctx, f, s.offset, s.safeIO)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", s.path, err)
	}

	if len(lines) == 0 {
		return nil, nil
	}

	results := scanLines(lines, offsets, rules, config)

	// 更新偏移到文件末尾之后
	if len(offsets) > 0 {
		lastLine := lines[len(lines)-1]
		s.offset = offsets[len(offsets)-1] + int64(len(lastLine)) + 1
	}

	return results, nil
}

// Reset 重置扫描位置到文件开头，同时重置仿真状态（如有）。
func (s *IncrementalScanner) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset = 0
	if s.emuTracker != nil {
		s.emuTracker.reset()
	}
}

// Offset 返回当前扫描偏移量。
func (s *IncrementalScanner) Offset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offset
}

// ScanEmu 执行增量扫描并同时进行仿真状态追踪。
// 返回仿真 case 结果列表和常规扫描结果列表。
// 首次调用时使用给定的 emuRules 初始化 emulation 追踪器，后续调用 emuRules 不可变更。
func (s *IncrementalScanner) ScanEmu(ctx context.Context, rules []Rule, emuRules *EmuRules, config *ScanConfig) ([]EmuCaseResult, []ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := compileRules(rules); err != nil {
		return nil, nil, err
	}

	if s.emuTracker == nil {
		if err := emuRules.compile(); err != nil {
			return nil, nil, err
		}
		s.emuTracker = &emuTracker{rules: emuRules}
	} else if s.emuTracker.rules != emuRules {
		return nil, nil, fmt.Errorf("emu rules changed between ScanEmu calls")
	}

	f, err := openFile(ctx, s.path, s.safeIO)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("open file %s: %w", s.path, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat file %s: %w", s.path, err)
	}
	if fi.Size() < s.offset {
		s.offset = 0
		s.emuTracker.reset()
	}

	if err := seekFile(ctx, f, s.offset, s.safeIO); err != nil {
		return nil, nil, fmt.Errorf("seek file %s: %w", s.path, err)
	}

	lines, offsets, err := readLines(ctx, f, s.offset, s.safeIO)
	if err != nil {
		return nil, nil, fmt.Errorf("read file %s: %w", s.path, err)
	}

	if len(lines) == 0 {
		return nil, nil, nil
	}

	for _, line := range lines {
		s.emuTracker.processLine(line)
	}

	scanResults := scanLines(lines, offsets, rules, config)
	emuResults := s.emuTracker.flushResults()

	if len(offsets) > 0 {
		lastLine := lines[len(lines)-1]
		s.offset = offsets[len(offsets)-1] + int64(len(lastLine)) + 1
	}

	return emuResults, scanResults, nil
}
