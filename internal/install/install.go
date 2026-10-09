// Package install 是 cpi 的内核：安装、列举、卸载。
//
// 命令行与（将来的）图形界面只是这层的两个壳，走的必须是同一条流水线。
package install

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/qiuzhanghua/cpi-go/internal/home"
	"github.com/qiuzhanghua/cpi-go/internal/integrate"
	"github.com/qiuzhanghua/cpi-go/internal/ledger"
	"github.com/qiuzhanghua/cpi-go/internal/manifest"
	"github.com/qiuzhanghua/cpi-go/internal/proc"
	"github.com/qiuzhanghua/cpi-go/internal/stage"
)

// Options 控制一次安装。
type Options struct {
	Dir        string    // --dir，覆盖 CPI_HOME
	Yes        bool      // 不询问，直接做 PATH 集成
	NoPath     bool      // 完全跳过 PATH 集成
	SkipVerify bool      // 跳过 SHA256SUMS 校验（只用于调试）
	Force      bool      // 应用正在运行也照做（覆盖安装与卸载都用得上）
	In         io.Reader // 默认 os.Stdin
	Out        io.Writer // 默认 os.Stdout
}

// Install 安装一个分发包（目录或 .zip）。
func Install(src string, opt Options) error {
	out := opt.Out
	if out == nil {
		out = os.Stdout
	}
	in := opt.In
	if in == nil {
		in = os.Stdin
	}

	h, err := home.Resolve(opt.Dir)
	if err != nil {
		return err
	}
	if err := h.Ensure(); err != nil {
		return err
	}

	unpack := filepath.Join(h.Staging(), fmt.Sprintf("unpack-%d", time.Now().UnixNano()))
	if err := os.RemoveAll(unpack); err != nil {
		return err
	}
	defer os.RemoveAll(unpack)

	if err := stage.Materialize(src, unpack); err != nil {
		return err
	}

	verified := false
	if opt.SkipVerify {
		fmt.Fprintln(out, "已按 --skip-verify 跳过校验。")
	} else {
		v, err := stage.VerifySums(unpack, manifest.PayloadDir)
		if err != nil {
			return err
		}
		verified = v
		if !v {
			fmt.Fprintln(out, "警告：包里没有 SHA256SUMS，本次安装标记为 unverified。")
		}
	}

	m, err := manifest.Load(unpack)
	if err != nil {
		return err
	}
	if err := m.Validate(h.GOOS); err != nil {
		return err
	}
	entry, _ := m.EntryFor(h.GOOS)

	payloadRoot := filepath.Join(unpack, manifest.PayloadDir)
	srcEntry := filepath.Join(payloadRoot, filepath.FromSlash(entry.Rel()))
	if _, err := os.Lstat(srcEntry); err != nil {
		return fmt.Errorf("清单声明的入口在包里找不到：payload/%s", entry.Rel())
	}

	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		return err
	}
	if prev := led.Find(m.ID); prev != nil {
		if err := checkNotRunning(prev, opt.Force, "重新运行一次 cpi install", out); err != nil {
			return err
		}
		fmt.Fprintf(out, "检测到已装过的 %s %s，先卸掉旧版本。\n", prev.Name, prev.Version)
		if err := remove(h, led, prev.ID, out); err != nil {
			return err
		}
	}

	pkgDir := h.PackageDir(m.ID, m.Version)
	var rollback []func()
	fail := func(err error) error {
		for i := len(rollback) - 1; i >= 0; i-- {
			rollback[i]()
		}
		return err
	}
	rollback = append(rollback, func() { os.RemoveAll(pkgDir) })

	if err := os.RemoveAll(pkgDir); err != nil {
		return err
	}
	if err := stage.CopyTree(payloadRoot, pkgDir); err != nil {
		return fail(err)
	}
	entryAbs := filepath.Join(pkgDir, filepath.FromSlash(entry.Rel()))
	if entry.Kind() == "exe" {
		if err := os.Chmod(entryAbs, 0o755); err != nil {
			return fail(err)
		}
	}
	fmt.Fprintf(out, "已释放到 %s\n", pkgDir)

	launcher, err := integrate.Launcher(h.Bin(), m.Launch.Cmd, entryAbs, entry.Kind(), h.GOOS, m.Mode())
	if err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() { os.Remove(launcher) })
	fmt.Fprintf(out, "已生成终端启动器 %s\n", launcher)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fail(err)
	}
	var links []ledger.Link
	link, note, err := integrate.AppLink(h.GOOS, homeDir, m.ID, m.Name, entryAbs, launcher)
	if err != nil {
		return fail(err)
	}
	if note != "" {
		fmt.Fprintln(out, note)
	}
	if link != nil {
		links = append(links, *link)
		lp := link.Path
		rollback = append(rollback, func() { os.Remove(lp) })
		fmt.Fprintf(out, "已建立图形入口 %s\n", link.Path)
	}

	var paths []ledger.PathEdit
	switch {
	case opt.NoPath:
		fmt.Fprintln(out, "已按 --no-path 跳过 PATH 集成。")
	case h.GOOS == "windows":
		// Windows 的 PATH 在注册表里，不在任何文件里，所以走另一条路。
		if askPath(out, in, opt.Yes, m.Launch.Cmd, h.Bin(), h.GOOS, nil) {
			changed, err := integrate.InstallWindowsPath(h.Bin())
			if err != nil {
				return fail(err)
			}
			if changed {
				paths = append(paths, ledger.PathEdit{Path: integrate.WindowsPathEditPath})
				rollback = append(rollback, func() { integrate.RemoveWindowsPath(h.Bin()) })
				fmt.Fprintf(out, "已写入用户 PATH（注册表 HKCU\\Environment），并广播了环境变更通知。\n")
			} else {
				fmt.Fprintf(out, "用户 PATH 里已经有 %s，没有重复添加。\n", h.Bin())
			}
		} else {
			fmt.Fprintf(out, "已跳过 PATH 集成。想让当前这个 cmd 立刻能用，执行：\n  set \"PATH=%s;%%PATH%%\"\n", h.Bin())
		}
	default:
		files := integrate.ShellProfiles(h.GOOS, homeDir)
		switch {
		case askPath(out, in, opt.Yes, m.Launch.Cmd, h.Bin(), h.GOOS, files):
			edits, err := integrate.InstallPathBlock(files, h.Bin(), homeDir)
			if err != nil {
				return fail(err)
			}
			paths = edits
			rollback = append(rollback, func() { integrate.RemovePathBlock(files) })
			for _, e := range edits {
				fmt.Fprintf(out, "已写入 PATH 标记块 %s\n", e.Path)
			}
		default:
			fmt.Fprintf(out, "已跳过 PATH 集成。想让当前这个终端立刻能用，执行：\n  export PATH=\"%s:$PATH\"\n", h.Bin())
		}
	}

	self, err := installSelf(h.Bin())
	if err != nil {
		fmt.Fprintf(out, "提示：没能把 cpi 自己拷进 %s：%v\n", h.Bin(), err)
	}

	led.Put(ledger.Package{
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Platform:    h.Platform(),
		InstalledAt: time.Now(),
		Dir:         pkgDir,
		Entry:       entryAbs,
		EntryKind:   entry.Kind(),
		Cmd:         m.Launch.Cmd,
		Launcher:    launcher,
		Links:       links,
		PathEdits:   paths,
		Verified:    verified,
	})
	led.Self = self
	if err := led.Save(h.LedgerPath()); err != nil {
		return fail(err)
	}

	fmt.Fprintf(out, "\n%s %s 装好了。\n  位置：%s\n  命令：%s\n", m.Name, m.Version, pkgDir, m.Launch.Cmd)
	return nil
}

