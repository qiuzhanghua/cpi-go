package home

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// clearToolchainEnv 把工具链的家的环境变量清掉，让每条用例从"没人提示"开始。
func clearToolchainEnv(t *testing.T) {
	t.Helper()
	t.Setenv("COT_HOME", "")
	t.Setenv("TDP_HOME", "")
}

// 卸载最后一个包时靠这个判断"这个家是不是归工具链管"。
func TestActiveToolchainEnv(t *testing.T) {
	clearToolchainEnv(t)
	if name, val, ok := ActiveToolchainEnv(); ok {
		t.Fatalf("两个变量都空却报有：%q %q", name, val)
	}

	t.Setenv("TDP_HOME", "/tmp/tdp-home")
	if name, val, ok := ActiveToolchainEnv(); !ok || name != "TDP_HOME" || val != "/tmp/tdp-home" {
		t.Fatalf("拿到 %q=%q ok=%v，想要 TDP_HOME=/tmp/tdp-home true", name, val, ok)
	}

	t.Setenv("COT_HOME", "  /tmp/cot-home  ")
	if name, val, ok := ActiveToolchainEnv(); !ok || name != "COT_HOME" || val != "/tmp/cot-home" {
		t.Fatalf("拿到 %q=%q ok=%v，想要 COT_HOME=/tmp/cot-home true（前后空白要 trim）", name, val, ok)
	}

	t.Setenv("COT_HOME", "   ")
	if name, val, ok := ActiveToolchainEnv(); !ok || name != "TDP_HOME" {
		t.Fatalf("全是空白该轮到 TDP_HOME，拿到 %q=%q ok=%v", name, val, ok)
	}
}

// Resolve 的优先级：--dir > 从自己的位置推断 > $COT_HOME > $TDP_HOME > 当前目录。
func TestResolvePriority(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cotHome := t.TempDir()
	tdpHome := t.TempDir()
	explicit := t.TempDir()

	t.Run("--dir 压过 $COT_HOME", func(t *testing.T) {
		stubSelf(t, filepath.Join(t.TempDir(), "gpm-dist", "gpm"), nil)
		t.Setenv("COT_HOME", cotHome)
		h, err := Resolve(explicit)
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != explicit {
			t.Fatalf("Root = %q，想要 %q", h.Root, explicit)
		}
	})

	t.Run("没有 --dir 就用 $COT_HOME", func(t *testing.T) {
		stubSelf(t, filepath.Join(t.TempDir(), "gpm-dist", "gpm"), nil)
		clearToolchainEnv(t)
		t.Setenv("COT_HOME", cotHome)
		h, err := Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != cotHome {
			t.Fatalf("Root = %q，想要 %q", h.Root, cotHome)
		}
	})

	t.Run("$TDP_HOME 排在 $COT_HOME 后面", func(t *testing.T) {
		stubSelf(t, filepath.Join(t.TempDir(), "gpm-dist", "gpm"), nil)
		clearToolchainEnv(t)
		t.Setenv("TDP_HOME", tdpHome)
		h, err := Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != tdpHome {
			t.Fatalf("Root = %q，想要 %q", h.Root, tdpHome)
		}
	})

	// 这一条是 v3.3 的重点：gpm 不该自己发明 ~/ad 这样的默认值。
	t.Run("什么都没给就落在当前目录", func(t *testing.T) {
		clearToolchainEnv(t)
		stubSelf(t, filepath.Join(t.TempDir(), "gpm-dist", "gpm"), nil)
		h, err := Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != wd {
			t.Fatalf("Root = %q，想要当前目录 %q", h.Root, wd)
		}
		if home, err := os.UserHomeDir(); err == nil && h.Root == home {
			t.Fatalf("Root = %q，落到了用户家目录", h.Root)
		}
	})

	t.Run("相对路径按当前目录展开", func(t *testing.T) {
		clearToolchainEnv(t)
		h, err := Resolve("sub/dir")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(wd, "sub", "dir")
		if h.Root != want {
			t.Fatalf("Root = %q，想要 %q", h.Root, want)
		}
	})
}

