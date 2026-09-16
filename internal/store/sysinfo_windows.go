//go:build windows

package store

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// memoryStatusEx 对应 Windows MEMORYSTATUSEX 结构。
// 当前只使用 totalPhys，但 syscall 要求字段布局完整一致。
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

// detectTotalMemoryBytes 返回 Windows 主机物理内存。
// 返回 0 表示探测失败，调用方应使用安全 fallback。
func detectTotalMemoryBytes() uint64 {
	var status memoryStatusEx
	status.length = uint32(unsafe.Sizeof(status))
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return 0
	}
	return status.totalPhys
}

// detectDiskFreeBytes 返回 dir 所在卷的剩余字节数。
// 如果目录不存在会先创建，因为 Windows 卷查询需要真实路径，而启动阶段本来也会创建数据目录。
func detectDiskFreeBytes(dir string) uint64 {
	if dir == "" {
		dir = "data"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0
	}
	_ = os.MkdirAll(abs, 0o755)

	pathPtr, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return 0
	}
	var freeBytesAvailable uint64
	ret, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		0,
		0,
	)
	if ret == 0 {
		return 0
	}
	return freeBytesAvailable
}