// List 打印已经装了什么。
func List(dir string, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	h, err := home.Resolve(dir)
	if err != nil {
		return err
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		return err
	}
	if len(led.Packages) == 0 {
		fmt.Fprintf(out, "%s 里还没装东西。\n", h.Root)
		return nil
	}
	fmt.Fprintf(out, "安装位置：%s\n", h.Root)
	for _, p := range led.Packages {
		flag := ""
		if !p.Verified {
			flag = "  [unverified]"
		}
		fmt.Fprintf(out, "  %s  %s  %s%s\n     入口：%s\n     命令：%s\n",
			p.ID, p.Version, p.Platform, flag, p.Entry, p.Cmd)
	}
	return nil
}

// Where 打印某个包的所有落点。
func Where(dir, id string, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	h, err := home.Resolve(dir)
	if err != nil {
		return err
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		return err
	}
	p := led.Find(id)
	if p == nil {
		return fmt.Errorf("账本里没有 %q", id)
	}
	fmt.Fprintf(out, "id        %s\nname      %s\nversion   %s\nplatform  %s\n安装目录  %s\n入口      %s\n启动器    %s\n",
		p.ID, p.Name, p.Version, p.Platform, p.Dir, p.Entry, p.Launcher)
	for _, l := range p.Links {
		fmt.Fprintf(out, "图形入口  %s\n", l.Path)
	}
	for _, e := range p.PathEdits {
		fmt.Fprintf(out, "PATH 块   %s\n", e.Path)
	}
	fmt.Fprintf(out, "安装时间  %s\n", p.InstalledAt.Format(time.RFC3339))
	return nil
}

