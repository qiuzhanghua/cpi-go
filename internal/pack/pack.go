// Package pack 把一个「已经装配好的目录」打成分发包。
//
// 装配目录里该有的是：`<简称>-manifest.yaml`、`payload/`，带工具链的包
// 再加 `tools/<os>_<arch>/`。打包要做三件在跨平台上很难做对的事 —— 算
// sha256、保住可执行位、处理符号链接，所以它留在 gpm 里而不是一个 shell
// 脚本里。Windows 的 Git Bash 里连 zip 都没有；而 macOS 的 .app 内部是有
// 符号链接的，普通 zip 会把它们展开成副本（甚至坏掉）。
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

// homeName 认出「某家工具链的家」这种写法：`~/cot`、`~\tdp`、`~`。
//
// 只认清单允许的那两家（manifest.KnownRequires）：把 `--default-dir ~/ad`
// 也变成 `$AD_HOME` 就等于替某个应用发明了一个环境变量。
func homeName(p string) (string, bool) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return "", false
	}
	rest := strings.Trim(p[1:], `/:\`)
	if rest == "" || strings.ContainsAny(rest, `/:\`) {
		return "", false
	}
	if !manifest.IsKnownRequire(rest) {
		return "", false
	}
	return rest, true
}

// toolchainDir 是「某家工具链的家」在脚本里的写法：环境变量优先，缺省落到
// 用户目录下的同名目录 —— 与 gpm 内部的 home.RequireHome 同一套规矩，
// 所以用户在 shell 里设了 COT_HOME，双击安装脚本也认。
func toolchainDir(req, goos string) (pre, arg string) {
	name := strings.ToUpper(req) + "_HOME"
	if goos == "windows" {
		// 行尾必须是 CRLF：这一行会被拼进 install.cmd，而 cmd.exe 在
		// 「LF 行尾 + 非 ASCII 注释」的组合下会把行尾吃掉、把残渣当命令执行
		// （见 InstallBatch 的注释）。
		pre = "if not defined " + name + " set \"" + name + "=%USERPROFILE%\\" + req + "\"\r\n"
		return pre, ` --dir "%` + name + `%"`
	}
	return "", ` --dir "${` + name + `:-$HOME/` + req + `}"`
}

// dirSpec 决定脚本里的 --dir 到底怎么写。
//
//	--default-dir 是工具链的家（~/cot）  → 走环境变量，#COT_HOME 能覆盖
//	--default-dir 是别的路径             → 原样照抄（$HOME / %USERPROFILE% 展开）
//	没给 --default-dir，但清单有 requires → 用第一家工具链的家
//	都没有                                → 不传 --dir，交给 gpm 定（平台数据目录/<简称>）
func dirSpec(defaultDir string, requires []string, goos string) (pre, arg string) {
	if defaultDir != "" {
		if name, ok := homeName(defaultDir); ok {
			return toolchainDir(name, goos)
		}
		return "", ` --dir "` + scriptDir(defaultDir, goos) + `"`
	}
	if len(requires) > 0 {
		return toolchainDir(requires[0], goos)
	}
	return "", ""
}

// InstallScript 是 install.sh 的原文。
//
// 它只做四件事：切到自己的目录、给 gpm 补可执行位、把安装交给 gpm、把用户
// 给的参数原样转交（v3.12、D42）。
// 之所以坚持 `chmod +x` 而不是指望解压工具：Windows 的资源管理器解压会丢掉
// Unix 权限位，用户也可能用别的方式解压。安装逻辑一行都不写在这里 ——
// 写在这里就等于每个平台各维护一份，而它们迟早会不一致。
//
// 转交参数的理由是一处真实的体验断裂：gpm 自己会在放弃 PATH 集成时打印一行
// 可照抄的 `./gpm install . --yes`，而用户站在解压出来的目录里更顺手的写法是
// `./install.sh --yes`。不转交的话 `--yes` 就被静默吞掉 —— 交互终端里退化成
// 一个 y/N 询问，非交互（CI、脚本）里干脆跳过 PATH 集成，而退出码仍是 0，
// 看上去像"装成功了、只是 PATH 没接上"。最后拼上 `"$@"` 就没有这回事了。
//
// 位置在 gpm 与烘进来的 `--dir` **之后**是有意的：flag 取后出现的那一个，
// 所以用户自己的 `--yes` / `--with` / `--dir` 都盖得住脚本里的默认值。
// 用带引号的 `"$@"` 而不是 `$@`，否则带空格的路径会被拆成多个参数；
// `set -u` 下 `"$@"` 也安全 —— 没有参数时它展开为空，不是未定义变量。
//
// 装到哪儿按这个次序定（与 D21 一致）：
//
//  1. --default-dir 烘进来的值（打包方明说）；写的是 `${COT_HOME:-$HOME/cot}`
//     这种带环境变量兜底的写法，所以用户在 shell 里设的家仍然算数；
//  2. 清单里 requires 的第一家工具链的家（$COT_HOME / $TDP_HOME，缺省 ~/cot、~/tdp）；
//  3. 什么都不传 —— 交给 gpm 自己定（平台数据目录/<简称>）。
func InstallScript(defaultDir string, requires []string) string {
	_, dir := dirSpec(defaultDir, requires, "linux")
	return fmt.Sprintf(`#!/bin/sh
# 由 gpm pack 生成，请勿手工编辑。
set -eu
cd "$(dirname "$0")"
chmod +x ./gpm 2>/dev/null || true
exec ./gpm install .%s "$@"
`, dir)
}

// InstallBatch 是 install.cmd 的原文（Windows 用户双击这个）。
//
// 在 Windows 上优先用 .cmd 而不是 .ps1：PowerShell 默认 ExecutionPolicy 是
// Restricted，右键「使用 PowerShell 运行」常常直接报「在此系统上禁止运行脚本」，
// 而 .cmd 双击就能跑。
//
// 两处与 install.sh 刻意不同，都是被 cmd.exe 的解析逼出来的（都实测过）：
//
//  1. 行尾必须是 CRLF。cmd.exe 按当前代码页逐行读批处理，注释里只要有非 ASCII
//     字节，UTF-8 那些字节在别的代码页下解码时会把行尾的 LF 一起吃进去，于是
//     注释行与下一行黏连、残渣被当成命令去执行：在 OEM 936 的中文 Windows 上
//     每次运行都会先多打一行 `'…' is not recognized as an internal or external
//     command`。2×2 对照（CRLF/LF × ASCII/中文）只有 LF+非 ASCII 复现，
//     CRLF 让行尾不再落在解码歧义里。install.sh 正好相反：CRLF 会让
//     `#!/bin/sh` 失效，所以两份脚本不能共用一个模板，也各有一个测试钉着。
//  2. 注释只用 ASCII。第 1 条只保证「中文 + CRLF」不再出错，而注释是给人看的：
//     在 936 的控制台里 `type install.cmd` 出来仍是乱码。这个脚本由打包机生成、
//     要在别人的机器上读，ASCII 是唯一稳的写法。
//
// 退出码也要带出去：末行若直接是 `pause`，`cmd /c install.cmd` 拿到的是 pause 的
// 0 —— 安装失败也判成功，从脚本或 CI 里调用就会误判。所以先存 %ERRORLEVEL%
// 再 pause，最后用它退出（pause 只负责让双击的用户看清输出）。
//
// 用户给的参数也原样转交（v3.12、D42，与 install.sh 同一件事）：双击时 `%*`
// 展开为空，脚本行为与从前逐字节一致；在终端里写 `install.cmd --yes` 才真的
// 把 `--yes` 送到 gpm 手上。用 `%*` 而不是 `%1 %2 …`（后者漏掉第 10 个之后的
// 参数），也不要给它加引号 —— `"%*"` 会把所有参数粘成一个。
func InstallBatch(defaultDir string, requires []string) string {
	pre, dir := dirSpec(defaultDir, requires, "windows")
	return fmt.Sprintf("@echo off\r\n"+
		"rem generated by gpm pack -- do not edit.\r\n"+
		"setlocal\r\n"+
		"cd /d \"%%~dp0\"\r\n"+
		"%sgpm.exe install .%s %%*\r\n"+
		"set \"RC=%%ERRORLEVEL%%\"\r\n"+
		"pause\r\n"+
		"exit /b %%RC%%\r\n", pre, dir)
}

// Options 是打一个包所需的全部输入。
type Options struct {
	Dir    string // 含 <简称>-manifest.yaml 与 payload/（必要时 tools/）的目录
	Out    string // 输出 zip 路径；留空则 dist/<id>-<version>-<goos>-<goarch>.zip
	GOOS   string // 目标平台；留空用当前平台
	GOARCH string
	Self   string // 要嵌进包里的 gpm 可执行文件；留空用当前进程
	// DefaultDir 烘进 install.sh / install.cmd 的默认安装根，例如 ~/cot。
	// 留空表示打包方不指定：清单里有 requires 就装进那家工具链的家，
	// 否则由 gpm 自己定（平台数据目录/<简称>）。
	DefaultDir string
	Log        io.Writer // 进度输出；留空则静默
}

// Build 打出一个可分发的 zip，返回它的绝对路径。
//
// 产物的内容与顺序都已固定：
//
//	install.sh  install.cmd  gpm  <简称>-manifest.yaml  SHA256SUMS  payload/…  tools/…
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
	err = writeAll(zw, dir, self, goos, opt.DefaultDir, sums, m)
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

func writeAll(zw *zip.Writer, dir, self, goos, defaultDir string, sums []byte, m *manifest.Manifest) error {
	when := time.Now()

	if err := addBytes(zw, InstallSH, 0o755, []byte(InstallScript(defaultDir, m.Requires)), when); err != nil {
		return err
	}
	if err := addBytes(zw, InstallCMD, 0o644, []byte(InstallBatch(defaultDir, m.Requires)), when); err != nil {
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

	// 清单名来自装配目录里那个真实文件（<简称>-manifest.yaml），不自己拼。
	name := manifest.FileNameFor(m.Short())
	if err := addFile(zw, name, 0o644, filepath.Join(dir, name), when); err != nil {
		return err
	}
	if err := addBytes(zw, stage.SumsFile, 0o644, sums, when); err != nil {
		return err
	}
	if err := addTree(zw, filepath.Join(dir, manifest.PayloadDir), manifest.PayloadDir, when); err != nil {
		return err
	}

	// 工具链是可选的：清单没写 requires 就不该有；写了就必须有（安装时会查）。
	tools := filepath.Join(dir, manifest.ToolsDir)
	if _, err := os.Stat(tools); err == nil {
		return addTree(zw, tools, manifest.ToolsDir, when)
	}
	return nil
}

// sumsFor 生成 SHA256SUMS 的内容，路径相对包根（形如 `payload/AI Desk.app/…`
// 或 `tools/darwin_arm64/cot`），只覆盖常规文件 —— 符号链接不算，这一点要和
// stage.VerifySums 保持一致。工具链二进制也在覆盖范围内（D32）。
func sumsFor(dir string) ([]byte, int, error) {
	type entry struct{ path, sum string }
	var entries []entry

	for _, sub := range []string{manifest.PayloadDir, manifest.ToolsDir} {
		root := filepath.Join(dir, sub)
		if _, err := os.Stat(root); err != nil {
			// 没有 tools/ 是正常的：不带工具链的包。
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, 0, err
		}
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
