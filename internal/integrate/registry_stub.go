//go:build !windows

package integrate

import "errors"

// errNotWindows 用来把「平台不对」这件事说清楚，
// 而不是让调用方以为注册表写失败了。
var errNotWindows = errors.New("注册表 PATH 集成只能在 Windows 上使用")

// InstallWindowsPath 在非 Windows 平台上永远失败，见 registry_windows.go。
func InstallWindowsPath(binDir string) (bool, error) { return false, errNotWindows }

// RemoveWindowsPath 在非 Windows 平台上永远失败，见 registry_windows.go。
func RemoveWindowsPath(binDir string) (bool, error) { return false, errNotWindows }