// ResolveInstall 的优先级（D21）：
// --dir > requires 指出的家 > 从自己的位置推断 > 平台数据目录/<简称> > 当前目录。
func TestResolveInstallPriority(t *testing.T) {
	cotHome := t.TempDir()
	tdpHome := t.TempDir()
	explicit := t.TempDir()

	t.Run("--dir 压过 requires", func(t *testing.T) {
		t.Setenv("COT_HOME", cotHome)
		h, err := ResolveInstall(explicit, "ad", []string{"cot"})
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != explicit {
			t.Fatalf("Root = %q，想要 %q", h.Root, explicit)
		}
	})

	t.Run("requires 指到哪就装到哪", func(t *testing.T) {
		clearToolchainEnv(t)
		t.Setenv("TDP_HOME", tdpHome)
		h, err := ResolveInstall("", "ad", []string{"tdp"})
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != tdpHome {
			t.Fatalf("Root = %q，想要 %q", h.Root, tdpHome)
		}
	})

	t.Run("没有 requires 也没有环境提示就装进平台数据目录/<简称>", func(t *testing.T) {
		clearToolchainEnv(t)
		stubSelf(t, filepath.Join(t.TempDir(), "gpm-dist", "gpm"), nil)
		fakeHome := t.TempDir()
		stubUserHome(t, fakeHome)

		base, ok := PlatformDataDir()
		if !ok {
			t.Skip("这台机器上问不出平台数据目录")
		}
		h, err := ResolveInstall("", "ad", nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(base, "ad"); h.Root != want {
			t.Fatalf("Root = %q，想要 %q", h.Root, want)
		}
	})

	t.Run("环境提示压过平台数据目录", func(t *testing.T) {
		clearToolchainEnv(t)
		stubSelf(t, filepath.Join(t.TempDir(), "gpm-dist", "gpm"), nil)
		stubUserHome(t, t.TempDir())
		t.Setenv("COT_HOME", cotHome)

		h, err := ResolveInstall("", "ad", []string{"cot"})
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != cotHome {
			t.Fatalf("Root = %q，想要 %q", h.Root, cotHome)
		}
	})
}

// 没有 requires 的包不带工具链，家就在平台惯例的位置：macOS 是
// ~/Library/Application Support，Linux 是 ~/.local/share。
func TestPlatformDataDir(t *testing.T) {
	fakeHome := t.TempDir()
	stubUserHome(t, fakeHome)
	if runtime.GOOS == "windows" {
		// 不然 PlatformDataDir 会拿到真 %LOCALAPPDATA%，跟上面那个假 HOME 对不上。
		t.Setenv("LOCALAPPDATA", "")
	}

	base, ok := PlatformDataDir()
	if !ok {
		t.Fatal("PlatformDataDir 说有用户目录却返回了 false")
	}
	if !strings.HasPrefix(base, fakeHome) {
		t.Errorf("base = %q，不在 %q 之下", base, fakeHome)
	}
	switch runtime.GOOS {
	case "darwin":
		if want := filepath.Join(fakeHome, "Library", "Application Support"); base != want {
			t.Errorf("base = %q，想要 %q", base, want)
		}
	case "windows":
		if !strings.Contains(base, "AppData") && strings.TrimSpace(os.Getenv("LOCALAPPDATA")) == "" {
			t.Errorf("base = %q，不像 Windows 的数据目录", base)
		}
	}
}

// 问不出用户目录时，平台数据目录这条路就走不通（调用方会落到当前目录）。
func TestPlatformDataDirWithoutUserHome(t *testing.T) {
	stubUserHome(t, "")
	if base, ok := PlatformDataDir(); ok || base != "" {
		t.Fatalf("PlatformDataDir = %q/%v，想要空/false", base, ok)
	}
}

// RequireHome 先认环境变量，再落到 ~/<名字>。
func TestRequireHome(t *testing.T) {
	clearToolchainEnv(t)
	fakeHome := t.TempDir()
	stubUserHome(t, fakeHome)

	if got, want := RequireHome("cot"), filepath.Join(fakeHome, "cot"); got != want {
		t.Errorf("cot = %q，想要 %q", got, want)
	}
	t.Setenv("COT_HOME", "/somewhere/else")
	if got := RequireHome("cot"); got != "/somewhere/else" {
		t.Errorf("cot = %q，想要 $COT_HOME 给的 /somewhere/else", got)
	}
}

// stubUserHome 把「用户家目录在哪儿」换成给定答案。
func stubUserHome(t *testing.T, dir string) {
	t.Helper()
	old := userHome
	userHome = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userHome = old })
}

// stubSelf 把「gpm 现在在哪儿」换成给定答案。
func stubSelf(t *testing.T, exe string, err error) {
	t.Helper()
	old := selfExecutable
	selfExecutable = func() (string, error) { return exe, err }
	t.Cleanup(func() { selfExecutable = old })
}

