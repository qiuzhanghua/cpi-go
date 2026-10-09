package integrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mustLauncher 跑一次 Launcher 并把生成的文件内容读出来。
func mustLauncher(t *testing.T, cmd, entryAbs, kind, goos, mode string, tcs ...Toolchain) (string, string) {
	t.Helper()
	binDir := t.TempDir()
	p, err := Launcher(Spec{
		BinDir:     binDir,
		Cmd:        cmd,
		EntryAbs:   entryAbs,
		Kind:       kind,
		GOOS:       goos,
		Mode:       mode,
		Toolchains: tcs,
	})
	if err != nil {
		t.Fatalf("Launcher(%s/%s/%s): %v", kind, goos, mode, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("启动器没写出来：%v", err)
	}
	return p, string(b)
}

func assertExecutable(t *testing.T, p string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// Windows 没有可执行位这回事，os.Stat 一律报 0666。
		return
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s 没有可执行位：%v", p, fi.Mode())
	}
}

// .app 外壳 + activate：必须走 open，否则拿不到「双击」的语义（激活已运行实例）。
func TestLauncherDarwinBundleActivate(t *testing.T) {
	app := "/tmp/x/AI Desk.app"
	p, body := mustLauncher(t, "ad", app, "bundle", "darwin", "activate")

	if filepath.Base(p) != "ad" {
		t.Errorf("文件名 = %q，想要 ad", filepath.Base(p))
	}
	if !strings.HasPrefix(body, "#!/bin/sh\n") {
		t.Errorf("第一行不是 shebang：%q", body)
	}
	if !strings.Contains(body, "exec open ") {
		t.Errorf("没有用 open：%q", body)
	}
	if !strings.Contains(body, app) {
		t.Errorf("没提到 .app 路径：%q", body)
	}
	if !strings.Contains(body, `--args "$@"`) {
		t.Errorf("参数没透传给 open：%q", body)
	}
	assertExecutable(t, p)
}

// .app 外壳 + direct：直接跑内层二进制，换 stdout、退出码与 Ctrl+C。
func TestLauncherDarwinBundleDirect(t *testing.T) {
	app := t.TempDir()
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(app, "Contents", "MacOS", "ai-desk")
	if err := os.WriteFile(inner, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0"?><plist><dict>
	<key>CFBundleExecutable</key><string>ai-desk</string>
	</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}

	_, body := mustLauncher(t, "ad", app, "bundle", "darwin", "direct")

	if strings.Contains(body, "open ") {
		t.Errorf("direct 模式不该走 open：%q", body)
	}
	if !strings.Contains(body, inner) {
		t.Errorf("没有 exec 内层二进制 %s：%q", inner, body)
	}
	if !strings.Contains(body, `"$@"`) {
		t.Errorf("参数没透传：%q", body)
	}
}

// 内层可执行文件不存在时必须报错，而不是写一个跑不起来的启动器。
func TestLauncherDarwinBundleDirectRejectsBrokenPlist(t *testing.T) {
	app := t.TempDir()
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0"?><plist><dict>
	<key>CFBundleExecutable</key><string>missing</string>
	</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Launcher(Spec{BinDir: t.TempDir(), Cmd: "ad", EntryAbs: app, Kind: "bundle", GOOS: "darwin", Mode: "direct"}); err == nil {
		t.Fatal("Info.plist 指向不存在的内层文件，Launcher 却成功了")
	}
}

// 裸可执行文件（上游自己发的 .app 之外的形态）：直接 exec。
func TestLauncherDirectExec(t *testing.T) {
	cases := []struct {
		name, goos string
	}{
		{"darwin", "darwin"},
		{"linux", "linux"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry := "/opt/AI Desk/ai-desk"
			_, body := mustLauncher(t, "ad", entry, "exe", c.goos, "")

			if strings.Contains(body, " open ") {
				t.Errorf("%s 上不该走 open：%q", c.goos, body)
			}
			if !strings.Contains(body, entry) {
				t.Errorf("没有 exec 入口：%q", body)
			}
			if !strings.Contains(body, `"$@"`) {
				t.Errorf("参数没透传：%q", body)
			}
		})
	}
}

// Windows：<cmd>.cmd 包装。用 .cmd 而不是 .ps1，因为 PowerShell 默认
// ExecutionPolicy Restricted，双击「使用 PowerShell 运行」常常直接报禁止运行脚本。
func TestLauncherWindowsWritesCmd(t *testing.T) {
	entry := `C:\Users\q\ad\lib\ai-desk_0.1.0_windows_amd64\ai-desk.exe`
	p, body := mustLauncher(t, "ad", entry, "exe", "windows", "")

	if filepath.Base(p) != "ad.cmd" {
		t.Errorf("文件名 = %q，想要 ad.cmd", filepath.Base(p))
	}
	if !strings.Contains(body, entry) {
		t.Errorf("没提到入口：%q", body)
	}
	if !strings.Contains(body, "%*") {
		t.Errorf("参数没通过 %%* 透传：%q", body)
	}
	if strings.Contains(strings.ToLower(body), "powershell") {
		t.Errorf("不该拉起 PowerShell：%q", body)
	}
}