// Uninstall 回放账本，把某个包留下的东西全部摘掉。
func Uninstall(dir, id string, force bool, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	h, err := home.Resolve(dir)
	if err != nil {
		return err
	}
	led, err := ledger.Load(h.LedgerPath())
	if err != nil {
		return err
	}
	p := led.Find(id)
	if p == nil {
		return fmt.Errorf("账本里没有 %q", id)
	}
	if err := checkNotRunning(p, force, "重新运行一次 cpi uninstall", out); err != nil {
		return err
	}
	if err := remove(h, led, id, out); err != nil {
		return err
	}
	if len(led.Packages) == 0 && led.Self != "" {
		if err := os.Remove(led.Self); err == nil {
			fmt.Fprintf(out, "已删除 %s\n", led.Self)
			led.Self = ""
		}
	}
	if err := led.Save(h.LedgerPath()); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s 已卸载。目录 %s 还在（里面可能还有你放的东西）。\n", id, h.Root)
	return nil
}

func remove(h *home.Home, led *ledger.Ledger, id string, out io.Writer) error {
	p := led.Find(id)
	if p == nil {
		return fmt.Errorf("账本里没有 %q", id)
	}
	for _, l := range p.Links {
		if err := os.Remove(l.Path); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintf(out, "警告：删不掉 %s：%v\n", l.Path, err)
			}
		} else {
			fmt.Fprintf(out, "已删除 %s\n", l.Path)
		}
	}
	if p.Launcher != "" {
		if err := os.Remove(p.Launcher); err == nil {
			fmt.Fprintf(out, "已删除 %s\n", p.Launcher)
		}
	}
	if p.Dir != "" {
		if err := os.RemoveAll(p.Dir); err == nil {
			fmt.Fprintf(out, "已删除 %s\n", p.Dir)
		}
	}
	if len(p.PathEdits) > 0 {
		// Windows 的 PATH 是一条注册表记录，账本里用伪路径表示；
		// 其余的才是 shell 配置文件里的标记块。两者不能混在一起处理。
		var files []string
		windowsPath := false
		for _, e := range p.PathEdits {
			if e.Path == integrate.WindowsPathEditPath {
				windowsPath = true
				continue
			}
			files = append(files, e.Path)
		}
		if windowsPath {
			if changed, err := integrate.RemoveWindowsPath(h.Bin()); err != nil {
				fmt.Fprintf(out, "警告：摘除注册表 PATH 失败：%v\n", err)
			} else if changed {
				fmt.Fprintln(out, "已从用户 PATH（注册表 HKCU\\Environment）里摘除")
			}
		}
		if len(files) > 0 {
			changed, err := integrate.RemovePathBlock(files)
			if err != nil {
				fmt.Fprintf(out, "警告：摘除 PATH 标记块失败：%v\n", err)
			}
			for _, f := range changed {
				fmt.Fprintf(out, "已摘除 PATH 标记块 %s\n", f)
			}
		}
		for _, e := range p.PathEdits {
			if !e.Created || e.Path == integrate.WindowsPathEditPath {
				continue
			}
			if b, err := os.ReadFile(e.Path); err == nil && strings.TrimSpace(string(b)) == "" {
				if err := os.Remove(e.Path); err == nil {
					fmt.Fprintf(out, "已删除（是我们建的空文件） %s\n", e.Path)
				}
			}
		}
	}
	led.Remove(id)
	return nil
}

