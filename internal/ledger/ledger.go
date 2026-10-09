// Package ledger 读写账本（v3.5 起这个家就是 $COT_HOME / $TDP_HOME）。
//
// 账本是所有外部副作用（启动器、软链、shell 配置标记块）的唯一真相，
// 卸载就是回放这份记录。
//
// v3.7 起账本按家命名：`<家>/<家目录名>-state.json`。老的
// `<家>/state.json`（v3.6 及以前）还能读进来，下一次写回时顺带改成新名字。
package ledger

import (
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
// 如果这份账本是从旧的 `<家>/state.json` 读来的，写回新位置之后就把旧
// 文件删掉：留着会有两份账本各自演化（DESIGN §0.3.7）。
func (l *Ledger) Save(path string) error {
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
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if l.legacyPath != "" && l.legacyPath != path {
		os.Remove(l.legacyPath) // 收不回来也不影响：新账本已经写好了
		l.legacyPath = ""
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
