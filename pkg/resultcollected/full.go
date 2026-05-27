package resultcollected

import (
	"context"
	"fmt"
)

// ScanFull 全量扫描日志文件，从文件开头完整匹配所有内容。
// 在所有命中结果中取优先级最高的单条返回；无匹配时返回 nil。
// 支持通过 context 控制超时以应对 NFS/IO 卡死场景。
func ScanFull(ctx context.Context, filePath string, rules []Rule, config *ScanConfig) (*ScanResult, error) {
	if err := compileRules(rules); err != nil {
		return nil, err
	}

	f, err := openFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", filePath, err)
	}
	defer f.Close()

	lines, offsets, err := readLines(ctx, f, 0)
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
		if results[i].Rule.Priority > best.Rule.Priority {
			best = &results[i]
		}
	}
	return best
}
