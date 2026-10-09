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

	"github.com/qiuzhanghua/gpm-go/internal/ledger"
)

const (
	markerBegin = "# >>> gpm >>>"
	markerEnd   = "# <<< gpm <<<"
)

// generatedMark 是所有由 gpm 写出来的文件都会带的一行。
//
// 它有两个用处：告诉用户别手改，以及让 gpm 认出「这个文件是我写的」。
// 后者是 FR-21 的启动器重名检查要用的：不是我们写的文件，绝不覆盖。
const generatedMark = "由 gpm 生成，请勿手工编辑。"

// Toolchain 是启动器要注入环境的一家工具链。
type Toolchain struct {
	Name string // cot / tdp
	Home string // 那一家自己的家目录（$COT_HOME / $TDP_HOME）
}

// Spec 描述一个终端启动器。
type Spec struct {
	BinDir     string      // <家>/bin
	Cmd        string      // 简称，也就是终端里敲的名字
	EntryAbs   string      // 入口绝对路径：.app 目录或可执行文件
	Kind       string      // bundle / exe
	GOOS       string      // darwin / linux / windows
	Mode       string      // activate（缺省）/ direct
	Toolchains []Toolchain // 要注入环境的工具链，来自清单的 requires
}

// LauncherPath 返回启动器该落在哪里。
//
// 用 filepath.Join 而不是写死分隔符：这个函数只在**目标机器上**被调用
// （goos 就是本机），本机风格的分隔符正是要的那个。
func LauncherPath(binDir, cmd, goos string) string {
	if goos == "windows" {
		return filepath.Join(binDir, cmd+".cmd")
	}
	return filepath.Join(binDir, cmd)
}

// OwnedByGpm 报告 path 处有没有文件、以及它是不是 gpm 写的。
//
// 「是不是 gpm 写的」只看那行生成标记：用户的 bin/ 里完全可能有别人装的
// 同名命令，认标记比认路径可靠。
func OwnedByGpm(path string) (exists, ours bool, err error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, strings.Contains(string(b), generatedMark), nil
}

// Launcher 写出终端启动器并返回它的路径。
//
// macOS 上应用是 .app，有两种接法：
//
//   - 需要注入工具链环境（requires 非空）或 mode=direct：直接 exec .app
//     内层的二进制。只有这条路能把环境交给应用进程 —— 实测 `open` 不传
//     环境（C1 / D33）。
//   - 没有工具链要注入、mode=activate（缺省）：仍然 `open`，保住 macOS 的
//     原有语义（与双击等价、能激活已经在跑的实例、Dock 行为正常）。
//
// Windows 上写 <cmd>.cmd（.CMD 默认在 PATHEXT 里，双击也能用）。
func Launcher(spec Spec) (string, error) {
	if err := os.MkdirAll(spec.BinDir, 0o755); err != nil {
		return "", err
	}
	p := LauncherPath(spec.BinDir, spec.Cmd, spec.GOOS)

	if spec.GOOS == "windows" {
		var b strings.Builder
		b.WriteString("@echo off\r\n")
		b.WriteString("rem " + generatedMark + "\r\n")
		writeCmdEnv(&b, spec.Toolchains)
		b.WriteString("\"" + spec.EntryAbs + "\" %*\r\n")
		return p, writeFile(p, b.String(), 0o755)
	}

	var b strings.Builder
	if spec.Kind == "bundle" && spec.GOOS == "darwin" && spec.Mode != "direct" && len(spec.Toolchains) == 0 {
		b.WriteString(header("与双击图标等价，能激活已经运行的实例。"))
		b.WriteString("exec open " + shq(spec.EntryAbs) + " --args \"$@\"\n")
		return p, writeFile(p, b.String(), 0o755)
	}

	target := spec.EntryAbs
	comment := "直接运行。"
	if spec.Kind == "bundle" && spec.GOOS == "darwin" {
		inner, err := BundleExecutable(spec.EntryAbs)
		if err != nil {
			return "", err
		}
		target = inner
		comment = "直接运行 .app 内层二进制：只有这条路能把工具链环境交给应用进程。"
	}
	b.WriteString(header(comment))
	b.WriteString(shEnv(spec.Toolchains))
	b.WriteString("exec " + shq(target) + " \"$@\"\n")
	return p, writeFile(p, b.String(), 0o755)
}

// shEnv 生成 POSIX sh 的环境注入：把工具链的家写进环境、把它的 bin 放进
// PATH，再 source 它自己的 env-<name>.vars。
//
// 那个文件里只有 `export NAME=...` 这类变量赋值，没有插件命令，所以直接
// source 是安全的（它正是给 shell 用的）。
func shEnv(tcs []Toolchain) string {
	var b strings.Builder
	for _, t := range tcs {
		if t.Home == "" {
			continue
		}
		name := strings.ToUpper(t.Name)
		bin := filepath.Join(t.Home, "bin")
		b.WriteString(name + "_HOME=" + shq(t.Home) + "; export " + name + "_HOME\n")
		b.WriteString("PATH=" + shq(bin) + ":$PATH; export PATH\n")
		vars := filepath.Join(bin, "env-"+t.Name+".vars")
		b.WriteString("if [ -f " + shq(vars) + " ]; then . " + shq(vars) + "; fi\n")
	}
	return b.String()
}

// writeCmdEnv 生成 cmd.exe 的环境注入。
func writeCmdEnv(b *strings.Builder, tcs []Toolchain) {
	for _, t := range tcs {
		if t.Home == "" {
			continue
		}
		name := strings.ToUpper(t.Name)
		bin := t.Home + `\bin`
		b.WriteString("set \"" + name + "_HOME=" + t.Home + "\"\r\n")
		b.WriteString("set \"PATH=" + bin + ";%PATH%\"\r\n")
		b.WriteString("if exist \"" + bin + "\\env-" + t.Name + ".bat\" call \"" + bin + "\\env-" + t.Name + ".bat\"\r\n")
	}
}

func header(comment string) string {
	return "#!/bin/sh\n# " + generatedMark + "\n# " + comment + "\n"
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
