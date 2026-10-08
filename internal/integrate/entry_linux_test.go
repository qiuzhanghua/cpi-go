//go:build linux

package integrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAppLinkLinuxWritesDesktopEntry 在真 Linux 上真的写一个 .desktop 出来，
// 再交给 desktop-file-validate 判一遍。
//
// 为什么非要真 Linux：.desktop 的语法、目录约定与「桌面环境到底认不认」，
// 在 macOS 上写多少单测都只能测到字符串本身。
func TestAppLinkLinuxWritesDesktopEntry(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdgdata")
	t.Setenv("XDG_DATA_HOME", xdg)

	binDir := filepath.Join(home, "ad", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(binDir, "ad")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec '/opt/AI Desk/ai-desk' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	link, note, err := AppLink("linux", home, "ai-desk", "AI Desk", "/opt/AI Desk/ai-desk", launcher)
	if err != nil {
		t.Fatalf("AppLink: %v", err)
	}
	if note != "" {
		t.Logf("备注：%s", note)
	}
	if link == nil {
		t.Fatal("AppLink 没给出账本记录")
	}
	if link.Kind != "desktop-entry" {
		t.Errorf("Kind = %q，想要 desktop-entry", link.Kind)
	}

	want := filepath.Join(xdg, "applications", "ai-desk.desktop")
	if link.Path != want {
		t.Errorf("路径 = %q，想要 %q", link.Path, want)
	}
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("桌面项没写出来：%v", err)
	}
	content := string(b)
	for _, needle := range []string{"[Desktop Entry]\n", "Type=Application\n", "Name=AI Desk\n", "Terminal=false\n"} {
		if !strings.Contains(content, needle) {
			t.Errorf("桌面项里没有 %q：\n%s", needle, content)
		}
	}
	if !strings.Contains(content, launcher) {
		t.Errorf("Exec 没有指向启动器 %s：\n%s", launcher, content)
	}

	// 系统里有 desktop-file-validate 就顺手判一下。判据看输出里的 "error:"：
	// 各发行版的 desktop-file-utils 对「只有警告」时的退出码并不一致。
	p, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		t.Log("没装 desktop-file-utils，跳过 desktop-file-validate")
		return
	}
	out, err := exec.Command(p, want).CombinedOutput()
	if err != nil && strings.Contains(string(out), "error:") {
		t.Errorf("desktop-file-validate 不认这个文件：%v\n%s", err, out)
	} else if len(out) > 0 {
		t.Logf("desktop-file-validate 说：%s", out)
	}
}

// 没有 XDG_DATA_HOME 时落到 ~/.local/share，这是绝大多数桌面的默认约定。
func TestAppLinkLinuxWithoutXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")

	binDir := filepath.Join(home, "ad", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(binDir, "ad")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	link, _, err := AppLink("linux", home, "ai-desk", "AI Desk", "/opt/AI Desk/ai-desk", launcher)
	if err != nil {
		t.Fatalf("AppLink: %v", err)
	}
	if link == nil {
		t.Fatal("AppLink 没给出账本记录")
	}
	want := filepath.Join(home, ".local", "share", "applications", "ai-desk.desktop")
	if link.Path != want {
		t.Errorf("路径 = %q，想要 %q", link.Path, want)
	}
}
