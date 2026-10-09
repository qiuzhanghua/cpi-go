//go:build darwin

package integrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// markQuarantine 给路径打上"下载来的"标记，模拟浏览器/邮件下载之后的模样。
func markQuarantine(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.Command(xattrBin, "-w", quarantineAttr, "0081;68f0a1b2;Safari;", path).CombinedOutput(); err != nil {
		t.Fatalf("打标记失败：%v（%s）", err, out)
	}
}

// readQuarantine 读回标记；没有这个属性时返回空串。
func readQuarantine(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command(xattrBin, "-p", quarantineAttr, path).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// 顶层与里面每个文件上的标记都要清掉。
//
// 递归（`-r`）不是顺手加的：Gatekeeper 看的是 `.app` 这个包，而 zip 解出来的
// 每个成员各带一份标记，只清顶层会留下"包看着干净、内层二进制还带着标记"
// 的混合状态。
func TestStripQuarantineClearsTree(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "AI Desk.app")
	inner := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(inner, "ai-desk")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	markQuarantine(t, app)
	markQuarantine(t, bin)

	if err := StripQuarantine(app); err != nil {
		t.Fatalf("StripQuarantine 报错：%v", err)
	}
	if got := readQuarantine(t, app); got != "" {
		t.Errorf(".app 上的标记还在：%q", got)
	}
	if got := readQuarantine(t, bin); got != "" {
		t.Errorf("内层二进制上的标记还在：%q", got)
	}
}

// 干净的东西上跑一遍是空转：不报错。
//
// 安装器每次都会调它，而绝大多数安装（终端里解压的包、本机自己 build 的包）
// 压根没有标记 —— 这条路径必须是安静的，否则每次安装都会多出一行提示。
func TestStripQuarantineOnCleanTreeIsQuiet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StripQuarantine(dir); err != nil {
		t.Fatalf("干净目录上不该报错：%v", err)
	}
	if got := readQuarantine(t, dir); got != "" {
		t.Errorf("空转之后反而多出了标记：%q", got)
	}
}
