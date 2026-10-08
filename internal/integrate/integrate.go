// Package integrate 把装好的应用接进系统：终端启动器、图形入口、PATH 标记块。
//
// 这里写下的每一样东西都是「外部副作用」，调用方必须把结果登记进账本，
// 否则卸载就回放不出来。
package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiuzhanghua/cpi-go/internal/ledger"
)

const (
	markerBegin = "# >>> cpi >>>"
	markerEnd   = "# <<< cpi <<<"
)

// Launcher 写出终端启动器并返回它的路径。
//
//   - macOS + bundle + activate：exec open "<app>" --args "$@"，与双击等价
//   - macOS + bundle + direct：直接跑 .app 内层二进制，拿得到 stdout 与退出码
//   - 其他：直接 exec 入口
//   - Windows：<cmd>.cmd 包装（.CMD 默认在 PATHEXT 里，双击也能用）
func Launcher(binDir, cmd, entryAbs, kind, goos, mode string) (string, error) {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}
	if goos == "windows" {
		p := filepath.Join(binDir, cmd+".cmd")
		body := "@echo off\r\nrem 由 cpi 生成，请勿手工编辑。\r\n\"" + entryAbs + "\" %*\r\n"
		return p, writeFile(p, body, 0o755)
	}

	var body string
	switch {
	case kind == "bundle" && goos == "darwin" && mode == "direct":
		inner, err := BundleExecutable(entryAbs)
		if err != nil {
			return "", err
		}
		body = header("直接运行 .app 内层二进制：拿得到 stdout、退出码与 Ctrl+C。") +
			"exec " + shq(inner) + " \"$@\"\n"
	case kind == "bundle" && goos == "darwin":
		body = header("与双击图标等价，能激活已经运行的实例。") +
			"exec open " + shq(entryAbs) + " --args \"$@\"\n"
	default:
		body = header("直接运行。") +
			"exec " + shq(entryAbs) + " \"$@\"\n"
	}
	p := filepath.Join(binDir, cmd)
	return p, writeFile(p, body, 0o755)
}

func header(comment string) string {
	return "#!/bin/sh\n# 由 cpi 生成，请勿手工编辑。\n# " + comment + "\n"
}

// BundleExecutable 从 Info.plist 里读出 CFBundleExecutable 并拼出内层二进制路径。
func BundleExecutable(appDir string) (string, error) {
	plist := filepath.Join(appDir, "Contents", "Info.plist")
	b, err := os.ReadFile(plist)
	if err != nil {
		return "", fmt.Errorf("读不到 %s: %w", plist, err)
	}
	name := plistString(string(b), "CFBundleExecutable")
	if name == "" {
		return "", fmt.Errorf("%s 里没有 CFBundleExecutable", plist)
	}
	p := filepath.Join(appDir, "Contents", "MacOS", name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("Info.plist 指向的内层可执行文件不存在：%s", p)
	}
	return p, nil
}

func plistString(doc, key string) string {
	open := "<key>" + key + "</key>"
	i := strings.Index(doc, open)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(open):]
	j := strings.Index(rest, "<string>")
	if j < 0 {
		return ""
	}
	rest = rest[j+len("<string>"):]
	k := strings.Index(rest, "</string>")
	if k < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:k])
}

// AppLink 建立图形入口。返回的 note 非空时是需要告诉用户的提示（通常是同名占用）。
func AppLink(goos, homeDir, id, name, entryAbs, launcher string) (link *ledger.Link, note string, err error) {
	switch goos {
	case "darwin":
		target := MacAppLinkPath(homeDir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, "", err
		}
		if _, err := os.Lstat(target); err == nil {
			return nil, fmt.Sprintf("提示：%s 已存在，没有覆盖它，启动台里可能看到的是旧的那个。", target), nil
		}
		if err := os.Symlink(entryAbs, target); err != nil {
			return nil, "", err
		}
		return &ledger.Link{Path: target, Target: entryAbs, Kind: "app-symlink"}, "", nil

	case "linux":
		p := DesktopEntryPath(homeDir, id)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, "", err
		}
		if err := writeFile(p, DesktopEntry(name, launcher, ""), 0o644); err != nil {
			return nil, "", err
		}
		refreshDesktopDatabase(filepath.Dir(p))
		return &ledger.Link{Path: p, Target: launcher, Kind: "desktop-entry"}, "", nil

	case "windows":
		return windowsStartMenuShortcut(name, entryAbs)
	}
	return nil, "", nil
}

// windowsStartMenuShortcut 在开始菜单里放一个 .lnk。
//
// 失败一律降级成「提示」而不是报错：快捷方式建不出来，
// 软件本身照样装好、终端里照样能用，没理由为此把整次安装判失败。
func windowsStartMenuShortcut(name, entryAbs string) (*ledger.Link, string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil, "提示：环境里没有 APPDATA，跳过开始菜单快捷方式。", nil
	}
	p := WindowsShortcutPath(appData, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, "", err
	}
	if _, err := os.Lstat(p); err == nil {
		return nil, fmt.Sprintf("提示：%s 已存在，没有覆盖它。", p), nil
	}
	sc := Shortcut{
		Target:      entryAbs,
		WorkingDir:  filepath.Dir(entryAbs),
		Description: name,
	}
	if err := createShortcut(p, sc); err != nil {
		os.Remove(p) // 别留半个文件让下次「已存在」判定误伤
		return nil, fmt.Sprintf("提示：没能创建开始菜单快捷方式（%v）；软件已装好，可以直接运行 %s。", err, entryAbs), nil
	}
	return &ledger.Link{Path: p, Target: entryAbs, Kind: "start-menu-lnk"}, "", nil
}

