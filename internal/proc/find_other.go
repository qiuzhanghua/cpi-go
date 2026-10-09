//go:build !darwin && !linux && !windows

package proc

import "errors"

// Find 在这些平台上还没有实现。
//
// 返回错误而不是空列表，是故意的：调用方会打印一行提示然后照常继续。
// 宁可漏拦一次，也不要因为「不知道」就把安装挡死。
func Find(dir string) ([]Process, error) {
	return nil, errors.New("这个平台还不支持查进程")
}
