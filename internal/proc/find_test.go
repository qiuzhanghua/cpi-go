//go:build darwin || linux

package proc

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// helperEnv 让被拷出去的那份测试二进制只睡觉，不再跑测试。
const helperEnv = "CPI_PROC_TEST_HELPER"

// TestFindRealProcess 真起一个进程，验证 Find 认得出来。
//
// 用「把测试二进制拷进临时目录再跑它」这一招，是为了让进程的可执行文件
// 真的落在被测目录里 —— 光看进程名证明不了什么。
func TestFindRealProcess(t *testing.T) {
	if os.Getenv(helperEnv) == "1" {
		time.Sleep(60 * time.Second)
		return
	}

	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "helper")
	copyExecutable(t, self, exe)

	cmd := exec.Command(exe, "-test.run=TestFindRealProcess")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("起不来辅助进程：%v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	if pid := waitFor(t, dir, cmd.Process.Pid); pid == 0 {
		t.Fatalf("%s 里刚起的进程 %d 没被认出来", dir, cmd.Process.Pid)
	}

	// 把可执行文件删掉 —— 正是 macOS 更新完之后那个「幽灵进程」的样子。
	// 文件没了不代表没在跑，Find 必须还认得出来。
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if pid := waitFor(t, dir, cmd.Process.Pid); pid == 0 {
		t.Fatalf("可执行文件删掉之后，进程 %d 就不认得了（幽灵进程漏网）", cmd.Process.Pid)
	}

	// 反过来：什么都没跑的目录必须是空的，不然会天天误拦。
	empty := t.TempDir()
	found, err := Find(empty)
	if err != nil {
		t.Fatalf("Find(%s) 出错：%v", empty, err)
	}
	if len(found) != 0 {
		t.Fatalf("%s 里没跑东西，却找到 %+v", empty, found)
	}
}

// waitFor 等 Find 报出想要的那个 pid，最多等 5 秒。
func waitFor(t *testing.T, dir string, pid int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		found, err := Find(dir)
		if err != nil {
			t.Fatalf("Find(%s) 出错：%v", dir, err)
		}
		for _, p := range found {
			if p.PID == pid {
				return p.PID
			}
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
