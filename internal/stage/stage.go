// Package stage 把分发包（目录或 .zip）展开到 staging 目录，并校验 SHA256SUMS。
package stage

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SumsFile 是校验和文件名。
const SumsFile = "SHA256SUMS"

// Materialize 把 src（目录或 .zip 文件）展开到 dst。
func Materialize(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return CopyTree(src, dst)
	}
	if strings.EqualFold(filepath.Ext(src), ".zip") {
		return extractZip(src, dst)
	}
	return fmt.Errorf("%s 既不是目录也不是 .zip 包", src)
}

// CopyTree 递归复制目录，保留权限位与符号链接。
func CopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return copyFile(p, target, info.Mode().Perm())
		}
	})
}

func extractZip(src, dst string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if name == "." {
			continue
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("zip 里有越界路径 %q，拒绝解包", f.Name)
		}
		target := filepath.Join(dst, name)
		mode := f.Mode()

		switch {
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			rc, err := f.Open()
			if err != nil {
				return err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(string(b), target); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = writeStream(rc, target, mode.Perm())
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// VerifySums 校验 dir/SHA256SUMS，并要求 coverDirs 下每个常规文件都被覆盖。
// 返回是否真的做了校验（没有 SHA256SUMS 时返回 false，由调用方决定是否警告）。
//
// coverDirs 可以为空（只按清单逐条校验）。v3.5 起传 payload 与 tools 两个
// 目录：工具链的二进制也随包发出去，它跟载荷一样要有出处可查。
func VerifySums(dir string, coverDirs ...string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, SumsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	want := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// 一行形如 "<sha256><空白><路径>"。路径里**可以含空格**（macOS 的
		// "AI Desk.app" 就是），所以只能按第一段空白切开，绝不能按所有空白切：
		// 那样会把 "payload/AI Desk.app/…" 取成 "Desk.app/…"。
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			return false, fmt.Errorf("%s 格式不对: %q", SumsFile, line)
		}
		sum := strings.ToLower(line[:i])
		if len(sum) != 64 {
			return false, fmt.Errorf("%s 里的 sha256 长度不对: %q", SumsFile, line)
		}
		p := strings.TrimSpace(line[i:])
		p = strings.TrimPrefix(p, "*") // sha256sum -b 会在路径前加一个 *
		if p == "" {
			return false, fmt.Errorf("%s 格式不对: %q", SumsFile, line)
		}
		want[filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))] = sum
	}
	if len(want) == 0 {
		return false, fmt.Errorf("%s 是空的", SumsFile)
	}

	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		got, err := HashFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return false, fmt.Errorf("校验 %s 失败: %w", p, err)
		}
		if got != want[p] {
			return false, fmt.Errorf("%s 的 sha256 对不上（清单 %s，实际 %s）", p, want[p], got)
		}
	}

	var missing []string
	for _, coverDir := range coverDirs {
		if coverDir == "" {
			continue
		}
		root := filepath.Join(dir, coverDir)
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			if _, ok := want[filepath.ToSlash(rel)]; !ok {
				missing = append(missing, filepath.ToSlash(rel))
			}
			return nil
		})
		// 没有这个目录不算漏：不是每个包都带工具链。
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return false, fmt.Errorf("%s 没有覆盖这些文件，拒绝安装: %s",
			SumsFile, strings.Join(missing, ", "))
	}
	return true, nil
}

// HashFile 返回文件的 sha256 十六进制串。
func HashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeStream(in, dst, perm)
}

func writeStream(r io.Reader, dst string, perm os.FileMode) error {
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}
