// 命令 cpi 是一个单应用安装器：把分发包装到 ~/ad，接好终端命令与图形入口，
// 并且能用 cpi uninstall 干净地撤掉。
//
// 命令行与（将来的）图形界面是同一层内核的两个壳。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/qiuzhanghua/cpi-go/internal/home"
	"github.com/qiuzhanghua/cpi-go/internal/install"
)

// reorder 把选项挪到位置参数前面。
//
// 标准库的 flag 一旦遇到第一个非选项参数就停止解析，于是
// `cpi install . --dir ~/ad` 会把 --dir 当成第二个位置参数。这里不打算
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

const version = "0.1.0-dev"

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
	case "env":
		err = cmdEnv(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("cpi %s\n", version)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "cpi: %v\n", err)
		os.Exit(1)
	}
}

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dir := fs.String("dir", "", "装到哪个家目录（默认 $CPI_HOME，再默认 ~/ad）")
	yes := fs.Bool("yes", false, "不询问，直接做 PATH 集成")
	noPath := fs.Bool("no-path", false, "完全不碰 PATH")
	skip := fs.Bool("skip-verify", false, "跳过 SHA256SUMS 校验（只用于调试）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: cpi install <目录或 .zip> [选项]")
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
	})
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（默认 $CPI_HOME，再默认 ~/ad）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: cpi uninstall <id> [选项]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("需要且只需要一个参数：包 id")
	}
	return install.Uninstall(*dir, fs.Arg(0), os.Stdout)
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（默认 $CPI_HOME，再默认 ~/ad）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: cpi list [选项]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	return install.List(*dir, os.Stdout)
}

func cmdWhere(args []string) error {
	fs := flag.NewFlagSet("where", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（默认 $CPI_HOME，再默认 ~/ad）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: cpi where <id> [选项]")
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

func cmdEnv(args []string) error {
	fs := flag.NewFlagSet("env", flag.ExitOnError)
	dir := fs.String("dir", "", "家目录（默认 $CPI_HOME，再默认 ~/ad）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: cpi env [选项]   # 打印把 bin/ 加进 PATH 的 shell 片段")
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
	fmt.Fprintf(w, `cpi %s —— 单应用安装器

用法:
  cpi install <目录或 .zip> [--dir PATH] [--yes] [--no-path] [--skip-verify]
  cpi list
  cpi where <id>
  cpi uninstall <id>
  cpi env
  cpi version

家目录按 --dir > $CPI_HOME > ~/ad 的顺序确定。
`, version)
}
