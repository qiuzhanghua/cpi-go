// 命令 gpm 是一个 GUI 应用安装器：把分发包装到安装器指定的家目录，接好终端
// 命令与图形入口，并且能用 gpm uninstall 干净地撤掉。
//
// 命令行与（将来的）图形界面是同一层内核的两个壳。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/qiuzhanghua/gpm-go/internal/home"
	"github.com/qiuzhanghua/gpm-go/internal/install"
	"github.com/qiuzhanghua/gpm-go/internal/pack"
)

// reorder 把选项挪到位置参数前面。
//
// 标准库的 flag 一旦遇到第一个非选项参数就停止解析，于是
// `gpm install . --dir ~/ad` 会把 --dir 当成第二个位置参数。这里不打算
// 教训用户，直接把参数重排一次；`--` 之后的一律当位置参数。
func reorder(fs *flag.FlagSet, args []string) []string {
	takesValue := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		if _, isBool := f.Value.(interface{ IsBoolFlag() bool }); !isBool {
			takesValue[f.Name] = true
		}
	})
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") || !takesValue[name] {
				continue
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

// version 是变量不是常量，好让发布流程用 -ldflags "-X main.version=..." 覆盖。
var version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "install":
		err = cmdInstall(os.Args[2:])
	case "uninstall", "remove", "rm":
		err = cmdUninstall(os.Args[2:])
	case "list", "ls":
		err = cmdList(os.Args[2:])
	case "where":
		err = cmdWhere(os.Args[2:])
	case "pack":
		err = cmdPack(os.Args[2:])
	case "env":
		err = cmdEnv(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("gpm %s\n", version)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpm: %v\n", err)
		os.Exit(1)
	}
}

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dir := fs.String("dir", "", "装到哪个家目录（$GPM_HOME > 从 gpm 自己的位置推断 > 当前目录）")
	yes := fs.Bool("yes", false, "不询问，直接做 PATH 集成")
	noPath := fs.Bool("no-path", false, "完全不碰 PATH")
	skip := fs.Bool("skip-verify", false, "跳过 SHA256SUMS 校验（只用于调试）")
	force := fs.Bool("force", false, "要换掉的那个应用正在运行也照做（不推荐）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: gpm install <目录或 .zip> [选项]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("需要且只需要一个参数：分发包路径")
	}
	return install.Install(fs.Arg(0), install.Options{
		Dir:        *dir,
		Yes:        *yes,
		NoPath:     *noPath,
		SkipVerify: *skip,
		Force:      *force,
	})
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（$GPM_HOME > 从 gpm 自己的位置推断 > 当前目录）")
	force := fs.Bool("force", false, "要删的那个应用正在运行也照做（不推荐）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: gpm uninstall <id> [选项]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("需要且只需要一个参数：包 id")
	}
	return install.Uninstall(*dir, fs.Arg(0), *force, os.Stdout)
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（$GPM_HOME > 从 gpm 自己的位置推断 > 当前目录）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: gpm list [选项]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	return install.List(*dir, os.Stdout)
}

func cmdWhere(args []string) error {
	fs := flag.NewFlagSet("where", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（$GPM_HOME > 从 gpm 自己的位置推断 > 当前目录）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: gpm where <id> [选项]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("需要且只需要一个参数：包 id")
	}
	return install.Where(*dir, fs.Arg(0), os.Stdout)
}

func cmdPack(args []string) error {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	out := fs.String("out", "", "输出 zip 路径（默认 dist/<id>-<version>-<os>-<arch>.zip）")
	goos := fs.String("os", "", "目标平台（默认当前平台）")
	goarch := fs.String("arch", "", "目标架构（默认当前架构）")
	self := fs.String("gpm", "", "要嵌进包里的 gpm 可执行文件（默认当前进程）")
	ddir := fs.String("default-dir", "", "烘进 install.sh/install.cmd 的默认安装根，如 ~/cot（留空则不指定）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: gpm pack <含 manifest.yaml 与 payload/ 的目录> [选项]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("需要且只需要一个参数：装配目录")
	}
	p, err := pack.Build(pack.Options{
		Dir:        fs.Arg(0),
		Out:        *out,
		GOOS:       *goos,
		GOARCH:     *goarch,
		Self:       *self,
		DefaultDir: *ddir,
		Log:        os.Stdout,
	})
	if err != nil {
		return err
	}
	fmt.Printf("已生成 %s\n", p)
	return nil
}

func cmdEnv(args []string) error {
	fs := flag.NewFlagSet("env", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（$GPM_HOME > 从 gpm 自己的位置推断 > 当前目录）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: gpm env [选项]   # 打印把 bin/ 加进 PATH 的 shell 片段")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	h, err := home.Resolve(*dir)
	if err != nil {
		return err
	}
	fmt.Printf("export PATH=\"%s:$PATH\"\n", h.Bin())
	return nil
}

func usage(w *os.File) {
	fmt.Fprintf(w, `gpm %s —— GUI 应用安装器

用法:
  gpm install <目录或 .zip> [--dir PATH] [--yes] [--no-path] [--skip-verify] [--force]
  gpm list
  gpm where <id>
  gpm uninstall <id> [--force]
  gpm pack <装配目录> [--out PATH] [--os OS] [--arch ARCH] [--gpm 可执行文件] [--default-dir PATH]
  gpm env
  gpm version

家目录按 --dir > $GPM_HOME > 从 gpm 自己的位置推断 > 当前目录 的顺序确定。
装到哪儿通常是安装器（install.sh / install.cmd）传进来的，那个值由
gpm pack --default-dir 烘进脚本；gpm 自己只认上面这个顺序。

「从 gpm 自己的位置推断」是给装完之后用的：布局规定带 GUI 的应用住在
<家目录>/lib，而 gpm 与终端启动器这类没有图形界面的小东西住在 <家目录>/bin。
于是 <家目录>/bin/gpm 这个位置本身就把家目录说出来了 —— 用户在新终端里
敲 gpm list / gpm where / gpm uninstall 时不必带 --dir，也不必让 shell
一直替 gpm 记着 GPM_HOME。判据要求所在目录正好叫 bin、文件名正好是 gpm、
且上一级有账本 state.json，免得把 /usr/local/bin/gpm 这种地方误当成家目录。
`, version)
}
