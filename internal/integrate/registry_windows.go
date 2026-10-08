//go:build windows

package integrate

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Windows 的用户 PATH 在注册表里，不在任何文件里。这里全部走原生调用，原因有两个：
//
//  1. Go 的 syscall 包只导出了 RegOpenKeyEx / RegQueryValueEx / RegCloseKey，
//     并没有 RegSetValueEx，写值只能自己 NewProc。
//  2. 只有自己写才保得住原来的值类型。PowerShell 的
//     [Environment]::SetEnvironmentVariable(..., "User") 有个老毛病：
//     会把 REG_EXPAND_SZ 写成 REG_SZ，用户原有的 %USERPROFILE% 之类就再也展开不了了。
//     我们读出什么类型就写回什么类型。
//
// 附带好处：不依赖任何第三方包，也不起子进程 ——
// 「下载来的程序悄悄改注册表还拉起 PowerShell」正是杀软最讨厌的形状。
var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procRegOpenKeyExW       = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW    = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW      = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey         = advapi32.NewProc("RegCloseKey")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
)

const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002

	regSZ       = 1
	regExpandSZ = 2

	errFileNotFound = 2

	hwndBroadcast   = 0xFFFF
	wmSettingChange = 0x001A
	smtoAbortIfHung = 0x0002
)

func openEnvironmentKey() (syscall.Handle, error) {
	sub, err := syscall.UTF16FromString("Environment")
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(&sub[0])),
		0,
		keyQueryValue|keySetValue,
		uintptr(unsafe.Pointer(&h)),
	)
	if r != 0 {
		return 0, fmt.Errorf("打不开 HKCU\\Environment（Windows 错误码 %d）", r)
	}
	return h, nil
}

func readEnvironmentPath(h syscall.Handle) (string, uint32, error) {
	name, err := syscall.UTF16FromString("Path")
	if err != nil {
		return "", 0, err
	}
	var typ, size uint32
	r, _, _ := procRegQueryValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&name[0])),
		0,
		uintptr(unsafe.Pointer(&typ)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	switch {
	case r == errFileNotFound:
		// 值不存在不是错误：第一次装东西的用户就是没有这个值。
		return "", regExpandSZ, nil
	case r != 0:
		return "", 0, fmt.Errorf("读 HKCU\\Environment 的 Path 失败（Windows 错误码 %d）", r)
	}

	buf := make([]uint16, size/2+2)
	r, _, _ = procRegQueryValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&name[0])),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r != 0 {
		return "", 0, fmt.Errorf("读 HKCU\\Environment 的 Path 失败（Windows 错误码 %d）", r)
	}
	return syscall.UTF16ToString(buf), typ, nil
}

func writeEnvironmentPath(h syscall.Handle, value string, typ uint32) error {
	name, err := syscall.UTF16FromString("Path")
	if err != nil {
		return err
	}
	// 未知类型一律当 REG_EXPAND_SZ：它能表示 REG_SZ 的全部内容，反过来不行。
	if typ != regSZ && typ != regExpandSZ {
		typ = regExpandSZ
	}
	v, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	r, _, _ := procRegSetValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&name[0])),
		0,
		uintptr(typ),
		uintptr(unsafe.Pointer(&v[0])),
		uintptr(len(v)*2), // 含结尾的 NUL，REG_SZ 系列要求这样
	)
	if r != 0 {
		return fmt.Errorf("写 HKCU\\Environment 的 Path 失败（Windows 错误码 %d）", r)
	}
	return nil
}

// broadcastEnvironmentChange 广播 WM_SETTINGCHANGE。
//
// 不广播的话，已经开着的资源管理器与新开的 cmd 拿到的仍是旧环境；
// 这一步是「装完就能用」与「得重启一下」的区别。
func broadcastEnvironmentChange() {
	s, err := syscall.UTF16FromString("Environment")
	if err != nil {
		return
	}
	procSendMessageTimeoutW.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(&s[0])),
		smtoAbortIfHung,
		5000,
		0,
	)
}

// InstallWindowsPath 把 binDir 插到 Windows 用户 PATH 最前面，保持原有的值类型。
// 第二个返回值表示是否真的改动了（已经加过就返回 false，不重复写）。
func InstallWindowsPath(binDir string) (bool, error) {
	h, err := openEnvironmentKey()
	if err != nil {
		return false, err
	}
	defer procRegCloseKey.Call(uintptr(h))

	old, typ, err := readEnvironmentPath(h)
	if err != nil {
		return false, err
	}
	value, changed := WindowsPathValue(old, binDir)
	if !changed {
		return false, nil
	}
	if err := writeEnvironmentPath(h, value, typ); err != nil {
		return false, err
	}
	broadcastEnvironmentChange()
	return true, nil
}

// RemoveWindowsPath 把 binDir 从 Windows 用户 PATH 里摘掉，同样保持类型、同样广播。
func RemoveWindowsPath(binDir string) (bool, error) {
	h, err := openEnvironmentKey()
	if err != nil {
		return false, err
	}
	defer procRegCloseKey.Call(uintptr(h))

	old, typ, err := readEnvironmentPath(h)
	if err != nil {
		return false, err
	}
	value, changed := WindowsPathRemove(old, binDir)
	if !changed {
		return false, nil
	}
	if err := writeEnvironmentPath(h, value, typ); err != nil {
		return false, err
	}
	broadcastEnvironmentChange()
	return true, nil
}
