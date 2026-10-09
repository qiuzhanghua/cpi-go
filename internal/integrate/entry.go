package integrate

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// refreshDesktopDatabase 让桌面环境立刻认出刚放进去的 .desktop。
//
// 多数桌面环境自己会扫这个目录，所以这一步只是「让图标马上出现」。
// 命令不存在（没装 desktop-file-utils 的发行版）就静默跳过 ——
// 不能因为缺一个可选的辅助命令就让整个安装失败。
func refreshDesktopDatabase(dir string) {
	p, err := exec.LookPath("update-desktop-database")
	if err != nil {
		return
	}
	cmd := exec.Command(p, dir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

// MacAppLinkPath 返回 macOS 上图形入口的位置：~/Applications/<名字>.app。
func MacAppLinkPath(homeDir, name string) string {
	return filepath.Join(homeDir, "Applications", name+".app")
}

// LinuxDataHome 返回 Linux 上的 XDG 数据目录。
//
// 环境里设了 XDG_DATA_HOME 就得听它的：忽略它会把 .desktop 写进
// 用户根本没在看的目录，表现就是「装好了但应用列表里没有」。
func LinuxDataHome(homeDir string) string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir, ".local", "share")
}

// DesktopEntryPath 返回 Linux 图形入口的 .desktop 路径。
func DesktopEntryPath(homeDir, id string) string {
	return filepath.Join(LinuxDataHome(homeDir), "applications", id+".desktop")
}

// WindowsStartMenuDir 返回 Windows 当前用户「开始菜单 → 程序」下的 gpm 目录。
//
// 用 %APPDATA% 而不是自己拼 %USERPROFILE%：前者才是漫游配置的真实位置，
// 硬拼路径在域环境或改过配置的机器上会写到一个没人看的角落。
func WindowsStartMenuDir(appData string) string {
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "gpm")
}

// WindowsShortcutPath 返回开始菜单快捷方式（.lnk）的完整路径。
func WindowsShortcutPath(appData, name string) string {
	return filepath.Join(WindowsStartMenuDir(appData), name+".lnk")
}

// DesktopEntry 生成 .desktop 文件的内容。
func DesktopEntry(name, launcher, icon string) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Version=1.0\n")
	b.WriteString("Name=" + name + "\n")
	b.WriteString("Exec=" + desktopQuote(launcher) + "\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("Categories=Utility;\n")
	b.WriteString("StartupNotify=true\n")
	if icon != "" {
		b.WriteString("Icon=" + icon + "\n")
	}
	return b.String()
}

// desktopQuote 按 Desktop Entry 规范给 Exec 里的字段加引号。
//
// 规范只认这四种转义：双引号、反引号、美元符、反斜杠。
// 它跟 shell 的引号规则不是一回事，不能拿 shell 的来凑 ——
// 这一点错了，桌面项就会静默地启动失败，终端里连报错都看不到。
func desktopQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
