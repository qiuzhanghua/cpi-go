# cpi — Cross Platform Installer 设计文档（v2）

> 状态：设计阶段，待实现。本文含需求（第 1 章）与设计（第 2 章），交付物仅此一份（D5）。
> 模块名：`github.com/qiuzhanghua/cpi-go`。可执行文件：`cpi`。
> **v2 的重大前提修正**：v1 把 cpi 设想成给程序员用的命令行工具（二进制叫 `c`、解压到 `~/.local`、依赖 `source` 激活）。
> 现确认 **cpi 是给完全不懂命令行的普通人用的图形安装器**，v1 中与"命令行优先"绑定的假设全部作废（见 §0.3）。
> **范围收窄（最新）**：只安装**带 GUI 的程序**，并要求装完后从终端之类的入口也能启动（D14–D17）——纯命令行工具与语言运行时、独立资源包都已出局。
> **v2 补充（实测驱动，D18）**：macOS 上"是个应用"这件事**由 `.app` 目录承载**；上游只发裸可执行文件时，由 cpi **合成一个最小 `.app` 外壳**（§2.11）。

---

## 0. 决策快照

### 0.1 已确认决策（不再重开讨论）

| # | 维度 | 结论 |
|---|------|------|
| D1 | 定位 | **独立安装器**，完全不依赖 cot / tdp：不写 `~/cot`、不读 `tdp_plugin.yaml`、不调用 `cot install` |
| D2 | 目标用户 | **完全不懂命令行的普通人**；不会 `source`、不会配 PATH、不看日志 |
| D3 | 产品形态 | **图形界面**（Wails），双击启动，勾选要装的软件 → 进度 → 完成 |
| D4 | 平台 | **三平台同等**：darwin / linux / windows × amd64 / arm64 |
| D5 | 目录 | 安装根 `~/.cpi/`（Windows 即 `%USERPROFILE%\.cpi`），可用 **`CPI_HOME`** 覆盖 |
| D6 | 内容 | **只收录带 GUI 的应用程序**（D6 已收窄，见 D14）；语言运行时与纯命令行工具**全部不收录** |
| D7 | 装完即用 | **自动**挂 PATH（不靠手工 source）+ **自动**建图形入口（开始菜单 / 启动台 / 应用列表） |
| D8 | 卸载 | 反向清理：删目录 + 摘 PATH + 删图标，不留残渣 |
| D9 | 权限 | **只装当前用户，全程不提权**（不触发 UAC、不 sudo） |
| D10 | 网络 | 支持代理 + 自定义源 + **内置可覆盖的国内镜像默认值** + 离线/内网模式 |
| D11 | 生命周期 | **单版本覆盖式** + `install` / `list` / `uninstall`（多版本共存不是首版目标） |
| D12 | 交付物 | 仅 `docs/DESIGN.md` 一份 |
| D13 | GUI 技术选型 | **Wails**（Go + 系统 WebView：Windows WebView2 / macOS WKWebView / Linux WebKitGTK） |
| D14 | 内容范围（**收窄**） | **只收录带 GUI 的应用程序**；`uv` / `node` / `go` / `ripgrep` / `fzf` / `jq` / `gh` 这类纯命令行工具**全部出局** |
| D15 | 终端启动 | 每个已装 GUI 应用自动获得一个**终端启动器** `<CPI_HOME>/bin/<cmd>`，**参数透传**（`vlc movie.mp4`、`code .` 都能用）；卸载即删 |
| D16 | 启动语义 | macOS 默认**激活式**：`exec open "<应用包绝对路径>" --args "$@"`（等价双击、能激活已在运行的实例）；Windows / Linux 直接运行应用主程序 |
| D17 | 捆绑 CLI | 应用自带真命令行（如 VSCode 的 `code`）走 `expose`，得到完整终端语义（stdout / 退出码 / `Ctrl+C`）；它是应用的**附属能力**，不是独立的包类型 |
| D18 | 应用外壳 | macOS 上 GUI 程序**必须以 `.app` 形态落地**。`entry.darwin` 有两个形态：**`bundle:`**（上游直接发 `.app`，用现成的）/ **`bin:`**（上游只发裸可执行文件，由 cpi **合成一个最小 `.app` 外壳**：`Info.plist` 4 键 + `Contents/MacOS/<exe>` **硬链接**；**禁用符号链接**，见 §2.11） |

### 0.2 由上述决策推导出的硬性设计约束

- 用户可见的一切**不得出现** `PATH` / `sha256` / `tarball` / `~/.cpi/lib/...` 这类术语；技术细节只允许出现在"查看详情"里。
- 不存在"激活"这一步。安装完成的判定必须是：**新开一个终端能直接敲命令**，**图标能直接点开**。
- GUI 必须能双击运行：macOS 要有 `.app`，Windows 要有无控制台窗口的 `.exe`，Linux 要有 AppImage 或等价。
- 只收录**有"当前用户级 / 便携式"官方分发**的软件；必须提权才能装的软件**不收录**（D9 优先于覆盖面）。
- macOS 上"应用身份"只能由 `.app` 给出：**落到 `lib/` 里的每个 macOS 应用都必须是一个 `.app`**——上游给了就用，没给就由 cpi 合成（D18、C9）。合成出来的外壳只带"身份"（名字、标识符、图标），**不代上游声明文件关联**。

### 0.3 v1 → v2 变更记录

| 项 | v1（作废） | v2（现行） | 原因 |
|---|---|---|---|
| 受众 | 程序员 | 不懂命令行的普通人 | 用户澄清：`c` 激活 cot 那套是给程序员用的 |
| 二进制名 | `c` | `cpi` | `~/.zshrc:124` 已有 `alias c=". ~/cot/bin/activate"`，命名冲突 |
| 界面 | CLI 为主 | **GUI 为主**，CLI 内核保留 | 普通人不会开终端 |
| 装到哪 | `~/.local/{bin,share/cpi}` | `~/.cpi/` | 独立自持、可整体删除、不与 XDG 混装 |
| 生效方式 | 写 shell 配置 + `c activate` | **自动**挂 PATH + **自动**建图标 | 普通人不会 source，也不会找 `~/.local/bin` |
| 内容 | 仅开发工具 | **只有带 GUI 的应用程序** | 用户明确："我要缩小范围，只安装带 GUI 的程序" |
| git/curl/make | 探测型交付 | **完全不纳入** | 它们无官方可搬运二进制，且"探测一个不存在的东西"对普通人无意义 |
| 权限 | 不 sudo | 不变，但扩展为"不提权"（Windows 上即不使用 UAC） | D9 |
| 清单 | 内置 YAML + 覆盖 | 不变（见 §5 开放问题 O1 的确认说明） | 工具数量决定不可能写死在代码里 |
| 平台优先级 | macOS 优先 | 三平台同等 | D4 |
| 终端启动 | 无此概念（v1 靠 `source` 激活 CLI） | **每个 GUI 应用自动获得终端启动器**，终端敲应用名即可拉起 | 用户要求"装完后从 terminal 之类的也可以启动" |
| 交付物分类 | 曾设想三类（cli / app / resource） | **收敛为一类：GUI 应用**（+ 可选捆绑 CLI + 自动合成的终端启动器） | 范围收窄后，纯命令行工具与独立资源包都不存在了 |
| PATH 落点 | 只写 `~/.profile` | **按 `$SHELL` 写对应文件**（zsh 不读 `.profile`）+ 兼顾 `~/.profile` | macOS 默认 zsh，只写 `.profile` 等于没装 |
| macOS 应用形态 | 隐含假设"上游都发 `.app`" | **两个形态**：`bundle:` 用现成的 / `bin:` 由 cpi 合成最小外壳（D18） | 实测：裸 Mach-O 的 UTI 是 `public.unix-executable`，LaunchServices 把它当"交给终端运行的文档"，不是应用 |
| 合成外壳的内层可执行文件 | —（v1 未考虑） | **硬链接（首选，零拷贝）或拷贝，禁用符号链接** | 实测：符号链接会让进程内的 `NSBundle.mainBundle` 解析到链接目标，`bundleIdentifier` / `CFBundleName` 全成 `(null)`，外壳形同虚设 |

### 0.4 待确认假设（请评审时裁决）

- **A1**：清单采用 `go:embed` 内置 YAML + 外部 YAML 字段级覆盖。若"不编"的意思是"先别写代码"而非"别写死在 Go 里"，本假设依然成立，不阻塞设计。
- **A2**：**校验策略**——上游提供官方 checksum 文件时**强制校验**；不提供时（不少 GUI 应用的 release 只发归档）**警告后继续**，并在账本中标注 `unverified`。
- **A3**：产品在界面上的显示名暂定「软件安装器」，`cpi` 只作为技术标识（见 §5 O2）。

---

## 1. 需求

### 1.1 一句话

