// Package home 解析并描述 gpm 的家目录。
//
// 家目录不由 gpm 自己发明：它由调用方决定 —— 包里的 install.sh/install.cmd
// 用 --dir 传过来（那个值由 `gpm pack --default-dir` 烘进脚本），用户也可以
// 直接敲 --dir 或设 GPM_HOME。都没给时，gpm 看一眼「自己现在在哪儿」：
// 布局规定带 GUI 的应用住在 <家目录>/lib，而 gpm 与终端启动器这类没有
// 图形界面的小东西住在 <家目录>/bin —— 于是 <家目录>/bin/gpm 这个位置
// 本身就把家目录说出来了（见 RootFromSelf）。再不行才落到当前目录。
// gpm 的全部内部结构都住在这个目录里，因此「卸载 = 删掉一个目录」成立。
package home

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// selfExecutable 是 os.Executable 的接缝，测试会替换它。
var selfExecutable = os.Executable

// Home 是 gpm 的家目录。
type Home struct {
	Root   string // 绝对路径
	GOOS   string
	GOARCH string
}

// Resolve 按 --dir > $GPM_HOME > RootFromSelf() > 当前目录 的优先级确定家目录。
//
// 这里故意没有「默认装到 ~/ad」「默认装到 ~/gpm」这类硬编码。装到哪儿是
// 调用方的决定：包里的 install.sh/install.cmd 把 --dir 传过来（值来自
// `gpm pack --default-dir`），用户也可以自己敲 --dir 或设 GPM_HOME。
// 一个都没给时先问「我正在哪儿」（见 RootFromSelf），最后才按相对路径算 ——
// 当前目录，于是骨架落在 ./bin、./lib、./state.json。
func Resolve(dir string) (*Home, error) {
	root := dir
	if root == "" {
		root = os.Getenv("GPM_HOME")
	}
	if root == "" {
		if self, ok := RootFromSelf(); ok {
			root = self
		}
	}
	if root == "" {
		root = "."
	}
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
// 也不必让 shell 一直替 gpm 记着 GPM_HOME。
//
// 三条判据缺一不可，免得把 /usr/local/bin/gpm 这种「恰好也叫 bin」的地方
// 误当成家目录：
//  1. 可执行文件所在目录正好叫 bin；
//  2. 文件名是 gpm（Windows 上 gpm.exe）；
//  3. 上一级里有账本 state.json —— 那是 gpm 家目录的记号，也是它的骨架
//     已经建过的证据。
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
	fi, err := os.Stat(filepath.Join(root, "state.json"))
	if err != nil || fi.IsDir() {
		return "", false
	}
	return root, true
}

// Bin 是启动器与 gpm 自拷贝所在目录。
func (h *Home) Bin() string { return filepath.Join(h.Root, "bin") }

// Lib 是各版本包目录的父目录。
func (h *Home) Lib() string { return filepath.Join(h.Root, "lib") }

// Staging 是解包中转目录。
func (h *Home) Staging() string { return filepath.Join(h.Root, "staging") }

// Log 是日志目录（本版尚未写入，先占位）。
func (h *Home) Log() string { return filepath.Join(h.Root, "log") }

// LedgerPath 是账本文件。
func (h *Home) LedgerPath() string { return filepath.Join(h.Root, "state.json") }

// Platform 是包目录名里的平台段，如 darwin_arm64。
func (h *Home) Platform() string { return h.GOOS + "_" + h.GOARCH }

// PackageDir 返回某个包在 lib/ 下的落点。
func (h *Home) PackageDir(id, version string) string {
	return filepath.Join(h.Lib(), fmt.Sprintf("%s_%s_%s", id, version, h.Platform()))
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
