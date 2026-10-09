//go:build darwin

package proc

import "testing"

func TestPatternAnchorsAtArgvZero(t *testing.T) {
	// 行首锚 + 转义 + 末尾补一个分隔符：这样 /a/lib/ai-desk_1 不会连累
	// /a/lib/ai-desk_1-extra，而命令行里恰好提到这个路径的 grep 也不会中招。
	const dir = "/a b/lib/ai-desk_0.1.0_darwin_arm64"
	want := `^(/a b/lib/ai-desk_0\.1\.0_darwin_arm64/)`
	if got := pattern(dir); got != want {
		t.Errorf("pattern(%q) = %q，想要 %q", dir, got, want)
	}
}

func TestPatternEmptyDirMatchesNothing(t *testing.T) {
	if got := pattern(""); got != "^$" {
		t.Errorf("空目录不该拼出一条能匹配所有人的正则，得到 %q", got)
	}
}

func TestParsePGrep(t *testing.T) {
	got := parsePGrep("1234\n\n  5678  \n不是数字\n")
	if len(got) != 2 || got[0].PID != 1234 || got[1].PID != 5678 {
		t.Errorf("parsePGrep 得到 %+v，想要 1234 和 5678", got)
	}
	if n := len(parsePGrep("")); n != 0 {
		t.Errorf("空输出该得到 0 个，得到 %d 个", n)
	}
}