给完全不懂命令行的普通人一个双击就能用的安装器：勾选想要的软件，点"安装"，装完终端里敲得动、图标里点得开、不想要了能干净卸掉，全程不需要管理员密码。

### 1.2 目标用户画像

| 属性 | 取值 | 对设计的直接后果 |
|---|---|---|
| 命令行能力 | 无 | 不许出现"请执行 xxx"；一切用按钮表达 |
| 对 PATH 的认知 | 无 | 必须自动挂；挂完要提示"重新打开终端后生效" |
| 对权限弹窗的反应 | 慌张、易放弃 | 全程不提权；若某软件必须提权则**不提供该软件** |
| 出错时的行为 | 关掉重来或放弃 | 错误必须人话 + 一个明确的下一步按钮 |
| 对进度/等待的容忍 | 低 | 必须有可见进度、可取消、失败能重试 |
| 语言 | 中文为主 | 界面默认中文，预留 i18n |

### 1.3 核心场景

| # | 场景 | 触发 | 期望 |
|---|------|------|------|
| S1 | 新机装机 | 双击打开 cpi，勾选推荐清单，点安装 | 进度跑完 → 图标出现在开始菜单/启动台/应用列表，**且新开终端敲应用名也能启动** |
| S2 | 单个补充 | 只想装 VSCode | 勾它一个 → 下载 → 出现在启动台，点开即用 |
| S3 | 重复运行 | 再打开 cpi | 已装的显示"已安装"，不重下、不重写 |
| S4 | 卸载 | 在界面里对某项点"卸载" | 目录删掉、PATH 摘掉、图标消失；`cpi list` 无此项 |
| S5 | 内网/离线 | 外网机下载好，拷到内网机 | 用离线模式零外网完成安装，校验照样通过 |
| S6 | 出问题 | 安装失败 | 界面显示人话原因 + "重试" + "查看详情"（详情里才给技术信息） |
| S7 | 从终端拉起 | 已经装好 VLC | 新开终端敲 `vlc movie.mp4` 直接播放；敲 `vlc` 不带参数就等于双击图标 |
| S8 | 高级用户 | 不想开图形界面 | `cpi install vlc` 等 CLI 动词可用，行为与 GUI 完全一致（装的东西仍是 GUI 应用） |

### 1.4 功能需求

| ID | 需求 | 优先级 |
|---|---|---|
| FR-1 | GUI 双击启动，展示分类软件卡片（名称、一句话说明、大小、是否已装） | 必须 |
| FR-2 | 勾选多个软件后一次性安装；串行或有限并发，进度逐项可见 | 必须 |
| FR-3 | 安装过程可**取消**；取消后已完成的项保留，未完成的项不留半成品 | 必须 |
| FR-4 | 失败可**重试**，重试只重做失败项 | 必须 |
| FR-5 | 下载后校验（策略见 A2），校验失败自动重下一次，再失败才报错 | 必须 |
| FR-6 | 每个已装应用在 `<CPI_HOME>/bin/<cmd>` 生成**终端启动器**并自动挂到 PATH，**新开终端敲应用名即可启动**，参数透传 | 必须 |
| FR-7 | 自动为应用建图形入口（Windows 开始菜单、macOS `~/Applications`、Linux `.desktop`） | 必须 |
| FR-8 | 卸载时反向清理目录、PATH 条目、图形入口、终端启动器 | 必须 |
| FR-9 | 已装检测与幂等：同版本重复安装 = 跳过（`--force` 可强制重装） | 必须 |
| FR-10 | 预演：安装前可算出总量、预计大小，且不产生副作用 | 应该 |
| FR-11 | 体检：报告安装完整性、PATH 是否生效、上游是否可达、并给修复建议 | 应该 |
| FR-12 | 离线/内网模式：从本地目录或内容寻址缓存安装，零外网 | 应该 |
| FR-13 | 自定义源/镜像：URL 前缀替换，可离线配置 | 应该 |
| FR-14 | 日志：本地滚动日志文件，普通人不用看，但排障时能一键打开所在目录 | 应该 |
| FR-15 | CLI 内核（`install` / `list` / `uninstall` / `plan` / `doctor` / `version`，支持 `--json`） | 应该 |
| FR-16 | 中英文界面切换 | 可选 |
| FR-17 | cpi 自身更新检查 | 暂缓 |
| FR-18 | 终端启动器默认按清单声明的模式生成（macOS 默认**激活式** `open`，Windows/Linux 直接跑主程序），并透传全部参数 | 必须 |
| FR-19 | 应用自带 CLI（如 `code`）时，终端语义完整：stdout、退出码、`Ctrl+C` 可用 | 必须 |
| FR-20 | 命令名与系统已有命令冲突时**不覆盖**，跳过该项启动器并明确告知 | 必须 |
| FR-21 | 上游只发裸可执行文件的 macOS 应用，由 cpi **合成最小 `.app` 外壳**（`Info.plist` + `Contents/MacOS/<exe>` 硬链接 + 可选图标），使它在启动台 / Spotlight / "打开方式"里的表现与上游自带 `.app` 的软件一致 | 必须 |

### 1.5 非功能需求

| ID | 需求 |
|---|---|
| NFR-1 | 全程当前用户权限，不写系统目录，不触发 UAC / sudo |
| NFR-2 | 失败可恢复：任何中断都不留半装目录、不破坏已有安装 |
| NFR-3 | 幂等：重复执行不产生重复副作用（PATH 块、图标、注册表项） |
| NFR-4 | 可整体删除：`~/.cpi` 删掉后，除 shell 配置里的标记块与图标外无残留 |
| NFR-5 | 原子落盘：临时目录与 staging 一律建在**目标根之下**，不用 `$TMPDIR`（跨文件系统 rename 非原子） |
| NFR-6 | 不修改被装软件自身的配置（不替用户改 npm/pip/git 配置），也不往用户可见的文档目录（`~/Documents`、桌面）写任何文件 |
| NFR-7 | 磁盘与网络友好：断点续传、并发上限、失败退避 |
| NFR-8 | 启动快：GUI 冷启动 < 2s（清单已 embed，不做网络请求即可渲染首屏） |
| NFR-9 | 界面可缩放、跟随系统深浅色、中文字体不缺字 |

### 1.6 非目标（v2 明确不做）

- **不收录纯命令行工具与语言运行时**（`uv` / `node` / `go` / `ripgrep` / `fzf` / `jq` / `gh`…），也不做独立的字体 / 配置 / 数据资源包（D14）。清单里只有带 GUI 的程序。
- 不做通用包管理器 / 不做应用商店 / 不做依赖求解（没有版本约束求解，一个软件一个版本）。
- 不管理系统级软件（`/Applications`、`Program Files`、`/usr/local`），不替代 brew / apt / winget / choco。
- **不收录必须提权的软件**，也不提供"以管理员身份重试"这种出口。
- 不做多版本共存与切换（D11）；升级 = 覆盖式重装新版本。
- 不做沙箱、不做容器、不做系统还原点。
- 不联网上报任何使用数据（遥测默认且仅有关闭选项）。

### 1.7 硬约束（已探明）

| # | 约束 | 后果 |
|---|------|------|
| C1 | git / curl / make 无官方可搬运二进制（curl.se 与 GNU make 只发源码；git 在 macOS 只发 `.pkg`，Linux 只有发行版包） | 完全不纳入清单（D6 修订）；普通人需要的 git 由桌面软件入口或系统自带解决 |
| C2 | 只装当前用户（D9）⇒ 只收便携/用户级分发 | 大量 Windows 软件（机器级 NSIS/MSI）只能排除；Windows 侧目录天然偏小 |
| C3 | macOS Gatekeeper：未签名/未公证的应用会被拦 | cpi 自身发布需签名+公证（§2.18）；被装软件的 `.app` 来自官方签名包，解开后仍带签名，通常可通过 |
| C4 | Windows SmartScreen：未签名 exe 首次运行弹警告 | 同上，需代码签名证书；无证书时的替代方案见 §2.18 |
| C5 | Wails 依赖系统 WebView | Windows 10+ 一般自带 WebView2；旧系统需引导安装运行时；Linux 需 WebKitGTK，缺失要给人话提示 |
| C6 | 磁盘与网络（实测本机网络对境外源偏慢，`go.dev` 3MB 页面曾 8s 超时） | 内置可覆盖镜像（D10）+ 断点续传 + 校验后缓存（store）成为刚需，而非优化项 |
| C7 | 归档格式三平台不一（`.tar.gz` / `.zip` / `.dmg` / 裸二进制 / 自解压 `.exe`） | 解包层必须多适配器（§2.4） |
| C8 | 安装目录名与"已装"判定要跨平台一致 | 统一用 `<id>_<version>_<os>_<arch>` 作为版本目录名 |
| C9 | macOS 的"应用身份"由 `.app` 目录承载（UTI `com.apple.application-bundle`）；裸 Mach-O 只是 `public.unix-executable`，会被 LaunchServices 交给**终端应用**处理（本机实测，见 §2.11） | 上游只发裸可执行文件时，cpi **必须**合成最小 `.app` 外壳，否则双击不进应用、无 Dock 图标、无文件关联、无 TCC 归属 |

