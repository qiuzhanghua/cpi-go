//go:build darwin || linux

package install

import (
	"os"
	"path/filepath"
	"testing"
)

// readSelfVersion 是唯一真的会去执行别人文件的地方，这条用例不替掉它。
//
// 用一个 shell 脚本扮演"家里那份 gpm"：Windows 上造不出这样的夹具，所以这条
// 用例按平台分文件 —— 与 find_linux_test.go / running_test.go 同一手法。
func TestReadSelfVersionRunsTheFile(t *testing.T) {
	dir := t.TempDir()
	script := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// 认得的输出（老版本打的就是这一行）
	p := script("good", "#!/bin/sh\n[ \"$1\" = \"--version\" ] || exit 3\necho 'gpm 0.6.0'\n")
	if v, ok := readSelfVersion(p); !ok || v.String() != "0.6.0" {
		t.Errorf("问出来的版本 = %q ok=%v，想要 0.6.0/true", v.String(), ok)
	}

	// 跑得起来、但输出不是版本号
	p = script("vague", "#!/bin/sh\necho '我不知道'\n")
	if v, ok := readSelfVersion(p); ok {
		t.Errorf("输出认不出来时该返回 ok=false，却拿到 %q", v.String())
	}

	// 跑得起来、也像版本号，但参数没被当 --version（老到不认这个参数的那种）
	p = script("oldflag", "#!/bin/sh\necho '用法: gpm ...'\nexit 1\n")
	if v, ok := readSelfVersion(p); ok {
		t.Errorf("退出码非 0 时该返回 ok=false，却拿到 %q", v.String())
	}

	// 根本不是一个能执行的东西
	p = filepath.Join(dir, "garbage")
	if err := os.WriteFile(p, []byte("这不是可执行文件\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v, ok := readSelfVersion(p); ok {
		t.Errorf("跑不起来时该返回 ok=false，却拿到 %q", v.String())
	}

	// 不存在
	if v, ok := readSelfVersion(filepath.Join(dir, "nope")); ok {
		t.Errorf("文件不存在时该返回 ok=false，却拿到 %q", v.String())
	}
}
