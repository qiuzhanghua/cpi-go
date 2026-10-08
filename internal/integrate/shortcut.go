package integrate

// Shortcut 描述一个 Windows 快捷方式要去哪里、带什么参数。
//
// 这个类型放在平台无关的文件里，是为了让「该填什么」这件事在任何平台上都能被测到；
// 真正把它写成 .lnk 的代码在 shortcut_windows.go。
type Shortcut struct {
	Target      string // 被启动的 exe
	Arguments   string // 固定参数（通常为空，参数靠命令行透传）
	WorkingDir  string
	IconPath    string
	IconIndex   int
	Description string
}
