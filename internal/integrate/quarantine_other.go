//go:build !darwin

package integrate

// StripQuarantine 在 macOS 之外什么都不做。
//
// 下载标记（`com.apple.quarantine`）是 macOS LaunchServices 的概念：
// Linux 上没有对应物；Windows 的 MOTW 是 NTFS 备用数据流，由 SmartScreen
// 在启动时判，不是安装器能"摘掉"的东西（见 R1、R3）。
//
// 返回 nil 而不是报错：调用方不该为了这一条去判断平台。
func StripQuarantine(path string) error { return nil }
