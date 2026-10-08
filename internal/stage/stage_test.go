package stage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sumOf(t *testing.T, dir, rel string) string {
	t.Helper()
	sum, err := HashFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func writeSums(t *testing.T, dir string, lines ...string) {
	t.Helper()
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, SumsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 回归测试：macOS 应用的 bundle 名字里常带空格（"AI Desk.app"）。
// SHA256SUMS 的一行只能按**第一段**空白切分；按所有空白切会把
// "payload/AI Desk.app/…" 取成 "Desk.app/…"，于是校验时报“文件不存在”。
func TestVerifySumsKeepsSpacesInPath(t *testing.T) {
	dir := t.TempDir()
	rel := "payload/AI Desk.app/Contents/Info.plist"
	writeFile(t, dir, rel, "hello cpi\n")
	writeSums(t, dir, sumOf(t, dir, rel)+"  "+rel)

	ok, err := VerifySums(dir, "payload")
	if err != nil {
		t.Fatalf("带空格的路径应当校验通过，却报错：%v", err)
	}
	if !ok {
		t.Fatal("有 SHA256SUMS 时应当返回 true")
	}
}

// 空格路径 + 内容被改动 → 必须报“对不上”，而不是“文件不存在”。
func TestVerifySumsDetectsTamperInSpacedPath(t *testing.T) {
	dir := t.TempDir()
	rel := "payload/AI Desk.app/Contents/MacOS/ai-desk"
	writeFile(t, dir, rel, "original\n")
	sum := sumOf(t, dir, rel)
	writeSums(t, dir, sum+"  "+rel)

	writeFile(t, dir, rel, "tampered\n")
	_, err := VerifySums(dir, "payload")
	if err == nil {
		t.Fatal("内容改过了，应当报错")
	}
	if !strings.Contains(err.Error(), "对不上") {
		t.Fatalf("应当是 sha256 对不上，实际：%v", err)
	}
}

// sha256sum -b（二进制模式）会在路径前加一个 *。
func TestVerifySumsAcceptsBinaryMarker(t *testing.T) {
	dir := t.TempDir()
	rel := "payload/AI Desk.app/Contents/Info.plist"
	writeFile(t, dir, rel, "hello cpi\n")
	writeSums(t, dir, sumOf(t, dir, rel)+"  *"+rel)

	if _, err := VerifySums(dir, "payload"); err != nil {
		t.Fatalf("带 * 的写法应当接受，却报错：%v", err)
	}
}

// payload/ 下有文件没被 SHA256SUMS 覆盖 → 拒绝安装。
func TestVerifySumsRejectsUncoveredFile(t *testing.T) {
	dir := t.TempDir()
	listed := "payload/AI Desk.app/Contents/Info.plist"
	writeFile(t, dir, listed, "hello cpi\n")
	writeFile(t, dir, "payload/AI Desk.app/Contents/MacOS/ai-desk", "sneaky\n")
	writeSums(t, dir, sumOf(t, dir, listed)+"  "+listed)

	_, err := VerifySums(dir, "payload")
	if err == nil {
		t.Fatal("有没被覆盖的文件时应当报错")
	}
	if !strings.Contains(err.Error(), "没有覆盖") {
		t.Fatalf("应当是“没有覆盖”，实际：%v", err)
	}
}

// 没有 SHA256SUMS → 返回 false，由调用方决定是否警告。
func TestVerifySumsMissingFile(t *testing.T) {
	dir := t.TempDir()
	ok, err := VerifySums(dir, "payload")
	if err != nil {
		t.Fatalf("缺 SHA256SUMS 不该报错：%v", err)
	}
	if ok {
		t.Fatal("缺 SHA256SUMS 时应当返回 false")
	}
}
