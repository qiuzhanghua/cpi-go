package integrate

import (
	"strings"
	"testing"
)

// fakeEnv 模仿 Windows 上的变量查找：大小写不敏感。
// WindowsPathValue 之类的函数只认一个 lookup，所以这里可以完全确定地测。
func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string {
		for kk, vv := range m {
			if strings.EqualFold(kk, k) {
				return vv
			}
		}
		return ""
	}
}

func TestExpandWindowsVars(t *testing.T) {
	env := fakeEnv(map[string]string{
		"USERPROFILE": `C:\Users\q`,
		"APPDATA":     `C:\Users\q\AppData\Roaming`,
	})

	cases := []struct {
		name, in, want string
	}{
		{"没有变量就原样返回", `C:\Windows`, `C:\Windows`},
		{"单个变量", `%USERPROFILE%\ad\bin`, `C:\Users\q\ad\bin`},
		{"变量名大小写不敏感", `%userprofile%\ad`, `C:\Users\q\ad`},
		{"一行里多个变量", `%USERPROFILE%;%APPDATA%`, `C:\Users\q;C:\Users\q\AppData\Roaming`},
		{"不认识的变量原样保留", `%NOPE%\x`, `%NOPE%\x`},
		{"认识与不认识的混在一起", `%USERPROFILE%\%NOPE%`, `C:\Users\q\%NOPE%`},
		{"落单的百分号", `100%done`, `100%done`},
		{"空的变量名", `%%x`, `%%x`},
		{"结尾是变量", `x=%USERPROFILE%`, `x=C:\Users\q`},
		{"变量在正中间", `a%USERPROFILE%b`, `aC:\Users\qb`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExpandWindowsVars(c.in, env); got != c.want {
				t.Errorf("ExpandWindowsVars(%q) = %q，想要 %q", c.in, got, c.want)
			}
		})
	}
}

func TestWindowsPathValueIsIdempotent(t *testing.T) {
	env := fakeEnv(map[string]string{"USERPROFILE": `C:\Users\q`})
	bin := `C:\Users\q\ad\bin`

	cases := []struct {
		name     string
		old      string
		wantVal  string
		wantChgd bool
	}{
		{"空的就加上", ``, bin, true},
		{"只有分隔符也当空的", `;;  ;`, bin, true},
		{"加在已有的前面", `C:\Windows`, bin + `;C:\Windows`, true},

		// 下面这些全是「已经加过了」的不同写法。少认一种，用户装第二次
		// 就会看到 PATH 里多出一条一模一样的路径。
		{"一字不差", bin, bin, false},
		{"大小写不同", `C:\USERS\Q\AD\BIN`, `C:\USERS\Q\AD\BIN`, false},
		{"正斜杠", `C:/Users/q/ad/bin`, `C:/Users/q/ad/bin`, false},
		{"结尾多一个反斜杠", bin + `\`, `C:\Users\q\ad\bin\`, false},
		{"加了引号", `"` + bin + `"`, `"` + bin + `"`, false},
		{"写成未展开的变量", `%USERPROFILE%\ad\bin`, `%USERPROFILE%\ad\bin`, false},
		{"夹在别的条目中间", `C:\Windows;` + bin + `;C:\Other`, `C:\Windows;` + bin + `;C:\Other`, false},
		{"周围有空格", `C:\Windows; ` + bin + ` `, `C:\Windows; ` + bin + ` `, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := windowsPathValue(c.old, bin, env)
			if changed != c.wantChgd {
				t.Errorf("changed = %v，想要 %v（old=%q → %q）", changed, c.wantChgd, c.old, got)
			}
			if got != c.wantVal {
				t.Errorf("值 = %q，想要 %q", got, c.wantVal)
			}
		})
	}
}

func TestWindowsPathRemoveOnlyTouchesOurEntry(t *testing.T) {
	env := fakeEnv(map[string]string{"USERPROFILE": `C:\Users\q`})
	bin := `C:\Users\q\ad\bin`

	cases := []struct {
		name     string
		old      string
		wantVal  string
		wantChgd bool
	}{
		{"摘掉我们那条，其余保持原顺序", bin + `;C:\Windows;C:\Other`, `C:\Windows;C:\Other`, true},
		{"在中间也能摘", `C:\Windows;` + bin + `;C:\Other`, `C:\Windows;C:\Other`, true},
		{"大小写不同也认得出", `C:\Windows;C:\USERS\Q\AD\BIN`, `C:\Windows`, true},
		{"未展开的变量也认得出", `C:\Windows;%USERPROFILE%\ad\bin`, `C:\Windows`, true},
		{"没有就不动", `C:\Windows;C:\Other`, `C:\Windows;C:\Other`, false},
		{"摘完是空的就返回空串", bin, ``, true},
		{"不留下连续分号", bin + `;;C:\Windows`, `C:\Windows`, true},
		{"相似但不相同的路径不许误伤", `C:\Windows;C:\Users\q\ad\bin2`, `C:\Windows;C:\Users\q\ad\bin2`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := windowsPathRemove(c.old, bin, env)
			if changed != c.wantChgd {
				t.Errorf("changed = %v，想要 %v（old=%q → %q）", changed, c.wantChgd, c.old, got)
			}
			if got != c.wantVal {
				t.Errorf("值 = %q，想要 %q", got, c.wantVal)
			}
		})
	}
}

// TestWindowsPathIsNotMistakenForAFile 守住一个很小但很要命的约定：
// 账本用 WindowsPathEditPath 代表注册表，卸载时会拿它和普通文件路径比。
// 它不能长得像一个真的文件路径，否则「是我们建的空文件就删掉」那一步
// 可能真的去删点什么。
func TestWindowsPathIsNotMistakenForAFile(t *testing.T) {
	if strings.ContainsAny(WindowsPathEditPath, "/") {
		t.Errorf("%q 里不该有正斜杠：它得和用户文件路径明显不同", WindowsPathEditPath)
	}
	if !strings.Contains(WindowsPathEditPath, `HKCU`) {
		t.Errorf("%q 看不出是注册表", WindowsPathEditPath)
	}
}
