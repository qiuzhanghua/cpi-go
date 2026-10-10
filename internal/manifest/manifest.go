// Package manifest 读写分发包里的清单。
//
// 契约见 docs/PACKAGE-FORMAT.md。要点：文件名是 `<简称>-manifest.yaml`，
// 简称就是终端命令名 `launch.cmd`；entry 里的路径相对于 payload/，落地后
// 也相对于包目录；entry.<goos> 必须恰好给 bundle 或 exe 之一；`requires`
// 声明这个包要不要捎带工具链（`cot` / `tdp`，见 D32）。
package manifest

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// Suffix 是清单文件名的后缀；文件名去掉它就是「简称」。
	Suffix = "-manifest.yaml"
	// PayloadDir 是载荷目录名，其中的内容会被原样安装到包目录。
	PayloadDir = "payload"
	// ToolsDir 是 zip 自带的工具链目录：tools/<os>_<arch>/{cot,tdp}。
	ToolsDir = "tools"
)

// KnownRequires 是清单允许声明的工具链。认得的只有这两家：多一个名字就
// 多一条 gpm 得替别人维护的契约。
var KnownRequires = []string{"cot", "tdp"}

// FileNameFor 返回简称对应的清单文件名。
func FileNameFor(short string) string { return short + Suffix }

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
	// Cmd 是终端里敲的命令名，也就是清单文件名里的「简称」。
	Cmd string `yaml:"cmd"`
	// Mode 只对 macOS 的 bundle 形态有意义：
	//   activate（默认）等价于双击；direct 直接跑 .app 内层二进制。
	//
	// v3.5 起两种模式在 macOS 上都直接 exec 内层二进制（要把工具链环境
	// 注入给应用进程，D33）；mode 只再决定"要不要假装成双击"这件事的
	// 文档含义，见 §2.8。
	Mode string `yaml:"mode,omitempty"`
}

// Manifest 是一个分发包的清单。
type Manifest struct {
	ID       string           `yaml:"id"`
	Name     string           `yaml:"name"`
	Version  string           `yaml:"version"`
	Requires []string         `yaml:"requires,omitempty"`
	Entry    map[string]Entry `yaml:"entry"`
	// Setup 是随包跑的 GUI 设置程序（GUI-Setup），按平台各一项，可缺省。
	//
	// 它与 Entry 的唯一区别是**相对谁**：entry 里的路径相对于 payload/，
	// setup 里的路径相对于 zip 顶层（install.sh 那一层）。GUI-Setup 不进家里，
	// 用户双击的就是它，装完就没用了（v3.13、D43）。
	Setup  map[string]Entry `yaml:"setup,omitempty"`
	Launch Launch           `yaml:"launch"`

	short string // 清单文件名里的简称；来自文件名，不来自 YAML
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	cmdRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Short 返回清单文件名里的简称（等于 launch.cmd）。
func (m *Manifest) Short() string { return m.short }

// Parse 解析清单内容。short 是文件名里的简称（`ad-manifest.yaml` → `ad`），
// from 只用来把报错指向那个文件。
func Parse(b []byte, short, from string) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s 解析失败: %w", from, err)
	}
	m.short = short
	m.Requires = normalizeRequires(m.Requires)
	if m.Launch.Cmd == "" {
		// 缺省等于简称：文件名已经把命令名说出来了，清单里不必重复。
		m.Launch.Cmd = short
	}
	return &m, nil
}

// Discover 在 root 目录里找那唯一一个 `<简称>-manifest.yaml`。
//
// 一个目录里有两个候选就报错，不替打包方挑一个：清单决定 id、版本、
// 装到哪（requires）、装完敲什么，猜错一个都会装出个莫名其妙的东西。
func Discover(root string) (short, path string, err error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", "", fmt.Errorf("读不到 %s: %w", root, err)
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), Suffix) {
			names = append(names, e.Name())
		}
	}
	switch len(names) {
	case 0:
		return "", "", fmt.Errorf("%s 里找不到清单：分发包里应当有且只有一个 <简称>%s", root, Suffix)
	case 1:
		return strings.TrimSuffix(names[0], Suffix), filepath.Join(root, names[0]), nil
	default:
		sort.Strings(names)
		return "", "", fmt.Errorf("%s 里有多个清单（%s），不知道该听谁的；一个分发包只装一个应用", root, strings.Join(names, "、"))
	}
}

// Load 读取 root 目录下的清单。
func Load(root string) (*Manifest, error) {
	short, p, err := Discover(root)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("读不到 %s: %w", p, err)
	}
	return Parse(b, short, p)
}

