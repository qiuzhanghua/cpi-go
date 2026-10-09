package install

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// gpmVersion 是一个能比较大小的 gpm 版本号。
//
// 只认 semver 那套形状：X[.Y[.Z]][-预发布][+构建]，构建段（`+` 之后）直接忽略。
// 缺的段按 0 算；`gpm ` 前缀与 `v` 前缀都剥掉 —— 老版本的 `--version` 打的正是
// `gpm 0.6.0`，而 `git describe` 出来的是 `0.6.1` 这种裸的。
type gpmVersion struct {
	nums [3]int
	pre  []string // 预发布标识符；空 = 正式版
}

// parseVersion 解析一个版本号；认不出来返回 ok=false。
//
// 认不出来就必须说"认不出来"，不能猜：调用方要靠它决定要不要覆盖用户机器上那份
// gpm，而猜错的方向恰好是最坏的一种 —— 拿旧的盖掉新的。
func parseVersion(s string) (gpmVersion, bool) {
	s = strings.TrimSpace(s)
	// 整行输出（比如 `gpm 0.6.0`）取最后一个字段
	if i := strings.LastIndexAny(s, " \t"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // 构建元数据不参与比较
	}
	var v gpmVersion
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
		for _, p := range v.pre {
			if p == "" { // `1.0.0-` / `1.0.0-a..b`：不合 semver，当认不出来
				return gpmVersion{}, false
			}
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return gpmVersion{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return gpmVersion{}, false
		}
		v.nums[i] = n
	}
	return v, true
}

// String 还原成规范写法（`0.6.1`、`1.0.0-rc.1`），用于提示里给人看。
func (v gpmVersion) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.nums[0], v.nums[1], v.nums[2])
	if len(v.pre) > 0 {
		s += "-" + strings.Join(v.pre, ".")
	}
	return s
}

// compare 按 semver 的优先级比较，返回 -1 / 0 / 1。
//
// 三条规则值得写下来，因为它们直接决定"换不换"：
//   - `1.0.0` > `1.0.0-rc.1`：带预发布后缀的比同名正式版**小**。所以
//     `0.6.1-2-g1a2b3c4`（GPM_REF 钉到 main 时 git describe 的产物）比 `0.6.1`
//     小 —— 拿它去顶一份正式的 `0.6.1` 会被判为降级，不动；
//   - 纯数字标识符比字母数字标识符小：`1.0.0-1` < `1.0.0-alpha`；
//   - 前面都相同，则标识符少的更小：`1.0.0-alpha` < `1.0.0-alpha.1`。
func (v gpmVersion) compare(o gpmVersion) int {
	for i := 0; i < 3; i++ {
		if c := cmpInt(v.nums[i], o.nums[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.pre) == 0 && len(o.pre) == 0:
		return 0
	case len(v.pre) == 0:
		return 1
	case len(o.pre) == 0:
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(o.pre); i++ {
		if c := cmpPre(v.pre[i], o.pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(v.pre), len(o.pre))
}

func cmpPre(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		return cmpInt(an, bn)
	case aerr == nil:
		return -1 // 数字标识符 < 字母数字标识符
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// querySelfVersion 是 installSelf 用来问"家里那份 gpm 是哪个版本"的那一步。
//
// 做成变量是为了测试能替掉它：真去 exec 一个文件、还要它在三个平台上都自报版本，
// 这样的夹具造不出来。写法与 home 包里的 selfExecutable / userHome 一致。
var querySelfVersion = readSelfVersion

// readSelfVersion 跑 `<path> --version` 把版本号读回来。
//
// 任何一步不顺利（不是可执行文件、跑不起来、超时、输出认不出）都返回 ok=false，
// 调用方一律当"不认识"，也就是**不动那份文件**。宁可漏升级一次，也不能因为读不
// 出来就把别人的 gpm 覆盖掉 —— 那正是 D29 当初那条"不覆盖"要防的事。
//
// 会去执行那个文件，这件事本身是安全的：它就在用户自己的家里、就叫 gpm，能往那儿
// 写文件的人本来就已经能以这个用户的身份跑代码了。`--version` 从 v0.5.0 起就有，
// 所以能问到的历史版本范围够用；再老的会走到 ok=false 那条路。
func readSelfVersion(path string) (gpmVersion, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return gpmVersion{}, false
	}
	return parseVersion(string(out))
}

// selfIsNewer 判断包里这份（selfVersion，来自 -ldflags 注进来的 main.version）
// 是不是比家里那份（old，问出来的）新。
//
// 任一边读不出来都返回 false：拿不准就不动，这是这个函数的全部要点。
func selfIsNewer(selfVersion, old string) bool {
	newer, ok1 := parseVersion(selfVersion)
	older, ok2 := parseVersion(old)
	if !ok1 || !ok2 {
		return false
	}
	return newer.compare(older) > 0
}
