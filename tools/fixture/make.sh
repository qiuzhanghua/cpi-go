#!/usr/bin/env bash
# 造一个用于测试 cpi 的分发包：<out>/<id>-<version>-<os>-<arch>.zip
#
#   tools/fixture/make.sh -c ./build/cpi -a /path/to/Real.app
#   tools/fixture/make.sh              # 不带 -a 时造一个假的 .app
#
# 选项：
#   -v 版本（默认 1.0.0）      -o 输出目录（默认 dist）
#   -a 真实 .app 目录          -c cpi 二进制（放进包里）
#   -n 显示名（默认 AI Desk）  -i 包 id（默认 ai-desk）  -x 命令名（默认 ad）
set -euo pipefail

id=${ID:-ai-desk}
name=${NAME:-AI Desk}
cmd=${CMD:-ad}
version=${VERSION:-1.0.0}
out=${OUT:-dist}
app_src=${APP_SRC:-}
cpi_bin=${CPI_BIN:-}

while getopts "v:o:a:c:n:i:x:h" opt; do
  case $opt in
    v) version=$OPTARG ;;
    o) out=$OPTARG ;;
    a) app_src=$OPTARG ;;
    c) cpi_bin=$OPTARG ;;
    n) name=$OPTARG ;;
    i) id=$OPTARG ;;
    x) cmd=$OPTARG ;;
    h) sed -n '2,14p' "$0"; exit 0 ;;
    *) exit 2 ;;
  esac
done

case $(uname -s) in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) echo "不认识的系统: $(uname -s)" >&2; exit 1 ;;
esac
case $(uname -m) in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=amd64 ;;
  *) echo "不认识的架构: $(uname -m)" >&2; exit 1 ;;
esac

root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
pkg="$root/pkg"
mkdir -p "$pkg/payload"

if [ "$os" = darwin ]; then
  bundle_name="$name.app"
  if [ -n "$app_src" ]; then
    cp -R "$app_src" "$pkg/payload/"
    bundle_name=$(basename "$app_src")
  else
    mkdir -p "$pkg/payload/$bundle_name/Contents/MacOS"
    cat > "$pkg/payload/$bundle_name/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>$name</string>
<key>CFBundleIdentifier</key><string>com.example.$(printf '%s' "$id" | tr -d -- '-')</string>
<key>CFBundleName</key><string>$name</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>
EOF
    printf '#!/bin/sh\necho "%s 起来了，参数：$*"\n' "$name" > "$pkg/payload/$bundle_name/Contents/MacOS/$name"
    chmod +x "$pkg/payload/$bundle_name/Contents/MacOS/$name"
  fi
  entry_block="  $os: { bundle: $bundle_name }"
else
  printf '#!/bin/sh\necho "%s 起来了，参数：$*"\n' "$name" > "$pkg/payload/$cmd"
  chmod +x "$pkg/payload/$cmd"
  entry_block="  $os: { exe: $cmd }"
fi

cat > "$pkg/manifest.yaml" <<EOF
id: $id
name: $name
version: $version
entry:
$entry_block
launch:
  cmd: $cmd
EOF

cat > "$pkg/install.sh" <<'EOF'
#!/bin/sh
set -eu
cd "$(dirname "$0")"
chmod +x ./cpi 2>/dev/null || true
exec ./cpi install . --dir "${CPI_HOME:-$HOME/ad}"
EOF
chmod +x "$pkg/install.sh"

cat > "$pkg/install.cmd" <<'EOF'
@echo off
setlocal
cd /d "%~dp0"
if not defined CPI_HOME set "CPI_HOME=%USERPROFILE%\ad"
cpi.exe install . --dir "%CPI_HOME%"
pause
EOF

members="install.sh install.cmd manifest.yaml payload"
if [ -n "$cpi_bin" ]; then
  cp "$cpi_bin" "$pkg/cpi"
  chmod +x "$pkg/cpi"
  members="install.sh install.cmd cpi manifest.yaml payload"
fi

# 逐条 sha256：用 -print0 读，才能应付 .app 名字里的空格
(
  cd "$pkg"
  find payload -type f -print0 | while IFS= read -r -d '' f; do shasum -a 256 "$f"; done | LC_ALL=C sort -k2 > SHA256SUMS
)
members="$members SHA256SUMS"

mkdir -p "$out"
zip_abs=$(cd "$out" && pwd)/"$id-$version-$os-$arch.zip"
rm -f "$zip_abs"
# shellcheck disable=SC2086
(cd "$pkg" && zip -q -r -y -X "$zip_abs" $members)

echo "造好了：$zip_abs"
echo "包内："
(cd "$pkg" && find . -maxdepth 2 | LC_ALL=C sort | sed 's/^/  /')
