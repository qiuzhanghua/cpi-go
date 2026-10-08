# cpi 包格式与安装布局（A 方案 · 已冻结）

本文只记录**已经拍板**的接口。它是 `AI Desk` 的 CI 与 `cpi` 之间唯一的契约，
两个仓库各自独立演进时以本文为准。

## 1. 三项裁决

| # | 问题 | 裁决 |
|---|---|---|
| 1 | `~/ad` 与 cpi 家目录的关系 | **`~/ad` 就是 `CPI_HOME`**。cpi 的全部内部结构（`bin/`、`lib/`、`state.json`、`staging/`）都住在里面。 |
| 2 | cpi 怎么到用户手上 | **分发包自带 cpi**：zip 里同时放 cpi 二进制与三行 bootstrap 脚本，用户解压后跑脚本即可，不需要预装任何东西。 |
| 3 | cpi 的范围 | **收窄为单应用安装器**：不再做多应用清单、镜像表、版本源、离线分发。 |

裁决 1 的直接好处：**卸载 = 删掉一个目录**，整个 `~/ad` 可以整体搬走或放进 U 盘。

裁决 1 的代价（明确接受）：**`~/ad` 只能装一个应用**——多应用会互相覆盖。
本项目的定位就是"给一个应用做一个安装器"，所以这个代价可以接受。

## 2. 分发包

一个平台一个归档，命名 `<id>-<version>-<os>-<arch>.zip`，例如
`ai-desk-1.0.0-darwin-arm64.zip`。

```
ai-desk-1.0.0-darwin-arm64.zip
├── install.sh          # macOS / Linux 的三行 bootstrap
├── install.cmd         # Windows 的三行 bootstrap
├── cpi                 # 本平台的 cpi 二进制（Windows 为 cpi.exe）
├── manifest.yaml       # 清单
├── payload/            # 载荷，内容会被原样安装到包目录
│   └── AI Desk.app
└── SHA256SUMS          # 校验和
```

* 归档里只放本平台需要的入口形态，`manifest.yaml` 里仍可写全三个平台（见下）。
* 平台不匹配时 cpi 直接拒绝安装，不会装出一个跑不起来的目录。
* 这个 zip 由 **`cpi pack`** 产出（`cpi pack <装配目录> [--os OS] [--arch ARCH] [--cpi 可执行文件]`）。
  装配目录里只需要 `manifest.yaml` + `payload/`，其余三样是 `cpi pack` 现场生成的——
  所以**打出来的 `SHA256SUMS`、`install.sh`、`install.cmd` 在三个平台上是同一份实现**，
  不靠每个仓库各写一遍打包脚本（理由见 [`DESIGN.md`](DESIGN.md) §2.15）。

## 3. manifest.yaml

```yaml
id: ai-desk            # 小写字母/数字/._-，目录名与账本主键
name: AI Desk          # 显示名，用来建 ~/Applications/AI Desk.app
version: 1.0.0
entry:
  darwin:  { bundle: AI Desk.app }   # 相对于 payload/，macOS 用 .app
  linux:   { exe: ad }               # 相对于 payload/，裸可执行文件
  windows: { exe: ad.exe }
launch:
  cmd: ad                          # 终端里敲的命令名
  mode: activate                   # 仅 macOS 的 bundle 有意义：activate | direct
```

规则：

* `entry.<goos>` **必须恰好**给 `bundle` 或 `exe` 之一。同时给或都不给都报错。
* `entry` 里的路径**相对于 `payload/`**，也**相对于安装后的包目录**。
  也就是说 `bundle: AI Desk.app` 会落到
  `~/ad/lib/ai-desk_1.0.0_darwin_arm64/AI Desk.app`。
