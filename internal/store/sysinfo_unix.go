//go:build !windows

package store

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// detectTotalMemoryBytes 在可用时读取 Linux /proc/meminfo。
// 其他 Unix-like 系统可能返回 0，这是可接受的，因为调参层对未知内存有保守 fallback。
func detectTotalMemoryBytes() uint64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kibValue, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kibValue * kib
	}
	return 0
}

// detectDiskFreeBytes 返回 dir 所在文件系统的剩余字节数。
// 新部署时数据路径可能不存在，因此先创建目录；RocksDB 启动时本来也会创建它。
func detectDiskFreeBytes(dir string) uint64 {
	if dir == "" {
		dir = "data"
	}
	_ = os.MkdirAll(dir, 0o755)

	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0
	}
	return stat.Bavail * uint64(stat.Bsize)
}
