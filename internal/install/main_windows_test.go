//go:build windows

package install

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain 把「用户级目录」整体挪进临时目录，再跑这个包的用例。
//
// 这个包的用例会走完整的安装流程，而图形入口在 Windows 上落在
// %APPDATA%\Microsoft\Windows\Start Menu\Programs\gpm\ —— 不重定向的话，
// `go test ./internal/install` 会在开发者真实的开始菜单里留下一个 .lnk
// （每个用例还会先撞一次 mkdir，在受限环境里直接变红）。
//
// LOCALAPPDATA 一并挪走：requires 为空时 gpm 落到平台数据目录
// （Windows 是 %LOCALAPPDATA%\<简称>），那也是真实用户的目录。
//
// 为什么单独一个 windows 文件：running_test.go 里的 TestMain 带着
// `//go:build darwin || linux`（它要留在那儿当「正在运行的假应用」的入口），
// 一个包只能有一个 TestMain，所以这份只能挂在 windows 上。
//
// 单个用例仍然可以用 t.Setenv 覆盖这里的默认值（integrate 包的快捷方式用例
// 就是自己设 APPDATA 的），TestMain 只负责把没人管的那些兜住。
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "gpm-install-test-")
	if err != nil {
		panic(err)
	}
	roaming := filepath.Join(tmp, "AppData", "Roaming")
	local := filepath.Join(tmp, "AppData", "Local")
	for _, p := range []string{roaming, local} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			panic(err)
		}
	}
	os.Setenv("APPDATA", roaming)
	os.Setenv("LOCALAPPDATA", local)

	code := m.Run()
	// os.Exit 不跑 defer，所以清理要显式写在这里。
	os.RemoveAll(tmp)
	os.Exit(code)
}
