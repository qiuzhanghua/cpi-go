package integrate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopEntry(t *testing.T) {
	got := DesktopEntry("AI Desk", "/home/q/ad/bin/ad", "")

	for _, want := range []string{
		"[Desktop Entry]\n",
		"Type=Application\n",
		"Name=AI Desk\n",
		`Exec="/home/q/ad/bin/ad"` + "\n",
		"Terminal=false\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("生成的 .desktop 里没有 %q：\n%s", want, got)
		}
	}
	if strings.Contains(got, "Icon=") {
		t.Errorf("没给图标却写了 Icon=：\n%s", got)
	}
	if !strings.Contains(DesktopEntry("X", "/bin/x", "/tmp/i.png"), "Icon=/tmp/i.png\n") {
		t.Error("给了图标却没写进 Icon=")
	}
}

// TestDesktopQuoteFollowsTheSpec 守的是 Desktop Entry 规范而不是 shell 规则。
// 这里错了，桌面项的启动会静默失败 —— 终端里连一行报错都看不到。
func TestDesktopQuoteFollowsTheSpec(t *testing.T) {
	cases := []struct{ in, want string }{
		{`/plain/path`, `"/plain/path"`},
		{`/has space/ad`, `"/has space/ad"`},
		{`/q"uote`, `"/q\"uote"`},
		{`/back` + "`" + `tick`, `"/back\` + "`" + `tick"`},
		{`/dol$lar`, `"/dol\$lar"`},
		{`/back\slash`, `"/back\\slash"`},
		// 规范说这四个之外的字符不加反斜杠 —— 比如单引号就是普通字符。
		{`/single'quote`, `"/single'quote"`},
	}
	for _, c := range cases {
		if got := desktopQuote(c.in); got != c.want {
			t.Errorf("desktopQuote(%q) = %s，想要 %s", c.in, got, c.want)
		}
	}
}

func TestLinuxDataHomeHonoursXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	if got, want := LinuxDataHome("/home/q"), filepath.Join("/home/q", ".local", "share"); got != want {
		t.Errorf("没有 XDG_DATA_HOME 时 = %q，想要 %q", got, want)
	}

	// 设了就得听：忽略它会把 .desktop 写进用户根本没在看的目录，
	// 表现就是「装好了但应用列表里找不到」。
	t.Setenv("XDG_DATA_HOME", "/custom/xdg")
	if got := LinuxDataHome("/home/q"); got != "/custom/xdg" {
		t.Errorf("有 XDG_DATA_HOME 时 = %q，想要 /custom/xdg", got)
	}
}

func TestEntryPaths(t *testing.T) {
	t.Run("macOS 图形入口", func(t *testing.T) {
		want := filepath.Join("/Users/q", "Applications", "AI Desk.app")
		if got := MacAppLinkPath("/Users/q", "AI Desk"); got != want {
			t.Errorf("= %q，想要 %q", got, want)
		}
	})

	t.Run("Linux 桌面项", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		want := filepath.Join("/home/q", ".local", "share", "applications", "ai-desk.desktop")
		if got := DesktopEntryPath("/home/q", "ai-desk"); got != want {
			t.Errorf("= %q，想要 %q", got, want)
		}
	})

	t.Run("Windows 开始菜单快捷方式", func(t *testing.T) {
		appData := `C:\Users\q\AppData\Roaming`
		dir := WindowsStartMenuDir(appData)
		for _, part := range []string{"Microsoft", "Windows", "Start Menu", "Programs", "cpi"} {
			if !strings.Contains(dir, part) {
				t.Errorf("开始菜单目录 %q 里少了 %q", dir, part)
			}
		}
		got := WindowsShortcutPath(appData, "AI Desk")
		if !strings.HasSuffix(got, "AI Desk.lnk") {
			t.Errorf("= %q，想要以 \"AI Desk.lnk\" 结尾", got)
		}
	})
}
