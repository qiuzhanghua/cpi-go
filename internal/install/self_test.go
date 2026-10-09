package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/qiuzhanghua/gpm-go/internal/home"
	"github.com/qiuzhanghua/gpm-go/internal/ledger"
)

func selfName() string {
	if runtime.GOOS == "windows" {
		return "gpm.exe"
	}
	return "gpm"
}

// 空 bin/ 时，gpm 把自己拷一份过去。
func TestInstallSelfCopiesWhenAbsent(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	dest, copied, keptOld, err := installSelf(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !copied || keptOld {
		t.Fatalf("copied=%v keptOld=%v，想要 true/false", copied, keptOld)
	}
	if want := filepath.Join(bin, selfName()); dest != want {
		t.Fatalf("dest = %q，想要 %q", dest, want)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() == 0 {
		t.Errorf("%s 是空的", dest)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s 没有可执行位：%v", dest, fi.Mode().Perm())
	}
}

// bin/ 里本来就有 gpm 时，一个字节都不动，也不认领它。
func TestInstallSelfKeepsExistingGpm(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(bin, selfName())
	const sentinel = "这是用户自己的 gpm，别动\n"
	if err := os.WriteFile(dest, []byte(sentinel), 0o755); err != nil {
		t.Fatal(err)
	}

	got, copied, keptOld, err := installSelf(bin)
	if err != nil {
		t.Fatal(err)
	}
	if copied || !keptOld {
		t.Fatalf("copied=%v keptOld=%v，想要 false/true", copied, keptOld)
	}
	if got != dest {
		t.Fatalf("dest = %q，想要 %q", got, dest)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinel {
		t.Fatalf("原有的 gpm 被动过：%q %v", b, err)
	}
}

// 装配目录：<简称>-manifest.yaml + payload/，按当前平台给一个能装的入口。
func assembly(t *testing.T) string {
	t.Helper()
	asm := t.TempDir()
	payload := filepath.Join(asm, "payload")

	var manifest string
	if runtime.GOOS == "darwin" {
		app := filepath.Join(payload, "Demo.app")
		writePayload(t, filepath.Join(app, "Contents", "MacOS", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
		writePayload(t, filepath.Join(app, "Contents", "Info.plist"),
			`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>demo</string></dict></plist>`,
			0o644)
		manifest = "id: demo\nname: Demo\nversion: 0.1.0\nentry:\n  darwin:\n    bundle: Demo.app\nlaunch:\n  cmd: demo\n"
	} else {
		name := "demo"
		if runtime.GOOS == "windows" {
			name = "demo.exe"
		}
		writePayload(t, filepath.Join(payload, name), "x", 0o755)
		manifest = fmt.Sprintf(
			"id: demo\nname: Demo\nversion: 0.1.0\nentry:\n  %s:\n    exe: %s\nlaunch:\n  cmd: demo\n",
			runtime.GOOS, name)
	}
	writePayload(t, filepath.Join(asm, "demo-manifest.yaml"), manifest, 0o644)
	return asm
}

func writePayload(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// 装完之后账本里的 Self 只指向「我们自己放进去的那一份」。
func TestInstallRecordsSelfOnlyWhenCopied(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir()) // 别碰真 ~/Applications 与 shell 配置
	}
	root := t.TempDir()
	if err := Install(assembly(t), Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.Bin(), selfName())
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("装完没有 %s：%v", dest, err)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if led.Self != dest {
		t.Fatalf("账本 Self = %q，想要 %q", led.Self, dest)
	}
	if len(led.Packages) != 1 || led.Packages[0].ID != "demo" {
		t.Fatalf("账本内容不对：%+v", led.Packages)
	}
}

// 用户那儿本来就有 gpm：不覆盖、不进账本（于是卸载时也不会删掉它）。
func TestInstallKeepsForeignGpmOutOfLedger(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir())
	}
	root := t.TempDir()

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.Bin(), selfName())
	const sentinel = "用户自己的 gpm\n"
	if err := os.WriteFile(dest, []byte(sentinel), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Install(assembly(t), Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}

	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinel {
		t.Fatalf("原有的 gpm 被覆盖了：%q %v", b, err)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if led.Self != "" {
		t.Fatalf("账本 Self = %q，想要空（那一份不是我们的）", led.Self)
	}

	// 卸载（最后一个包）之后它还得在。
	if err := Uninstall(root, "demo", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinel {
		t.Fatalf("卸载把用户自己的 gpm 删了/改了：%q %v", b, err)
	}
}