* 路径必须是相对路径，禁止绝对路径、盘符、`..`。
* `launch.cmd` 必须是合法的可执行文件名（不含 `/`、`\`、`..`）。
* `launch.mode`：
  * `activate`（默认）：macOS 上生成 `exec open "<app>" --args "$@"`，
    与双击完全等价，能激活已运行的实例。
  * `direct`：macOS 上直接跑 `.app/Contents/MacOS/<CFBundleExecutable>`，
    能拿到 stdout、退出码和 `Ctrl+C`。
* 上游直接发 `.app` 就用 `bundle`。**本版没有 `bin`**：上游只发裸可执行文件的
  macOS 应用，现在装不了（合成 `.app` 外壳的设计已定、代码未实现，见
  [`DESIGN.md`](DESIGN.md) §2.7 与 R8）。硬把裸 Mach-O 当 `exe` 收进来，
  用户拿到的是一个没有应用身份的东西——双击会被交给终端应用执行。

## 4. SHA256SUMS

标准格式，一行一条：

```
<64 位小写 hex>  <相对路径>
```

* `payload/` 下的**每一个常规文件都必须出现在清单里**，cpi 逐条校验，
  有一条对不上就中止安装。符号链接不参与校验。
* 缺失 `SHA256SUMS` 时 cpi 打印警告并继续，账本把这次安装标成 `unverified`。
* `cpi install --skip-verify` 可以显式跳过校验（只用于调试）。
* 这份文件由 `cpi pack` 生成（按路径排序、每行 `<hash>` + 两个空格 + 路径），
  发布者不需要自己算——包括 `AI Desk` 的 CI。

## 5. 安装后的布局

```
~/ad/
├── bin/
│   ├── cpi                       # 自动拷进来，保证之后 list/uninstall 可用
│   └── ad                        # 终端启动器（Windows 上是 ad.cmd）
├── lib/
│   └── ai-desk_1.0.0_darwin_arm64/
│       └── AI Desk.app           # payload/ 的内容原样落在这里
├── staging/                      # 解包中转，每次安装一个 unpack-<纳秒>，结束即删
└── state.json                    # 账本：所有外部副作用的唯一真相
```

除这四样之外，`~/ad` 下不放别的东西。

包目录名固定为 `<id>_<version>_<os>_<arch>`。**重装是覆盖式的、不是并存的**：
同一个 `id` 再装一次，cpi 先按卸载流程删掉旧包目录与启动器，再落新的
（所以"升级"中间有一小段窗口期旧版本已经不在了——本版接受这个代价，
换来的是一次只有一份 `lib/` 的简单状态）。

## 6. 外部副作用（全部登记在 `state.json`）

| 平台 | 副作用 | 卸载时 |
|---|---|---|
| 全平台 | `<CPI_HOME>/bin/<cmd>` 启动器（Windows 上是 `<cmd>.cmd`） | 删除 |
| 全平台 | `<CPI_HOME>/bin/cpi`（Windows 上是 `cpi.exe`）自拷贝 | 账本为空时删除 |
| 全平台 | `<CPI_HOME>/lib/<id>_<ver>_<os>_<arch>/` | 整目录删除 |
| macOS | `~/Applications/<name>.app` 软链（进启动台 / Spotlight） | 删除 |
| Linux | `~/.local/share/applications/<id>.desktop`（遵守 `$XDG_DATA_HOME`） | 删除 |
| POSIX | shell 配置里的 `# >>> cpi >>>` … `# <<< cpi <<<` 标记块 | 摘除标记块 |
| Windows | `HKCU\Environment` 的 `Path` 最前面那一条 | 摘除那一条 |
| Windows | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\cpi\<name>.lnk` | 删除 |

**PATH 集成是唯一需要用户点头的副作用**：写入 shell 配置前必须用人话问一次，
用户拒绝时软件照装、图标照建，只是终端里敲不出来（功能降级，不是失败）。

macOS 上 zsh 不读 `~/.profile`，所以落点按 `$SHELL` 计算：zsh 写
`~/.zprofile` + `~/.zshrc`，bash 写 `~/.bash_profile`，fish 写
`~/.config/fish/config.fish`；不论 `$SHELL` 都再写一份 `~/.profile` 兜底。
标记块本身是幂等守卫，重复写入不会把 PATH 撑大。

Windows 上的两条硬规定：

* **读出什么类型就写回什么类型**（`REG_SZ` / `REG_EXPAND_SZ`）。用
  `[Environment]::SetEnvironmentVariable(..., "User")` 会把 `REG_EXPAND_SZ`
  压成 `REG_SZ`，用户原有的 `%USERPROFILE%` 之类从此不再展开——所以必须直接操作
  注册表原值。写完广播 `WM_SETTINGCHANGE`，否则已经开着的进程不会重读环境。
* **往返必须逐字节保真**：插入时原值一个字节不动（只在最前面加一条），摘除时按
  `;` 切分、只丢掉命中的那一条、**空条目原样保留**（Windows 上空条目表示"当前
  目录"，用户 PATH 的默认值结尾本来就带一个分号）。判重要归一化大小写、正反斜杠
  与未展开的 `%VAR%`。

## 7. bootstrap 脚本

`install.sh`：

```sh
#!/bin/sh
set -eu
cd "$(dirname "$0")"
chmod +x ./cpi 2>/dev/null || true
exec ./cpi install . --dir "${CPI_HOME:-$HOME/ad}"
```

`install.cmd`：

```bat
@echo off
setlocal
cd /d "%~dp0"
if not defined CPI_HOME set "CPI_HOME=%USERPROFILE%\ad"
cpi.exe install . --dir "%CPI_HOME%"
pause
```

脚本只做三件事：切到自己的目录、把 CPI_HOME 解析成一个具体路径、把 cpi 叫起来。
**安装逻辑一行都不在脚本里**，所以三个平台不会各自跑偏。

## 8. CLI（本版）

```
cpi install <目录或 .zip> [--dir PATH] [--yes] [--no-path] [--skip-verify]
cpi list
cpi where <id>
cpi uninstall <id> [--dir PATH]
cpi env [--dir PATH]          # 打印 export PATH=... （PATH 集成被拒时用）
cpi pack <装配目录> [--out PATH] [--os OS] [--arch ARCH] [--cpi 可执行文件]
```

`CPI_HOME` 的解析优先级：`--dir` > 环境变量 `CPI_HOME` > `~/ad`。

`cpi pack` 是**发布者**侧的（给自己 CI 用），不是终端用户用的；它的输出就是本文第 2 节那个 zip。

## 9. 与设计文档的关系

本文是**契约**：字段、路径、脚本、CLI 以本文为准。
"为什么这么设计"、三平台现状、CI、风险与开放问题、以及被废弃的 v1/v2 范围，
都在 [`DESIGN.md`](DESIGN.md)（现为 v3.1：单应用范围 + 三平台已落地 + CI 跑绿）。
