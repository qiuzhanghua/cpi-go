//go:build darwin || linux

package install

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qiuzhanghua/cpi-go/internal/home"
	"github.com/qiuzhanghua/cpi-go/internal/ledger"
	"github.com/qiuzhanghua/cpi-go/internal/proc"
)

// helperEnv 让被拷出去的那份测试二进制只睡觉：它扮演"正在运行的那个应用"。
const helperEnv = "CPI_INSTALL_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeInstall 搭一个最小可用的家目录：账本里有一个 demo 0.1.0，
// 而它的安装目录里真有一个进程在跑。
func fakeInstall(t *testing.T) (root, pkgDir string, app *exec.Cmd) {
	t.Helper()
	root = t.TempDir()
	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}

	pkgDir = h.PackageDir("demo", "0.1.0")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(pkgDir, "demo")
	copyFile(t, self, exe)

	app = exec.Command(exe, "-test.run=TestNothing")
	app.Env = append(os.Environ(), helperEnv+"=1")
	app.Stdout, app.Stderr = io.Discard, io.Discard
	if err := app.Start(); err != nil {
		t.Fatalf("起不来假应用：%v", err)
	}
	t.Cleanup(func() {
		_ = app.Process.Kill()
		_, _ = app.Process.Wait()
	})
	waitRunning(t, pkgDir, app.Process.Pid)

	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	led.Put(ledger.Package{
		ID:          "demo",
		Name:        "Demo",
		Version:     "0.1.0",
		Platform:    h.Platform(),
		InstalledAt: time.Now(),
		Dir:         pkgDir,
		Entry:       exe,
		EntryKind:   "exe",
		Cmd:         "demo",
		Launcher:    filepath.Join(h.Bin(), "demo"),
	})
	if err := led.Save(h.LedgerPath()); err != nil {
		t.Fatal(err)
	}
	return root, pkgDir, app
}

func TestUninstallRefusesWhileTheAppIsRunning(t *testing.T) {
	root, pkgDir, app := fakeInstall(t)

	var out bytes.Buffer
	err := Uninstall(root, "demo", false, &out)
	if err == nil {
		t.Fatalf("应用（pid %d）正在跑，卸载不该成功。输出：\n%s", app.Process.Pid, out.String())
	}
	text := out.String()
	for _, want := range []string{"Demo 正在运行", "请先退出 Demo", "--force"} {
		if !strings.Contains(text, want) {
			t.Errorf("人话里少了 %q，实际输出：\n%s", want, text)
		}
	}

	// 拦下就得是真的没动手：账本原样，目录原样。
	led, err := ledger.Load(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if led.Find("demo") == nil {
		t.Error("被拦下之后账本不该被改")
	}
	if _, err := os.Stat(filepath.Join(pkgDir, "demo")); err != nil {
		t.Errorf("被拦下之后安装目录不该被动：%v", err)
	}

	// 应用退出之后，同一个命令就该放行。
	_ = app.Process.Kill()
	_, _ = app.Process.Wait()
	waitGone(t, pkgDir)

	out.Reset()
	if err := Uninstall(root, "demo", false, &out); err != nil {
		t.Fatalf("应用已经退出了，卸载该成功：%v\n输出：\n%s", err, out.String())
	}
	led, err = ledger.Load(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if led.Find("demo") != nil {
		t.Error("卸载成功后账本里不该还有 demo")
	}
}

func TestUninstallForceGoesThroughButSaysSo(t *testing.T) {
	root, _, _ := fakeInstall(t)

	var out bytes.Buffer
	if err := Uninstall(root, "demo", true, &out); err != nil {
		t.Fatalf("--force 之后该放行：%v\n输出：\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "警告") || !strings.Contains(out.String(), "--force") {
		t.Errorf("--force 至少得留下一句警告，实际输出：\n%s", out.String())
	}
}

// TestInstallRefusesToOverwriteWhileTheAppIsRunning 走的是完整的一条路：
// 真打一个包、真装一次、真把它跑起来、再真装第二次。
//
// 覆盖安装比卸载更险：cpi 会先把整个包目录 RemoveAll 掉，跑着的那个进程
// 于是抓着一份已经被删掉的文件继续执行新拷进来的同名文件 —— 版本混用。
func TestInstallRefusesToOverwriteWhileTheAppIsRunning(t *testing.T) {
	// 图形入口（~/Applications 或 XDG 的 .desktop）落在真家目录里，
	// 所以把 HOME 也挪到临时目录，别污染开发机。
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(fakeHome, ".local", "share"))

	dist := t.TempDir()
	writeFixture(t, filepath.Join(dist, "manifest.yaml"), `id: demo
name: Demo
version: 0.1.0
entry:
  `+runtime.GOOS+`:
    exe: demo
launch:
  cmd: demo
`)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dist, "payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, self, filepath.Join(dist, "payload", "demo"))

	root := t.TempDir()
	opt := Options{Dir: root, Yes: true, NoPath: true, SkipVerify: true}

	var out bytes.Buffer
	opt.Out = &out
	if err := Install(dist, opt); err != nil {
		t.Fatalf("第一次装就该成功：%v\n输出：\n%s", err, out.String())
	}

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	pkgDir := h.PackageDir("demo", "0.1.0")
	app := exec.Command(filepath.Join(pkgDir, "demo"), "-test.run=TestNothing")
	app.Env = append(os.Environ(), helperEnv+"=1")
	app.Stdout, app.Stderr = io.Discard, io.Discard
	if err := app.Start(); err != nil {
		t.Fatalf("起不来假应用：%v", err)
	}
	t.Cleanup(func() {
		_ = app.Process.Kill()
		_, _ = app.Process.Wait()
	})
	waitRunning(t, pkgDir, app.Process.Pid)

	out.Reset()
	opt.Force = false
	if err := Install(dist, opt); err == nil {
		t.Fatalf("应用（pid %d）正在跑，覆盖安装不该成功。输出：\n%s", app.Process.Pid, out.String())
	}
	for _, want := range []string{"Demo 正在运行", "请先退出 Demo", "--force"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("人话里少了 %q，实际输出：\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(pkgDir, "demo")); err != nil {
		t.Errorf("被拦下之后安装目录不该被动：%v", err)
	}
	if led, err := ledger.Load(h.LedgerPath()); err != nil {
		t.Fatal(err)
	} else if p := led.Find("demo"); p == nil || p.Version != "0.1.0" {
		t.Errorf("被拦下之后账本不该被改，现在记的是 %+v", p)
	}

	out.Reset()
	opt.Force = true
	if err := Install(dist, opt); err != nil {
		t.Fatalf("--force 之后该放行：%v\n输出：\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "警告") {
		t.Errorf("--force 至少得留下一句警告，实际输出：\n%s", out.String())
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitRunning(t *testing.T, dir string, pid int) {
	t.Helper()
	if !eventually(func() bool {
		got, err := proc.Find(dir)
		if err != nil {
			t.Fatalf("proc.Find 出错：%v", err)
		}
		for _, p := range got {
			if p.PID == pid {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("等了 5 秒也没在 %s 里认出进程 %d", dir, pid)
	}
}

func waitGone(t *testing.T, dir string) {
	t.Helper()
	if !eventually(func() bool {
		got, err := proc.Find(dir)
		return err == nil && len(got) == 0
	}) {
		t.Fatalf("等了 5 秒，%s 里的进程还没走干净", dir)
	}
}

func eventually(ok func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ok() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
