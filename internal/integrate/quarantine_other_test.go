//go:build !darwin

package integrate

import "testing"

// 非 macOS 上是空转：Linux 没有这个属性，Windows 的 MOTW 归 SmartScreen。
//
// 这条用例存在的意义是钉住"调用方不必自己判断平台"这个约定：
// install 流水线会在三个平台上都调它，任何平台上报错都会把安装带偏。
func TestStripQuarantineIsNoopOffDarwin(t *testing.T) {
	if err := StripQuarantine(t.TempDir()); err != nil {
		t.Fatalf("非 macOS 上不该报错：%v", err)
	}
}
