package resultcollected

import (
	"context"
	"fmt"
)

// ScanFull 全量扫描日志文件，从文件开头完整匹配所有内容。
// 支持通过 context 控制超时以应对 NFS/IO 卡死场景。
func ScanFull(ctx context.Context, filePath string, rules []Rule, config *ScanConfig) ([]ScanResult, error) {
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

	return scanLines(lines, offsets, rules, config), nil
}
