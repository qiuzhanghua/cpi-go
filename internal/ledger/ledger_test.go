package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newHome 造一个家的样子：<tmp>/cot —— 账本按家命名，所以目录名有意义。
func newHome(t *testing.T) (root, path string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "cot")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, filepath.Base(root)+"-state.json")
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 账本不存在：给一本空账，不是报错。
func TestLoadMissingIsEmpty(t *testing.T) {
	_, path := newHome(t)
	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d，想要 %d", l.SchemaVersion, SchemaVersion)
	}
	if len(l.Packages) != 0 {
		t.Errorf("空账里不该有包：%+v", l.Packages)
	}
}

// 只有 v3.6 的 state.json 时照样读得到；写回新名字并收掉旧文件。
func TestLoadFallsBackToLegacyNameAndMigrates(t *testing.T) {
	root, path := newHome(t)
	legacy := filepath.Join(root, "state.json")
	// 用 Marshal 拼 JSON：Windows 的路径里全是反斜杠，手写会写出非法转义。
	seed := Ledger{
		SchemaVersion: SchemaVersion,
		Self:          filepath.Join(root, "bin", "gpm"),
		Packages: []Package{{
			ID: "ai-desk", Name: "AI Desk", Version: "0.2.0", Cmd: "ad",
			Dir: filepath.Join(root, "AI Desk.app"),
		}},
	}
	b, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, legacy, string(b))

	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.Find("ai-desk") == nil {
		t.Fatalf("没读到旧账本：%+v", l.Packages)
	}
	if l.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d，想要 %d", l.SchemaVersion, SchemaVersion)
	}

	// 写回：新名字有了，旧文件收掉（不然两份账本各自演化）。
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("新账本没写出来：%v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("旧账本该被收掉：%v", err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Find("ai-desk") == nil {
		t.Errorf("迁移之后读不到那个包了：%+v", again.Packages)
	}
}

// 两个名字都在时：新的说了算，旧的原地不动（没人碰它）。
func TestLoadPrefersTheNewName(t *testing.T) {
	root, path := newHome(t)
	legacy := filepath.Join(root, "state.json")
	write(t, legacy, `{"schemaVersion":1,"packages":[{"id":"old"}]}`)
	write(t, path, `{"schemaVersion":1,"packages":[{"id":"new"}]}`)

	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Packages) != 1 || l.Packages[0].ID != "new" {
		t.Fatalf("读到的不是新账本：%+v", l.Packages)
	}
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("新账本已在，就不该去动旧文件：%v", err)
	}
}

// 坏 JSON：报错指向真正读到的那份文件。
func TestLoadRejectsBadJSON(t *testing.T) {
	root, path := newHome(t)
	legacy := filepath.Join(root, "state.json")
	write(t, legacy, `{ not json`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("坏账本该报错")
	}
	if !strings.Contains(err.Error(), legacy) || !strings.Contains(err.Error(), "解析失败") {
		t.Errorf("报错没指向读到的那份文件：%v", err)
	}
}

// 写回之前要比一次原文（v3.8）：这次操作期间别人动过账本，就不写。
//
// 两个 gpm 同时改一个家，最后写的那份会把先写的那份盖掉 —— 文件装上了却
// 没记进账，之后 list 看不见、uninstall 也回放不掉。宁可这次失败。
func TestSaveRefusesWhenLedgerChangedUnderneath(t *testing.T) {
	_, path := newHome(t)
	write(t, path, `{"schemaVersion":1,"packages":[{"id":"ai-desk"}]}`)

	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// 别人（另一个 gpm）在这个窗口里装了一个东西。
	other := `{"schemaVersion":1,"packages":[{"id":"ai-desk"},{"id":"other-app"}]}`
	write(t, path, other)

	err = l.Save(path)
	if err == nil {
		t.Fatal("账本在脚下被改过，却还是写回去了")
	}
	if !strings.Contains(err.Error(), "重跑") {
		t.Errorf("报错没告诉用户怎么办：%v", err)
	}
	// 别人的账目原封不动。
	if b, err := os.ReadFile(path); err != nil || string(b) != other {
		t.Errorf("把别人的账本覆盖了：%q %v", b, err)
	}
}

// 账本本来是空的，这次操作期间被别人建了出来：同样不写。
func TestSaveRefusesWhenLedgerAppeared(t *testing.T) {
	_, path := newHome(t)

	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Put(Package{ID: "ai-desk"})
	write(t, path, `{"schemaVersion":1,"packages":[{"id":"other-app"}]}`)

	if err := l.Save(path); err == nil {
		t.Fatal("账本被别人建出来了，却还是写回去了")
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "other-app") {
		t.Errorf("把别人的账本覆盖了：%q", b)
	}
}

// 同一个进程里连着写两次不该自己撞自己：第一次写下去的那份就是新底稿。
func TestSaveTwiceInARow(t *testing.T) {
	root, path := newHome(t)

	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Put(Package{ID: "ai-desk"})
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	l.Put(Package{ID: "other-app"})
	if err := l.Save(path); err != nil {
		t.Fatalf("第二次写回失败：%v", err)
	}
	again, err := Load(filepath.Join(root, filepath.Base(root)+"-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Packages) != 2 {
		t.Errorf("两个包都该在账本里：%+v", again.Packages)
	}
}
