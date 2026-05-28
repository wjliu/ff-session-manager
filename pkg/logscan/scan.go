package logscan

import (
	"context"
	"fmt"
)

// Scan 一次性全量扫描日志文件，返回优先级最高的单条命中结果。
// 无匹配时返回 nil。设置 config.SafeIO=true 可启用 NFS/IO 卡死防护。
func Scan(ctx context.Context, filePath string, rules []Rule, config *ScanConfig) (*ScanResult, error) {
	if err := compileRules(rules); err != nil {
		return nil, err
	}

	safeIO := config != nil && config.SafeIO
	f, err := openFile(ctx, filePath, safeIO)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", filePath, err)
	}
	defer f.Close()

	lines, offsets, err := readLines(ctx, f, 0, safeIO)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", filePath, err)
	}

	results := scanLines(lines, offsets, rules, config)
	return pickHighestPriority(results), nil
}

// pickHighestPriority 从命中结果中选取优先级最高的单条结果。
// 优先级相同时取最先命中的。结果列表为空时返回 nil。
func pickHighestPriority(results []ScanResult) *ScanResult {
	if len(results) == 0 {
		return nil
	}
	best := &results[0]
	for i := 1; i < len(results); i++ {
		if results[i].Rule.Priority < best.Rule.Priority {
			best = &results[i]
		}
	}
	return best
}
