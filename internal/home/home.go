// Package home 解析并描述 gpm 的家目录。
//
// 家目录不由 gpm 自己发明：包里的 install.sh/install.cmd 用 --dir 把它传
// 过来（那个值由 `gpm pack --default-dir` 或清单里的 `requires` 决定）。
// 一个都没给时，gpm 按"证据有多具体"往下找：
//
//	--dir                    ← 调用方明说
//	requires 指出的那个家     ← 清单明说（$COT_HOME / $TDP_HOME，缺省 ~/cot、~/tdp）
//	从 gpm 自己的位置推断     ← 布局明说（见 RootFromSelf）
//	平台数据目录/<简称>       ← 谁都没说、也没有工具链：装到 ~/.local/share/ad 这种地方
//	当前目录                 ← 实在没辙
//
// v3.5 起没有 `GPM_HOME`：这个家就是工具链自己的家，gpm 的内部结构
// （bin/、lib/、账本、staging/）与 cot / tdp 的东西住在同一个目录里。
// 因此「卸载 = 删掉一个目录」不再成立：卸载只按账本回放（D34）。
//
// v3.7 起账本按家命名：`<家>/<家目录名>-state.json`（`~/cot/cot-state.json`），
// 不再叫那个放在谁家都通用的 `state.json`。
package home

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// selfExecutable 是 os.Executable 的接缝，测试会替换它。
var selfExecutable = os.Executable

// userHome 是 os.UserHomeDir 的接缝，测试会替换它。
var userHome = os.UserHomeDir

// getenv 是 os.Getenv 的接缝，测试会替换它。
var getenv = os.Getenv

// Home 是 gpm 的家目录。
type Home struct {
	Root   string // 绝对路径
	GOOS   string
	GOARCH string
}

// Resolve 给"装完之后"的命令（list / where / uninstall / env）定家。
//
// 优先级：--dir > RootFromSelf() > $COT_HOME > $TDP_HOME > 当前目录。
//
// 这里把"自己住在哪个家里"排在环境变量前面，是因为它是**更具体的证据**：
// 用户敲的是 `<某个家>/bin/gpm list`，那他要看的就是那个家。环境变量只是
// 提示，而且很可能是另一个家留下的（比如 shell 里激活着 cot，人却在看
// 一个不需要工具链的应用）。都推不出来时落到当前目录，骨架就落在
// ./bin、./lib、./<目录名>-state.json。
func Resolve(dir string) (*Home, error) {
	root := dir
	if root == "" {
		if self, ok := RootFromSelf(); ok {
			root = self
		}
	}
	if root == "" {
		for _, name := range []string{"COT_HOME", "TDP_HOME"} {
			if v := strings.TrimSpace(getenv(name)); v != "" {
				root = v
				break
			}
		}
	}
	if root == "" {
		root = "."
	}
	return newHome(root)
}

// ResolveInstall 给一次安装定家：`requires` 决定装到哪个工具链的家，
// 没有 `requires` 时按简称装进平台的数据目录。
//
// 优先级：--dir > requires 指出的家 > RootFromSelf() > 平台数据目录/<简称>
// > 当前目录。
func ResolveInstall(dir, short string, requires []string) (*Home, error) {
	root := dir
	if root == "" && len(requires) > 0 {
		root = RequireHome(requires[0])
	}
	if root == "" {
		if self, ok := RootFromSelf(); ok {
			root = self
		}
	}
	if root == "" {
		if base, ok := PlatformDataDir(); ok && short != "" {
			root = filepath.Join(base, short)
		}
	}
	if root == "" {
		root = "."
	}
	return newHome(root)
}

// RequireHome 返回某家工具链的家目录：先看它自己的环境变量，再落到
// 用户目录下的同名目录（cot → ~/cot，tdp → ~/tdp）。
func RequireHome(req string) string {
	name := strings.ToUpper(req) + "_HOME"
	if v := strings.TrimSpace(getenv(name)); v != "" {
		return v
	}
	if hd, err := userHome(); err == nil && hd != "" {
		return filepath.Join(hd, req)
	}
	return ""
}