// fakeRoot 造一个"家目录已经建好"的样子：bin/gpm 与账本（<家目录名>-state.json）。
func fakeRoot(t *testing.T, exeName string) (root string, exe string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir()) // macOS 的 /var → /private/var
	if err != nil {
		root = t.TempDir()
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe = filepath.Join(root, "bin", exeName)
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile((&Home{Root: root}).LedgerPath(), []byte(`{"packages":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, exe
}

// 这一条是 O11 的答案：<家目录>/bin/gpm 这个位置本身就把家目录说出来了，
// 用户在新终端里敲 gpm list 不必带 --dir，也不必让 shell 记着 COT_HOME。
func TestResolveFromSelfLocation(t *testing.T) {
	root, exe := fakeRoot(t, "gpm")
	stubSelf(t, exe, nil)
	clearToolchainEnv(t)

	h, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if h.Root != root {
		t.Fatalf("Root = %q，想要从自己的位置推断出的 %q", h.Root, root)
	}
}

// 「自己住在哪个家里」是比环境变量更具体的证据：用户敲的是
// `<某个家>/bin/gpm list`，他要看的就是那个家。
func TestResolveSelfLocationBeatsEnv(t *testing.T) {
	root, exe := fakeRoot(t, "gpm")
	stubSelf(t, exe, nil)
	clearToolchainEnv(t)
	t.Setenv("COT_HOME", t.TempDir())

	h, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if h.Root != root {
		t.Fatalf("Root = %q，想要从自己的位置推断出的 %q", h.Root, root)
	}
}

func TestResolveSelfLocationIsNotEverything(t *testing.T) {
	_, exe := fakeRoot(t, "gpm")
	stubSelf(t, exe, nil)

	t.Run("--dir 仍然压过推断", func(t *testing.T) {
		other := t.TempDir()
		h, err := Resolve(other)
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != other {
			t.Fatalf("Root = %q，想要 --dir 给的 %q", h.Root, other)
		}
	})

	t.Run("推断不出来时才轮到 $COT_HOME", func(t *testing.T) {
		other := t.TempDir()
		t.Setenv("COT_HOME", other)
		// 换一个不叫 bin、也没有账本的位置：推断失效。
		stubSelf(t, filepath.Join(t.TempDir(), "gpm-dist", "gpm"), nil)
		h, err := Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != other {
			t.Fatalf("Root = %q，想要 $COT_HOME 给的 %q", h.Root, other)
		}
	})
}

// 三条判据各自都能否掉一个「看着像但不是」的位置。
func TestRootFromSelfRejectsLookalikes(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("所在目录不叫 bin", func(t *testing.T) {
		root, exe := fakeRoot(t, "gpm")
		moved := filepath.Join(root, "gpm-dist", "gpm")
		if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(exe, moved); err != nil {
			t.Fatal(err)
		}
		stubSelf(t, moved, nil)
		clearToolchainEnv(t)
		if _, ok := RootFromSelf(); ok {
			t.Fatal("bin 之外的 gpm 不该被当成家目录的线索")
		}
		h, err := Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != wd {
			t.Fatalf("Root = %q，想要回落到当前目录 %q", h.Root, wd)
		}
	})

	t.Run("文件名不叫 gpm", func(t *testing.T) {
		_, exe := fakeRoot(t, "ad")
		stubSelf(t, exe, nil)
		if _, ok := RootFromSelf(); ok {
			t.Fatal("启动器不该被当成 gpm 自己")
		}
	})

	// 这条是给 /usr/local/bin/gpm 这类地方准备的：目录确实叫 bin，
	// 但上一级没有 gpm 的账本 —— 那不是它的家。
	t.Run("上一级没有账本", func(t *testing.T) {
		tmp := t.TempDir()
		if err := os.MkdirAll(filepath.Join(tmp, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		exe := filepath.Join(tmp, "bin", "gpm")
		if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		stubSelf(t, exe, nil)
		if _, ok := RootFromSelf(); ok {
			t.Fatal("没有账本的地方不该被当成家目录")
		}
	})

	// 升级路径：v3.6 及以前的家只有 state.json，第一声 gpm list 还得认出它。
	t.Run("上一级只有旧账本 state.json", func(t *testing.T) {
		tmp := t.TempDir()
		if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
			tmp = resolved // macOS 的 /var → /private/var
		}
		if err := os.MkdirAll(filepath.Join(tmp, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		exe := filepath.Join(tmp, "bin", "gpm")
		if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "state.json"), []byte(`{"packages":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		stubSelf(t, exe, nil)
		root, ok := RootFromSelf()
		if !ok {
			t.Fatal("旧账本也说明这里是 gpm 的家")
		}
		if root != tmp {
			t.Fatalf("Root = %q，想要 %q", root, tmp)
		}
	})

	t.Run("取不到自己的路径", func(t *testing.T) {
		stubSelf(t, "", errors.New("无可奉告"))
		if _, ok := RootFromSelf(); ok {
			t.Fatal("连自己在哪都不知道时不该猜")
		}
	})
}

func TestHomeLayout(t *testing.T) {
	root := t.TempDir()
	h, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		got, want string
	}{
		{"Bin", h.Bin(), filepath.Join(root, "bin")},
		{"Lib", h.Lib(), filepath.Join(root, "lib")},
		{"Staging", h.Staging(), filepath.Join(root, "staging")},
		{"LedgerPath", h.LedgerPath(), filepath.Join(root, filepath.Base(root)+"-state.json")},
		{"LegacyLedgerPath", h.LegacyLedgerPath(), filepath.Join(root, "state.json")},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q，想要 %q", c.name, c.got, c.want)
		}
	}

	if got, want := h.Platform(), h.GOOS+"_"+h.GOARCH; got != want {
		t.Errorf("Platform = %q，想要 %q", got, want)
	}
	if got, want := h.AppDir("AI Desk.app"),
		filepath.Join(root, "AI Desk.app"); got != want {
		t.Errorf("AppDir = %q，想要 %q", got, want)
	}
}

func TestEnsureCreatesSkeleton(t *testing.T) {
	h, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{h.Bin(), h.Lib(), h.Staging()} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatalf("%s 没建起来：%v", d, err)
		}
		if !fi.IsDir() {
			t.Errorf("%s 不是目录", d)
		}
	}
	if _, err := os.Stat(h.LedgerPath()); !os.IsNotExist(err) {
		t.Errorf("Ensure 不该顺手建账本：%v", err)
	}
}
