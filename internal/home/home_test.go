package home

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Resolve 的优先级：--dir > $GPM_HOME > 从自己的位置推断 > 当前目录。
func TestResolvePriority(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	gpmHome := t.TempDir()
	explicit := t.TempDir()

	t.Run("--dir 压过 $GPM_HOME", func(t *testing.T) {
		t.Setenv("GPM_HOME", gpmHome)
		h, err := Resolve(explicit)
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != explicit {
			t.Fatalf("Root = %q，想要 %q", h.Root, explicit)
		}
	})

	t.Run("没有 --dir 就用 $GPM_HOME", func(t *testing.T) {
		t.Setenv("GPM_HOME", gpmHome)
		h, err := Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != gpmHome {
			t.Fatalf("Root = %q，想要 %q", h.Root, gpmHome)
		}
	})

	// 这一条是 v3.3 的重点：gpm 不该自己发明 ~/ad 这样的默认值。
	t.Run("两个都没给就落在当前目录", func(t *testing.T) {
		t.Setenv("GPM_HOME", "")
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
		t.Setenv("GPM_HOME", "")
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

// stubSelf 把「gpm 现在在哪儿」换成给定答案。
func stubSelf(t *testing.T, exe string, err error) {
	t.Helper()
	old := selfExecutable
	selfExecutable = func() (string, error) { return exe, err }
	t.Cleanup(func() { selfExecutable = old })
}

// fakeRoot 造一个"家目录已经建好"的样子：bin/gpm 与 state.json。
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
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"packages":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, exe
}

// 这一条是 O11 的答案：<家目录>/bin/gpm 这个位置本身就把家目录说出来了，
// 用户在新终端里敲 gpm list 不必带 --dir，也不必让 shell 记着 GPM_HOME。
func TestResolveFromSelfLocation(t *testing.T) {
	root, exe := fakeRoot(t, "gpm")
	stubSelf(t, exe, nil)
	t.Setenv("GPM_HOME", "")

	h, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if h.Root != root {
		t.Fatalf("Root = %q，想要从自己的位置推断出的 %q", h.Root, root)
	}
}

func TestResolveSelfLocationIsTheLastResort(t *testing.T) {
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

	t.Run("$GPM_HOME 仍然压过推断", func(t *testing.T) {
		other := t.TempDir()
		t.Setenv("GPM_HOME", other)
		h, err := Resolve("")
		if err != nil {
			t.Fatal(err)
		}
		if h.Root != other {
			t.Fatalf("Root = %q，想要 $GPM_HOME 给的 %q", h.Root, other)
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
		t.Setenv("GPM_HOME", "")
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
	t.Run("上一级没有 state.json", func(t *testing.T) {
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
		{"LedgerPath", h.LedgerPath(), filepath.Join(root, "state.json")},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q，想要 %q", c.name, c.got, c.want)
		}
	}

	if got, want := h.Platform(), h.GOOS+"_"+h.GOARCH; got != want {
		t.Errorf("Platform = %q，想要 %q", got, want)
	}
	if got, want := h.PackageDir("ai-desk", "1.0.0"),
		filepath.Join(root, "lib", "ai-desk_1.0.0_"+h.Platform()); got != want {
		t.Errorf("PackageDir = %q，想要 %q", got, want)
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