// checkNotRunning 在动一个已装应用的目录之前，先看看它是不是正开着。
//
// 返回错误时调用方必须停手。真让它过去会出两种事，见 internal/proc 的开头。
//
// 查不出来的时候不拦：宁可漏拦一次，也不要因为「不知道」把安装挡死 ——
// 未来多一个平台、或者某个受限环境不让枚举进程，用户还能照常干活。
func checkNotRunning(p *ledger.Package, force bool, again string, out io.Writer) error {
	if p == nil || p.Dir == "" {
		return nil
	}
	procs, err := proc.Find(p.Dir)
	if err != nil {
		fmt.Fprintf(out, "提示：没能确认 %s 是不是正在运行（%v），这次不拦。\n", p.Name, err)
		return nil
	}
	if len(procs) == 0 {
		return nil
	}
	if force {
		fmt.Fprintf(out, "警告：%s 正在运行（%s），按 --force 继续。\n", p.Name, procsText(procs))
		return nil
	}
	fmt.Fprintf(out, "\n%s 正在运行（%s），现在动它，它脚下的文件会被换掉或抽走。\n", p.Name, procsText(procs))
	fmt.Fprintln(out, "它自己不会马上退出，但之后读到的资源、动态库、拉起的子进程都可能是另一份（版本混用）；")
	fmt.Fprintln(out, "在 macOS 上还会留下一个幽灵进程：那个位置一直指着这个旧进程，你下次点图标只会把它唤到前台，拿不到新的。")
	fmt.Fprintf(out, "请先退出 %s，再%s。\n", p.Name, again)
	fmt.Fprintln(out, "确定不在乎（比如在脚本里批量处理），加 --force。")
	return fmt.Errorf("%s 正在运行，没动手", p.Name)
}

// procsText 把进程列成给人看的一行。太多就截断：这里不需要完整清单。
func procsText(procs []proc.Process) string {
	const show = 3
	var parts []string
	for _, p := range procs {
		if p.Exe != "" {
			parts = append(parts, fmt.Sprintf("进程 %d：%s", p.PID, p.Exe))
		} else {
			parts = append(parts, fmt.Sprintf("进程 %d", p.PID))
		}
		if len(parts) == show && len(procs) > show {
			parts = append(parts, fmt.Sprintf("共 %d 个", len(procs)))
			break
		}
	}
	return strings.Join(parts, "，")
}

// installSelf 把正在运行的 cpi 拷进 <CPI_HOME>/bin，保证之后 list/uninstall 还找得到它。
func installSelf(binDir string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dest := filepath.Join(binDir, "cpi")
	if runtime.GOOS == "windows" {
		dest += ".exe"
	}
	if filepath.Clean(exe) == filepath.Clean(dest) {
		return dest, nil
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

// askPath 用人话请求一次许可。非交互环境（不是终端）时直接跳过。
//
// files 只对类 Unix 有意义；Windows 上 PATH 在注册表里，没有文件可列。
func askPath(out io.Writer, in io.Reader, yes bool, cmd, binDir, goos string, files []string) bool {
	if yes {
		return true
	}
	if !isTerminal(in) {
		fmt.Fprintln(out, "当前不是交互终端，跳过 PATH 集成（软件本身照装）。")
		return false
	}
	if goos == "windows" {
		fmt.Fprintf(out, "\n为了让命令 %q 在终端里能用，cpi 需要把\n  %s\n加进你的用户 PATH（注册表 HKCU\\Environment）。随时可以用 cpi uninstall 撤销。\n", cmd, binDir)
	} else {
		fmt.Fprintf(out, "\n为了让命令 %q 在终端里能用，cpi 需要把\n  %s\n加进 PATH。做法是在下面这些文件末尾追加一小段标记块（随时可用 cpi uninstall 撤销）：\n", cmd, binDir)
		for _, f := range files {
			fmt.Fprintf(out, "  %s\n", f)
		}
	}
	fmt.Fprint(out, "是否继续？[y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		// 一个字节都没读到：从脚本里跑的、双击运行、或者 stdin 是 /dev/null。
		// 这时候别假装用户回答了「不」，要把发生了什么、以及怎么才能装全说清楚。
		fmt.Fprintf(out, "没有读到你的输入，已跳过 PATH 集成。\n想让命令 %q 在终端里能用，重跑一次并明确同意：\n  ./cpi install . --yes\n", cmd)
		return false
	}
	s := strings.ToLower(strings.TrimSpace(line))
	return s == "y" || s == "yes"
}

func isTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
