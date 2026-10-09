//go:build linux

package proc

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeProc 搭一棵长得像 /proc 的目录树，好让 readProc 在任何机器上都能测。
//
// exe 必须是**软链**（真的 /proc 里就是），cmdline 是普通文件。
func fakeProc(t *testing.T, links map[string]string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, target := range links {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestReadProcPrefersExeAndFallsBackToArgvZero(t *testing.T) {
	const pkg = "/home/q/ad/lib/demo_0.1.0_linux_amd64"
	exe := filepath.Join(pkg, "demo")

	root := fakeProc(t,
		map[string]string{
			// 正主：exe 指在包里。
			"1234/exe": exe,
			// AppImage：exe 在内核给的临时挂载点里，只有 argv[0] 认得原位置。
			"2345/exe": "/tmp/.mount_demoXXXX/usr/bin/demo",
			// 幽灵：exe 带 " (deleted)" 尾巴。
			"3456/exe": exe + " (deleted)",
			// 无关进程。
			"4567/exe": "/usr/bin/other",
			// /proc 底下不都是进程目录，而且没有 exe 的更常见。
			"uptime/exe": "/nope",
		},
		map[string]string{
			"1234/cmdline":   exe + "\x00--flag\x00",
			"2345/cmdline":   exe + "\x00",
			"3456/cmdline":   exe + "\x00",
			"4567/cmdline":   "/usr/bin/other\x00",
			"uptime/cmdline": "99.9 1.0\x00",
		})

	found, err := readProc(root, pkg)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]string{}
	for _, p := range found {
		got[p.PID] = p.Exe
	}
	if len(got) != 3 {
		t.Fatalf("该找到 1234/2345/3456 三个，得到 %v", got)
	}
	if got[1234] != exe {
		t.Errorf("1234 的路径认错了：%q", got[1234])
	}
	if got[2345] != exe {
		t.Errorf("AppImage 那一份该退回到 argv[0]，得到 %q", got[2345])
	}
	if got[3456] != exe {
		t.Errorf("幽灵进程的 (deleted) 尾巴没被去掉：%q", got[3456])
	}
}

func TestReadProcMissingRootIsAnError(t *testing.T) {
	if _, err := readProc(filepath.Join(t.TempDir(), "没有这个"), "/x"); err == nil {
		t.Error("目录不存在时该报错，好让调用方打印提示后继续")
	}
}
