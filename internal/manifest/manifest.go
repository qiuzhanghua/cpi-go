// Package manifest 读写分发包里的 manifest.yaml。
//
// 契约见 docs/PACKAGE-FORMAT.md。要点：entry 里的路径相对于 payload/，
// 落地后也相对于包目录；entry.<goos> 必须恰好给 bundle 或 exe 之一。
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// FileName 是清单文件名。
	FileName = "manifest.yaml"
	// PayloadDir 是载荷目录名，其中的内容会被原样安装到包目录。
	PayloadDir = "payload"
)

// Entry 是应用在某个平台上的可执行入口，bundle 与 exe 二选一。
type Entry struct {
	// Bundle 是 .app 目录（macOS）。
	Bundle string `yaml:"bundle,omitempty"`
	// Exe 是裸可执行文件。
	Exe string `yaml:"exe,omitempty"`
}

// Kind 返回入口形态："bundle" 或 "exe"。
func (e Entry) Kind() string {
	if e.Bundle != "" {
		return "bundle"
	}
	return "exe"
}

// Rel 返回入口相对于 payload/ 的路径。
func (e Entry) Rel() string {
	if e.Bundle != "" {
		return e.Bundle
	}
	return e.Exe
}

// Launch 描述终端启动器。
type Launch struct {
	// Cmd 是终端里敲的命令名。
	Cmd string `yaml:"cmd"`
	// Mode 只对 macOS 的 bundle 形态有意义：
	//   activate（默认）等价于双击；direct 直接跑 .app 内层二进制。
	Mode string `yaml:"mode,omitempty"`
}

// Manifest 是一个分发包的清单。
type Manifest struct {
	ID      string           `yaml:"id"`
	Name    string           `yaml:"name"`
	Version string           `yaml:"version"`
	Entry   map[string]Entry `yaml:"entry"`
	Launch  Launch           `yaml:"launch"`
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	cmdRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Load 读取 root 目录下的 manifest.yaml。
func Load(root string) (*Manifest, error) {
	p := filepath.Join(root, FileName)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("读不到 %s: %w", p, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s 解析失败: %w", p, err)
	}
	return &m, nil
}

// EntryFor 返回 goos 平台的入口，并校验其形态。
func (m *Manifest) EntryFor(goos string) (Entry, error) {
	e, ok := m.Entry[goos]
	if !ok {
		return Entry{}, fmt.Errorf("清单的 entry 里没有 %s 平台，这个包不适用于本机", goos)
	}
	if (e.Bundle == "") == (e.Exe == "") {
		return Entry{}, fmt.Errorf("entry.%s 必须恰好给 bundle 或 exe 之一", goos)
	}
	if err := checkRel(e.Rel()); err != nil {
		return Entry{}, fmt.Errorf("entry.%s: %w", goos, err)
	}
	return e, nil
}

// Validate 检查清单自身是否合法，并确认 entry 覆盖了 goos。
func (m *Manifest) Validate(goos string) error {
	if !idRe.MatchString(m.ID) {
		return fmt.Errorf("id %q 非法：只允许小写字母、数字与 . _ -，且以字母或数字开头", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	if strings.ContainsAny(m.Name, `/\`) {
		return fmt.Errorf("name %q 不能含路径分隔符", m.Name)
	}
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("version %q 非法", m.Version)
	}
	e, err := m.EntryFor(goos)
	if err != nil {
		return err
	}
	if !cmdRe.MatchString(m.Launch.Cmd) {
		return fmt.Errorf("launch.cmd %q 非法：不能含路径分隔符或 ..", m.Launch.Cmd)
	}
	switch m.Launch.Mode {
	case "", "activate", "direct":
	default:
		return fmt.Errorf("launch.mode %q 非法：只能是 activate 或 direct", m.Launch.Mode)
	}
	if m.Launch.Mode == "direct" && e.Kind() != "bundle" {
		return fmt.Errorf("launch.mode=direct 只对 bundle 形态有意义")
	}
	return nil
}

// Mode 返回生效的启动模式。
func (m *Manifest) Mode() string {
	if m.Launch.Mode == "" {
		return "activate"
	}
	return m.Launch.Mode
}

func checkRel(p string) error {
	if p == "" {
		return fmt.Errorf("入口路径不能为空")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("入口路径必须是相对路径，得到 %q", p)
	}
	if len(p) >= 2 && p[1] == ':' {
		return fmt.Errorf("入口路径不能带盘符，得到 %q", p)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("入口路径不能越出 payload/，得到 %q", p)
	}
	return nil
}