// 名字里有需要转义的字符时也得生成一个语法正确的脚本 —— 交给真 shell 判。
func TestLauncherQuotesAwkwardPaths(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("这台机器上没有 sh")
	}

	entry := `/opt/it's here/ai-desk`
	p, _ := mustLauncher(t, "ad", entry, "exe", "linux", "")

	out, err := exec.Command(sh, "-n", p).CombinedOutput()
	if err != nil {
		t.Errorf("sh -n 不认这个启动器：%v\n%s", err, out)
	}
}

// 有工具链要注入时，macOS 的 bundle 也必须直接 exec 内层二进制：
// `open` 不把环境交给应用（实测，见 D33 / C1）。
func TestLauncherDarwinBundleWithToolchainSkipsOpen(t *testing.T) {
	app := t.TempDir()
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(app, "Contents", "MacOS", "ai-desk")
	if err := os.WriteFile(inner, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0"?><plist><dict>
	<key>CFBundleExecutable</key><string>ai-desk</string>
	</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}

	_, body := mustLauncher(t, "ad", app, "bundle", "darwin", "activate",
		Toolchain{Name: "cot", Home: "/Users/q/cot"})

	if strings.Contains(body, "exec open ") {
		t.Errorf("有工具链要注入时不该走 open：%q", body)
	}
	if !strings.Contains(body, inner) {
		t.Errorf("没有 exec 内层二进制：%q", body)
	}
	if !strings.Contains(body, `COT_HOME='/Users/q/cot'; export COT_HOME`) {
		t.Errorf("没注入 COT_HOME：%q", body)
	}
	if !strings.Contains(body, `PATH='/Users/q/cot/bin':$PATH; export PATH`) {
		t.Errorf("没把工具链的 bin 放进 PATH：%q", body)
	}
	if !strings.Contains(body, `. '/Users/q/cot/bin/env-cot.vars'`) {
		t.Errorf("没有 source env-cot.vars：%q", body)
	}
}

// 注入进去的脚本必须是语法正确的：交给真 shell 判。
func TestLauncherToolchainEnvIsValidSh(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("这台机器上没有 sh")
	}
	_, body := mustLauncher(t, "ad", "/opt/ai-desk", "exe", "linux", "",
		Toolchain{Name: "cot", Home: "/Users/q/it's cot"},
		Toolchain{Name: "tdp", Home: "/Users/q/tdp"})
	p := filepath.Join(t.TempDir(), "ad")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sh, "-n", p).CombinedOutput(); err != nil {
		t.Errorf("sh -n 不认这个启动器：%v\n%s", err, out)
	}
}

// Windows 上环境用 set 注入。
func TestLauncherWindowsInjectsEnv(t *testing.T) {
	_, body := mustLauncher(t, "ad", `C:\ad\ad.exe`, "exe", "windows", "",
		Toolchain{Name: "cot", Home: `C:\Users\q\cot`})

	if !strings.Contains(body, `set "COT_HOME=C:\Users\q\cot"`) {
		t.Errorf("没注入 COT_HOME：%q", body)
	}
	if !strings.Contains(body, `set "PATH=C:\Users\q\cot\bin;%PATH%"`) {
		t.Errorf("没把工具链的 bin 放进 PATH：%q", body)
	}
	if !strings.Contains(body, `call "C:\Users\q\cot\bin\env-cot.bat"`) {
		t.Errorf("没有 call env-cot.bat：%q", body)
	}
}

// OwnedByGpm 只认那行生成标记：别人的同名文件不能被当成我们的。
func TestOwnedByGpm(t *testing.T) {
	dir := t.TempDir()
	ours := filepath.Join(dir, "ours")
	if err := os.WriteFile(ours, []byte("#!/bin/sh\n# 由 gpm 生成，请勿手工编辑。\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	theirs := filepath.Join(dir, "theirs")
	if err := os.WriteFile(theirs, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")

	if exists, mine, err := OwnedByGpm(ours); err != nil || !exists || !mine {
		t.Errorf("ours: exists=%v ours=%v err=%v，想要 true/true/nil", exists, mine, err)
	}
	if exists, mine, err := OwnedByGpm(theirs); err != nil || !exists || mine {
		t.Errorf("theirs: exists=%v ours=%v err=%v，想要 true/false/nil", exists, mine, err)
	}
	if exists, mine, err := OwnedByGpm(missing); err != nil || exists || mine {
		t.Errorf("missing: exists=%v ours=%v err=%v，想要 false/false/nil", exists, mine, err)
	}
}

// LauncherPath 在 Windows 上加 .cmd。
func TestLauncherPath(t *testing.T) {
	dir := t.TempDir()
	if got, want := LauncherPath(dir, "ad", "darwin"), filepath.Join(dir, "ad"); got != want {
		t.Errorf("darwin = %q，想要 %q", got, want)
	}
	if got, want := LauncherPath(dir, "ad", "windows"), filepath.Join(dir, "ad.cmd"); got != want {
		t.Errorf("windows = %q，想要 %q", got, want)
	}
}
