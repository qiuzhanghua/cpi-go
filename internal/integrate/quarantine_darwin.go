//go:build darwin

package integrate

import (
	"fmt"
	"os/exec"
	"strings"
)

// xattrBin 是 macOS 自带的扩展属性工具。
//
// 特意写绝对路径、不走 exec.LookPath：安装器是在用户当前这个终端里跑的，
// PATH 是用户的（而且可能刚被 gpm 注入过 `$HOME/cot/bin`）。去 PATH 里
// 找一个同名命令，等于把执行权交给用户环境里第一个叫 xattr 的东西。
const xattrBin = "/usr/bin/xattr"

// quarantineAttr 是下载器（浏览器、邮件、聊天工具）给下载来的文件打上的标记。
//
// LaunchServices 在"打开"时会看它：带标记、又没有 Developer ID 签名的 `.app`
// 会被 Gatekeeper 直接拒掉（`open` 返回 `-128`），界面上是"Apple 无法检查它
// 是否包含恶意软件"。这就是 R1 说的那个"发布阻塞项"。
const quarantineAttr = "com.apple.quarantine"

// StripQuarantine 把 path（含其下所有内容）上的下载标记清掉。
//
// 这是 v3.10 / D40 的裁决：**推翻** R1 原先那句"安装器不许碰它"。理由是
// 包本来就是用户自己解压、自己跑 install.sh 装的，这个动作已经表达了
// "我要装它"；而应用是从解压出来的 payload 里复刻进家目录的，复刻这条路
// 现在走 stage.CopyTree（逐字节写文件、不搬扩展属性），标记碰巧进不来 ——
// 但"碰巧"不是契约：哪天有人为了保住符号链接或资源分支把复制换成 `ditto`，
// 标记就会跟着进家目录，而症状是"装好了却双击打不开"，看着跟安装毫无关系。
// 与其依赖复制实现的细节，不如在这里把它清一遍，让结果不再取决于怎么复制。
//
// 清不掉不算安装失败：调用方只打印一行提示，让用户手动补一刀。R1 的正解
// 仍然是签名 + 公证（票据订在包上，断网也验），这一步只是内网过渡期的兜底。
func StripQuarantine(path string) error {
	cmd := exec.Command(xattrBin, "-dr", quarantineAttr, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%v（%s）", err, msg)
		}
		return err
	}
	return nil
}