---

## 2. 设计

### 2.1 总体架构

一个 Go module，两个入口，内核只有一份：

```
cmd/cpi/              CLI 入口（FR-15）           ┐
cmd/cpi-gui/          GUI 入口（Wails，D13）      ├─ 都只调用 internal/，不互相 import
internal/…            内核：清单/计划/下载/解包/落盘/集成/账本 ┘
```

铁律：**内核不 import 任何 GUI 包**。GUI 通过 Wails 绑定的服务结构体调用同一批 `internal` API，并以事件流接收进度；`cpi` 命令行与 `cpi-gui` 走的是同一条安装流水线，因此"CLI 能装好的，GUI 一定能装好"。

```
┌──────────────────────────── cpi-gui (Wails) ────────────────────────────┐
│  前端（HTML/CSS/JS）  四屏状态机  ← 事件流(progress/state)  →  按钮动作   │
│         ▲ Wails bindings                        │                        │
│         └──────────────┬─────────────────────────┘                       │
└────────────────────────┼─────────────────────────────────────────────────┘
                         ▼
              ┌── internal/app（用例编排，事务边界）──┐
              │ plan.Build → install.Apply → 集成层   │
              └──┬────────┬─────────┬─────────┬──────┘
                 │        │         │         │
            manifest   fetch     archive    integrate
             (清单)   (取回)     (解包)   (PATH/启动器/图标/账本)
                                        │
                                   state.json（账本，所有外部副作用的唯一真相）
```

### 2.2 包布局

```
cmd/cpi/                     CLI
cmd/cpi-gui/                 GUI（Wails；main.go + frontend/）
internal/manifest/           清单解析、合并、校验（builtin.yaml 用 go:embed）
internal/plan/               纯函数：清单 + 环境 → Plan（无副作用）
internal/install/            执行 Plan：下载→校验→解包→落盘→集成
internal/fetch/              Fetcher：http / file / store / memfake
internal/archive/            Extractor：targz / zip / zst / raw / dmg / selfexe
internal/integrate/          PATH 集成、终端启动器、图形入口、反向清理
  shellenv/                  zsh / bash / fish / pwsh
  shortcut/                  windows(.lnk) / darwin(.app 链接) / linux(.desktop)
  appshell/                  darwin：为"上游只发裸可执行文件"的应用合成最小 .app 外壳（D18）
internal/state/              账本读写（原子）
internal/platform/           OS/arch 探测、默认目录、WebView/依赖探测
internal/app/                用例：InstallSelected / Uninstall / Plan / Doctor / List
internal/logx/               滚动日志
internal/ui/                 Reporter 接口（text / json；GUI 用事件）
frontend/                    Wails 前端
docs/DESIGN.md               本文
```

### 2.3 核心接口

```go
// manifest
type Catalog struct { Version int; Categories map[string]Category; Profiles map[string][]ToolID; Tools map[ToolID]Tool }

// plan —— 无副作用
type Plan struct { Steps []Step; Skipped []Skipped; Blocked []Blocked; TotalBytes int64 }
func Build(cat *Catalog, env *platform.Env, opt Options) (*Plan, error)

// install
type Step struct { Tool ToolID; Version string; Method Method; URL string; SHA256 string; Expose []Expose; Post [][]string }
func Apply(ctx context.Context, p *Plan, r ui.Reporter, hooks install.Hooks) (*Result, error)

// ui.Reporter（GUI 用事件实现，CLI 用 text/json 实现）
type Reporter interface {
    Plan(*Plan)
    Start(tool ToolID, total int64)
    Progress(tool ToolID, done, total int64)
    Phase(tool ToolID, ph Phase)      // 下载/校验/解包/安装/建入口
    Done(tool ToolID, err error, detail string)
    Finished(*Result)
}
```

`install.Hooks` 是 GUI 用来注入"取消检查""确认重试"的接缝。

### 2.4 接缝与适配器：哪些是真的

原则：**一个适配器是假想接缝，两个才是真接缝**。

| 接口 | 现有适配器 | 真接缝 | 理由 |
|------|-----------|--------|------|
| `fetch.Fetcher` | `http` / `file` / `store` / `memfake`(测试) | ✅ | 在线、自定义源、离线、测试各一个 |
| `archive.Extractor` | `targz` / `zip` / `zst` / `raw` / `dmg` / `selfexe` | ✅ | 三平台归档格式各异（C7） |
| `integrate.ShellEnv` | `zsh` / `bash` / `fish` / `pwsh` / `none` | ✅ | 四种壳语法与配置文件不同 |
| `integrate.Shortcut` | `windows-lnk` / `darwin-app` / `linux-desktop` | ✅ | 三平台入口机制完全不同 |
| `integrate.AppShell` | `darwin-minimal` / `none`（上游已发 `.app` 或非 macOS） | ✅ | 只有 macOS 需要合成外壳，Windows / Linux 的"外壳"就是它们本来的形态（D18） |
| `ui.Reporter` | `text` / `json` / `eventbus`(GUI) | ✅ | 人看 / 机读 / GUI 各一 |
| `state.Prober` | `exec --version`（有捆绑 CLI 时） / `file-exists` / `shortcut-exists` | ✅ | 有的应用没有 `--version`，甚至没有可执行 CLI |
| `VersionResolver` | `pin` / `github-release` / `url-feed` | ✅ | 上游元数据形态差异真实存在（可扩展，但**不为已出局的 CLI 工具保留专用解析器**） |
| `link.Linker` | `symlink` / `copy` | ✅ | Windows 无权限建符号链接时只能拷贝 |

明确**不引入**的接缝（只有一个实现的假想接缝）：命令执行器、文件系统抽象、时钟、HTTP 客户端（`*http.Client` 直接构造，测试用 `httptest.Server`）。

### 2.5 磁盘布局与 store 格式

```
<CPI_HOME>/                       默认 ~/.cpi（Windows: %USERPROFILE%\.cpi）
  bin/                            唯一需要进 PATH 的目录；里面是两类小文件：`expose` 出来的
                                  shim（软链或拷贝）与 cpi 合成的终端启动器（§2.12）
  lib/<id>_<version>_<os>_<arch>/ 解压后的软件树，版本目录不可变；macOS 应用在这里以
                                  `<Name>.app` 存在——上游给的，或 cpi 合成的（D18、§2.11）
  store/blobs/<sha256[:2]>/<sha256>  内容寻址存储（离线分发的载体），只读
  store/index.json                id+version+platform → sha256 + origin URL
  state.json                      账本：安装记录 + 所有外部副作用
  cache/                          下载临时区（.part 断点续传）
  log/cpi.log                     滚动日志
  staging/                        同文件系统的原子改名中转区（见 NFR-5）
```

**store 即离线格式**：blob 按内容命名、安装时永远校验 sha256，于是
- 外网机"下载"= 填充 store；
- 拷 `<CPI_HOME>/store` 整个目录到内网机 = 完成分发；
- 内网机 `--offline --store <dir>` = 零网络安装；
- 分发方**无法投毒**（改内容哈希就对不上），无需签名基础设施。

**原子性约束**：`rename(2)` 只在同一文件系统内原子。临时目录、staging、blob 落地一律建在 `<CPI_HOME>` 之下，绝不用系统临时目录。`state.json` 与 `index.json` 用"写临时文件 + rename"更新。

**硬链接的两处用法**：`expose` 的 shim（Windows 以外）与合成 `.app` 外壳里的 `Contents/MacOS/<exe>`（D18）。两者都落在 `<CPI_HOME>` 之下、同一文件系统，所以可以零拷贝；跨文件系统或平台不支持时由 `link.Linker` 退回拷贝（§2.4）。**唯一禁用符号链接的地方是后者**——符号链接会让应用失去自己的身份（§2.11）。

### 2.6 清单模型

清单是**数据**，不是代码（A1）。内置默认清单 `go:embed` 进二进制，零配置可用；外部文件字段级覆盖。

