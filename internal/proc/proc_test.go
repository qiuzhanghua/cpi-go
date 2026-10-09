package proc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnder(t *testing.T) {
	cases := []struct {
		dir, path string
		want      bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b", "/a/b/c", true},
		{"/a/b", "/a/b/c/d/e", true},
		{"/a/b/", "/a/b/c", true}, // Clean 之后是一样的
		{"/a/b", "/a/bc", false},  // 前缀对上了但不是一层
		{"/a/b", "/a", false},
		{"/a/b", "/x/a/b/c", false},
		{"/a/b", "/a/b-1/c", false},
		{"", "/a/b", false},
		{"/a/b", "", false},
	}
	for _, c := range cases {
		if got := under(c.dir, c.path); got != c.want {
			t.Errorf("under(%q, %q) = %v，想要 %v", c.dir, c.path, got, c.want)
		}
	}
}

func TestUnderCaseInsensitiveOnlyOnDarwinAndWindows(t *testing.T) {
	got := under("/A/B", "/a/b/c")
	want := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	if got != want {
		t.Errorf("under(/A/B, /a/b/c) 在 %s 上是 %v，想要 %v", runtime.GOOS, got, want)
	}
}

func TestTrimDeleted(t *testing.T) {
	cases := map[string]string{
		"/x/y":            "/x/y",
		"/x/y (deleted)":  "/x/y",
		"/x/y (deleted))": "/x/y (deleted))",
		"":                "",
		" (deleted)":      "",
	}
	for in, want := range cases {
		if got := trimDeleted(in); got != want {
			t.Errorf("trimDeleted(%q) = %q，想要 %q", in, got, want)
		}
	}
}

func TestSplitNUL(t *testing.T) {
	got := splitNUL([]byte("/x/y\x00--flag\x00value\x00"))
	want := []string{"/x/y", "--flag", "value"}
	if len(got) != len(want) {
		t.Fatalf("splitNUL 得到 %q，想要 %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitNUL 得到 %q，想要 %q", got, want)
		}
	}
	if n := len(splitNUL(nil)); n != 0 {
		t.Errorf("空 cmdline 应该切出 0 段，得到 %d 段", n)
	}
}

func TestMatch(t *testing.T) {
	// 第一段是空的、第二段才是正主，顺带证明空串不会被当成命中。
	if p, ok := match("/a/b", 7, []string{"", "/a/b/c"}); !ok || p.PID != 7 || p.Exe != "/a/b/c" {
		t.Errorf("match 得到 %+v, %v", p, ok)
	}
	// 幽灵进程：路径带 " (deleted)" 尾巴也要认出来。
	if p, ok := match("/a/b", 8, []string{"/a/b/c (deleted)"}); !ok || p.Exe != "/a/b/c" {
		t.Errorf("幽灵进程没被认出来：%+v, %v", p, ok)
	}
	if _, ok := match("/a/b", 9, []string{"/elsewhere/c"}); ok {
		t.Error("目录外的进程不该命中")
	}
}

func TestPrefixesFollowsSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("这个平台建不了软链：%v", err)
	}

	got := prefixes(filepath.Join(link, "x"))
	if len(got) != 2 {
		t.Fatalf("想要「原样」和「解析后」两份，得到 %q", got)
	}
	if got[0] != filepath.Join(link, "x") {
		t.Errorf("第一份该是原样的路径，得到 %q", got[0])
	}
	// 期望值也要解析一遍：t.TempDir() 在 macOS 上给的是 /var/...，
	// 而 /var 本身是个指向 /private/var 的软链。
	wantReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got[1] != filepath.Join(wantReal, "x") {
		t.Errorf("第二份该是解析后的路径 %q，得到 %q", filepath.Join(wantReal, "x"), got[1])
	}

	// 已经不存在（幽灵进程那个目录早没了）时不能报错，原样返回。
	if got := prefixes(filepath.Join(base, "flying", "x")); len(got) != 1 {
		t.Errorf("目录不存在时该原样返回一份，得到 %q", got)
	}
}
