//go:build darwin

package proc

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// pgrep 在 /usr/bin 下，且不是 setuid 的。
//
// 特意不用 /bin/ps：它是 setuid root 的，在受限环境里连 exec 都会被拒
// （`/bin/ps: Operation not permitted`），而进程枚举这种只读的活儿
// 不值得拖一个 setuid 程序下水。
const pgrep = "/usr/bin/pgrep"

// Find 找「argv[0] 就落在 dir 里」的进程。
//
// macOS 上没有 /proc，能用的只有命令行。pgrep -f 拿正则去匹配整条命令行
// （各参数用空格拼起来），而进程启动时 argv[0] 就是那个可执行文件的绝对路径：
// gpm 的启动器走 `open '<绝对路径>'`，双击和 Dock 走 LaunchServices，
// 两者交出来的都是绝对路径。所以把正则锚在行首（^），匹配到的就正好是
// 应用自己的进程：
//
//   - 你在另一个终端里 grep 这个路径？那条命令的 argv[0] 是 grep，不会误伤。
//   - 更新之后留下的那个幽灵进程？它的 argv[0] 还是原来那个路径，照样认得出。
func Find(dir string) ([]Process, error) {
	if dir == "" {
		return nil, nil
	}
	out, err := exec.Command(pgrep, "-f", pattern(dir)).Output()
	if err != nil {
		// 一个都没找到时 pgrep 退 1，这不是故障。
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("跑 %s 失败：%w", pgrep, err)
	}
	return parsePGrep(string(out)), nil
}

// pattern 拼出那条锚在行首的正则。
//
// regexp.QuoteMeta 转义的那些字符（. + * ? ( ) | [ ] { } ^ $）恰好也是
// POSIX 扩展正则的元字符，所以拼出来的串 pgrep 那边也认。
func pattern(dir string) string {
	var alts []string
	for _, d := range prefixes(dir) {
		alts = append(alts, regexp.QuoteMeta(strings.TrimRight(d, "/"))+"/")
	}
	if len(alts) == 0 {
		// 到不了这儿（Find 已经把空 dir 挡掉了），但别让一个空正则去匹配所有人。
		return "^$"
	}
	return "^(" + strings.Join(alts, "|") + ")"
}

// parsePGrep 解析 pgrep 的输出：一行一个 pid。
func parsePGrep(out string) []Process {
	var found []Process
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		found = append(found, Process{PID: pid})
	}
	return found
}
