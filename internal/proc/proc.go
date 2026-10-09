// Package proc 回答一个问题：某个已经装好的应用，此刻有没有在跑。
//
// 为什么非问不可：gpm 覆盖安装或卸载时，会把应用的目录整个删掉再重新拷。
// 对已经跑起来的进程来说，脚下的文件被抽走并不会让它立刻退出 —— 内核还攥着
// 那个 inode，它继续照着旧代码跑，而它按路径去读的资源、去 dlopen 的库、
// 去拉起的 sidecar 却已经换成另一份了（版本混用）。
//
// macOS 上还多一层：LaunchServices 会把那个 .app 路径一直绑在这个旧进程上，
// 于是你下次再点图标，只是把它唤到前台，永远拿不到新版本 —— 一个幽灵进程。
//
// 所以动手之前先问这一句。查不出来的时候宁可不拦（只打印一行提示）：
// 「查不到」不等于「在跑」，而误拦会把所有脚本化的安装都挡死。
package proc

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Process 是一个正在运行的进程。
type Process struct {
	PID int
	// Exe 是它的可执行文件路径。尽力而为：拿不到具体路径时留空。
	Exe string
}

// prefixes 是比对时要认的目录前缀。
//
// 给两个：账本里原样记着的那个路径，以及它解析软链之后的样子。
// 进程报出来的路径是这两种之一，取决于这个家这条路上有没有软链。
func prefixes(dir string) []string {
	if dir == "" {
		return nil
	}
	out := []string{dir}
	if r, err := filepath.EvalSymlinks(dir); err == nil && filepath.Clean(r) != filepath.Clean(dir) {
		out = append(out, r)
	}
	return out
}

// under 判断 path 是不是落在 dir 里面（path 就是 dir 本身也算）。
//
// macOS 和 Windows 的文件系统默认大小写不敏感，这两个平台上按大小写无关比。
// 纯字符串比对，不碰磁盘：进程报出来的路径可能早就没了（幽灵进程），
// 这时候再去 stat 只会得到"不存在"，反倒认不出来。
func under(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	dir = filepath.Clean(dir)
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		dir = strings.ToLower(dir)
		path = strings.ToLower(path)
	}
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// match 从一批候选路径里挑出第一个落在 dir 之下的，挑到就返回这个进程。
func match(dir string, pid int, candidates []string) (Process, bool) {
	for _, c := range candidates {
		c = trimDeleted(c)
		if c == "" {
			continue
		}
		if under(dir, c) {
			return Process{PID: pid, Exe: c}, true
		}
	}
	return Process{}, false
}

// trimDeleted 去掉 Linux 给「可执行文件已经不在了」的进程加的尾巴。
//
// 这个尾巴恰恰是幽灵进程的样子：文件被删了，进程还在跑。
func trimDeleted(p string) string {
	return strings.TrimSuffix(p, " (deleted)")
}

// splitNUL 把 /proc/<pid>/cmdline 切成一个一个参数（它以 NUL 分隔并收尾）。
func splitNUL(b []byte) []string {
	var out []string
	for _, f := range strings.Split(string(b), "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