// ToolchainEnvNames 是"这个 shell 已经站在某家工具链里"的环境变量名。
//
// 卸载最后一个包时看的就是它们：有值说明用户是在激活过的 shell 里敲的
// 命令，那个家归 cot/tdp 管，不是 gpm 的私产 —— 里面的 <家>/bin/gpm
// 留着不删（D29 修订）。
var ToolchainEnvNames = []string{"COT_HOME", "TDP_HOME"}

// ActiveToolchainEnv 回报当前环境里第一个有值的工具链家变量。
// 空白不算有值；名字与值都 trim 过。
func ActiveToolchainEnv() (name, value string, ok bool) {
	for _, n := range ToolchainEnvNames {
		if v := strings.TrimSpace(getenv(n)); v != "" {
			return n, v, true
		}
	}
	return "", "", false
}

// PlatformDataDir 返回本平台放"用户级程序数据"的地方。
//
//	macOS   ~/Library/Application Support
//	Windows %LOCALAPPDATA%（缺省 %USERPROFILE%\AppData\Local）
//	Linux   $XDG_DATA_HOME 或 ~/.local/share
//
// 这是 `requires` 为空时的家：没有工具链可以借住，就走平台惯例。第二个
// 返回值是 false 表示连用户目录都问不出来（那调用方会落到当前目录）。
func PlatformDataDir() (string, bool) {
	hd, err := userHome()
	if err != nil || hd == "" {
		return "", false
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(hd, "Library", "Application Support"), true
	case "windows":
		if v := strings.TrimSpace(getenv("LOCALAPPDATA")); v != "" {
			return v, true
		}
		return filepath.Join(hd, "AppData", "Local"), true
	default:
		if v := strings.TrimSpace(getenv("XDG_DATA_HOME")); v != "" {
			return v, true
		}
		return filepath.Join(hd, ".local", "share"), true
	}
}