// ShellProfiles 按 $SHELL 与平台算出该写哪几个 shell 配置文件。
//
// macOS 自 Catalina 起默认 shell 是 zsh，而 zsh 不读 ~/.profile，
// 所以不能只写 .profile；不论 $SHELL 都再写一份 .profile 兜底。
func ShellProfiles(goos, homeDir string) []string {
	if goos == "windows" {
		return nil
	}
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell == "" {
		if goos == "darwin" {
			shell = "zsh"
		} else {
			shell = "bash"
		}
	}

	var out []string
	if shell == "fish" {
		return []string{filepath.Join(homeDir, ".config", "fish", "config.fish")}
	}
	switch shell {
	case "zsh":
		out = append(out, filepath.Join(homeDir, ".zprofile"))
		out = append(out, filepath.Join(homeDir, ".zshrc"))
	case "bash":
		if goos == "darwin" {
			out = append(out, filepath.Join(homeDir, ".bash_profile"))
		} else {
			out = append(out, filepath.Join(homeDir, ".bashrc"))
		}
	}
	out = append(out, filepath.Join(homeDir, ".profile"))

	seen := map[string]bool{}
	uniq := out[:0]
	for _, f := range out {
		if !seen[f] {
			seen[f] = true
			uniq = append(uniq, f)
		}
	}
	return uniq
}

// PathBlock 返回追加到 shell 配置里的标记块（含换行）。
func PathBlock(file, binDir, homeDir string) string {
	p := homeRelative(binDir, homeDir)
	var body string
	if strings.HasSuffix(filepath.ToSlash(file), "/config.fish") {
		body = "contains " + p + " $PATH; or set -gx PATH " + p + " $PATH\n"
	} else {
		body = "case \":$PATH:\" in\n" +
			"  *\":" + p + ":\"*) ;;\n" +
			"  *) PATH=\"" + p + ":$PATH\" ;;\n" +
			"esac\nexport PATH\n"
	}
	return markerBegin + "\n" + body + markerEnd + "\n"
}

// InstallPathBlock 把标记块写进每个文件（幂等：先摘掉旧的再追加）。
func InstallPathBlock(files []string, binDir, homeDir string) ([]ledger.PathEdit, error) {
	var edits []ledger.PathEdit
	for _, f := range files {
		old, err := os.ReadFile(f)
		existed := err == nil
		if err != nil && !os.IsNotExist(err) {
			return edits, err
		}
		base := strings.TrimRight(stripBlock(string(old)), "\n")
		content := base
		if content != "" {
			content += "\n"
		}
		content += PathBlock(f, binDir, homeDir)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			return edits, err
		}
		if err := writeFile(f, content, 0o644); err != nil {
			return edits, err
		}
		edits = append(edits, ledger.PathEdit{Path: f, Created: !existed})
	}
	return edits, nil
}

// RemovePathBlock 摘掉标记块，返回实际改动过的文件。
func RemovePathBlock(files []string) ([]string, error) {
	var changed []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return changed, err
		}
		s := string(b)
		if !strings.Contains(s, markerBegin) {
			continue
		}
		if err := writeFile(f, strings.TrimRight(stripBlock(s), "\n"), 0o644); err != nil {
			return changed, err
		}
		changed = append(changed, f)
	}
	return changed, nil
}

// stripBlock 摘掉（可能存在的）标记块。
func stripBlock(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, ln := range lines {
		t := strings.TrimSpace(strings.TrimRight(ln, "\r"))
		switch {
		case t == markerBegin:
			skip = true
		case skip && t == markerEnd:
			skip = false
		case skip:
		default:
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

func homeRelative(p, homeDir string) string {
	p = filepath.ToSlash(p)
	if homeDir == "" {
		return p
	}
	// 两边都先归一成斜杠再比前缀：不这样做的话，只要 homeDir 与 p 的分隔符
	// 写法不一致（Windows 上很容易），前缀就匹配不上，绝对路径会直接写进
	// 用户的 shell 配置里。
	home := strings.TrimRight(filepath.ToSlash(homeDir), "/")
	if home == "" {
		return p
	}
	if strings.HasPrefix(p, home+"/") {
		return "$HOME/" + strings.TrimPrefix(p, home+"/")
	}
	return p
}

// shq 把字符串安全地放进 sh 的单引号里。
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func writeFile(p, body string, perm os.FileMode) error {
	if err := os.WriteFile(p, []byte(body), perm); err != nil {
		return err
	}
	return os.Chmod(p, perm)
}
