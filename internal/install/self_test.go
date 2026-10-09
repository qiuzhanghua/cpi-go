package install

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qiuzhanghua/gpm-go/internal/home"
	"github.com/qiuzhanghua/gpm-go/internal/ledger"
)

func selfName() string {
	if runtime.GOOS == "windows" {
		return "gpm.exe"
	}
	return "gpm"
}

// 空 bin/ 时，gpm 把自己拷一份过去。
func TestInstallSelfCopiesWhenAbsent(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	res, err := installSelf(bin, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Wrote || res.Kept {
		t.Fatalf("Wrote=%v Kept=%v，想要 true/false", res.Wrote, res.Kept)
	}
	if res.Replaced {
		t.Error("全新安装不该算成替换")
	}
	if want := filepath.Join(bin, selfName()); res.Dest != want {
		t.Fatalf("Dest = %q，想要 %q", res.Dest, want)
	}
	fi, err := os.Stat(res.Dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() == 0 {
		t.Errorf("%s 是空的", res.Dest)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s 没有可执行位：%v", res.Dest, fi.Mode().Perm())
	}
}

// bin/ 里本来就有一份、版本又读不出来时：一个字节都不动，也不认领它。
//
// 这里刻意不替掉 querySelfVersion —— 那份文件是纯文本、根本 exec 不起来，
// 走的正是"问不出所以不动"这条真实路径（v3.11、D41）。
func TestInstallSelfKeepsExistingGpm(t *testing.T) {
	bin, dest := existingSelf(t, "这是用户自己的 gpm，别动\n")

	res, err := installSelf(bin, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Wrote || !res.Kept {
		t.Fatalf("Wrote=%v Kept=%v，想要 false/true", res.Wrote, res.Kept)
	}
	if res.Dest != dest {
		t.Fatalf("Dest = %q，想要 %q", res.Dest, dest)
	}
	if res.OldVer != "" {
		t.Errorf("OldVer = %q，想要空（读不出来）", res.OldVer)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinelSelf {
		t.Fatalf("原有的 gpm 被动过：%q %v", b, err)
	}
}

// existingSelf 在临时 bin/ 里放一份"已有的 gpm"，返回那个目录与那个文件的路径。
func existingSelf(t *testing.T, content string) (bin, dest string) {
	t.Helper()
	bin = filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dest = filepath.Join(bin, selfName())
	if err := os.WriteFile(dest, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dest
}

// sentinelSelf 是"别人放的那份 gpm"的内容，用来验证它有没有被动过。
const sentinelSelf = "这是用户自己的 gpm，别动\n"

// stubSelfVersion 把"问家里那份是哪个版本"这一步替掉（v3.11、D41）。
//
// 真去 exec 一个文件、还要它在三个平台上都自报版本，夹具造不出来；这个包已经用
// 同一手法处理过 os.Executable / os.UserHomeDir（见 home 包的 selfExecutable）。
func stubSelfVersion(t *testing.T, version string, ok bool) {
	t.Helper()
	old := querySelfVersion
	querySelfVersion = func(string) (gpmVersion, bool) {
		if !ok {
			return gpmVersion{}, false
		}
		v, parsed := parseVersion(version)
		if !parsed {
			t.Fatalf("用例里的版本号 %q 自己就解析不了", version)
		}
		return v, true
	}
	t.Cleanup(func() { querySelfVersion = old })
}

// 家里那份比这份旧：换掉它，并说清楚换的是哪个版本。
func TestInstallSelfReplacesOlderGpm(t *testing.T) {
	stubSelfVersion(t, "0.6.0", true)
	bin, dest := existingSelf(t, sentinelSelf)

	res, err := installSelf(bin, "0.6.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Wrote || res.Kept {
		t.Fatalf("Wrote=%v Kept=%v，想要 true/false", res.Wrote, res.Kept)
	}
	if !res.Replaced {
		t.Error("这条路该算成替换")
	}
	if res.OldVer != "0.6.0" {
		t.Errorf("OldVer = %q，想要 0.6.0", res.OldVer)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) == sentinelSelf {
		t.Fatalf("旧的那份没被换掉：%v", err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Size() <= int64(len(sentinelSelf)) {
		t.Errorf("换上去的不是我们这个二进制：%v", err)
	}
}

// 家里那份比这份新：一个字节都不动（这条是"不降级"的底线）。
func TestInstallSelfKeepsNewerGpm(t *testing.T) {
	stubSelfVersion(t, "0.6.2", true)
	bin, dest := existingSelf(t, sentinelSelf)

	res, err := installSelf(bin, "0.6.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Wrote || !res.Kept {
		t.Fatalf("Wrote=%v Kept=%v，想要 false/true", res.Wrote, res.Kept)
	}
	if res.OldVer != "0.6.2" {
		t.Errorf("OldVer = %q，想要 0.6.2", res.OldVer)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinelSelf {
		t.Fatalf("比我们新的那份被动过：%q %v", b, err)
	}
}

// 同版本：也不动 —— 没必要把用户那个文件重写一遍。
func TestInstallSelfKeepsSameVersion(t *testing.T) {
	stubSelfVersion(t, "0.6.1", true)
	bin, dest := existingSelf(t, sentinelSelf)

	res, err := installSelf(bin, "0.6.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Wrote || !res.Kept {
		t.Fatalf("Wrote=%v Kept=%v，想要 false/true", res.Wrote, res.Kept)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinelSelf {
		t.Fatalf("同版本那份被动过：%q %v", b, err)
	}
}

// 自己不知道自己是哪个版本（SelfVersion 为空）：谁都不动。
//
// 这一条守着 install.Options 的契约：库调用方没给版本号时，行为与 v3.10 完全一致。
func TestInstallSelfKeepsWhenOwnVersionUnknown(t *testing.T) {
	stubSelfVersion(t, "0.6.0", true)
	bin, dest := existingSelf(t, sentinelSelf)

	res, err := installSelf(bin, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Wrote || !res.Kept {
		t.Fatalf("Wrote=%v Kept=%v，想要 false/true", res.Wrote, res.Kept)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinelSelf {
		t.Fatalf("版本号未知时那份被动过：%q %v", b, err)
	}
}

// 版本读不出来：默认不动，--force 才换（用户明确的"我就是要这份"）。
func TestInstallSelfForceReplacesUnreadable(t *testing.T) {
	stubSelfVersion(t, "", false)
	bin, dest := existingSelf(t, sentinelSelf)

	res, err := installSelf(bin, "0.6.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Wrote || !res.Kept || res.OldVer != "" {
		t.Fatalf("读不出来时该原样留着：%+v", res)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinelSelf {
		t.Fatalf("读不出来时那份被动过：%q %v", b, err)
	}

	res, err = installSelf(bin, "0.6.1", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Wrote || !res.Replaced {
		t.Fatalf("--force 该换掉它：%+v", res)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) == sentinelSelf {
		t.Fatalf("--force 之后还是老内容：%v", err)
	}
}

// 装配目录：<简称>-manifest.yaml + payload/，按当前平台给一个能装的入口。
func assembly(t *testing.T) string {
	t.Helper()
	asm := t.TempDir()
	payload := filepath.Join(asm, "payload")

	var manifest string
	if runtime.GOOS == "darwin" {
		app := filepath.Join(payload, "Demo.app")
		writePayload(t, filepath.Join(app, "Contents", "MacOS", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
		writePayload(t, filepath.Join(app, "Contents", "Info.plist"),
			`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>demo</string></dict></plist>`,
			0o644)
		manifest = "id: demo\nname: Demo\nversion: 0.1.0\nentry:\n  darwin:\n    bundle: Demo.app\nlaunch:\n  cmd: demo\n"
	} else {
		name := "demo"
		if runtime.GOOS == "windows" {
			name = "demo.exe"
		}
		writePayload(t, filepath.Join(payload, name), "x", 0o755)
		manifest = fmt.Sprintf(
			"id: demo\nname: Demo\nversion: 0.1.0\nentry:\n  %s:\n    exe: %s\nlaunch:\n  cmd: demo\n",
			runtime.GOOS, name)
	}
	writePayload(t, filepath.Join(asm, "demo-manifest.yaml"), manifest, 0o644)
	return asm
}

func writePayload(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// 装完之后账本里的 Self 只指向「我们自己放进去的那一份」。
func TestInstallRecordsSelfOnlyWhenCopied(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir()) // 别碰真 ~/Applications 与 shell 配置
	}
	root := t.TempDir()
	if err := Install(assembly(t), Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.Bin(), selfName())
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("装完没有 %s：%v", dest, err)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if led.Self != dest {
		t.Fatalf("账本 Self = %q，想要 %q", led.Self, dest)
	}
	if len(led.Packages) != 1 || led.Packages[0].ID != "demo" {
		t.Fatalf("账本内容不对：%+v", led.Packages)
	}
}

// 用户那儿本来就有 gpm：不覆盖、不进账本（于是卸载时也不会删掉它）。
func TestInstallKeepsForeignGpmOutOfLedger(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir())
	}
	root := t.TempDir()

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.Bin(), selfName())
	const sentinel = "用户自己的 gpm\n"
	if err := os.WriteFile(dest, []byte(sentinel), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Install(assembly(t), Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}

	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinel {
		t.Fatalf("原有的 gpm 被覆盖了：%q %v", b, err)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if led.Self != "" {
		t.Fatalf("账本 Self = %q，想要空（那一份不是我们的）", led.Self)
	}

	// 卸载（最后一个包）之后它还得在。
	if err := Uninstall("demo", UninstallOptions{Dir: root, Force: false, Yes: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinel {
		t.Fatalf("卸载把用户自己的 gpm 删了/改了：%q %v", b, err)
	}
}

// 家里那份比包里这份旧：装的时候换掉，账本也认领它（v3.11、D41）。
//
// "认领"意味着卸载最后一个包时，那份文件按"我们自己放的"处置（见 Uninstall）——
// 它确实是这次由我们写下去的。
func TestInstallReplacesOlderSelfInTheHome(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir())
	}
	stubSelfVersion(t, "0.6.0", true)
	root := t.TempDir()

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.Bin(), selfName())
	if err := os.WriteFile(dest, []byte(sentinelSelf), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Install(assembly(t), Options{
		Dir: root, Yes: true, NoPath: true, Out: &out, SelfVersion: "0.6.1",
	}); err != nil {
		t.Fatal(err)
	}

	if b, err := os.ReadFile(dest); err != nil || string(b) == sentinelSelf {
		t.Fatalf("旧的那份没被换掉：%v", err)
	}
	if s := out.String(); !strings.Contains(s, "从 gpm 0.6.0 换成 gpm 0.6.1") {
		t.Errorf("输出里没说清楚换了什么：\n%s", s)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if led.Self != dest {
		t.Errorf("账本 Self = %q，想要 %q", led.Self, dest)
	}
}

// 家里那份比包里这份新：不动它，也不认领（于是卸载时不会删掉它）。
func TestInstallKeepsNewerSelfOutOfLedger(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir())
	}
	stubSelfVersion(t, "0.6.2", true)
	root := t.TempDir()

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h.Bin(), selfName())
	if err := os.WriteFile(dest, []byte(sentinelSelf), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Install(assembly(t), Options{
		Dir: root, Yes: true, NoPath: true, Out: &out, SelfVersion: "0.6.1",
	}); err != nil {
		t.Fatal(err)
	}

	if b, err := os.ReadFile(dest); err != nil || string(b) != sentinelSelf {
		t.Fatalf("比我们新的那份被动过：%q %v", b, err)
	}
	if s := out.String(); !strings.Contains(s, "已经有一个 gpm 0.6.2") {
		t.Errorf("输出里该说明留着哪一份：\n%s", s)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if led.Self != "" {
		t.Errorf("账本 Self = %q，想要空（那一份不是我们的）", led.Self)
	}
}

// 全新的家里没有 bin/gpm：静默拷一份，不该冒出"覆盖 / 已经有一个"这类话。
//
// 这条是开发 v3.11 时真机 e2e 抓出来的：一开始用 `Wrote && OldVer == ""` 同时表示
// "新装"和"强制覆盖了读不出来的那份"，于是全新安装也会打一行"已按 --force 覆盖…"。
func TestInstallSaysNothingAboutSelfWhenFresh(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir())
	}
	root := t.TempDir()

	var out bytes.Buffer
	if err := Install(assembly(t), Options{
		Dir: root, Yes: true, NoPath: true, Out: &out, SelfVersion: "0.6.1",
	}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"覆盖", "已经有一个 gpm", "换成"} {
		if strings.Contains(out.String(), bad) {
			t.Errorf("全新安装不该出现 %q：\n%s", bad, out.String())
		}
	}
}

// installDemo 在临时 HOME 里装一个 demo，返回安装根（顺带清掉工具链变量）。
func installDemo(t *testing.T) string {
	t.Helper()
	fakeHome(t)
	root := t.TempDir()
	if err := Install(assembly(t), Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	return root
}

// 卸载最后一个包时，当前 shell 已经站在某家工具链里（COT_HOME /
// TDP_HOME 有值）：那个家归 cot/tdp 管，账本里那份 gpm 留着不删。
func TestUninstallKeepsSelfWhenToolchainEnvSet(t *testing.T) {
	root := installDemo(t)
	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(h.Bin(), selfName())
	if _, err := os.Stat(self); err != nil {
		t.Fatalf("装完没有 %s：%v", self, err)
	}

	t.Setenv("TDP_HOME", filepath.Join(t.TempDir(), "tdp"))
	if err := Uninstall("demo", UninstallOptions{Dir: root, Force: false, Yes: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(self); err != nil {
		t.Fatalf("环境里有 TDP_HOME，卸载却把 %s 删了：%v", self, err)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(led.Packages) != 0 {
		t.Fatalf("包该卸干净了：%+v", led.Packages)
	}
	if led.Self != self {
		t.Fatalf("账本 Self = %q，想要留着 %q", led.Self, self)
	}
}

// 没有那两个变量时，最后一份 gpm 照旧跟着卸载走。
func TestUninstallRemovesSelfWithoutToolchainEnv(t *testing.T) {
	root := installDemo(t)
	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(h.Bin(), selfName())

	if err := Uninstall("demo", UninstallOptions{Dir: root, Force: false, Yes: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(self); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("卸载后 %s 该没了，Stat 给 %v", self, err)
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	if led.Self != "" {
		t.Fatalf("账本 Self = %q，想要空", led.Self)
	}
}
