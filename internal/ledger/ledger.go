// Package ledger 读写账本（v3.5 起这个家就是 $COT_HOME / $TDP_HOME）。
//
// 账本是所有外部副作用（启动器、软链、shell 配置标记块）的唯一真相，
// 卸载就是回放这份记录。
//
// v3.7 起账本按家命名：`<家>/<家目录名>-state.json`。老的
// `<家>/state.json`（v3.6 及以前）还能读进来，下一次写回时顺带改成新名字。
//
// v3.8 起写回之前会跟 Load 时的原文比一次：这次操作期间要是别的 gpm 动过
// 这本账，就拒绝写（宁可让这次安装失败回滚，也不能把别人的账目覆盖掉）。
package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion 是账本格式版本。
const SchemaVersion = 1

// Link 是一条图形入口记录。
type Link struct {
	Path   string `json:"path"`
	Target string `json:"target,omitempty"`
	Kind   string `json:"kind"` // app-symlink | desktop-entry | shortcut
}

// PathEdit 是我们动过的 shell 配置文件。
type PathEdit struct {
	Path    string `json:"path"`
	Created bool   `json:"created"` // 文件原本不存在，是我们建的
}

// Package 是一个已安装的包。
type Package struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Platform    string     `json:"platform"`
	InstalledAt time.Time  `json:"installedAt"`
	Dir         string     `json:"dir"`
	Entry       string     `json:"entry"`
	EntryKind   string     `json:"entryKind"` // bundle | exe
	Cmd         string     `json:"cmd"`
	Launcher    string     `json:"launcher"`
	Links       []Link     `json:"links,omitempty"`
	PathEdits   []PathEdit `json:"pathEdits,omitempty"`
	Verified    bool       `json:"verified"`
}

// Ledger 是整个家目录的账本。
type Ledger struct {
	SchemaVersion int       `json:"schemaVersion"`
	Self          string    `json:"self,omitempty"`
	Packages      []Package `json:"packages"`

	// legacyPath 记着这份内容是刚从哪个旧位置的账本读来的（v3.6 的
	// state.json）。写回新位置之后顺手把它收掉；不参与 JSON。
	legacyPath string

	// loadedFrom / rawAtLoad 是 Load 那一刻的快照（v3.8）：写回之前拿它
	// 跟磁盘上的现状对一次，别人插过手就不写。loadedFrom 为空表示当时
	// 这个家还没有账本。都不参与 JSON。
	loadedFrom string
	rawAtLoad  []byte
}

// Load 读取账本；文件不存在时返回空账本。
//
// 先读 path，读不到就退回同目录下的旧账本 `state.json`（v3.6 及以前）——
// 升级 gpm 之后 `gpm list` / `gpm uninstall` 还得看得见老账本。
func Load(path string) (*Ledger, error) {
	b, from, err := readLedger(path)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return &Ledger{SchemaVersion: SchemaVersion}, nil
	}
	var l Ledger
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("账本 %s 解析失败: %w", from, err)
	}
	if l.SchemaVersion == 0 {
		l.SchemaVersion = SchemaVersion
	}
	if from != path {
		l.legacyPath = from
	}
	l.loadedFrom = from
	l.rawAtLoad = b
	return &l, nil
}

// readLedger 读账本原文：先 path，没有再试同目录下的 state.json。
// 返回读到的内容与真正读的那个路径（都没读到就是 nil）。
func readLedger(path string) (b []byte, from string, err error) {
	b, err = os.ReadFile(path)
	if err == nil {
		return b, path, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	if filepath.Base(path) == "state.json" {
		return nil, "", nil // 要的就是旧名字，没有就是没有
	}
	legacy := filepath.Join(filepath.Dir(path), "state.json")
	b, err = os.ReadFile(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return b, legacy, nil
}

// Save 原子地写回账本（临时文件 + rename）。
//
// 写之前先确认这本账从 Load 到现在没被别人动过（v3.8）：两个 gpm 同时改
// 一个家的时候，最后写的那一份会把先写的那份覆盖掉 —— 文件装上了却没记进
// 账，之后 list 看不见、uninstall 也回放不掉。这里不引锁，用一次「原文比对」
// 把这件事从「悄悄丢账目」变成「这次失败，请重跑」。
//
// 如果这份账本是从旧的 `<家>/state.json` 读来的，写回新位置之后就把旧
// 文件删掉：留着会有两份账本各自演化（DESIGN §0.3.7）。
func (l *Ledger) Save(path string) error {
	if err := l.checkUnchanged(path); err != nil {
		return err
	}
	l.SchemaVersion = SchemaVersion
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if l.legacyPath != "" && l.legacyPath != path {
		os.Remove(l.legacyPath) // 收不回来也不影响：新账本已经写好了
		l.legacyPath = ""
	}
	// 刚写下去的这份就是新的底稿：同一个进程里连着 Save 两次不该自己撞自己。
	l.loadedFrom, l.rawAtLoad = path, b
	return nil
}

// checkUnchanged 对一次「我读到的那份」和「现在磁盘上的那份」。
//
// 只比字节：账本是我们自己写的小 JSON，没有别的写入方，谁改过一眼就看得出来。
func (l *Ledger) checkUnchanged(path string) error {
	if l.loadedFrom == "" {
		// Load 的时候这个家还没有账本；现在要是有了，就是别人先动的手。
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("账本 %s 在这次操作期间被别的 gpm 建了出来，请重跑一次", path)
		}
		return nil
	}
	now, err := os.ReadFile(l.loadedFrom)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("账本 %s 在这次操作期间被别的 gpm 挪走了，请重跑一次", l.loadedFrom)
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(now, l.rawAtLoad) {
		return fmt.Errorf("账本 %s 在这次操作期间被别的 gpm 改过，请重跑一次（不然会把对方装的/卸的东西漏掉）", l.loadedFrom)
	}
	return nil
}

// Find 按 id 找包。
func (l *Ledger) Find(id string) *Package {
	for i := range l.Packages {
		if l.Packages[i].ID == id {
			return &l.Packages[i]
		}
	}
	return nil
}

// FindByCmd 按终端命令名找包。
//
// 命令名住在 <家>/bin 这个共用命名空间里，两个包用同一个简称就会互相
// 覆盖启动器，所以这个名字要能跨 id 查（FR-21）。
func (l *Ledger) FindByCmd(cmd string) *Package {
	if cmd == "" {
		return nil
	}
	for i := range l.Packages {
		if l.Packages[i].Cmd == cmd {
			return &l.Packages[i]
		}
	}
	return nil
}

// Put 覆盖写入一个包记录。
func (l *Ledger) Put(p Package) {
	for i := range l.Packages {
		if l.Packages[i].ID == p.ID {
			l.Packages[i] = p
			return
		}
	}
	l.Packages = append(l.Packages, p)
}

// Remove 删掉一个包记录。
func (l *Ledger) Remove(id string) {
	out := l.Packages[:0]
	for _, p := range l.Packages {
		if p.ID != id {
			out = append(out, p)
		}
	}
	l.Packages = out
}
