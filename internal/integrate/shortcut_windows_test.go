//go:build windows

package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateShortcutRoundTrip 写一个 .lnk 再读回来，逐项比对。
//
// 这个测试之所以值得写，是因为 createShortcut 是手搓的 COM vtable 调用：
// 下标写错一个，调到的就是另一个方法 —— 编译照过，写文件甚至不一定报错。
// 「写进去再读出来一模一样」是唯一能证明那串下标对了的办法。
func TestCreateShortcutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lnk := filepath.Join(dir, "AI Desk.lnk")

	exe := filepath.Join(dir, "ai-desk.exe")
	want := Shortcut{
		Target:      exe,
		Arguments:   `--open "some file.txt"`,
		WorkingDir:  dir,
		Description: "AI Desk",
	}
	if err := createShortcut(lnk, want); err != nil {
		t.Fatalf("createShortcut: %v", err)
	}

	fi, err := os.Stat(lnk)
	if err != nil {
		t.Fatalf("没写出 .lnk：%v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("写出一个 0 字节的 .lnk")
	}

	got, err := readShortcut(lnk)
	if err != nil {
		t.Fatalf("readShortcut: %v", err)
	}
	if !strings.EqualFold(got.Target, want.Target) {
		t.Errorf("Target = %q，想要 %q", got.Target, want.Target)
	}
	if got.Arguments != want.Arguments {
		t.Errorf("Arguments = %q，想要 %q", got.Arguments, want.Arguments)
	}
	if !strings.EqualFold(got.WorkingDir, want.WorkingDir) {
		t.Errorf("WorkingDir = %q，想要 %q", got.WorkingDir, want.WorkingDir)
	}
	if got.Description != want.Description {
		t.Errorf("Description = %q，想要 %q", got.Description, want.Description)
	}
}

// TestAppLinkWindowsCreatesStartMenuShortcut 走一遍完整的 Windows 图形入口。
//
// APPDATA 被 t.Setenv 指到临时目录，所以不会真的往这台机器的开始菜单里塞东西。
func TestAppLinkWindowsCreatesStartMenuShortcut(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("APPDATA", appData)

	binDir := filepath.Join(home, "ad", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(binDir, "ad.cmd")
	if err := os.WriteFile(launcher, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entryAbs := filepath.Join(home, "ad", "lib", "ai-desk_0.1.0_windows_amd64", "ai-desk.exe")

	link, note, err := AppLink("windows", home, "ai-desk", "AI Desk", entryAbs, launcher)
	if err != nil {
		t.Fatalf("AppLink: %v", err)
	}
	if note != "" {
		t.Logf("备注：%s", note)
	}
	if link == nil {
		t.Fatal("AppLink 没给出账本记录")
	}

	want := WindowsShortcutPath(appData, "AI Desk")
	if link.Path != want {
		t.Errorf("路径 = %q，想要 %q", link.Path, want)
	}
	if link.Kind != "start-menu-lnk" {
		t.Errorf("Kind = %q，想要 start-menu-lnk", link.Kind)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("快捷方式没写出来：%v", err)
	}

	got, err := readShortcut(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got.Target, entryAbs) {
		t.Errorf("快捷方式指向 %q，想要 %q", got.Target, entryAbs)
	}
	if got.Description != "AI Desk" {
		t.Errorf("Description = %q，想要 AI Desk", got.Description)
	}

	// 第二次必须不覆盖，而是回一条提示 —— 用户自己放的入口不能被我们盖掉。
	link2, note2, err := AppLink("windows", home, "ai-desk", "AI Desk", entryAbs, launcher)
	if err != nil {
		t.Fatalf("AppLink（第二次）: %v", err)
	}
	if link2 != nil {
		t.Error("第二次 AppLink 又建了一个入口")
	}
	if note2 == "" {
		t.Error("第二次 AppLink 应该提示已存在")
	}
}

// TestUserPathRoundTrip 真的去改一次 HKCU\Environment 的 Path，再放回原样。
//
// 它动的是这台机器真实的用户 PATH，所以默认跳过，只在一次性的 CI 机器上打开：
//
//	GPM_TEST_REGISTRY=1 go test ./internal/integrate/
func TestUserPathRoundTrip(t *testing.T) {
	if os.Getenv("GPM_TEST_REGISTRY") != "1" {
		t.Skip("会改动真实的用户 PATH；设 GPM_TEST_REGISTRY=1 才跑（CI 上开）")
	}

	h, err := openEnvironmentKey()
	if err != nil {
		t.Fatal(err)
	}
	defer procRegCloseKey.Call(uintptr(h))

	orig, origType, err := readEnvironmentPath(h)
	if err != nil {
		t.Fatal(err)
	}
	// 无论测试怎么结束都把原值放回去。原值本来不存在时这里会写回一个空值 ——
	// 对 PATH 的语义来说「空值」和「没有这个值」是一样的。
	t.Cleanup(func() {
		h, err := openEnvironmentKey()
		if err != nil {
			t.Errorf("恢复 PATH 失败：%v", err)
			return
		}
		defer procRegCloseKey.Call(uintptr(h))
		if err := writeEnvironmentPath(h, orig, origType); err != nil {
			t.Errorf("恢复 PATH 失败：%v", err)
		}
	})

	binDir := filepath.Join(t.TempDir(), "ad", "bin")

	changed, err := InstallWindowsPath(binDir)
	if err != nil {
		t.Fatalf("InstallWindowsPath: %v", err)
	}
	if !changed {
		t.Error("第一次装却说没改动 PATH")
	}

	got, gotType, err := readEnvironmentPath(h)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(binDir)) {
		t.Errorf("加完之后的 PATH 没有以 %q 开头：%q", binDir, got)
	}
	if gotType != origType {
		t.Errorf("值类型被改了：原来是 %s，现在是 %s", regTypeName(origType), regTypeName(gotType))
	}

	// 再装一次必须什么都不做：这是「装一百次也不会越积越长」的那条保证。
	changed, err = InstallWindowsPath(binDir)
	if err != nil {
		t.Fatalf("InstallWindowsPath（第二次）: %v", err)
	}
	if changed {
		t.Error("同一个 binDir 装了两次，第二次又改了 PATH")
	}

	changed, err = RemoveWindowsPath(binDir)
	if err != nil {
		t.Fatalf("RemoveWindowsPath: %v", err)
	}
	if !changed {
		t.Error("摘的时候说没改动")
	}

	got, _, err = readEnvironmentPath(h)
	if err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Errorf("摘完之后没回到原样：\n  原值 %q\n  现在 %q", orig, got)
	}
}

func regTypeName(typ uint32) string {
	switch typ {
	case regSZ:
		return "REG_SZ"
	case regExpandSZ:
		return "REG_EXPAND_SZ"
	}
	return "未知类型"
}
