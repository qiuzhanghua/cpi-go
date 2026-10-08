// Package home 解析并描述 cpi 的家目录。
//
// 按 A 方案的裁决，cpi 的家目录就是 ~/ad（Windows 为 %USERPROFILE%\ad），
// 可用 --dir 或环境变量 CPI_HOME 覆盖。cpi 的全部内部结构都住在这个目录里，
// 因此「卸载 = 删掉一个目录」成立。
package home

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Home 是 cpi 的家目录。
type Home struct {
	Root   string // 绝对路径
	GOOS   string
	GOARCH string
}

// Resolve 按 --dir > $CPI_HOME > ~/ad 的优先级确定家目录。
func Resolve(dir string) (*Home, error) {
	root := dir
	if root == "" {
		root = os.Getenv("CPI_HOME")
	}
	if root == "" {
		u, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("无法确定用户主目录: %w", err)
		}
		root = filepath.Join(u, "ad")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Home{Root: abs, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, nil
}

// Bin 是启动器与 cpi 自拷贝所在目录。
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
