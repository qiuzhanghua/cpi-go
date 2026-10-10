package integrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 生成的标记块必须能被真正的 shell 解析。
//
// 这里踩过一次坑：PathBlock 少写了一个引号（生成 *":$HOME/ad/bin:*) 而不是
// *":$HOME/ad/bin:"*），.zprofile 因此是语法错误的文件，zsh 每次启动都报
// `unmatched "`，PATH 压根没生效——而单元测试只看字符串是发现不了的。
// 所以这个测试真的把块喂给 zsh/bash/sh 的 `-n`。
func TestPathBlockParsesInRealShells(t *testing.T) {
	home := "/tmp/example-home"
	binDir := filepath.Join(home, "ad", "bin")

	files := []string{
		filepath.Join(home, ".zprofile"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
		filepath.Join(home, ".bash_profile"),
	}

	for _, sh := range []string{"zsh", "bash", "sh"} {
		if _, err := exec.LookPath(sh); err != nil {
			t.Logf("跳过 %s：没装", sh)
			continue
		}
		for _, f := range files {
			block := PathBlock(f, binDir, home)
			p := filepath.Join(t.TempDir(), "block.sh")
			if err := os.WriteFile(p, []byte(block), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(sh, "-n", p).CombinedOutput()
			if err != nil {
				t.Errorf("%s -n 说 %s 的块有问题：%v\n块内容：\n%s\n%s",
					sh, filepath.Base(f), err, block, out)
			}
		}
	}
}

// 块里的路径必须是 $HOME 相对形式，不能写死绝对路径。
func TestPathBlockIsHomeRelative(t *testing.T) {
	home := "/tmp/example-home"
	block := PathBlock(filepath.Join(home, ".zshrc"), filepath.Join(home, "ad", "bin"), home)
	if !strings.Contains(block, "$HOME/ad/bin") {
		t.Fatalf("路径应当相对化成 $HOME/ad/bin，实际块：\n%s", block)
	}
	if strings.Contains(block, home) {
		t.Fatalf("块里不该出现绝对路径 %s：\n%s", home, block)
	}
}

// 幂等：真拿 zsh 连续 source 三次，$HOME/ad/bin 只应出现一次。
func TestPathBlockIsIdempotentInZsh(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("没装 zsh")
	}
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("没有 HOME")
	}
	binDir := filepath.Join(home, "ad", "bin")
	block := PathBlock(filepath.Join(home, ".zshrc"), binDir, home)

	p := filepath.Join(t.TempDir(), "block.zsh")
	if err := os.WriteFile(p, []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}

	script := fmt.Sprintf(". %s; . %s; . %s; printf '%%s' \"$PATH\"", p, p, p)
	out, err := exec.Command("zsh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("source 失败：%v\n%s", err, out)
	}

	n := 0
	for _, entry := range strings.Split(string(out), ":") {
		if entry == binDir {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("连 source 三次，%s 应当只出现 1 次，实际 %d 次：%s", binDir, n, out)
	}

	if first := strings.SplitN(string(out), ":", 2)[0]; first != binDir {
		t.Errorf("应当被插到最前面，实际第一个是 %s", first)
	}
}

// 落点表：不论 $SHELL 都要有 ~/.profile 兜底 —— fish 也一样。
//
// 这里曾经漏过 fish：那个分支提前 return 了，于是契约（DESIGN.md §2.6 与
// PACKAGE-FORMAT.md 第 6 节都写着"不论 $SHELL 都再写一份 ~/.profile"）
// 与代码不一致：用户把 shell 换成 bash/sh、或者跑 `sh -l` 时 PATH 就没了。
func TestShellProfilesAlwaysIncludeDotProfile(t *testing.T) {
	home := "/tmp/example-home"
	cases := []struct {
		shell string
		goos  string
		want  []string
	}{
		{"zsh", "darwin", []string{".zprofile", ".zshrc", ".profile"}},
		{"bash", "darwin", []string{".bash_profile", ".profile"}},
		{"bash", "linux", []string{".bashrc", ".profile"}},
		{"fish", "darwin", []string{filepath.Join(".config", "fish", "config.fish"), ".profile"}},
		{"fish", "linux", []string{filepath.Join(".config", "fish", "config.fish"), ".profile"}},
		{"", "darwin", []string{".zprofile", ".zshrc", ".profile"}}, // 没 $SHELL 时按平台默认
		{"", "linux", []string{".bashrc", ".profile"}},
	}
	for _, c := range cases {
		t.Setenv("SHELL", c.shell)
		var want []string
		for _, w := range c.want {
			want = append(want, filepath.Join(home, w))
		}
		got := ShellProfiles(c.goos, home)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("SHELL=%q goos=%s：得到 %v，想要 %v", c.shell, c.goos, got, want)
		}
	}
}

// Windows 上 PATH 走注册表，不碰 shell 配置。
func TestShellProfilesWindowsIsEmpty(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	if got := ShellProfiles("windows", "/tmp/example-home"); len(got) != 0 {
		t.Fatalf("Windows 上不该写 shell 配置，得到 %v", got)
	}
}

// fish 的写法不一样，至少确认它不掺进 POSIX 的 case 语法。
func TestPathBlockFishForm(t *testing.T) {
	home := "/tmp/example-home"
	block := PathBlock(filepath.Join(home, ".config", "fish", "config.fish"),
		filepath.Join(home, "ad", "bin"), home)
	if !strings.Contains(block, "contains $HOME/ad/bin $PATH") {
		t.Fatalf("fish 块应当用 contains，实际：\n%s", block)
	}
	if strings.Contains(block, "esac") {
		t.Fatalf("fish 块里不该有 POSIX 的 esac：\n%s", block)
	}
}