```yaml
# internal/manifest/builtin.yaml（节选）
version: 1

categories:
  dev:      { title: 开发工具,  order: 1 }
  media:    { title: 影音播放,  order: 2 }
  utility:  { title: 实用工具,  order: 3 }

profiles:
  recommended: [vscode, vlc, keepassxc]

tools:
  # 每一项都是一个「带 GUI 的应用程序」（D14）。
  # 一个应用有三样东西可以声明：主程序（entry）、捆绑 CLI（expose）、终端启动方式（launch）。
  vscode:
    category: dev
    title: Visual Studio Code
    summary: 写代码、看代码的编辑器
    version: latest
    checksum: { kind: none }
    methods:
      - kind: archive
        templates:
          darwin/arm64:  { url: "https://update.code.visualstudio.com/latest/darwin-arm64/stable", archive: zip }
          windows/amd64: { url: "https://update.code.visualstudio.com/latest/win32-x64-user/stable", archive: inno }
        # ① 主程序：装完出现在开始菜单 / 启动台 / 应用列表
        entry:
          darwin:  { bundle: "Visual Studio Code.app" }
          windows: { exe: "Code.exe", name: "Visual Studio Code" }
        # ② 捆绑 CLI：这个应用自带真命令行，终端语义完整（stdout / 退出码 / Ctrl+C）
        expose: [ { from: "Visual Studio Code.app/Contents/Resources/app/bin/code", as: code } ]
        # ③ 终端启动器：本应用由 ② 占了 code 这个名字，所以不必再合成
    probe: { kind: entry-exists }

  vlc:
    category: media
    title: VLC 媒体播放器
    summary: 什么格式都能放的播放器
    checksum: { kind: none }
    methods:
      - kind: archive
        templates:
          darwin/arm64:  { url: "https://get.videolan.org/vlc/{ver}/macosx/vlc-{ver}-arm64.dmg" }
          windows/amd64: { url: "https://get.videolan.org/vlc/{ver}/win64/vlc-{ver}-win64.zip", archive: zip }
          linux/amd64:   { url: "https://get.videolan.org/vlc/{ver}/linux/vlc-{ver}-x86_64.AppImage", archive: raw }
        entry:
          darwin:  { bundle: "VLC.app" }
          windows: { exe: "vlc.exe", name: "VLC media player" }
          linux:   { desktop: "vlc", exec: "vlc", icon: "vlc" }
        # 这个应用没有自带 CLI，所以由 cpi 合成终端启动器
        launch:
          cmd: vlc                     # ⇒ <CPI_HOME>/bin/vlc
          darwin: { mode: activate }   # exec open "<lib>/…/VLC.app" --args "$@"
          # windows / linux 不写也是 direct：直接跑主程序，与 .lnk / .desktop 是同一个二进制
    probe: { kind: entry-exists }

```

`entry` 在 macOS 上的两个形态（D18，**二选一**；两者落到 `lib/` 里的结果都是**一个 `.app`**）：

```yaml
entry: { darwin: { bundle: "VLC.app" } }          # ① 上游直接发 .app —— 用现成的
entry: { darwin: { bin: "vlc", name: "VLC" } }    # ② 上游只发裸可执行文件 —— cpi 合成 VLC.app 外壳（§2.11）
```

**一个应用可以声明的三样东西（"内容"的全部形态）**：

| 声明 | 作用 | 缺省行为 |
|---|---|---|
| `entry` | **主程序**：图形入口指向什么（macOS `.app` 包 / Windows `.exe` / Linux `.desktop` 的 `Exec`） | **必填**——没有它就不是 GUI 应用，校验直接报错。macOS 侧有两个形态（D18）：`bundle:` = 上游发 `.app`；`bin:` = 上游只发裸可执行文件，由 cpi 合成 `<Name>.app` 外壳（§2.11） |
| `expose` | **捆绑 CLI**：应用自带的真命令行（如 `code`），终端语义完整 | 没有就跳过 |
| `launch` | **终端启动器**：cpi 合成 `<CPI_HOME>/bin/<cmd>`，让"装完能从终端启动"成立（D15） | 没有 `expose` 时**默认生成**；`cmd` 缺省取 `id` |

- 三者互不冲突、可同时存在：VSCode 是 `entry` + `expose`（`code` 走②，不再合成③）；VLC 只有 `entry`，由③合成一个 `vlc`。
- 若 `expose` 的名字与 `launch.cmd` 相同（例如应用既自带 `code` 又想合成 `code`），**以 `expose` 为准只建一个**，不重复登记。
- `launch.mode`：`activate`（macOS 默认，等价双击、能激活已运行的实例）与 `direct`（直接跑主程序，有 stdout / 退出码）。Windows 与 Linux 上两者本就是同一件事，所以只有 macOS 有分叉。
- 由 `launch` 合成出来的启动器是**真实文件**，必须逐条进账本，卸载时逐条删除（NFR-4）。

**覆盖合并规则**（必须无歧义）：
- 按 `tools.<id>` map 合并，**字段级**覆盖（用户写 `version: "1.27.1"` 只改版本，其余继承内置）。
- `disable: [x]` 从所有 profile 剔除；`profiles.recommended: [...]` 整体替换。
- 合并后**整体校验**：未知字段报错（防拼错静默失效）、占位符只允许 `{ver}`、`expose.from` 必须在对应归档里存在、profile 引用的 id 必须存在、每个 tool 必须有 `entry`（D14：没有 `entry` 就不是 GUI 应用）、`launch.cmd` 必须是合法的可执行文件名。错误信息带 YAML 路径（如 `tools.vscode.methods[0].templates`）。
- **`launch` 合成规则（不可放宽）**：`cmd` 必须是单个文件名（禁路径分隔符、禁 `..`）；目标一律是 `<CPI_HOME>/lib/` 下的绝对路径（写入前做前缀校验）；已存在同名文件**不覆盖**，改为跳过并在界面上列为"已跳过（名称冲突）"（FR-20）。
- **`entry` 的 macOS 形态校验（D18）**：`entry.darwin` 必须**恰好**是 `bundle:` 或 `bin:` 之一（同时写两个 = 报错，防歧义）；`bundle:` 的路径必须真的出现在解包结果里；`bin:` 必须同时给 `name`（用作合成外壳的 `CFBundleName`），且 `bin` 指向的可执行文件必须真的出现在解包结果里。Windows / Linux 的 `entry` 形态不变。
- **`post` 钩子边界**（防止清单退化成脚本语言）：只能 `run: []` 执行 argv，**不经 shell**、不做变量展开、只允许调用本工具已 expose 的可执行文件，执行时把该工具的 `bin` 前置到 PATH。仅此一种形态。

### 2.7 计划算法（`plan.Build`，无副作用）

对每个候选工具按序判定，第一个命中即定：

```
1. disabled?                   → 跳过（不算失败）
2. 配置锁定版本？
   → 探测版本 == 锁定版本       → Skipped(already)
   → 否则                      → Step(pin)
3. 未锁定 → 解析 latest（github-release / url-feed；
                          离线时只查 store/index.json，不发请求）
4. 探测：可执行/入口已存在且版本满足？→ Skipped(evidence=实际版本与路径)
5. 选择交付方法（methods 顺序即优先级，取第一个当前平台有模板的）
   - archive/raw → 解析 URL + 期望 sha256 → Step
   - 平台无模板  → Blocked(upstream-no-artifact)
   - macOS：按 entry 形态决定这个 Step 是否附带"合成 .app 外壳"（bundle: 否 / bin: 是，D18）
6. 离线且 store 无此 blob → Blocked(offline-miss，提示"请先在联网机器下载"）
```

产物 `Plan` 由 `install.Apply` 执行。**计划与执行严格分离**：`cpi plan` 就是 `Build` 后直接打印（天然满足 FR-10 的"无副作用"），`Apply` 不做任何决策，只执行 `Step`。

### 2.8 安装流水线（原子步骤）

```
for each Step:
  1. fetch   → store/blobs/<sha>；已在 store 且校验通过 → 直接复用（0 下载）
               下载写 <CPI_HOME>/cache/xxx.part（支持 Range 续传）
               校验 sha256 → 通过才 rename 进 store（同 FS，原子）
  2. stage   → <CPI_HOME>/staging/<id>-<pid>/ 解包
               （strip 顶层目录、路径穿越防护、symlink 策略、可执行位恢复）
  3. post    → 执行声明式钩子（§2.6）
  4. swap    → rename staging → lib/<id>_<ver>_<os>_<arch>
               目标已存在且校验一致 → 丢弃 staging（幂等）
  5. expose  → 逐个 expose：软链或拷贝到 bin/（先建 .tmp 再 rename）
  5b. shell  → 仅 macOS 且 entry 是 bin: 形态：在 lib/<id>_…/ 里合成 <Name>.app 外壳
               （写 Info.plist + 硬链接 Contents/MacOS/<exe>，§2.11；不做符号链接）
  6. entry   → 建图形入口（§2.11），并登记到账本
  7. launch  → 合成终端启动器（§2.12）：写 bin/<cmd> + chmod +x；名字冲突则跳过并登记 Skipped
  8. ledger  → 更新 state.json（tmp + rename），登记本步产生的全部外部副作用
失败 → 删 staging 与 .part，撤销本步已建的 bin 链接/启动器/图标，state.json 不动；
       （若失败发生在 swap 之后，连 lib/<id>_…/ 整个目录一起删——含刚合成的 .app 外壳）
       已成功的工具不回滚（部分失败 = 退出码 3，GUI 显示"部分失败"）
```

