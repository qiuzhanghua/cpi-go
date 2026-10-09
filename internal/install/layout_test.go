package install

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qiuzhanghua/gpm-go/internal/home"
	"github.com/qiuzhanghua/gpm-go/internal/ledger"
)

// 当前平台上，assembly() 造出来的那个入口在 payload/ 根下的名字。
func assemblyTop() string {
	switch runtime.GOOS {
	case "darwin":
		return "Demo.app"
	case "windows":
		return "demo.exe"
	default:
		return "demo"
	}
}

// layoutAssembly 造一个最小装配目录，入口形状与 payload/ 里的杂物都由调用方说。
func layoutAssembly(t *testing.T, id, short, entryYAML, payloadName string, extras ...string) string {
	t.Helper()
	asm := t.TempDir()
	writePayload(t, filepath.Join(asm, short+"-manifest.yaml"),
		"id: "+id+"\nname: Demo\nversion: 0.1.0\nentry:\n  "+runtime.GOOS+":\n    "+entryYAML+
			"\nlaunch:\n  cmd: "+short+"\n", 0o644)
	writePayload(t, filepath.Join(asm, "payload", payloadName), "#!/bin/sh\nexit 0\n", 0o755)
	for _, e := range extras {
		writePayload(t, filepath.Join(asm, "payload", e), "杂物\n", 0o644)
	}
	return asm
}

// v3.6：GUI 的实体直接落在家目录下，不进 lib/<id>_<版本>_<平台>/。
// 这条用例在三条平台上都跑：darwin 是 .app 目录、windows/linux 是裸可执行文件。
func TestInstallLandsAppInTheHome(t *testing.T) {
	homeDir := fakeHome(t)
	root := installHome(homeDir)
	asm := assembly(t)
	top := assemblyTop()

	if err := Install(asm, Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}

	h, err := home.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	app := h.AppDir(top)
	if _, err := os.Stat(app); err != nil {
		t.Fatalf("入口没落在家里：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", top)); !os.IsNotExist(err) {
		t.Errorf("入口不该再进 lib/：%v", err)
	}
	if items, err := os.ReadDir(h.Lib()); err != nil {
		t.Fatal(err)
	} else if len(items) != 0 {
		t.Errorf("lib/ 该是空的（那是命令行插件的位置），现在有 %d 项", len(items))
	}
	// staging/ 是解包的中转场地：装完它里外都该是空的，连着这一层一起收掉，
	// 免得每个家目录顶层都留一个看不懂的空目录（v3.10）。
	if _, err := os.Stat(h.Staging()); !os.IsNotExist(err) {
		t.Errorf("装完不该留下空的 staging/：%v", err)
	}

	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	p := led.Find("demo")
	if p == nil {
		t.Fatal("账本里没有 demo")
	}
	if p.Dir != app {
		t.Errorf("账本记的安装目录 = %q，想要 %q", p.Dir, app)
	}
	if !strings.HasPrefix(p.Entry, root+string(filepath.Separator)) {
		t.Errorf("入口 %q 不在家里", p.Entry)
	}

	// 卸载按账本回放，家里那一个入口也该走掉，lib/ 依旧空着。
	if err := Uninstall("demo", UninstallOptions{Dir: root, Force: false, Yes: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(app); !os.IsNotExist(err) {
		t.Errorf("卸载之后入口还在：%v", err)
	}
}

// payload/ 根下只能放入口那一个东西：多出来的没法记账，也就没法干净卸载。
func TestInstallRejectsPayloadWithSiblings(t *testing.T) {
	homeDir := fakeHome(t)
	root := installHome(homeDir)
	asm := layoutAssembly(t, "demo", "demo", "exe: demo", "demo", "readme.txt")

	err := Install(asm, Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard})
	if err == nil {
		t.Fatal("payload/ 里多了一个文件，却装成功了")
	}
	for _, want := range []string{"payload/", "demo", "readme.txt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错里少了 %q：%v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "demo")); !os.IsNotExist(err) {
		t.Errorf("被拦下之后不该留下东西：%v", err)
	}
	// 失败路径同样不该留 staging/：入口还没落盘就被拦下了，中转场地是唯一
	// 被建出来的东西，收不回去就成了一份"看着像安装失败的残骸"。
	if _, err := os.Stat(filepath.Join(root, "staging")); !os.IsNotExist(err) {
		t.Errorf("被拦下之后不该留下 staging/：%v", err)
	}
}

// 家里已经有个同名的东西（不是 gpm 装的）：停手，--force 才放行。
func TestInstallRefusesToClobberForeignFileInHome(t *testing.T) {
	homeDir := fakeHome(t)
	root := installHome(homeDir)
	asm := layoutAssembly(t, "demo", "demo", "exe: demo", "demo")

	theirs := filepath.Join(root, "demo")
	writePayload(t, theirs, "别人手放的东西\n", 0o755)

	err := Install(asm, Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard})
	if err == nil {
		t.Fatal("覆盖了别人的东西")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("报错没告诉用户可以 --force：%v", err)
	}
	if b, _ := os.ReadFile(theirs); !strings.Contains(string(b), "别人手放的") {
		t.Errorf("别人的文件被动过：%q", b)
	}

	if err := Install(asm, Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard, Force: true}); err != nil {
		t.Fatalf("--force 之后仍然失败：%v", err)
	}
	if b, _ := os.ReadFile(theirs); !strings.Contains(string(b), "exit 0") {
		t.Errorf("--force 之后该是包里的东西了：%q", b)
	}
}

// 入口名撞上家自己的骨架（bin/、lib/、staging/、账本）：--force 也不行。
func TestInstallRejectsReservedEntryName(t *testing.T) {
	homeDir := fakeHome(t)
	root := installHome(homeDir)
	// 账本按家命名（v3.7），新旧两个名字都是家的骨架。
	ledgerName := filepath.Base(root) + "-state.json"

	for _, reserved := range []string{"bin", "lib", "staging", "state.json", ledgerName} {
		t.Run(reserved, func(t *testing.T) {
			asm := layoutAssembly(t, "demo", "demo", "exe: "+reserved, reserved)
			err := Install(asm, Options{Dir: root, Yes: true, NoPath: true, Out: io.Discard, Force: true})
			if err == nil {
				t.Fatalf("入口叫 %q 却装成功了", reserved)
			}
			if !strings.Contains(err.Error(), reserved) {
				t.Errorf("报错里没提 %q：%v", reserved, err)
			}
		})
	}

	// 骨架还在原处（bin/ 还是目录）。
	if fi, err := os.Stat(filepath.Join(root, "bin")); err != nil || !fi.IsDir() {
		t.Errorf("家的骨架被动了：%v", err)
	}
}
