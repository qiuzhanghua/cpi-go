//go:build windows

package proc

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot   = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW            = kernel32.NewProc("Process32FirstW")
	procProcess32NextW             = kernel32.NewProc("Process32NextW")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	th32csSnapProcess              = 0x00000002
	invalidHandleValue             = ^uintptr(0)
	processQueryLimitedInformation = 0x1000
	maxPath                        = 32768
)

// processEntry32 就是 Win32 的 PROCESSENTRY32W。字段顺序、大小、对齐都是固定的，
// 可以直接当内存布局用（Toolhelp32 系列只认这个结构，没有版本字段的余地）。
type processEntry32 struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

// Find 用 Toolhelp32 把进程过一遍，再问内核要每个进程的完整可执行文件路径。
//
// 同样不起子进程：tasklist / wmic 的输出随系统语言变，而且
// 「下载来的程序悄悄枚举进程、还顺手拉起一个 cmd」正是杀软最讨厌的形状。
//
// 这里查的是**完整路径**而不是进程名：叫 ai-desk.exe 的东西可以有好几份，
// 只有路径能说明它是不是我们装在 lib/ 下的那一份。
func Find(dir string) ([]Process, error) {
	if dir == "" {
		return nil, nil
	}
	snap, _, callErr := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if snap == invalidHandleValue {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot 失败：%v", callErr)
	}
	defer syscall.CloseHandle(syscall.Handle(snap))

	var entry processEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&entry))); r == 0 {
		return nil, nil
	}
	var found []Process
	for {
		if exe, ok := imagePath(entry.ProcessID); ok {
			if p, ok := match(dir, int(entry.ProcessID), []string{exe}); ok {
				found = append(found, p)
			}
		}
		r, _, _ := procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&entry)))
		if r == 0 {
			break
		}
	}
	return found, nil
}

// imagePath 问内核要某个进程的可执行文件完整路径。
//
// 拿不到的（别的用户的、受保护的系统进程、刚退出的）直接放过 ——
// 我们只关心自己装的应用，而它的进程一定属于当前用户。
func imagePath(pid uint32) (string, bool) {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return "", false
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, maxPath)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf[:n]), true
}
