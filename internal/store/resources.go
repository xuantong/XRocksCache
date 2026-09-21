package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// constrainResources 同时检查当前 cgroup 及祖先限制；裸机和非 Linux 环境保持探测值。
// 同时兼容统一层级与常见的 v1 独立控制器挂载。
func constrainResources(memory uint64, cpus int) (uint64, int) {
	roots := []string{"/sys/fs/cgroup", "/sys/fs/cgroup/memory", "/sys/fs/cgroup/cpu"}
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) != 3 {
				continue
			}
			path := filepath.Clean("/" + parts[2])
			if parts[1] == "" {
				roots = append(roots, filepath.Join("/sys/fs/cgroup", path))
			}
			for _, controller := range strings.Split(parts[1], ",") {
				if controller == "memory" || controller == "cpu" {
					roots = append(roots, filepath.Join("/sys/fs/cgroup", controller, path))
				}
			}
		}
	}
	for _, root := range roots {
		for dir := root; strings.HasPrefix(dir, "/sys/fs/cgroup"); dir = filepath.Dir(dir) {
			for _, name := range []string{"memory.max", "memory.limit_in_bytes"} {
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					continue
				}
				value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
				if err == nil && value > 0 && (memory == 0 || value < memory) {
					memory = value
				}
			}
			quota, period := int64(0), int64(0)
			if data, err := os.ReadFile(filepath.Join(dir, "cpu.max")); err == nil {
				fields := strings.Fields(string(data))
				if len(fields) == 2 {
					quota, _ = strconv.ParseInt(fields[0], 10, 64)
					period, _ = strconv.ParseInt(fields[1], 10, 64)
				}
			} else {
				q, _ := os.ReadFile(filepath.Join(dir, "cpu.cfs_quota_us"))
				p, _ := os.ReadFile(filepath.Join(dir, "cpu.cfs_period_us"))
				quota, _ = strconv.ParseInt(strings.TrimSpace(string(q)), 10, 64)
				period, _ = strconv.ParseInt(strings.TrimSpace(string(p)), 10, 64)
			}
			if quota > 0 && period > 0 {
				limit := int((quota + period - 1) / period)
				if limit < cpus {
					cpus = limit
				}
			}
		}
	}
	return memory, cpus
}
