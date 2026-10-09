// Package home 解析并描述 gpm 的家目录。
//
// 家目录不由 gpm 自己发明：它由调用方决定 —— 包里的 install.sh/install.cmd
// 用 --dir 传过来（那个值由 `gpm pack --default-dir` 烘进脚本），用户也可以
// 直接敲 --dir 或设 GPM_HOME。三者都没有时按相对路径算，即当前目录。
// gpm 的全部内部结构都住在这个目录里，因此「卸载 = 删掉一个目录」成立。
package home

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Home 是 gpm 的家目录。
type Home struct {
	Root   string // 绝对路径
	GOOS   string
	GOARCH string
}

// Resolve 按 --dir > $GPM_HOME > 当前目录 的优先级确定家目录。
//
// 这里故意没有「默认装到 ~/ad」「默认装到 ~/gpm」这类硬编码。装到哪儿是
// 调用方的决定：包里的 install.sh/install.cmd 把 --dir 传过来（值来自
// `gpm pack --default-dir`），用户也可以自己敲 --dir 或设 GPM_HOME。
// 一个都没给时按相对路径算 —— 当前目录，于是骨架落在 ./bin、./lib、./state.json。
func Resolve(dir string) (*Home, error) {
	root := dir
	if root == "" {
		root = os.Getenv("GPM_HOME")
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
