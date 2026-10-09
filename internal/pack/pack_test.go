package pack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qiuzhanghua/gpm-go/internal/stage"
)

// writeFile 写一个文件并显式设定权限位 —— 权限位正是这里要验的东西之一。
func writeFile(t *testing.T, path string, mode os.FileMode, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// assembly 造一个最小的「已装配目录」：一个带空格的 .app（macOS 上很常见），
// 里面既有可执行文件，也有符号链接。
func assembly(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "manifest.yaml"), 0o644, `id: ai-desk
name: AI Desk
version: 1.2.3
entry:
  darwin:
    bundle: "AI Desk.app"
  windows:
    exe: AI Desk.exe
  linux:
    exe: ai-desk
launch:
  cmd: ad
  mode: activate
`)

	app := filepath.Join(dir, "payload", "AI Desk.app")
	writeFile(t, filepath.Join(app, "Contents", "Info.plist"), 0o644, "<plist/>\n")
	writeFile(t, filepath.Join(app, "Contents", "MacOS", "ai-desk"), 0o755, "#!/bin/sh\necho hi\n")
	writeFile(t, filepath.Join(app, "Contents", "Frameworks", "V1", "libx.dylib"), 0o644, "BINARY")

	// 另外两个平台各自的产物。同一个装配目录里放三份，正是这套设计的意思：
	// 清单描述形态，打包时按目标平台挑一份。
	writeFile(t, filepath.Join(dir, "payload", "AI Desk.exe"), 0o755, "MZ")
	writeFile(t, filepath.Join(dir, "payload", "ai-desk"), 0o755, "ELF")

	// .app 里出现符号链接是常态，而且它们必须原样保留：
	// 展开成副本会让包变大，跟着链接走还可能直接报错。
	link := filepath.Join(app, "Contents", "Frameworks", "Current")
	if err := os.Symlink("V1", link); err != nil {
		t.Fatal(err)
	}
	return dir
}

func names(t *testing.T, zipPath string) []string {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var out []string
	for _, f := range r.File {
		out = append(out, f.Name)
	}
	return out
}

func has(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestBuildRoundTrip 是这里最重要的一个测试：打出来的包必须能被
// stage.Materialize 原样解回来，权限位、符号链接、SHA256SUMS 一样不差。
// 「打得开」和「解回来还是对的」是两回事。
func TestBuildRoundTrip(t *testing.T) {
	dir := assembly(t)
	out := filepath.Join(t.TempDir(), "out.zip")

	self := filepath.Join(t.TempDir(), "gpm")
	writeFile(t, self, 0o755, "FAKE-GPM")

	got, err := Build(Options{Dir: dir, Out: out, GOOS: "darwin", GOARCH: "arm64", Self: self})
	if err != nil {
		t.Fatal(err)
	}
	if got != out {
		t.Errorf("返回的路径 = %q，想要 %q", got, out)
	}

	ns := names(t, got)
	for _, want := range []string{
		InstallSH, InstallCMD, "gpm", "manifest.yaml", stage.SumsFile,
		"payload/AI Desk.app/Contents/Info.plist",
		"payload/AI Desk.app/Contents/MacOS/ai-desk",
	} {
		if !has(ns, want) {
			t.Errorf("包里少了 %q（实际有 %v）", want, ns)
		}
	}

	dst := filepath.Join(t.TempDir(), "unpacked")
	if err := stage.Materialize(got, dst); err != nil {
		t.Fatal(err)
	}

	// 校验值必须是「照着这个包算出来的」，所以解回来之后一定能对上。
	ok, err := stage.VerifySums(dst, "payload")
	if err != nil {
		t.Fatalf("SumsVerify 失败: %v", err)
	}
	if !ok {
		t.Fatal("VerifySums 说文件被改过，但它刚从 zip 里解出来")
	}

	exe := filepath.Join(dst, "payload", "AI Desk.app", "Contents", "MacOS", "ai-desk")
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		// Windows 没有可执行位这回事，os.Stat 一律报 0666。
		t.Errorf("可执行位丢了：%v", fi.Mode())
	}

	link := filepath.Join(dst, "payload", "AI Desk.app", "Contents", "Frameworks", "Current")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("符号链接没保留：%v", err)
	}
	if target != "V1" {
		t.Errorf("链接目标 = %q，想要 %q", target, "V1")
	}
}

// TestBuildEmbedsGpmExeOnWindows：Windows 上 install.cmd 调的是 gpm.exe，
// 包里就必须真的叫这个名字。这条在 macOS 上跑也能验证，不必等 CI。
func TestBuildEmbedsGpmExeOnWindows(t *testing.T) {
	dir := assembly(t)
	out := filepath.Join(t.TempDir(), "out.zip")
	self := filepath.Join(t.TempDir(), "gpm")
	writeFile(t, self, 0o755, "FAKE-GPM")

	if _, err := Build(Options{Dir: dir, Out: out, GOOS: "windows", GOARCH: "amd64", Self: self}); err != nil {
		t.Fatal(err)
	}
	ns := names(t, out)
	if !has(ns, "gpm.exe") {
		t.Errorf("Windows 包里没有 gpm.exe：%v", ns)
	}
	if has(ns, "gpm") {
		t.Error("Windows 包里不该有叫 gpm 的文件")
	}
}

