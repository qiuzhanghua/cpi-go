//go:build !windows

package integrate

import "errors"

// errNotWindowsShortcut 让「平台不对」和「COM 调用失败」两种原因在报错里分得开。
var errNotWindowsShortcut = errors.New("开始菜单快捷方式只能在 Windows 上创建")

// createShortcut 在非 Windows 平台上永远失败，见 shortcut_windows.go。
func createShortcut(lnkPath string, sc Shortcut) error { return errNotWindowsShortcut }

// shortcutTarget 在非 Windows 平台上永远失败，见 shortcut_windows.go。
func shortcutTarget(lnkPath string) (string, error) { return "", errNotWindowsShortcut }
