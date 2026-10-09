package manifest

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write 写一个文件并建好目录。
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const minimalAD = `id: ai-desk
name: AI Desk
version: 1.2.3
entry:
  darwin:
    bundle: "AI Desk.app"
launch:
  cmd: ad
`

// 清单的名字本身就是简称：`ad-manifest.yaml` → ad。
func TestFileNameFor(t *testing.T) {
	if got, want := FileNameFor("ad"), "ad-manifest.yaml"; got != want {
		t.Errorf("FileNameFor(ad) = %q，想要 %q", got, want)
	}
	if got, want := FileNameFor("sag"), "sag-manifest.yaml"; got != want {
		t.Errorf("FileNameFor(sag) = %q，想要 %q", got, want)
	}
}

func TestDiscover(t *testing.T) {
	t.Run("找到唯一那个", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "ad-manifest.yaml"), minimalAD)

		short, path, err := Discover(dir)
		if err != nil {
			t.Fatal(err)
		}
		if short != "ad" {
			t.Errorf("short = %q，想要 ad", short)
		}
		if path != filepath.Join(dir, "ad-manifest.yaml") {
			t.Errorf("path = %q", path)
		}
	})

	t.Run("目录不算候选", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "ad-manifest.yaml", "inner"), "x")
		if _, _, err := Discover(dir); err == nil {
			t.Fatal("一个叫 xxx-manifest.yaml 的目录被当成清单了")
		}
	})

	t.Run("一个都没有就报错", func(t *testing.T) {
		if _, _, err := Discover(t.TempDir()); err == nil {
			t.Fatal("空目录却找到了清单")
		}
	})

	// 两个候选时绝不替打包方挑一个：清单决定 id、版本、家、命令名。
	t.Run("有两个就报错", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "ad-manifest.yaml"), minimalAD)
		write(t, filepath.Join(dir, "ac-manifest.yaml"), minimalAD)
		_, _, err := Discover(dir)
		if err == nil {
			t.Fatal("两个清单却选了一个")
		}
		for _, want := range []string{"ad-manifest.yaml", "ac-manifest.yaml"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("报错里没提 %s：%v", want, err)
			}
		}
	})
}

// launch.cmd 缺省等于简称：文件名已经把命令名说出来了。
func TestParseDefaultsCmdToShort(t *testing.T) {
	m, err := Parse([]byte("id: ai-desk\nname: AI Desk\nversion: 1\nentry:\n  darwin:\n    bundle: X.app\n"), "ad", "ad-manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Launch.Cmd != "ad" {
		t.Errorf("launch.cmd = %q，想要缺省的 ad", m.Launch.Cmd)
	}
	if m.Short() != "ad" {
		t.Errorf("Short() = %q，想要 ad", m.Short())
	}
}

func TestParseRejectsBadYAML(t *testing.T) {
	if _, err := Parse([]byte("id: [unclosed\n"), "ad", "ad-manifest.yaml"); err == nil {
		t.Fatal("坏 YAML 却解析成功了")
	}
}

func TestValidateRequires(t *testing.T) {
	base := "id: ai-desk\nname: AI Desk\nversion: 1\nentry:\n  %s:\n    exe: ad\nlaunch:\n  cmd: ad\n"

	t.Run("认得的工具链", func(t *testing.T) {
		m, err := Parse([]byte(strings.Replace(base, "%s", "darwin", 1)+"requires: [cot]\n"), "ad", "ad-manifest.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Validate("darwin"); err != nil {
			t.Fatalf("requires: [cot] 被拒了：%v", err)
		}
		if !m.HasRequire("cot") || m.HasRequire("tdp") {
			t.Errorf("HasRequire 不对：cot=%v tdp=%v", m.HasRequire("cot"), m.HasRequire("tdp"))
		}
	})

	// 多一个名字就多一条 gpm 得替别人维护的契约，所以不认识的当场拒。
	t.Run("不认识的工具链", func(t *testing.T) {
		m, err := Parse([]byte(strings.Replace(base, "%s", "darwin", 1)+"requires: [nix]\n"), "ad", "ad-manifest.yaml")
		if err != nil {
			t.Fatal(err)
		}
		err = m.Validate("darwin")
		if err == nil {
			t.Fatal("requires: [nix] 却通过了")
		}
		if !strings.Contains(err.Error(), "nix") {
			t.Errorf("报错里没提 nix：%v", err)
		}
	})

	t.Run("重复的 requires 只算一次", func(t *testing.T) {
		m, err := Parse([]byte(strings.Replace(base, "%s", "darwin", 1)+"requires: [cot, cot]\n"), "ad", "ad-manifest.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Requires) != 1 {
			t.Errorf("requires = %v，想要只有一个 cot", m.Requires)
		}
	})
}