// TestBuildSumsCoverEveryFile：SHA256SUMS 的作用是「包被人动过就要发现」，
// 所以它必须覆盖 payload/ 下的每一个常规文件。
func TestBuildSumsCoverEveryFile(t *testing.T) {
	dir := assembly(t)
	out := filepath.Join(t.TempDir(), "out.zip")
	self := filepath.Join(t.TempDir(), "gpm")
	writeFile(t, self, 0o755, "FAKE-GPM")

	if _, err := Build(Options{Dir: dir, Out: out, GOOS: "darwin", Self: self}); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "unpacked")
	if err := stage.Materialize(out, dst); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(dst, stage.SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{
		"payload/AI Desk.app/Contents/Info.plist",
		"payload/AI Desk.app/Contents/MacOS/ai-desk",
		"payload/AI Desk.app/Contents/Frameworks/V1/libx.dylib",
	} {
		if !strings.Contains(text, "  "+want+"\n") {
			t.Errorf("SHA256SUMS 里没有 %q：\n%s", want, text)
		}
	}
	// 符号链接不参与校验（和 stage.VerifySums 的约定一致）。
	if strings.Contains(text, "Frameworks/Current") {
		t.Errorf("符号链接不该出现在 SHA256SUMS 里：\n%s", text)
	}
}

func TestBuildRejectsMissingEntryForTargetOS(t *testing.T) {
	dir := assembly(t)
	// 只有 darwin 的清单：拿到 windows 上打包就该当场报错，
	// 而不是打出一个装上去跑不起来的包。
	writeFile(t, filepath.Join(dir, "manifest.yaml"), 0o644, `id: ai-desk
name: AI Desk
version: 1.2.3
entry:
  darwin:
    bundle: "AI Desk.app"
launch:
  cmd: ad
  mode: activate
`)
	_, err := Build(Options{
		Dir:  dir,
		Out:  filepath.Join(t.TempDir(), "out.zip"),
		GOOS: "windows",
		Self: os.Args[0],
	})
	if err == nil {
		t.Fatal("windows 没有 entry，却打包成功了")
	}
}

// TestBuildLeavesNoHalfZip：打包中途失败时不能在磁盘上留一个半个 zip。
// 一个看起来打成功了的坏包，比一次明确的失败危险得多。
func TestBuildLeavesNoHalfZip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "manifest.yaml"), 0o644, `id: x
name: X
version: 1
entry:
  darwin:
    bundle: X.app
`)
	// 有 manifest，没有 payload/ —— addTree 会失败。
	out := filepath.Join(t.TempDir(), "out.zip")
	if _, err := Build(Options{Dir: dir, Out: out, GOOS: "darwin", Self: os.Args[0]}); err == nil {
		t.Fatal("没有 payload/ 却打包成功了")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("失败后留下了 %s", out)
	}
}

// TestInstallScriptsAreSelfContained 守住两个入口脚本的关键性质：
// 它们不能把安装逻辑写在脚本里（那就等于每平台各维护一份）。
func TestInstallScriptsAreSelfContained(t *testing.T) {
	sh := InstallScript("")
	if !strings.Contains(sh, `cd "$(dirname "$0")"`) {
		t.Error("install.sh 必须以自己的位置为工作目录，否则用户在任何目录下运行都会找错文件")
	}
	if !strings.Contains(sh, "gpm install .") {
		t.Error("install.sh 必须把安装交给 gpm")
	}
	for _, forbidden := range []string{"mkdir", "cp ", "sha256"} {
		if strings.Contains(sh, forbidden) {
			t.Errorf("install.sh 里不该出现 %q —— 安装逻辑属于 gpm", forbidden)
		}
	}

	cmd := InstallBatch("")
	if !strings.Contains(cmd, "gpm.exe install .") {
		t.Error("install.cmd 必须调用 gpm.exe")
	}
	if strings.Contains(cmd, "powershell") || strings.Contains(cmd, "PowerShell") {
		t.Error("install.cmd 不该依赖 PowerShell：默认 ExecutionPolicy 会拦住它")
	}

	// 不指定 --default-dir 时不能凭空编出一个绝对路径来 —— 装到哪儿是打包方的
	// 决定，gpm 只负责在没有指示时落到自己的相对路径默认值（解压出来的目录）。
	for _, s := range []string{sh, cmd} {
		for _, hard := range []string{"$HOME/", "%USERPROFILE%\\", "~/"} {
			if strings.Contains(s, hard) {
				t.Errorf("没给 --default-dir 却出现了硬编码安装根 %q：\n%s", hard, s)
			}
		}
	}
}

// TestDefaultDirIsBakedIntoInstallers 守住 --default-dir 的唯一职责：
// 把「装到哪儿」写进脚本，并且把开头的 ~ 换成能在双引号里展开的家目录变量。
func TestDefaultDirIsBakedIntoInstallers(t *testing.T) {
	sh := InstallScript("~/cot")
	if !strings.Contains(sh, `"${GPM_HOME:-$HOME/cot}"`) {
		t.Errorf("install.sh 没把 ~ 展开成 $HOME：\n%s", sh)
	}
	// $GPM_HOME 必须仍然优先：脚本里给的是默认值，用户能覆盖。
	if !strings.Contains(sh, "GPM_HOME:-") {
		t.Errorf("install.sh 的默认值必须挂在 ${GPM_HOME:-…} 上，否则用户覆盖不了：\n%s", sh)
	}

	cmd := InstallBatch("~/cot")
	if !strings.Contains(cmd, `set "GPM_HOME=%USERPROFILE%\cot"`) {
		t.Errorf("install.cmd 没把 ~ 展开成 %%USERPROFILE%%：\n%s", cmd)
	}
	if !strings.Contains(cmd, `if not defined GPM_HOME`) {
		t.Errorf("install.cmd 必须在 GPM_HOME 未定义时才设默认值：\n%s", cmd)
	}

	// 不含 ~ 的路径原样照抄，不该被改动。
	if got := InstallScript("/opt/apps"); !strings.Contains(got, `"${GPM_HOME:-/opt/apps}"`) {
		t.Errorf("绝对路径 --default-dir 被改动了：\n%s", got)
	}
}
