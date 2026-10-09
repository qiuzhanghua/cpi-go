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

	writeFile(t, filepath.Join(dir, "ad-manifest.yaml"), 0o644, `id: ai-desk
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
		InstallSH, InstallCMD, "gpm", "ad-manifest.yaml", stage.SumsFile,
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
	writeFile(t, filepath.Join(dir, "ad-manifest.yaml"), 0o644, `id: ai-desk
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
	writeFile(t, filepath.Join(dir, "x-manifest.yaml"), 0o644, `id: x
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
	sh := InstallScript("", nil)
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

	cmd := InstallBatch("", nil)
	if !strings.Contains(cmd, "gpm.exe install .") {
		t.Error("install.cmd 必须调用 gpm.exe")
	}
	if strings.Contains(cmd, "powershell") || strings.Contains(cmd, "PowerShell") {
		t.Error("install.cmd 不该依赖 PowerShell：默认 ExecutionPolicy 会拦住它")
	}

	// 没给 --default-dir、清单也没有 requires 时，不能凭空编出一个安装根来 ——
	// 装到哪儿是打包方的决定，gpm 会落到平台数据目录/<简称>。
	for _, one := range []string{sh, cmd} {
		if strings.Contains(one, "--dir") {
			t.Errorf("既没给 --default-dir 也没有 requires，却出现了 --dir：\n%s", one)
		}
	}
}

// TestInstallBatchLineEndingsAndEncoding 钉住 install.cmd 的两个「必须」：
// 行尾只能是 CRLF，内容只能是 ASCII。
//
// 这不是洁癖。cmd.exe 按当前代码页逐行读批处理，而 UTF-8 的非 ASCII 字节在
// 别的代码页下解码时会把行尾的 LF 一起吞掉，于是注释行与下一行黏连、解码残渣
// 被当成命令去执行 —— 实测（OEM 936 的中文 Windows）用户每次双击 install.cmd
// 都会先看到一行 `'…' is not recognized as an internal or external command`。
// 2×2 对照：CRLF+中文干净、LF+ASCII 干净，只有 LF+非 ASCII 复现。install.sh
// 那边要求正好相反（CRLF 会让 `#!/bin/sh` 失效），所以两份脚本各钉一个测试，
// 免得日后有人「顺手统一」了行尾。
func TestInstallBatchLineEndingsAndEncoding(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"没给 --default-dir", InstallBatch("", nil)},
		{"--default-dir 是工具链的家", InstallBatch("~/cot", nil)},
		{"按 requires 定家", InstallBatch("", []string{"cot"})},
		{"--default-dir 是绝对路径", InstallBatch(`D:\apps\ad`, nil)},
	}
	for _, tc := range cases {
		if lf, crlf := strings.Count(tc.got, "\n"), strings.Count(tc.got, "\r\n"); lf != crlf {
			t.Errorf("%s：有 %d 个换行不是 CRLF —— cmd 会把注释行和下一行黏连", tc.name, lf-crlf)
		}
		if i := firstNonASCII(tc.got); i >= 0 {
			t.Errorf("%s：第 %d 个字节是 0x%02X —— 非 ASCII 在别的代码页下会变乱码，叠加 LF 还会被当成命令执行",
				tc.name, i, tc.got[i])
		}
		if !strings.Contains(tc.got, `set "RC=%ERRORLEVEL%"`) || !strings.Contains(tc.got, "exit /b %RC%") {
			t.Errorf("%s：install.cmd 没把 gpm 的退出码带出去 —— 末行若是 pause，装失败也会 exit 0", tc.name)
		}
	}

	// 反方向：install.sh 必须是 LF。
	if sh := InstallScript("~/cot", []string{"cot"}); strings.Contains(sh, "\r\n") {
		t.Error("install.sh 里出现了 CRLF：`#!/bin/sh` 会失效，unix 上直接跑不起来")
	}
}

// firstNonASCII 返回第一个非 ASCII 字节的下标，全 ASCII 时返回 -1。
func firstNonASCII(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return i
		}
	}
	return -1
}

