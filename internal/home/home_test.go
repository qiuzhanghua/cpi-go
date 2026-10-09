package home

import (
	"os"
	"path/filepath"
	"testing"
)

// Resolve 的三档优先级：--dir > $GPM_HOME > 当前目录。
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
