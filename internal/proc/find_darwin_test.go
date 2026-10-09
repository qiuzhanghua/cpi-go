//go:build darwin

package proc

import (
	"regexp"
	"testing"
)

func TestPatternAnchorsAtArgvZero(t *testing.T) {
	// 行首锚 + 转义 + 末尾补一个边界：这样 /a/lib/ai-desk_1 不会连累
	// /a/lib/ai-desk_1-extra，而命令行里恰好提到这个路径的 grep 也不会中招。
	const dir = "/a b/lib/ai-desk_0.1.0_darwin_arm64"
	want := `^(/a b/lib/ai-desk_0\.1\.0_darwin_arm64([ /]|$))`
	if got := pattern(dir); got != want {
		t.Errorf("pattern(%q) = %q，想要 %q", dir, got, want)
	}
}

// v3.6 起入口可以是一个裸可执行文件，那时进程的 argv[0] 就等于它本身。
func TestPatternMatchesTheEntryItself(t *testing.T) {
	re := regexp.MustCompile(pattern("/Users/q/cot/ad"))
	for _, line := range []string{
		"/Users/q/cot/ad",
		"/Users/q/cot/ad --flag",
		"/Users/q/cot/ad/sub",
	} {
		if !re.MatchString(line) {
			t.Errorf("%q 该认出来", line)
		}
	}
	for _, line := range []string{
		"/Users/q/cot/ad-extra",
		"/Users/q/cot/ads",
		"/Users/q/bin/ad",
		"grep /Users/q/cot/ad",
	} {
		if re.MatchString(line) {
			t.Errorf("%q 不该认出来", line)
		}
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
