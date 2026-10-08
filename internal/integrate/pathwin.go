package integrate

import (
	"os"
	"strings"
)

// WindowsPathEditPath 是账本里代表「Windows 用户 PATH」的伪路径。
//
// Windows 的 PATH 不在任何文件里，而在注册表 HKCU\Environment 的 Path 值上。
// 账本的回放逻辑只需要一个稳定的标识来认出这条记录，所以借一个伪路径；
// 卸载时看到它就走注册表那条路，而不是去删一个叫这个名字的文件。
const WindowsPathEditPath = `HKCU\Environment:Path`

// ExpandWindowsVars 展开 Windows 风格的 %VAR%（变量名大小写不敏感）。
//
// 不认识的变量原样保留：注册表里的值往往是写给别的程序看的，
// 展开不了就猜一个空串，比留着更容易坏事。
func ExpandWindowsVars(s string, lookup func(string) string) string {
	if !strings.ContainsRune(s, '%') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '%' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		name := s[i+1 : i+1+j]
		if name != "" {
			if v := lookup(name); v != "" {
				b.WriteString(v)
				i += j + 2
				continue
			}
		}
		b.WriteString(s[i : i+1+j+1]) // 原样吐回 %NAME%
		i += j + 2
	}
	return b.String()
}

// windowsPathNorm 把一条 PATH 条目归一化到可比较的形态。
//
// Windows 的 PATH 判重踩三个坑：路径大小写不敏感、正反斜杠混用、
// 值是 REG_EXPAND_SZ 时里面可能是 %USERPROFILE% 这种未展开的变量。
// 三个都要归一化，否则「已经加过了」会被判成「没加过」，装多次就重复追加。
func windowsPathNorm(p string, lookup func(string) string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"`)
	p = ExpandWindowsVars(p, lookup)
	p = strings.ReplaceAll(p, `\`, `/`)
	p = strings.TrimRight(p, "/")
	return strings.ToLower(p)
}

func windowsPathValue(old, binDir string, lookup func(string) string) (string, bool) {
	want := windowsPathNorm(binDir, lookup)
	for _, e := range strings.Split(old, ";") {
		if strings.TrimSpace(e) == "" {
			continue
		}
		if windowsPathNorm(e, lookup) == want {
			return old, false
		}
	}
	old = strings.Trim(old, "; \t")
	if old == "" {
		return binDir, true
	}
	return binDir + ";" + old, true
}

// WindowsPathValue 把 binDir 插到 Windows 用户 PATH 的最前面。
//
// 第二个返回值表示是否真的改动了 —— 这才是幂等的落点：
// 已经装过一次的机器，再装一次不会再追加一遍。
func WindowsPathValue(old, binDir string) (string, bool) {
	return windowsPathValue(old, binDir, os.Getenv)
}

func windowsPathRemove(old, binDir string, lookup func(string) string) (string, bool) {
	want := windowsPathNorm(binDir, lookup)
	kept := make([]string, 0, 8)
	changed := false
	for _, e := range strings.Split(old, ";") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if windowsPathNorm(e, lookup) == want {
			changed = true
			continue
		}
		kept = append(kept, e)
	}
	if !changed {
		return old, false
	}
	return strings.Join(kept, ";"), true
}

// WindowsPathRemove 从 Windows 用户 PATH 里摘掉 binDir。
//
// 只摘掉我们加的那一条，其余原样保留；这是卸载「不留残渣、也不误伤」的分界线。
func WindowsPathRemove(old, binDir string) (string, bool) {
	return windowsPathRemove(old, binDir, os.Getenv)
}