版本目录不可变 ⇒ "安装新版本" = 装新目录 + 重指 shim + 登记新入口 + 删旧目录与旧入口。这样任何时刻都在运行一个完整可用的二进制，没有"正在覆盖 exe 时崩溃"的窗口。

### 2.9 网络、代理、自定义源、镜像、离线（统一模型）

一切下载都经一个 **base URL 重写** 和一条 **store 优先链**，不散落在各工具里：

```
Local ← store(命中即返回，永远校验)
      ← file base（file:// 或本地目录）
      ← mirror base（内置默认镜像，可被配置覆盖）
      ← http base（上游默认 URL）            ← 代理在此生效
```

- **镜像（D10）**：内置一份可覆盖的默认镜像表，按"上游域名 → 镜像前缀"表达；`--source` / 配置可整体关闭或改写。默认值随清单一起 embed，评审时可逐条讨论。**镜像只影响下载来源，不影响清单与校验值**——sha256 仍来自上游官方元数据（离线场景取自 `store/index.json`）。
- **`--source` 语义**：**前缀替换**。`--source https://my.mirror/go` 把上游 URL 前缀换掉，路径与文件名保持不变（通用镜像的通行约定，无需为每个镜像写适配）；`--source /mnt/usb/cpi` 则走 `file`。
- **代理**：`--proxy` > `HTTPS_PROXY`/`HTTP_PROXY`/`ALL_PROXY` > 配置项；`NO_PROXY` 原样传给 `httpproxy`。仅作用于 cpi 自己，**不写进被装软件的配置**（NFR-6）。
- **重试**：连接错误/5xx/超时 → 指数退避（1s, 2s, 4s，最多 3 次）；支持 `Range` 续传；4xx 立即失败不重试；重定向上限 10。
- **始终 TLS 校验，不提供 `--insecure`。**
- **离线/内网（FR-12）**：`--offline` 时只认 store 与 `--store <dir>`/`--source <dir>`，任何网络请求直接判 `Blocked(offline-miss)`，并在界面给出"请先在联网机器上准备"的一句话指引。

### 2.10 PATH 集成（不提权，D7）

核心目录只有一个：`<CPI_HOME>/bin`。它存在的理由就是本需求的核心——**让 GUI 应用装完之后，在终端里敲应用名也能启动**（D15）。三平台做法：

| 平台 | 机制 | 生效时机 |
|------|------|---------|
| Windows | 写 **`HKCU\Environment` 的 `Path`**（无需管理员），随后广播 `WM_SETTINGCHANGE`；新开的终端/资源管理器即生效。**PowerShell 不需要额外配置**，它继承用户环境变量 | 立即（新进程） |
| macOS | **按 `$SHELL` 决定**：zsh → `~/.zprofile`（Terminal.app 每开新窗口都是**登录** shell，读这里）**并同时写** `~/.zshrc`（应对非登录的交互 shell，如部分终端的新标签页）；bash → `~/.bash_profile`；fish → `~/.config/fish/config.fish`。**不论 `$SHELL` 是什么，都再写一份 `~/.profile`**，覆盖 bash / sh 用户及其它终端 | 新开终端 |
| Linux | 同上：bash → `~/.profile` + `~/.bashrc`，zsh → `~/.zprofile`，fish 同上；若 systemd 用户会话可用，额外写 `~/.config/environment.d/cpi.conf` | 新开终端 / 重新登录 |

> **为什么不能只写 `~/.profile`（重要）**：macOS 自 Catalina 起默认 shell 是 zsh，而 **zsh 不读 `~/.profile`**（只在被当作 `sh` / `ksh` 调用时才读）。只写 `.profile` 的后果是：软件装好了、图标也在，但普通人新开终端敲 `vlc` 依然 `command not found`——而且用 bash 自测时还发现不了。所以落点必须按 `$SHELL` 走，并保留 `.profile` 兜底。

标记块一律写成**幂等守卫**形态（重复 `source` 不会重复叠加 PATH；同一段写进多个文件也安全）：

```sh
# >>> cpi >>>
case ":$PATH:" in *":$HOME/.cpi/bin:"*) ;; *) PATH="$HOME/.cpi/bin:$PATH" ;; esac
export PATH
# <<< cpi <<<
```

```fish
# >>> cpi >>>
if not contains "$HOME/.cpi/bin" $PATH
    set -gx PATH "$HOME/.cpi/bin" $PATH
end
# <<< cpi <<<
```

- 所有写入都带 `cpi` 标记块 / 命名空间，**幂等**（已存在等价条目则不重复写）且**可撤销**（卸载时按账本回放删除）。NFR-4 的"用户手动删 `~/.cpi`"的收尾工具 = `cpi doctor --fix`。
- 已存在等价 PATH 条目（例如用户早就把 `~/.cpi/bin` 加过）→ 只提示不重复写。
- **写之前必须在 GUI 里用人话说明**："为了让软件的启动命令在终端里能用，需要修改你的 shell 配置文件（按上面规则算出的实际文件，如 `~/.zprofile`、`~/.profile`），是否允许？" —— 普通人有权拒绝；拒绝后软件仍装好、图标仍能点，只是终端里敲不出来（功能降级，不是失败）。
- 写完后提示："请**重新打开一个终端窗口**，然后输入 `vlc` 试试。"（普通人能执行的唯一动作）

### 2.11 图形入口集成与 `.app` 外壳合成（D7、D18）

| 平台 | 机制 | 落点 |
|------|------|------|
| Windows | 建 `.lnk` 快捷方式（IShellLink COM，或直接写 `.lnk` 二进制） | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\cpi\<Name>.lnk`（开始菜单，无需管理员） |
| macOS | 在 `~/Applications` 建**符号链接**指向 `lib/.../<Name>.app` | `~/Applications/<Name>.app`（启动台/聚焦会索引 `~/Applications`） |
| Linux | 写 `.desktop` 文件 + 图标 | `~/.local/share/applications/<id>.desktop`、图标进 `~/.local/share/icons/hicolor/<size>/apps/`；若 `update-desktop-database` 存在则调用 |

- 未签名 cpi 自身在 macOS 上被 Gatekeeper 拦是**在拦 cpi**，不是拦被装软件；被装软件的 `.app` 来自官方签名包，解开后签名仍在（C3）。
- Linux 上优先选 AppImage；`.deb`/`.rpm` 一律排除（要提权，违反 D9）。
- 每条入口都在账本里登记（路径 + 类型），卸载即回放删除。

**为什么 macOS 上"必须有 `.app`"（D18，本机实测）**

`.app` 不是一种文件格式，而是一个**目录**（bundle）；真正可执行的是 `<Name>.app/Contents/MacOS/<Name>`，一个普通 Mach-O。同一个 GUI 程序编成裸 Mach-O **也能跑**（窗口照常、`isActive=1`），但系统不认它是"应用"。macOS 27.0.1 / arm64 上的对照实测：

| 事实 | 裸可执行文件 | `.app` |
|---|---|---|
| 文件类型（`mdls` 给出的 UTI） | `public.unix-executable`「Unix可执行文件」 | `com.apple.application-bundle`「应用程序」 |
| Finder 双击 | 当成一份**"交给终端运行"的文档**（LaunchServices 里 `public.unix-executable` 的 `all roles:` 指向终端应用；本机是 Ghostty，stock 机器是 Terminal，装了 iTerm2 又可能变） | 正常启动应用 |
| 进程内 `NSBundle.bundlePath` | 它所在的那个**目录**（如 `/tmp/cpi-exp`） | 那个 `.app` |
| `bundleIdentifier` / `CFBundleName` | `(null)` / `(null)` | `com.example.cpi.exp` / `CpiExp` |
| 启动时 `NSApp.activationPolicy` | **2（Prohibited）**——得程序自己调 `setActivationPolicy:Regular` 才有 Dock 图标与菜单栏 | **0（Regular）** |
| 启动台 / Spotlight 应用索引、"打开方式"、文件关联（`CFBundleDocumentTypes`）、URL scheme、TCC 权限归属、公证 | 全都没有 | 有 |

→ cpi 必须保证**每个收录的 macOS 应用都以 `.app` 形态落到 `lib/` 里**：上游给了 `.app` 就用（`entry.darwin.bundle`）；上游只发裸可执行文件时，**由 cpi 合成一个最小外壳**（`entry.darwin.bin`）。这与 §2.12「上游没给终端命令，cpi 替它合成一个」是同一种补位。

> 取证口径：表中"双击"一行的依据是 LaunchServices 对 `public.unix-executable` 的绑定与角色声明（`lsregister -dump` 里 `all roles:` 指向终端应用），**不是一次亲眼看到的双击**——实验环境里终端窗口没能被拉起（`NSWorkspace openURL:` 返回成功却无反应，同一命令早前又确实执行过，行为不一致）。其余各行都是探针程序自报的身份字段，可直接复现。因此这条按"系统绑定如此"记录，不作为铁证。

**合成配方（本机已跑通）**：

```
<CPI_HOME>/lib/<id>_<ver>_darwin_<arch>/<Name>.app/Contents/
    Info.plist        # 由 cpi 生成（见下）
    MacOS/<exe>       # 指向上游裸可执行文件：硬链接（首选）或拷贝
    Resources/        # 可选：清单给了图标才建
