// Package pack 把一个「已经装配好的目录」（manifest.yaml + payload/）打成分发包。
//
// 为什么这件事要收进 gpm、而不是留一个 shell 脚本：
// 打包要做三件在跨平台上很难做对的事 —— 算 sha256、保住可执行位、处理符号链接。
// Windows 的 Git Bash 里连 zip 都没有，sha256sum 也不保证有；而 macOS 的 .app
// 内部是有符号链接的，普通 zip 会把它们展开成副本（甚至坏掉）。
// gpm 本来就是跨平台的 Go 程序，同一份实现三个平台都一样。
package pack

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/qiuzhanghua/gpm-go/internal/manifest"
	"github.com/qiuzhanghua/gpm-go/internal/stage"
)

// 包里那两个「用户双击/运行」的入口。由 pack 生成，装配方不用自己写。
const (
	InstallSH  = "install.sh"
	InstallCMD = "install.cmd"
)

// scriptDir 把 --default-dir 给的路径转成脚本里能直接用的写法。
//
// 要处理的只有开头的 `~`：双引号里的 `~` 不会展开，`$HOME` / `%USERPROFILE%` 会。
// 除此之外原样照抄 —— 装到哪儿是打包方的决定，这里不替它做主。
func scriptDir(defaultDir, goos string) string {
	if defaultDir != "~" && !strings.HasPrefix(defaultDir, "~/") && !strings.HasPrefix(defaultDir, `~\`) {
		return defaultDir
	}
	rest := defaultDir[1:]
	if goos == "windows" {
		// 顺手把分隔符也换掉：`~/cot` 展开成 `%USERPROFILE%/cot` 能用，
		// 但在 Windows 上看起来就是个错的东西。
		return `%USERPROFILE%` + strings.ReplaceAll(rest, "/", `\`)
	}
	return "$HOME" + rest
}

// InstallScript 是 install.sh 的原文。
//
// 它只做三件事：切到自己的目录、给 gpm 补可执行位、把安装交给 gpm。
// 之所以坚持 `chmod +x` 而不是指望解压工具：Windows 的资源管理器解压会丢掉
// Unix 权限位，用户也可能用别的方式解压。安装逻辑一行都不写在这里 ——
// 写在这里就等于每个平台各维护一份，而它们迟早会不一致。
//
// defaultDir 是「装到哪儿」的默认值，由 `gpm pack --default-dir` 烘进来；
// 留空表示打包方没有指定，那就交给 gpm 自己的相对路径默认值（当前目录）。
// 无论如何 $GPM_HOME 的优先级都更高，用户能覆盖。
func InstallScript(defaultDir string) string {
	fallback := "."
	if defaultDir != "" {
		fallback = scriptDir(defaultDir, "linux")
	}
	return fmt.Sprintf(`#!/bin/sh
# 由 gpm pack 生成，请勿手工编辑。
set -eu
cd "$(dirname "$0")"
chmod +x ./gpm 2>/dev/null || true
exec ./gpm install . --dir "${GPM_HOME:-%s}"
`, fallback)
}

// InstallBatch 是 install.cmd 的原文（Windows 用户双击这个）。
//
// 在 Windows 上优先用 .cmd 而不是 .ps1：PowerShell 默认 ExecutionPolicy 是
// Restricted，右键「使用 PowerShell 运行」常常直接报「在此系统上禁止运行脚本」，
// 而 .cmd 双击就能跑。
func InstallBatch(defaultDir string) string {
	fallback := "."
	if defaultDir != "" {
		fallback = scriptDir(defaultDir, "windows")
	}
	return fmt.Sprintf(`@echo off
rem 由 gpm pack 生成，请勿手工编辑。
setlocal
cd /d "%%~dp0"
if not defined GPM_HOME set "GPM_HOME=%s"
gpm.exe install . --dir "%%GPM_HOME%%"
pause
`, fallback)
}

// Options 是打一个包所需的全部输入。
type Options struct {
	Dir    string // 含 manifest.yaml 与 payload/ 的目录
	Out    string // 输出 zip 路径；留空则 dist/<id>-<version>-<goos>-<goarch>.zip
	GOOS   string // 目标平台；留空用当前平台
	GOARCH string
	Self   string // 要嵌进包里的 gpm 可执行文件；留空用当前进程
	// DefaultDir 烘进 install.sh / install.cmd 的默认安装根，例如 ~/cot。
	// 留空表示打包方不指定，落到 gpm 自己的相对路径默认值（解压出来的那个目录）。
	DefaultDir string
	Log        io.Writer // 进度输出；留空则静默
}

// Build 打出一个可分发的 zip，返回它的绝对路径。
//
// 产物的内容与顺序都已固定：
//
//	install.sh  install.cmd  gpm  manifest.yaml  SHA256SUMS  payload/…
func Build(opt Options) (string, error) {
	goos, goarch := opt.GOOS, opt.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	log := opt.Log
	if log == nil {
		log = io.Discard
	}

	dir, err := filepath.Abs(opt.Dir)
	if err != nil {
		return "", err
	}
	m, err := manifest.Load(dir)
	if err != nil {
		return "", err
	}
	if err := m.Validate(goos); err != nil {
		return "", err
	}

	self := opt.Self
	if self == "" {
		self, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("找不到当前可执行文件：%w", err)
		}
	}
	fi, err := os.Stat(self)
	if err != nil {
		return "", fmt.Errorf("找不到 gpm 可执行文件 %s: %w", self, err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("%s 是个目录，不是可执行文件", self)
	}

	out := opt.Out
	if out == "" {
		out = filepath.Join("dist", fmt.Sprintf("%s-%s-%s-%s.zip", m.ID, m.Version, goos, goarch))
	}
	if a, err := filepath.Abs(out); err == nil {
		out = a
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}

	// 先算 SHA256SUMS：它得在写 zip 之前就是一个确定的字节串。
	sums, n, err := sumsFor(dir)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(log, "已核算 %d 个文件\n", n)

	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	err = writeAll(zw, dir, self, goos, opt.DefaultDir, sums)
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		f.Close()
		os.Remove(out) // 半个 zip 比没有 zip 更糟：它会让人以为打包成功了
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(out)
		return "", err
	}
	return out, nil
}

func writeAll(zw *zip.Writer, dir, self, goos, defaultDir string, sums []byte) error {
	when := time.Now()

	if err := addBytes(zw, InstallSH, 0o755, []byte(InstallScript(defaultDir)), when); err != nil {
		return err
	}
	if err := addBytes(zw, InstallCMD, 0o644, []byte(InstallBatch(defaultDir)), when); err != nil {
		return err
	}

	// 目标平台是 Windows 时，包里的二进制必须叫 gpm.exe —— install.cmd 调的就是它。
	selfName := "gpm"
	if goos == "windows" {
		selfName = "gpm.exe"
	}
	mode := fs.FileMode(0o755)
	if fi, err := os.Stat(self); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := addFile(zw, selfName, mode, self, when); err != nil {
		return err
	}

	if err := addFile(zw, manifest.FileName, 0o644, filepath.Join(dir, manifest.FileName), when); err != nil {
		return err
	}
	if err := addBytes(zw, stage.SumsFile, 0o644, sums, when); err != nil {
		return err
	}
	return addTree(zw, filepath.Join(dir, manifest.PayloadDir), manifest.PayloadDir, when)
}

// sumsFor 生成 SHA256SUMS 的内容，路径相对包根（形如 `payload/AI Desk.app/…`），
// 只覆盖 payload/ 下的常规文件 —— 符号链接不算，这一点要和 stage.VerifySums 一致。
func sumsFor(dir string) ([]byte, int, error) {
	type entry struct{ path, sum string }
	var entries []entry

	root := filepath.Join(dir, manifest.PayloadDir)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		sum, err := stage.HashFile(p)
		if err != nil {
			return err
		}
		entries = append(entries, entry{filepath.ToSlash(rel), sum})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("%s/ 里一个常规文件都没有", manifest.PayloadDir)
	}

	// 按路径排序，而不是按 hash：出问题时人眼要能一行行对着文件看。
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		// 两个空格是 sha256sum 的格式，读回来的解析器只按第一段空白切分，
		// 所以路径里的空格（macOS 的 "AI Desk.app"）不会出问题。
		lines = append(lines, e.sum+"  "+e.path)
	}
	return []byte(strings.Join(lines, "\n") + "\n"), len(entries), nil
}

func addBytes(zw *zip.Writer, name string, mode fs.FileMode, data []byte, when time.Time) error {
	method := zip.Deflate
	if mode&fs.ModeSymlink != 0 {
		// 符号链接的内容是链接目标，几字节而已，压缩没有意义。
		method = zip.Store
	}
	h := &zip.FileHeader{Name: filepath.ToSlash(name), Method: method, Modified: when}
	h.SetMode(mode) // 只有调这个才会把 Unix 权限位写进 ExternalAttrs
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func addFile(zw *zip.Writer, name string, mode fs.FileMode, src string, when time.Time) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return addBytes(zw, name, mode, b, when)
}

// addTree 把一棵目录树整个搬进 zip，保留权限位与符号链接。
//
// filepath.WalkDir 不会跟着符号链接走，正合需要：.app 里的链接该原样复制，
// 不能展开成它指向的那份副本。
func addTree(zw *zip.Writer, root, prefix string, when time.Time) error {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("找不到 %s/", prefix)
		}
		return err
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := prefix
		if rel != "." {
			name = prefix + "/" + filepath.ToSlash(rel)
		}

		switch {
		case d.IsDir():
			if rel == "." {
				return nil
			}
			h := &zip.FileHeader{Name: name + "/", Method: zip.Store, Modified: when}
			h.SetMode(fs.ModeDir | 0o755)
			_, err := zw.CreateHeader(h)
			return err

		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return addBytes(zw, name, fs.ModeSymlink|0o777, []byte(target), when)

		case !d.Type().IsRegular():
			// 设备文件、命名管道之类既不常见也无法在 zip 里表达，跳过。
			return nil

		default:
			fi, err := d.Info()
			if err != nil {
				return err
			}
			return addFile(zw, name, fi.Mode().Perm(), p, when)
		}
	})
}
