package install

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/qiuzhanghua/gpm-go/internal/ledger"
)

// installedDemo 真装一次，拿到家目录与账本里记着的那个包。
// 装的时候带 --no-path：不碰真的 ~/.zprofile 之类。
func installedDemo(t *testing.T) (root string, p *ledger.Package) {
	t.Helper()
	root = installHome(fakeHome(t))
	if err := Install(assembly(t), Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	led, err := ledger.Load(ledgerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	p = led.Find("demo")
	if p == nil {
		t.Fatal("装完了账本里却没有 demo")
	}
	return root, p
}

// v3.9：非交互环境里不给 --yes，就什么都不删 —— 跟安装时请求 PATH 许可
// （askPath）同一套口径，而且卸载后面那件事回不来。
func TestUninstallNeedsYesWhenNotATerminal(t *testing.T) {
	root, p := installedDemo(t)

	var out bytes.Buffer
	// strings.Reader 不是终端：isTerminal 为假，走「必须明确同意」那条路。
	err := Uninstall("demo", UninstallOptions{Dir: root, In: strings.NewReader(""), Out: &out})
	if err != nil {
		t.Fatalf("没同意不算出错，只是不做：%v", err)
	}
	text := out.String()
	for _, want := range []string{"要卸载的是", "启动器", "不是交互终端", "gpm uninstall demo --yes", "已取消"} {
		if !strings.Contains(text, want) {
			t.Errorf("人话里少了 %q，实际输出：\n%s", want, text)
		}
	}

	if _, err := os.Stat(p.Entry); err != nil {
		t.Errorf("没同意就把入口动了：%v", err)
	}
	if _, err := os.Stat(p.Launcher); err != nil {
		t.Errorf("没同意就把启动器动了：%v", err)
	}
	led, err := ledger.Load(ledgerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if led.Find("demo") == nil {
		t.Error("没同意就改了账本")
	}
}

func TestUninstallWithYesGoesThrough(t *testing.T) {
	root, p := installedDemo(t)

	var out bytes.Buffer
	if err := Uninstall("demo", UninstallOptions{Dir: root, Yes: true, Out: &out}); err != nil {
		t.Fatalf("--yes 之后该直接做：%v\n输出：\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "是否继续") {
		t.Errorf("--yes 之后不该再问：\n%s", out.String())
	}
	if _, err := os.Stat(p.Entry); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("入口没被删掉：%v", err)
	}
	if _, err := os.Stat(p.Launcher); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("启动器没被删掉：%v", err)
	}
	led, err := ledger.Load(ledgerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if led.Find("demo") != nil {
		t.Error("卸载成功后账本里不该还有 demo")
	}
}

// 终端上问了一句、却一个字节都没读到（stdin 是 /dev/null、双击运行、
// 父进程把输入关了）：这时候不能假装用户答了「是」。
//
// /dev/null 是字符设备，isTerminal 认为是终端 —— 正好走到读那一行。
func TestUninstallPromptWithoutInputKeepsEverything(t *testing.T) {
	root, p := installedDemo(t)

	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("打不开 %s：%v", os.DevNull, err)
	}
	defer f.Close()

	var out bytes.Buffer
	if err := Uninstall("demo", UninstallOptions{Dir: root, In: f, Out: &out}); err != nil {
		t.Fatalf("没读到输入不算出错，只是不做：%v", err)
	}
	if !strings.Contains(out.String(), "没有读到你的输入") {
		t.Errorf("该说清楚「什么都没删」，实际输出：\n%s", out.String())
	}
	if _, err := os.Stat(p.Entry); err != nil {
		t.Errorf("没读到输入就动了入口：%v", err)
	}
	led, err := ledger.Load(ledgerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if led.Find("demo") == nil {
		t.Error("没读到输入就改了账本")
	}
}

// 被拦下之后再明确同意，同一条路要能走通（人看到提示、重跑一次的场景）。
func TestUninstallAsksThenDoesIt(t *testing.T) {
	root, _ := installedDemo(t)

	var refused bytes.Buffer
	if err := Uninstall("demo", UninstallOptions{Dir: root, In: strings.NewReader(""), Out: &refused}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Uninstall("demo", UninstallOptions{Dir: root, Yes: true, Out: &out}); err != nil {
		t.Fatalf("第二次明确同意了，该成功：%v\n输出：\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "已卸载") {
		t.Errorf("该报告卸载完成，实际输出：\n%s", out.String())
	}
}
