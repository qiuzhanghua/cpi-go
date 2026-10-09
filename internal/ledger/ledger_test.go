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