```

- `Info.plist` **最小集只要 4 个键**就换来完整的应用身份：`CFBundleExecutable`（必须等于 `MacOS/` 里的文件名）、`CFBundleIdentifier`、`CFBundleName`、`CFBundlePackageType` = `APPL`；可选补 `CFBundleShortVersionString` / `CFBundleVersion` / `CFBundleIconFile`。实测这 4 个键足以让 `localizedName`、LaunchServices 注册、`bundleIdentifier` 全部正确。
- `CFBundleIdentifier` 可由清单覆盖，缺省 `<id>.cpi.local`——**刻意不冒用上游的官方标识**（如 `org.videolan.vlc`），免得与用户真正装的官方 `.app` 在偏好、权限、通知归属上串味。
- **`Contents/MacOS/<exe>` 只能用硬链接或拷贝，绝不能用符号链接**（实测）：符号链接会让进程内的 `NSBundle.mainBundle` 解析到链接目标，于是"外面看着是应用、里面自己不认"——`bundlePath` 变成裸二进制所在目录，`bundleIdentifier` 与 `CFBundleName` 全成 `(null)`，`pathForResource:` 之类的资源查找全部失效。硬链接与拷贝的结果都完全正确；由于 `lib/` 与 `store/` 同在 `<CPI_HOME>` 之下（同一文件系统），**硬链接零拷贝、是首选**。
- **首版只生成"身份"，不代上游声明文件关联**：`CFBundleDocumentTypes` / URL scheme 需要上游自己的类型定义，写错了会抢注系统默认打开方式，比"没有关联"更糟；往后若要支持，必须是清单**显式声明**才生成。
- **外壳没有独立签名**：`codesign -dv <合成外壳>.app` 报出的是内部 Mach-O 的 ad-hoc 身份，外壳本身不是一个签名包。所以"合成外壳的应用"与"上游自己发的 `.app`"在 Gatekeeper 面前**不同命**——见 §5 R8。

### 2.12 终端启动器集成（D15–D17）

**要解决的问题**：普通人装的是 GUI 应用，但装完往往还需要在终端里敲一句（`vlc movie.mp4`、`code .`）。而 GUI 应用默认**不会**在 PATH 里留下任何东西，所以这一步只能由 cpi 主动合成。

**两条路，按应用实际情况选**：

| 情况 | 走哪条 | 终端语义 |
|---|---|---|
| 应用自带真 CLI（VSCode 的 `code`） | 清单里的 `expose`，**cpi 不合成** | 完整：stdout、退出码、`Ctrl+C`、可进管道 |
| 应用没有 CLI（VLC、Krita…） | cpi **合成**一个启动器（`launch`） | 参数可用；输出取决于启动模式 |

**合成物的形态（三平台）**：

| 平台 | 落点 | 内容 | 与双击的等价性 |
|---|---|---|---|
| macOS | `<CPI_HOME>/bin/<cmd>`，`0755` shell 脚本 | 默认 `mode: activate`：`#!/bin/sh` + `exec open "<lib 绝对路径>/<X>.app" --args "$@"` | **完全等价双击**：`open` 走 LaunchServices，已在运行的实例会被**激活**而不是开第二个 |
| macOS | 同上（`mode: direct`） | `exec "<lib 绝对路径>/<X>.app/Contents/MacOS/<X>" "$@"` | 绕过 LaunchServices，换来 stdout / 退出码 / `Ctrl+C` 可杀 |
| Windows | `<CPI_HOME>/bin/<cmd>.cmd` | `@echo off` + `"<lib 绝对路径>\<X>.exe" %*` | 与开始菜单 `.lnk` 指向**同一个 exe** |
| Linux | `<CPI_HOME>/bin/<cmd>`，`0755` 包装脚本 | `#!/bin/sh` + `exec "<lib 绝对路径>/<X>.AppImage" "$@"` | 与 `.desktop` 的 `Exec` 指向**同一个文件** |

- **`activate` / `direct` 对两种来源的 `.app` 一视同仁**：无论 `<Name>.app` 是上游给的（`entry.darwin.bundle`）还是 cpi 合成的（`entry.darwin.bin`，§2.11），基准路径都是 `<lib 绝对路径>/<Name>.app`，**不需要分叉**。
- **为什么 Linux 用包装脚本而不是符号链接**：AppImage 依赖自身路径（`$APPIMAGE` / `argv[0]`）定位挂载点，软链在部分实现下会解析失败；脚本 `exec` 真实路径最稳。
- **为什么 Windows 用 `.cmd` 而不是把 exe 硬链接进 `bin/`**：Windows 的 exe 常按相对路径加载同目录的 DLL 与资源，硬链 / 拷贝会把它搬离自己的目录而损坏；`.cmd` 包装没有这个问题，且 `.CMD` 默认在 `PATHEXT` 里，`cmd` 与 PowerShell 下敲 `<cmd>` 都能直接命中。
- **Windows 的固有行为**：GUI 子系统的 exe 从终端启动会**立即返回、不输出、不阻塞**。这是操作系统的设计，不是 cpi 的缺陷——终端启动的价值在于"不用去开始菜单里翻"。
- **参数一律透传**：启动器必须带 `"$@"` / `%*`，否则 `vlc movie.mp4`、`code .` 这类最常见的用法会失效。
- **命令名**：`launch.cmd` 缺省取工具 id。**名字冲突时不覆盖**（FR-20）：若 `bin/<cmd>` 已存在且不归 cpi 所有，或系统 PATH 上已有一个同名命令，则跳过该启动器，并在界面与 `cpi list` 里标注"已跳过（名称冲突）"，应用本身照常装好、图标照常能用。

**顺带覆盖的其它入口**：Windows 挂上 `HKCU\Environment` 的 `Path` 之后，Win+R 里敲应用名也能启动；macOS 侧 Spotlight 索引 `~/Applications`。所以"从 terminal **之类的**也能启动"这句话里的每一种入口，都是被同一套 `bin/` 机制覆盖的，不需要额外适配。

**账本**：每个合成启动器都是**真实文件**，逐条登记（路径 + 归属工具 + 内容哈希）。卸载时按账本回放删除——这是 NFR-4「删掉 `~/.cpi` 之后除 shell 标记块与图标外无残留」在 `bin/` 这一侧的保证。

### 2.13 卸载与反向清理（D8）

`uninstall <id>`：删 `lib/<id>_<ver>_*` → 回放删除该 id 的图形入口 → **回放删除该 id 合成的终端启动器** → 摘除该 id 在 `bin/` 的 shim（`expose` 出来的）→ **仅当没有其它工具还需要 PATH 条目时**才摘 PATH 块（本设计里 `bin` 是所有工具共用的，所以 PATH 块只在"卸掉最后一个工具"时移除）→ 更新账本 → 可选清 `store` 中无引用的 blob（`cpi gc`，暂缓）。

**验收标准（可测）**：卸载后 `cpi list` 无该项；新开终端 `command -v <cmd>`（无论是 `expose` 还是合成的启动器）都找不到；开始菜单 / 启动台 / 应用列表里图标消失，`~/Applications` 下的链接消失；`lib/` 下无同名目录残留；账本里无该 id 的悬空副作用记录。

### 2.14 GUI 设计（Wails，D13）

**技术栈**：Wails v2（Go 后端 + 前端 HTML/CSS/JS，无 Node 运行时依赖），系统 WebView 渲染（C5）。前端构建产物 `go:embed` 进二进制 ⇒ 单文件分发。

**架构约定**
- `cmd/cpi-gui/main.go` 只做 Wails 装配：注册 `AppService`（薄封装 `internal/app`）与事件名。
- 进度通过 Wails 事件（`cpi:plan` / `cpi:progress` / `cpi:phase` / `cpi:done` / `cpi:finished`）推给前端；前端不轮询。
- 取消 = `context.Cancel`，走与 CLI 相同的路径。
- 单实例锁：第二次启动聚焦已有窗口，避免两个进程并发写 `~/.cpi`。
- Windows 构建使用 `-H windowsgui`（不弹黑框）。

**四屏状态机**

```
[1 选择] ──选择变化──> [2 确认] ──点"开始安装"──> [3 进度] ──全部结束──> [4 完成]
   ↑                       │                        │                     │
   └───────"返回"──────────┘                   "取消"→[4 完成(已取消)]   "重试失败项"→[3]
```

