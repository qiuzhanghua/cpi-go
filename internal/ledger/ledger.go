// Package ledger 读写 <CPI_HOME>/state.json。
//
// 账本是所有外部副作用（启动器、软链、shell 配置标记块）的唯一真相，
// 卸载就是回放这份记录。
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
}

// Load 读取账本；文件不存在时返回空账本。
func Load(path string) (*Ledger, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Ledger{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Ledger
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("账本 %s 解析失败: %w", path, err)
	}
	if l.SchemaVersion == 0 {
		l.SchemaVersion = SchemaVersion
	}
	return &l, nil
}

// Save 原子地写回账本（临时文件 + rename）。
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
	return os.Rename(tmp, path)
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
