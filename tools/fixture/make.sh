#!/usr/bin/env bash
# 造一个用于测试 gpm 的分发包：<out>/<id>-<version>-<os>-<arch>.zip
#
#   tools/fixture/make.sh -c ./build/gpm -a /path/to/Real.app
#   tools/fixture/make.sh                          # 不带 -a 时造一个假的 .app
#   tools/fixture/make.sh -r cot -t ./build/cot    # 连工具链一起装进包里
#
# 选项：
#   -v 版本（默认 1.0.0）      -o 输出目录（默认 dist）
#   -a 真实 .app 目录          -c gpm 二进制（放进包里）
#   -n 显示名（默认 AI Desk）  -i 包 id（默认 ai-desk）  -x 简称/命令名（默认 ad）
#   -r 工具链（cot / tdp，默认不带）  -t 那份工具链的二进制（放 tools/<os>_<arch>/）
set -euo pipefail

id=${ID:-ai-desk}
name=${NAME:-AI Desk}
cmd=${CMD:-ad}
version=${VERSION:-1.0.0}
out=${OUT:-dist}
app_src=${APP_SRC:-}
gpm_bin=${GPM_BIN:-}
requires=${REQUIRES:-}
tool_bin=${TOOL_BIN:-}

while getopts "v:o:a:c:n:i:x:r:t:h" opt; do
  case $opt in
    v) version=$OPTARG ;;
    o) out=$OPTARG ;;
    a) app_src=$OPTARG ;;
    c) gpm_bin=$OPTARG ;;
    n) name=$OPTARG ;;
    i) id=$OPTARG ;;
    x) cmd=$OPTARG ;;
    r) requires=$OPTARG ;;
    t) tool_bin=$OPTARG ;;
    h) sed -n '2,17p' "$0"; exit 0 ;;
    *) exit 2 ;;
  esac
done

if [ -n "$requires" ] && [ -z "$tool_bin" ]; then
  echo "声明了 -r $requires 却没给 -t：包里没有工具链，gpm 会当场拒绝装（D32）" >&2
  exit 1
fi

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

# 清单名就是简称：ad → ad-manifest.yaml（v3.5 起，见 D22）。
manifest="$cmd-manifest.yaml"
{
  echo "id: $id"
  echo "name: $name"
  echo "version: $version"
  if [ -n "$requires" ]; then
    echo "requires: [${requires%%,*}]"
  fi
  echo "entry:"
  echo "$entry_block"
  echo "launch:"
  echo "  cmd: $cmd"
} > "$pkg/$manifest"

# install.sh / install.cmd 只负责切目录、补可执行位、把安装交给 gpm
# （与 `gpm pack` 生成的一模一样）。装到哪儿由清单的 requires 决定：
# 有工具链就装进那家的家，没有就交给 gpm 自己定（平台数据目录/<简称>）。
if [ -n "$requires" ]; then
  first=${requires%%,*}
  upper=$(printf '%s' "$first" | tr '[:lower:]' '[:upper:]')
  cat > "$pkg/install.sh" <<EOF
#!/bin/sh
# 由 tools/fixture/make.sh 生成，请勿手工编辑。
set -eu
cd "\$(dirname "\$0")"
chmod +x ./gpm 2>/dev/null || true
exec ./gpm install . --dir "\${${upper}_HOME:-\$HOME/$first}"
EOF
  cat > "$pkg/install.cmd" <<EOF
@echo off
rem 由 tools/fixture/make.sh 生成，请勿手工编辑。
setlocal
cd /d "%~dp0"
if not defined ${upper}_HOME set "${upper}_HOME=%USERPROFILE%\\$first"
gpm.exe install . --dir "%${upper}_HOME%"
pause
EOF
else
  cat > "$pkg/install.sh" <<'EOF'
#!/bin/sh
# 由 tools/fixture/make.sh 生成，请勿手工编辑。
set -eu
cd "$(dirname "$0")"
chmod +x ./gpm 2>/dev/null || true
exec ./gpm install .
EOF
  cat > "$pkg/install.cmd" <<'EOF'
@echo off
rem 由 tools/fixture/make.sh 生成，请勿手工编辑。
setlocal
cd /d "%~dp0"
gpm.exe install .
pause
EOF
fi
chmod +x "$pkg/install.sh"

if [ -n "$requires" ]; then
  first=${requires%%,*}
  dest=$first
  [ "$os" = windows ] && dest="$first.exe"
  mkdir -p "$pkg/tools/${os}_${arch}"
  cp "$tool_bin" "$pkg/tools/${os}_${arch}/$dest"
  chmod +x "$pkg/tools/${os}_${arch}/$dest"
fi

members="install.sh install.cmd $manifest payload"
if [ -n "$gpm_bin" ]; then
  cp "$gpm_bin" "$pkg/gpm"
  chmod +x "$pkg/gpm"
  members="install.sh install.cmd gpm $manifest payload"
fi
[ -n "$requires" ] && members="$members tools"

# 逐条 sha256：用 -print0 读，才能应付 .app 名字里的空格；工具链也在覆盖范围内。
(
  cd "$pkg"
  for d in payload tools; do
    [ -d "$d" ] || continue
    find "$d" -type f -print0 | while IFS= read -r -d '' f; do shasum -a 256 "$f"; done
  done | LC_ALL=C sort -k2 > SHA256SUMS
)
members="$members SHA256SUMS"

mkdir -p "$out"
zip_abs=$(cd "$out" && pwd)/"$id-$version-$os-$arch.zip"
rm -f "$zip_abs"
# shellcheck disable=SC2086
(cd "$pkg" && zip -q -r -y -X "$zip_abs" $members)

echo "造好了：$zip_abs"
echo "包内："
(cd "$pkg" && find . -maxdepth 3 | LC_ALL=C sort | sed 's/^/  /')
