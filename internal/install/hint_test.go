package install

import "testing"

// 提示语里给用户「照抄」的命令必须在他那个平台上真能敲出来：Windows 上包里的
// 二进制叫 gpm.exe，而 `./gpm install . --yes` 在 cmd 里根本不是一条命令。
func TestRetryHintNamesTheRightBinary(t *testing.T) {
	if got := retryHint("windows"); got != "gpm.exe install . --yes" {
		t.Errorf("Windows 上的重跑提示不对：%q", got)
	}
	for _, goos := range []string{"darwin", "linux"} {
		if got := retryHint(goos); got != "./gpm install . --yes" {
			t.Errorf("%s 上的重跑提示被改动了：%q", goos, got)
		}
	}
}
