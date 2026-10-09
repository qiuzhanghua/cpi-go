//go:build linux

package proc

import (
	"os"
	"path/filepath"
	"strconv"
)

// procRoot 抽成变量，好让测试塞一棵假的 /proc 进去。
var procRoot = "/proc"

// Find 扫 /proc。Linux 上不必起任何子进程。
//
// 每个进程看两处：
//
//   - /proc/<pid>/exe —— 真身，最准；
//   - /proc/<pid>/cmdline 的 argv[0] —— 进程启动时那个路径。
//
// 后者不是冗余：AppImage 自解压之后 exe 指向 /tmp/.mount_xxxx 里的临时文件，
// 只有 argv[0] 还认得它原来待在哪儿。
func Find(dir string) ([]Process, error) {
	if dir == "" {
		return nil, nil
	}
	return readProc(procRoot, dir)
}

func readProc(root, dir string) ([]Process, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var found []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // /proc 底下还有 cpuinfo、self 之类，不是进程目录
		}
		base := filepath.Join(root, e.Name())
		var candidates []string
		if exe, err := os.Readlink(filepath.Join(base, "exe")); err == nil {
			candidates = append(candidates, exe)
		}
		if b, err := os.ReadFile(filepath.Join(base, "cmdline")); err == nil {
			if fields := splitNUL(b); len(fields) > 0 {
				candidates = append(candidates, fields[0])
			}
		}
		if p, ok := match(dir, pid, candidates); ok {
			found = append(found, p)
		}
	}
	return found, nil
}