1. **选择**：按分类分组的卡片列表；每张卡片 = 名称 + 一句话说明 + 体积 + 状态徽章（未装/已装 v1.2.3/系统自带）。顶部"一键选择推荐项"。绝不出现版本号以外的技术信息。
2. **确认**：将安装 N 项、需要下载约 X MB、装到"你的个人目录"（不显示路径，可在"高级"里看到）；若需要改 shell 配置，此处用人话请求许可（§2.10）。
3. **进度**：每个软件一行（等待 → 下载 45% → 校验 → 正在安装 → 完成 / 失败），顶部总进度；"取消"始终可见。失败项旁给"重试"。
4. **完成**：成功的项列出来，每项带**"打开"按钮**（直接启动刚装好的软件）；若这一项还有终端命令，下面多一行小字"也可以在终端里输入 `vlc` 使用"；失败项给"重试"与"查看详情"（详情 = 日志片段 + 错误码，是唯一允许出现技术文案的地方）。

**文案原则**
- 禁用词表（对普通人）：`PATH`、`环境变量`、`符号链接`、`sha256`、`解压`、`tar.gz`、`用户目录路径`、`权限不足`。
- 替换表：`下载失败，正在重试（第 2 次）`、`正在安装，请稍等`、`需要重新打开终端才能生效`、`上次没装完，已为你清理干净`。
- 错误分级：**可自愈**（网络抖动、校验失败重下）不打扰用户，只更新那一行；**需用户动作**（磁盘空间不足、WebView 缺失、网络全断）弹一次性人话提示 + 一个按钮（"重试"/"打开日志目录"/"知道了"）。

**可访问性**：字号随系统缩放；深浅色跟随系统；键盘可达（Tab/Enter）；中文界面不缺字（内置字体子集或复用系统字体）。

### 2.15 CLI 内核（FR-15）

普通人不用，但它是同一内核的另一个壳，也是自动化与排障的入口。

```
cpi install [id...] [--all|--profile recommended] [--force] [--offline]
            [--source URL|DIR] [--proxy URL] [--no-modify-path] [--dry-run]
cpi list [--json] [--installed|--available]
cpi uninstall <id> [--keep-store]
cpi plan [id...] [--json]          # 等价 GUI 的"确认"屏，无副作用
cpi doctor [--fix] [--json]        # 完整性 / PATH 生效 / 上游可达 / WebView 依赖
cpi version | cpi where <id> | cpi open-log
```

- 退出码：`0` 全成功；`1` 用法/配置错误；`2` 全部失败；`3` 部分失败（有软件失败但至少一个成功）。
- `cpi where <id>` 打印三样东西：安装目录、图形入口路径、终端命令（若该应用有）。
- `--json` 输出稳定 schema，供 GUI 之外的工具消费。
- `cpi plan` 与 GUI 的确认屏**共用** `plan.Build`，不会出现两套判定逻辑。

### 2.16 版本来源

| 来源 | 用于 | 取 sha256 的方式 |
|------|------|-----------------|
| `github-release` | 绝大多数 GUI 应用（GitHub Releases 分发） | GitHub Releases API 选资产；同名 `.sha256` / `checksums.txt` 存在则取，缺失按 A2 处理 |
| `url-feed` | VSCode 等"latest 重定向"型 | 从 HTTP 响应头/重定向解析版本；无校验文件 |
| `pin` | 任意锁版本 | 仍需从上述来源取 sha256 |

> 具体资产名与 URL 模板**在 M1 用"清单一致性测试"逐条打真实请求验证**（§3），本表不是最终事实。

### 2.17 默认清单（首版候选）

**收录规则（硬性）**：① **带 GUI**（有 `entry`，且能出现在开始菜单 / 启动台 / 应用列表里）；② 有官方可搬运二进制；③ 支持当前用户级安装，**不提权**；④ 三平台（或明确标注仅某平台）。

| id | 分类 | 平台 | 用户级分发形态 | 捆绑 CLI | 状态 |
|---|---|---|---|---|---|
| `vscode` | dev | 三平台 | 官方 `.zip`（macOS / Windows user setup）/ `.tar.gz`（Linux） | `code` | 候选，M3 验证 |
| `vlc` | media | 三平台 | macOS `.dmg`、Windows 便携 `.zip`、Linux AppImage | 无（合成启动器） | 候选，M3 验证 |
| `obsidian` | dev | 三平台 | Windows per-user NSIS、macOS `.dmg`、Linux AppImage | 无 | 候选 |
| `keepassxc` | utility | 三平台 | 官方 AppImage / `.dmg` / 便携 `.zip` | 无 | 候选 |
| `krita` | media | 三平台 | 官方 `.dmg` / 便携 `.zip` / AppImage | 无 | 待核实体积与用户级可行性 |
| `7zip` | utility | Windows | 官方 `.exe` 是机器级（要 UAC，违反 C2）；仅"7-Zip Extra"便携包可能可用 | 无 | 待定 |

> **Windows 的现实**：绝大多数桌面软件的官方安装器是机器级（要 UAC），因此 Windows 侧目录天然比 macOS/Linux 小（C2）。这份清单必须在 M3 逐条核实"是否真的能不提权装进用户目录"，核不实的**直接不收录**。
>
> **不再收录**：`uv` / `node` / `go` / `ripgrep` / `fzf` / `jq` / `gh` 这类纯命令行工具，以及字体 / 配置 / 数据等独立资源包。它们不是"暂时没写"，而是**明确不在范围内**——cpi 只装带 GUI 的程序（D14）。
>
> **上游只发裸可执行文件（macOS）怎么办**：**照收**。清单写 `entry.darwin.bin`，cpi 合成 `.app` 外壳（D18、§2.11）——这是设计的正常路径，不是例外。代价是这时的**图标与文件关联会缺**（上游没给 `.icns`、没声明类型），清单可显式提供 `icon:` 补齐；macOS 生态里这类软件不多（惯例就是发 `.app`），但不为零。

### 2.18 cpi 自身的打包与分发

| 平台 | 产物 | 备注 |
|------|------|------|
| macOS | `.app`（universal：amd64+arm64）+ `.dmg` | 需 **Developer ID 签名 + 公证**，否则普通人被 Gatekeeper 拦住（C3） |
| Windows | 单文件 `.exe`（`-H windowsgui`，内嵌前端） | 需 **代码签名**，否则 SmartScreen 警告（C4）；WebView2 缺失时引导安装 Evergreen Runtime（C5） |
| Linux | AppImage（x86_64 + aarch64） | 依赖宿主 WebKitGTK；缺失时提示安装对应包名 |

**无签名证书时的降级方案**（诚实记录，不是推荐）：内网/教学场景直接分发，附一页"首次打开被系统拦时怎么办"的图文；GUI 首次运行检测到自己被隔离标记（macOS `com.apple.quarantine`）时主动提示处理方式。签名是发布前必须解决的阻塞项（§5 R1）。

---

## 3. 测试策略

| 层 | 方法 | 覆盖 |
|---|---|---|
| 清单一致性 | 对内置清单里**每一条** URL 模板渲染出真实 URL 并发 `HEAD`/小 `Range` GET，校验状态码、`Content-Length`、归档类型；有 checksum 源的验证校验值可取到 | 防"清单写着实际 404"（M1 必做，可 nightly 跑） |
| 合并与校验 | 表驱动：合法覆盖、字段拼错、profile 引用不存在、`expose.from` 不存在、**缺 `entry` 的 tool 被拒**、**`entry.darwin` 既不是 `bundle:` 也不是 `bin:`（或两者并存）被拒**、`launch.cmd` 含路径分隔符或 `..` 被拒、`launch` 与 `expose` 同名时的去重规则 | `internal/manifest` |
| 计划算法 | 表驱动：每个分支（跳过/锁定/升级/无制品/离线缺 blob）都有用例 | `internal/plan` |
| 流水线 | `httptest.Server` 造假归档 + 假 FS（真临时目录，但根在 `t.TempDir()` 下）+ 注入校验失败/中断 | `internal/install` |
| 集成副作用 | 隔离 `HOME` 与 `CPI_HOME` 后跑真实安装，**夹具是合成的小 app bundle**（一个几十 KB 的假 `.app` / 假 `.exe`，由 `httptest.Server` 提供；范围收窄后不能再拿 jq/ripgrep 这类小 CLI 当夹具），断言：目录存在、`expose` shim 可执行、图形入口已建、`state.json` 记录正确、**卸载后无残留**；shell 配置用假 HOME 下的假 `~/.zprofile` / `~/.profile` / `~/.zshrc` 断言标记块与幂等（重复写入不叠加） | `internal/integrate` 的 e2e |
| 终端启动器 | 隔离 HOME 下装一个**没有自带 CLI** 的假应用，断言：`bin/<cmd>` 生成且为 `0755`、**参数被逐个透传**（假应用把自己的 argv 写到文件供断言）、macOS `activate` 模式生成的是 `open … --args` 形态而 `direct` 模式是直接 exec、**预置同名文件则不覆盖且标注 `Skipped(名称冲突)`**、卸载后启动器消失 | `internal/integrate` 的 e2e |
| 中断安全 | 在 fetch/stage/swap/expose/shell/entry/launch 各阶段注入 kill，重启后断言无半成品、可重入 | NFR-2/NFR-5 |
| 应用外壳合成 | 表驱动：`bundle:` 直接用 / `bin:` 走合成。合成后断言：`Info.plist` 的 4 个身份键齐全、`CFBundleExecutable` 与 `Contents/MacOS/` 里的文件名一致、内层可执行文件与上游 blob **同 inode**（确认是硬链接而**不是符号链接**——符号链接会让应用失去身份，§2.11）、`Resources/` 只在清单给了图标时出现；卸载后整个 `<Name>.app` 目录消失 | `internal/integrate`（darwin） |
| GUI | 前端状态机单测（4 屏迁移、取消、部分失败）+ Wails 绑定契约测试（事件名与 payload schema 冻结） | `cmd/cpi-gui` |
| 跨平台 | CI 矩阵：`{darwin,linux,windows} × {amd64,arm64}` 至少 `go build` + 单测；真机 e2e 在 macOS 与 Windows 各一台 | D4 |
| 静态检查 | `go vet`、`golangci-lint`（含 `gofumpt`）、`go test -race` | 全仓 |