// 清单文件名里的简称与 launch.cmd 必须一致：两个地方说同一个东西，
// 说法不一样时只能报错，不能猜。
func TestValidateCmdMustMatchFileName(t *testing.T) {
	m, err := Parse([]byte(minimalAD), "ac", "ac-manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	err = m.Validate("darwin")
	if err == nil {
		t.Fatal("ac-manifest.yaml 里写 launch.cmd: ad，却通过了")
	}
	for _, want := range []string{"ad", "ac"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错里没提 %q：%v", want, err)
		}
	}
}

func TestValidateEntry(t *testing.T) {
	t.Run("目标平台没有 entry", func(t *testing.T) {
		m, _ := Parse([]byte(minimalAD), "ad", "ad-manifest.yaml")
		if err := m.Validate("windows"); err == nil {
			t.Fatal("windows 没 entry 却通过了")
		}
	})

	t.Run("bundle 与 exe 同时给", func(t *testing.T) {
		m, _ := Parse([]byte("id: x\nname: X\nversion: 1\nentry:\n  darwin:\n    bundle: X.app\n    exe: x\nlaunch:\n  cmd: ad\n"), "ad", "ad-manifest.yaml")
		if err := m.Validate("darwin"); err == nil {
			t.Fatal("bundle 与 exe 同时给却通过了")
		}
	})

	t.Run("入口不许跑出 payload", func(t *testing.T) {
		m, _ := Parse([]byte("id: x\nname: X\nversion: 1\nentry:\n  darwin:\n    exe: ../evil\nlaunch:\n  cmd: ad\n"), "ad", "ad-manifest.yaml")
		if err := m.Validate("darwin"); err == nil {
			t.Fatal("../ 开头的入口却通过了")
		}
	})

	t.Run("绝对路径入口也不行", func(t *testing.T) {
		m, _ := Parse([]byte("id: x\nname: X\nversion: 1\nentry:\n  darwin:\n    exe: /usr/bin/evil\nlaunch:\n  cmd: ad\n"), "ad", "ad-manifest.yaml")
		if err := m.Validate("darwin"); err == nil {
			t.Fatal("/ 开头的入口却通过了")
		}
	})
}

// Peek 只看清单那几百字节，不展开 payload —— 家的位置由 requires 决定，
// 定家之前就得知道 requires，而载荷可能有好几 GB。
func TestPeekZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	add := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	add("ad-manifest.yaml", minimalAD+"requires: [cot]\n")
	// payload/ 里恰好也叫这个名字的文件不算清单。
	add("payload/ad-manifest.yaml", "这不是清单")
	add("payload/AI Desk.app/Contents/MacOS/ai-desk", "MZ")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	m, err := Peek(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if m.Short() != "ad" {
		t.Errorf("Short() = %q，想要 ad", m.Short())
	}
	if !m.HasRequire("cot") {
		t.Errorf("requires 没读出来：%v", m.Requires)
	}
	if m.Name != "AI Desk" {
		t.Errorf("name = %q", m.Name)
	}
}

func TestPeekRejectsJunk(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.txt")
	write(t, plain, "hi")
	if _, err := Peek(plain); err == nil {
		t.Fatal("一个 .txt 文件却被当成包了")
	}

	// 空 zip 里没有清单。
	empty := filepath.Join(dir, "empty.zip")
	f, err := os.Create(empty)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Peek(empty); err == nil {
		t.Fatal("空 zip 却读出了清单")
	}
}