func newHome(root string) (*Home, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Home{Root: abs, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, nil
}

// RootFromSelf 从「正在运行的 gpm 自己」推断家目录。
//
// 布局规定非图形界面的小东西（gpm 自己、终端启动器）住在 <家目录>/bin，
// 带 GUI 的应用住在 <家目录>/lib。于是用户在新终端里敲 `gpm list` 时，
// 光凭 <家目录>/bin/gpm 这个位置就能把家目录找回来 —— 既不必记得带 --dir，
// 也不必让 shell 一直替 gpm 记着 COT_HOME。
//
// 三条判据缺一不可，免得把 /usr/local/bin/gpm 这种「恰好也叫 bin」的地方
// 误当成家目录：
//  1. 可执行文件所在目录正好叫 bin；
//  2. 文件名是 gpm（Windows 上 gpm.exe）；
//  3. 上一级里有账本 —— 那是 gpm 家目录的记号，也是它的骨架已经建过的
//     证据。账本叫 `<家目录名>-state.json`（v3.7），v3.6 及以前的
//     `state.json` 也认，好让升级后的第一声 `gpm list` 还能看见老家。
func RootFromSelf() (string, bool) {
	exe, err := selfExecutable()
	if err != nil || exe == "" {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	binDir := filepath.Dir(exe)
	if filepath.Base(binDir) != "bin" {
		return "", false
	}
	name := filepath.Base(exe)
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}
	if name != "gpm" {
		return "", false
	}
	root := filepath.Dir(binDir)
	return root, HasLedger(root)
}

// HasLedger 报告 root 底下有没有 gpm 的账本：先看新名字
// `<root 的目录名>-state.json`，再认旧的 `state.json`（v3.6 及以前）。
func HasLedger(root string) bool {
	for _, p := range []string{
		filepath.Join(root, filepath.Base(root)+"-"+LedgerSuffix),
		filepath.Join(root, LegacyLedgerName),
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// Bin 是启动器与 gpm 自拷贝所在目录。
func (h *Home) Bin() string { return filepath.Join(h.Root, "bin") }

// Lib 是命令行插件目录的父目录（cot 把自己装的插件放这儿）。v3.6 起
// gpm 不再往 lib/ 里放东西。
func (h *Home) Lib() string { return filepath.Join(h.Root, "lib") }

// Staging 是解包中转目录。
func (h *Home) Staging() string { return filepath.Join(h.Root, "staging") }

// Log 是日志目录（本版尚未写入，先占位）。
func (h *Home) Log() string { return filepath.Join(h.Root, "log") }

// LedgerSuffix 是账本文件名的后缀：账本按家命名，`<家目录名>-state.json`。
const LedgerSuffix = "state.json"

// LegacyLedgerName 是 v3.6 及以前那个放在谁家都通用的账本名。
const LegacyLedgerName = "state.json"

// LedgerPath 是账本文件：`<家>/<家目录名>-state.json`（v3.7）。
//
// 按家命名是为了在 `~/cot` 这种与 cot 共用的目录里一眼看出这本账是谁的，
// 也不至于和 cot 自己可能写的 `state.json` 撞上（DESIGN O16）。
func (h *Home) LedgerPath() string {
	return filepath.Join(h.Root, filepath.Base(h.Root)+"-"+LedgerSuffix)
}

// LegacyLedgerPath 是 v3.6 及以前用的账本位置，只为读旧账与迁移保留。
func (h *Home) LegacyLedgerPath() string { return filepath.Join(h.Root, LegacyLedgerName) }

// Platform 是包目录名里的平台段，如 darwin_arm64。
func (h *Home) Platform() string { return h.GOOS + "_" + h.GOARCH }

// AppDir 返回入口在家的落点：v3.6 起 GUI 的实体直接放在家目录下，
// 跟 bin/、lib/、账本平级 —— lib/<id>_<版本>_<平台>/ 那套是
// 命令行插件的位置，GUI 程序不放那儿。
//
// top 是入口在 payload/ 根下的顶层名字（"AI Desk.app"、"ai-desk.exe"）。
func (h *Home) AppDir(top string) string {
	return filepath.Join(h.Root, filepath.FromSlash(top))
}

// Ensure 建立家目录骨架。
func (h *Home) Ensure() error {
	for _, d := range []string{h.Bin(), h.Lib(), h.Staging()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Exists 报告家目录在本次动它之前是不是已经存在。
//
// 用来回答"这个骨架是原本就有的，还是我们刚建的"：安装早早失败时
// （比如自举那个 cot 跑不起来），只有我们刚建的空骨架才该被摘掉。
func (h *Home) Exists() bool {
	fi, err := os.Stat(h.Root)
	return err == nil && fi.IsDir()
}

// DropIfEmpty 把刚建出来的空骨架收回去：只删空目录，删不掉就留着。
//
// 顺序是从里往外（staging → lib → bin → 家），而且只用 Remove（不是
// RemoveAll）—— 里面有东西就说明那不是我建的，绝不能顺手带走。
func (h *Home) DropIfEmpty() {
	for _, d := range []string{h.Staging(), h.Lib(), h.Bin(), h.Root} {
		os.Remove(d)
	}
}

// DropStagingIfEmpty 收掉空的 staging/：它是解包的中转场地，不是家的一部分。
//
// 装完（成功或失败）那里都该是空的 —— 解包目录由 Install 的
// `defer os.RemoveAll(unpack)` 收走。不收这一层的话，每个家目录里都会
// 多出一个空的 staging/，看的人只会猜它是不是坏了、能不能删。
//
// 判据与 DropIfEmpty 同一条：只用 Remove（不是 RemoveAll）。并发跑着的
// 另一个 gpm 可能正在里面解包，用户也可能往里放了东西 —— 那种时候留着。
func (h *Home) DropStagingIfEmpty() {
	os.Remove(h.Staging())
}
