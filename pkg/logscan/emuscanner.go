package logscan

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// EmuScanner 仿真日志扫描器，每次调用从上次结束位置继续扫描并追踪 emulation 状态。
// 默认走直接 I/O 快速路径；如需 NFS/IO 卡死防护，使用 NewSafeEmuScanner 创建。
//
// 并发安全: Scan 方法不可并发调用，但 Reset 和 Offset 可在任意时候调用。
type EmuScanner struct {
	mu         sync.Mutex
	path       string
	offset     int64
	safeIO     bool
	emuTracker *emuTracker
}

// NewEmuScanner 创建一个仿真日志扫描器，走直接 I/O 快速路径。
// 初始偏移为 0，即从文件开头开始扫描。
func NewEmuScanner(filePath string) *EmuScanner {
	return &EmuScanner{path: filePath}
}

// NewSafeEmuScanner 创建一个启用 NFS/IO 卡死防护的仿真日志扫描器。
// 所有 I/O 操作通过 goroutine+channel 包装以响应 context 取消，但有额外调度开销。
func NewSafeEmuScanner(filePath string) *EmuScanner {
	return &EmuScanner{path: filePath, safeIO: true}
}

// NewEmuScannerAtOffset 从指定字节偏移开始扫描，用于恢复上次扫描进度。
// offset 通常来自上次 EmuScanner.Offset() 的返回值。
// 若文件大小小于给定 offset（如文件被截断/重置），首次 Scan 时会自动归零并重置状态机。
func NewEmuScannerAtOffset(filePath string, offset int64) *EmuScanner {
	return &EmuScanner{path: filePath, offset: offset}
}

// Scan 从上次扫描结束位置继续扫描文件，并进行仿真状态追踪。
// 返回仿真 case 结果列表和状态机状态。
// 首次调用时使用给定的 emuRules 初始化追踪器，后续调用沿用已有追踪器（emuRules 可传 nil）。
// 如果文件未发生变化或没有新匹配，返回空切片。
func (s *EmuScanner) Scan(ctx context.Context, emuRules *EmuRules) ([]EmuCaseResult, EmuScanState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.emuTracker == nil {
		if emuRules == nil {
			return nil, EmuScanState{}, fmt.Errorf("emuRules must not be nil on first Scan call")
		}
		if err := emuRules.compile(); err != nil {
			return nil, EmuScanState{}, err
		}
		s.emuTracker = &emuTracker{rules: emuRules}
	}

	f, err := openFile(ctx, s.path, s.safeIO)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, EmuScanState{}, nil
		}
		return nil, EmuScanState{}, fmt.Errorf("open file %s: %w", s.path, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, EmuScanState{}, fmt.Errorf("stat file %s: %w", s.path, err)
	}
	if fi.Size() < s.offset {
		s.offset = 0
		s.emuTracker.reset()
	}

	if err := seekFile(ctx, f, s.offset, s.safeIO); err != nil {
		return nil, EmuScanState{}, fmt.Errorf("seek file %s: %w", s.path, err)
	}

	lines, offsets, err := readLines(ctx, f, s.offset, s.safeIO)
	if err != nil {
		return nil, EmuScanState{}, fmt.Errorf("read file %s: %w", s.path, err)
	}

	if len(lines) == 0 {
		return nil, s.emuTracker.flushState(), nil
	}

	for _, line := range lines {
		s.emuTracker.processLine(line)
	}

	emuResults := s.emuTracker.flushResults()
	state := s.emuTracker.flushState()

	if len(offsets) > 0 {
		lastLine := lines[len(lines)-1]
		s.offset = offsets[len(offsets)-1] + int64(len(lastLine)) + 1
	}

	return emuResults, state, nil
}

// Reset 重置扫描位置到文件开头，同时重置仿真状态。
func (s *EmuScanner) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset = 0
	if s.emuTracker != nil {
		s.emuTracker.reset()
	}
}

// Offset 返回当前扫描偏移量。
func (s *EmuScanner) Offset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offset
}