// TestDefaultDirIsBakedIntoInstallers 守住 --default-dir 的职责：把「装到
// 哪儿」写进脚本。工具链的家（~/cot）写成 `${COT_HOME:-$HOME/cot}` ——
// 脚本里给的是默认值，用户在 shell 里设的家仍然算数。
func TestDefaultDirIsBakedIntoInstallers(t *testing.T) {
	sh := InstallScript("~/cot", nil)
	if !strings.Contains(sh, `--dir "${COT_HOME:-$HOME/cot}"`) {
		t.Errorf("install.sh 没把 ~/cot 写成 ${COT_HOME:-$HOME/cot}：\n%s", sh)
	}

	cmd := InstallBatch("~/cot", nil)
	if !strings.Contains(cmd, `set "COT_HOME=%USERPROFILE%\cot"`) {
		t.Errorf("install.cmd 没把 ~ 展开成 %%USERPROFILE%%：\n%s", cmd)
	}
	if !strings.Contains(cmd, `if not defined COT_HOME`) {
		t.Errorf("install.cmd 必须在 COT_HOME 未定义时才设默认值：\n%s", cmd)
	}
	if !strings.Contains(cmd, `--dir "%COT_HOME%"`) {
		t.Errorf("install.cmd 没把 --dir 指到 COT_HOME：\n%s", cmd)
	}

	// 不是工具链的家（那两家之外的路径）原样照抄，也不替它发明环境变量。
	if got := InstallScript("/opt/apps", nil); !strings.Contains(got, `--dir "/opt/apps"`) {
		t.Errorf("绝对路径 --default-dir 被改动了：\n%s", got)
	}
	if got := InstallScript("~/ad", nil); !strings.Contains(got, `--dir "$HOME/ad"`) {
		t.Errorf("非工具链的家不该被写成环境变量：\n%s", got)
	}
}

// 没给 --default-dir 时，装到哪儿由清单的 requires 决定。
func TestRequiresDrivesInstallerDir(t *testing.T) {
	sh := InstallScript("", []string{"cot"})
	if !strings.Contains(sh, `--dir "${COT_HOME:-$HOME/cot}"`) {
		t.Errorf("install.sh 没按 requires 定家：\n%s", sh)
	}
	if !strings.Contains(InstallScript("", []string{"tdp"}), `--dir "${TDP_HOME:-$HOME/tdp}"`) {
		t.Errorf("requires: [tdp] 的家不对：\n%s", sh)
	}

	cmd := InstallBatch("", []string{"cot"})
	if !strings.Contains(cmd, `--dir "%COT_HOME%"`) {
		t.Errorf("install.cmd 没按 requires 定家：\n%s", cmd)
	}
	if !strings.Contains(cmd, `set "COT_HOME=%USERPROFILE%\cot"`) {
		t.Errorf("install.cmd 的兜底值应当是 %%USERPROFILE%%\\cot，而不是 %%COT_HOME%%：\n%s", cmd)
	}
}

// 带工具链的包：tools/ 也要进 zip、也要进 SHA256SUMS（D32）。
func TestBuildPacksToolsAndCoversThem(t *testing.T) {
	dir := assembly(t)
	writeFile(t, filepath.Join(dir, "ad-manifest.yaml"), 0o644, `id: ai-desk
name: AI Desk
version: 1.2.3
requires: [cot]
entry:
  darwin:
    bundle: "AI Desk.app"
launch:
  cmd: ad
`)
	tool := filepath.Join(dir, "tools", "darwin_arm64", "cot")
	writeFile(t, tool, 0o755, "#!/bin/sh\necho cot\n")

	out := filepath.Join(t.TempDir(), "out.zip")
	self := filepath.Join(t.TempDir(), "gpm")
	writeFile(t, self, 0o755, "FAKE-GPM")

	if _, err := Build(Options{Dir: dir, Out: out, GOOS: "darwin", GOARCH: "arm64", Self: self}); err != nil {
		t.Fatal(err)
	}
	if ns := names(t, out); !has(ns, "tools/darwin_arm64/cot") {
		t.Errorf("包里没有工具链：%v", ns)
	}

	dst := filepath.Join(t.TempDir(), "unpacked")
	if err := stage.Materialize(out, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, stage.SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "  tools/darwin_arm64/cot\n") {
		t.Errorf("工具链没进 SHA256SUMS：\n%s", b)
	}
	// 安装时会照 payload/ 与 tools/ 两个目录查覆盖。
	if ok, err := stage.VerifySums(dst, "payload", "tools"); err != nil || !ok {
		t.Fatalf("VerifySums(payload, tools) = %v/%v", ok, err)
	}
	// 安装脚本要把 requires 的家烘出来。
	sh, err := os.ReadFile(filepath.Join(dst, InstallSH))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sh), `--dir "${COT_HOME:-$HOME/cot}"`) {
		t.Errorf("install.sh 没按 requires 定家：\n%s", sh)
	}
}
