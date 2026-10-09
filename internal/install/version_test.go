package install

import "testing"

// 版本号解析：认得的都要认（包括老 gpm 那种带 `gpm ` 前缀的整行输出），
// 认不得的必须说认不得 —— 调用方靠这个决定动不动别人的文件。
func TestParseVersion(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"0.6.1", "0.6.1"},
		{"gpm 0.6.1", "0.6.1"}, // v0.5.0 起 `--version` 打的就是这一行
		{"gpm 0.6.1\n", "0.6.1"},
		{"v0.6.1", "0.6.1"},
		{"  0.6.1  ", "0.6.1"},
		{"1", "1.0.0"},   // 缺的段按 0 算
		{"1.2", "1.2.0"}, //
		{"0.6.1-dev", "0.6.1-dev"},
		{"0.6.1-2-g1a2b3c4", "0.6.1-2-g1a2b3c4"}, // git describe 的产物
		{"1.0.0-rc.1", "1.0.0-rc.1"},
		{"1.0.0+build.5", "1.0.0"}, // 构建元数据不参与比较
		{"v1.0.0+exp.sha.5114f85", "1.0.0"},
		{"10.20.30", "10.20.30"},
	} {
		v, ok := parseVersion(c.in)
		if !ok {
			t.Errorf("%q 该认得出来", c.in)
			continue
		}
		if got := v.String(); got != c.want {
			t.Errorf("%q 解析成 %q，想要 %q", c.in, got, c.want)
		}
	}

	for _, in := range []string{
		"", "abc", "gpm", "0.6.x", "0.6.1.2", "-1.0.0", "1.0.0-", "1.0.0-a..b",
	} {
		if v, ok := parseVersion(in); ok {
			t.Errorf("%q 该认不出来，却解析成 %q", in, v.String())
		}
	}
}

// semver 的优先级规则，逐条钉住 —— 它们直接决定"换不换"。
func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.6.1", "0.6.0", 1},
		{"0.6.0", "0.6.1", -1},
		{"0.6.1", "0.6.1", 0},
		{"0.7.0", "0.6.9", 1},
		{"1.0.0", "0.99.99", 1},
		{"1.0.0", "1.0.0", 0},
		// 预发布比同名正式版小
		{"0.6.1", "0.6.1-dev", 1},
		{"0.6.1-2-g1a2b3c4", "0.6.1", -1},
		// 但比上一个正式版大
		{"0.6.1-2-g1a2b3c4", "0.6.0", 1},
		{"0.6.1-dev", "0.6.0", 1},
		// 预发布之间：数字按数值比、数字 < 字母、少的更小
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
	} {
		a, okA := parseVersion(c.a)
		b, okB := parseVersion(c.b)
		if !okA || !okB {
			t.Fatalf("%q / %q 该认得出来", c.a, c.b)
		}
		if got := a.compare(b); got != c.want {
			t.Errorf("%s vs %s = %d，想要 %d", c.a, c.b, got, c.want)
		}
		if got := b.compare(a); got != -c.want {
			t.Errorf("%s vs %s = %d，想要 %d（反对称）", c.b, c.a, got, -c.want)
		}
		if c.want == 0 && a.String() != b.String() {
			t.Errorf("%s 与 %s 判为相等，规范写法却不同：%q vs %q",
				c.a, c.b, a.String(), b.String())
		}
	}
}

// 拿不准就必须返回 false：宁可漏升级一次，也不能因为读不出来就覆盖别人的文件。
func TestSelfIsNewerRefusesToGuess(t *testing.T) {
	for _, c := range []struct {
		self, old string
		want      bool
	}{
		{"0.6.1", "0.6.0", true},
		{"0.6.0", "0.6.1", false},
		{"0.6.1", "0.6.1", false},
		{"0.6.1", "0.6.1-2-g1a2b3c4", true}, // 正式版比预发布新
		{"0.6.1-2-g1a2b3c4", "0.6.1", false},
		{"", "0.6.0", false},         // 不知道自己是谁
		{"0.6.1", "", false},         // 问不出对方是谁
		{"abc", "0.6.0", false},      //
		{"0.6.1", "abc", false},      //
		{"0.6.1", "gpm", false},      //
		{"gpm 0.6.1", "0.6.0", true}, // 自己这边也容错
	} {
		if got := selfIsNewer(c.self, c.old); got != c.want {
			t.Errorf("selfIsNewer(%q, %q) = %v，想要 %v", c.self, c.old, got, c.want)
		}
	}
}