// Peek 从一个分发包（目录或 .zip）里读出清单，**不展开 payload**。
//
// 家的位置由清单里的 `requires` 决定（D21），所以定家之前就得先读到清单；
// 这一步只把清单本身那几百字节拿来看一眼，几 GB 的载荷还是等家定下来、
// staging 建好之后再解。
func Peek(src string) (*Manifest, error) {
	fi, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return Load(src)
	}
	if strings.EqualFold(filepath.Ext(src), ".zip") {
		return peekZip(src)
	}
	return nil, fmt.Errorf("%s 既不是目录也不是 .zip 包", src)
}

func peekZip(src string) (*Manifest, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var cands []*zip.File
	for _, f := range zr.File {
		name := filepath.ToSlash(filepath.FromSlash(f.Name))
		// 只看包根上那一层：payload/ 里恰好也叫这个名字的文件不算清单。
		if strings.Contains(name, "/") || !strings.HasSuffix(name, Suffix) {
			continue
		}
		if f.FileInfo().IsDir() {
			continue
		}
		cands = append(cands, f)
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("%s 里找不到清单：分发包里应当有且只有一个 <简称>%s", filepath.Base(src), Suffix)
	}
	if len(cands) > 1 {
		names := make([]string, 0, len(cands))
		for _, f := range cands {
			names = append(names, filepath.Base(filepath.FromSlash(f.Name)))
		}
		sort.Strings(names)
		return nil, fmt.Errorf("%s 里有多个清单（%s），不知道该听谁的；一个分发包只装一个应用", filepath.Base(src), strings.Join(names, "、"))
	}
	f := cands[0]
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	short := strings.TrimSuffix(filepath.Base(filepath.FromSlash(f.Name)), Suffix)
	return Parse(b, short, f.Name)
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
	if err := checkRel(e.Rel(), "payload/"); err != nil {
		return Entry{}, fmt.Errorf("entry.%s: %w", goos, err)
	}
	return e, nil
}

// SetupFor 返回 goos 平台的 GUI 设置程序，并校验其形态。
//
// 清单没写 setup 是正常的（老包、不需要图形向导的包），返回零值；写了就必须
// 覆盖被打包的平台 —— 否则发出的 zip 里躺着个双击没反应的目录，而清单还说有。
func (m *Manifest) SetupFor(goos string) (Entry, error) {
	if len(m.Setup) == 0 {
		return Entry{}, nil
	}
	e, ok := m.Setup[goos]
	if !ok {
		return Entry{}, fmt.Errorf("清单的 setup 里没有 %s 平台，这个包不适用于本机", goos)
	}
	if (e.Bundle == "") == (e.Exe == "") {
		return Entry{}, fmt.Errorf("setup.%s 必须恰好给 bundle 或 exe 之一", goos)
	}
	if err := checkRel(e.Rel(), "包根"); err != nil {
		return Entry{}, fmt.Errorf("setup.%s: %w", goos, err)
	}
	return e, nil
}

// HasSetup 报告清单是否声明了随包的 GUI 设置程序。
func (m *Manifest) HasSetup() bool { return len(m.Setup) > 0 }

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
	for _, r := range m.Requires {
		if !IsKnownRequire(r) {
			return fmt.Errorf("requires 里的 %q 不认识：只认 %s", r, strings.Join(KnownRequires, "、"))
		}
	}
	e, err := m.EntryFor(goos)
	if err != nil {
		return err
	}
	if _, err := m.SetupFor(goos); err != nil {
		return err
	}
	if !cmdRe.MatchString(m.Launch.Cmd) {
		return fmt.Errorf("launch.cmd %q 非法：不能含路径分隔符或 ..", m.Launch.Cmd)
	}
	if m.short != "" && m.Launch.Cmd != m.short {
		return fmt.Errorf("launch.cmd %q 与清单文件名里的简称 %q 对不上：这个清单应当叫 %s",
			m.Launch.Cmd, m.short, FileNameFor(m.Launch.Cmd))
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

// HasRequire 报告清单是否声明了某家工具链。
func (m *Manifest) HasRequire(name string) bool {
	for _, r := range m.Requires {
		if r == name {
			return true
		}
	}
	return false
}

// IsKnownRequire 报告某个名字是不是认得的工具链。
func IsKnownRequire(name string) bool {
	for _, r := range KnownRequires {
		if r == name {
			return true
		}
	}
	return false
}

// normalizeRequires 去掉空白与重复，保留打包方写的顺序。
func normalizeRequires(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// checkRel 校验清单里的相对路径：where 是它相对的根，只用来写报错。
func checkRel(p, where string) error {
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
		return fmt.Errorf("入口路径不能越出 %s，得到 %q", where, p)
	}
	return nil
}