**隔离原则**：所有测试不得触碰真实 `$HOME`。e2e 一律通过覆盖 `HOME` + `CPI_HOME` + `SHELL` 到临时目录进行（这也是唯一能安全测 PATH/shell 配置写入的方式）。

---

## 4. 里程碑

| 里程碑 | 内容 | 验收 |
|---|---|---|
| **M0 内核跑通** | 清单解析 + 计划 + 下载/校验/解包/落盘 + shim + 账本；CLI `install/list/uninstall`；清单里先用一个**合成夹具应用**（`httptest` 提供的几十 KB 假 app bundle）跑通 | 隔离 HOME 下 `cpi install <夹具>` 成功；新终端敲应用名能启动；`uninstall` 无残留 |
| **M1 清单与校验保真** | 清单一致性测试；`github-release` / `url-feed` / `pin` 三种版本源；store + 断点续传 + 镜像 + 代理 | 每条 URL 真实可下、校验通过；断网重跑 0 下载 |
| **M2 GUI 可用** | Wails 四屏 + 事件流 + 取消/重试 + 文案与错误分级 | 普通人（找一个真实非技术用户）能独立装好 VSCode 并点开 |
| **M3a 终端启动器** | `launch` 合成 + 三平台形态（macOS `activate`/`direct`、Windows `.cmd`、Linux 包装脚本）+ 名字冲突跳过 + 账本回放 | 装一个没有自带 CLI 的应用后，新终端敲应用名能起来且能带参数；同名冲突时跳过并告知；卸载后启动器消失 |
| **M3b 图形入口与清单扩充** | 三平台入口集成（开始菜单 / `~/Applications` / `.desktop`）+ **合成 `.app` 外壳**（D18：`entry.darwin.bin`）+ 反向清理 + §2.17 清单逐条核证 | VSCode/VLC 在三平台出现在正确位置，卸载后消失；一个"上游只发裸可执行文件"的 macOS 应用装完后能双击、能出现在启动台 |
| **M4 离线与内网** | `--offline` / `--store` / `--source dir` + store 目录整体分发 | 外网机准备 → 内网机零网络装好且校验通过 |
| **M5 自身发布** | 三平台产物 + 签名/公证 + 无签名降级文档 | 陌生环境双击不被拦 |

---

## 5. 风险与开放问题

### 风险

| # | 风险 | 影响 | 对策 |
|---|---|---|---|
| R1 | **cpi 自身签名/公证**（macOS Developer ID、Windows 代码签名证书）是发布阻塞项（C3/C4） | 不解决，普通人第一步就被系统拦住 | M5 前必须解决；M0–M4 用内网分发过渡 |
| R2 | **用户级可分发的桌面软件目录有限**（C2） | Windows 侧可选软件少，"装机清单"名不副实 | 收录规则前置筛选；宁可少而真，不可多而假 |
| R3 | WebView2 / WebKitGTK 缺失（C5） | GUI 打不开，对普通人等于完全坏掉 | 启动自检 + 人话提示 + 引导安装；兜底提供 CLI 内核 |
| R4 | 上游 URL / 资产命名漂移 | 清单过期，安装失败 | 清单一致性测试进 CI（nightly）；镜像表与清单分离 |
| R5 | 上游无 checksum（A2） | 供应链可信度降低 | 强制 TLS + store 内容寻址 + 账本标注 `unverified`；可选支持用户提供 hash |
| R6 | 修改 shell 配置被视为越界 | 用户反感或系统管理员禁止 | 明确请求许可、标记块、可一键撤销、拒绝后功能降级而非失败 |
| R7 | 与用户已装软件冲突（已有 VSCode / 同名命令） | 覆盖或 PATH 抢占 | 安装前探测并提示"检测到你已有 X，是否仍安装"；不主动删除非 cpi 管理的文件 |
| R8 | **合成外壳没有签名**（D18） | 上游只发裸二进制、由 cpi 合成 `.app` 时，外壳本身不是签名包（`codesign -dv` 报出的是内部 Mach-O 的 ad-hoc 身份）；若下载物带 `com.apple.quarantine`，Gatekeeper 可能直接拦，普通人看到的是"无法验证开发者 / 文件已损坏" | 与 R1 同源：外壳随 cpi 一起走签名流程，或对合成外壳补 ad-hoc 签名并在界面上说清；**优先收录上游自己发 `.app` 的软件**，把合成外壳留给确有必要的少数条目 |

### 开放问题

| # | 问题 | 需要谁定 | 备注 |
|---|---|---|---|
| O1 | 清单是否确认为"内置 YAML + 外部覆盖"（A1） | 用户 | 「不编」的两种读法；本设计按"不写死在 Go 里"处理 |
| O2 | 产品在界面上的显示名（暂用「软件安装器」） | 用户 | 纯文案，不阻塞 |
| O3 | 无官方 checksum 时"警告后继续"是否接受（A2） | 用户 | 不接受则这类应用需用户手填 hash 才能装 |
| O4 | 内置镜像默认值具体取哪些、默认开还是默认关 | 用户 | D10 已定"有默认值"，清单待评审 |
| O5 | 是否需要"安装完成后自动打开终端"之类的引导 | 设计 | 对普通人体验影响大，实现成本低 |
| O6 | i18n 是否首版只做中文 | 用户 | FR-16 目前标为可选 |
| O7 | cpi 自身更新机制（首版暂缓，FR-17） | 用户 | 普通人不会手动升级 |
| O8 | 是否提供"便携模式"（整个 `CPI_HOME` 放 U 盘，随身带走） | 用户 | `CPI_HOME` 已可覆盖，成本主要在入口与 PATH 的处理 |
| O9 | 启动器名字冲突时，是否允许用户在界面里另指定一个命令名（而不是直接跳过） | 用户 | FR-20 的默认是"跳过并告知"；允许改名会多出一个输入框 |

---

## 附：术语

| 术语 | 含义 |
|---|---|
| cpi | 本产品：Cross Platform Installer；可执行文件 `cpi` 与 `cpi-gui` |
| 清单 / Catalog | 描述"能装什么、从哪装、怎么装"的数据（YAML） |
| Profile | 清单里的预设集合，如 `recommended` |
| 计划 / Plan | 无副作用的执行预案：装什么、下多少、装到哪 |
| store | 内容寻址存储，按 sha256 命名；离线分发的载体 |
| 账本 / state.json | 记录装了什么 + 产生了哪些外部副作用（PATH 块、图标）的唯一真相 |
| 集成 / integrate | 让装好的东西"能被用上"：PATH、终端启动器、图形入口、及其反向清理 |
| 交付物 | 只有一种：**带 GUI 的应用程序**（可自带捆绑 CLI）；纯命令行工具与独立资源包都不在范围内（D14） |
| 启动器 / launch | cpi 为**没有自带 CLI** 的 GUI 应用合成的、放在 `bin/` 下的小脚本，让"装完能从终端启动"成立（§2.12） |
| 应用外壳 / shell | cpi 为"上游只发裸可执行文件"的 macOS 应用合成的**最小 `.app` 目录**（`Info.plist` 4 键 + `Contents/MacOS/<exe>` 硬链接 + 可选 `Resources/`），使它在系统里成为一个真正的应用（D18、§2.11） |
| 入口 / entry | 图形入口：Windows `.lnk`、macOS `~/Applications` 链接、Linux `.desktop` |
| expose | 把工具树里的某个可执行暴露为 `bin/` 下的 shim（软链或拷贝） |
