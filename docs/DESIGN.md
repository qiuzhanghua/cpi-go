# cpi — Cross Platform Installer 设计文档（v3.1）

> **契约在 [`PACKAGE-FORMAT.md`](PACKAGE-FORMAT.md)，本文讲"为什么这么设计"。**
> 两者冲突时以 `PACKAGE-FORMAT.md` 为准——它是冻结的对外接口，本文是内部推理。

v3 把范围从 v2 的"通用装机清单 + 图形安装器"收窄为**单应用安装器**：一次安装一个产品（当前的具体对象是 Tauri 应用 **AI Desk**），落到 `~/ad`，装完终端能敲、图标能点、能干净卸载。

v2 里那套"多应用清单 / 镜像表 / 版本源 / 离线 store / Wails 四屏"**全部退出范围**，理由与保留价值记在 §7 附录。

**v3.1 把三平台的后半程补上了**：Windows 的 PATH（直写 `HKCU\Environment`）与开始菜单 `.lnk`（原生 COM）、Linux 的 `.desktop`，都已实现；打包从 shell 脚本搬进 `cpi pack` 子命令；两个仓库都接上 CI，**Linux 与 Windows 的路径由真 Linux / 真 Windows runner 跑测试来验证**。全文里"未实现 / 未在真机验证"的说法已按此更新（§0.3、§2.13、§3）。

---

## 0. 决策快照

### 0.1 现行决策（不再重开讨论）

| # | 决策 | 出处 |
|---|---|---|
| **D19** | **范围 = 单应用安装器**。一次安装一个 `id`；不做多应用清单、不做计划算法、不做镜像表/版本源/断点续传/离线 store。**图形安装器暂缓**（原 D13 的 Wails 四屏不在首版范围）。 | 本项目 |
| **D20** | **分发形态 = zip 自带 cpi**。产物是一个 `<id>-<version>-<os>-<arch>.zip`，内含 `install.sh`（mac/Linux）/ `install.cmd`（Windows）+ `cpi` + `manifest.yaml` + `payload/` + `SHA256SUMS`。用户解压后运行其中一个，脚本只做三行 bootstrap。 | 本项目 |
| **D21** | **安装根 = `~/ad`**，且 **`~/ad` 就是 `CPI_HOME`**（不拆成"应用目录"与"cpi 自己的目录"两个）。优先级 `--dir` > `$CPI_HOME` > `~/ad`。 | 本项目 |
| **D22** | **输入契约**：一个目录或一个 zip，里面有 `manifest.yaml` + `payload/` + `SHA256SUMS`。没有别的输入形式。 | 本项目 |
| **D23** | **校验**：`SHA256SUMS` 必须覆盖 `payload/` 下每一个常规文件；对不上即拒绝安装。**缺失 `SHA256SUMS` 时警告后继续**，并把本次安装记为 `unverified`（`cpi list` 会显示）。`--skip-verify` 只用于调试。 | 沿用 A2 |
| **D24** | **macOS 应用必须以 `.app` 形态落地**：上游给了就用（`entry.darwin.bundle`）；上游只发裸可执行文件时由 cpi 合成最小外壳。 | 沿用 D18。**合成外壳这一半本版尚未实现**，见 §2.7、§2.13 |
| **D25** | **终端启动器**：每个应用在 `<CPI_HOME>/bin/<cmd>` 生成一个启动器，参数透传；`mode: activate`（默认，等价双击）或 `direct`（要 stdout / 退出码）。 | 沿用 D15–D17 |
| **D26** | **PATH 集成**：按 `$SHELL` 决定落点，写幂等标记块，**写之前必须用人话请求许可**，拒绝则功能降级而非失败。 | 沿用 D7 |
| **D27** | **cpi 不吃 `.dmg` / NSIS `.exe` / AppImage 安装器**，只吃已经摆成 `payload/` 形状的归档。理由见 §2.12。 | 本项目 |
| **D28** | **账本 `state.json` 是所有外部副作用（PATH 块、`~/Applications` 链接、`.desktop`）的唯一真相**，卸载即回放删除。 | 沿用 D8 |
| **D29** | **cpi 把自己也装进 `<CPI_HOME>/bin/cpi`**，保证装完之后 `cpi list` / `cpi uninstall` 还找得到它；卸载时一并删掉。 | 本项目 |
| **D30** | **单版本覆盖式**：同一个 `id` 再装一次，先删旧的包目录与启动器再装新的，不做多版本共存。 | 沿用 D10 |

### 0.2 由上述决策推导出的硬性设计约束

- **全程不提权**：不 sudo、不触发 UAC、不写机器级位置。所有落点都在当前用户家目录下。
- **内核不依赖 GUI**：`internal/` 绝不 import 任何 GUI 包（暂缓 GUI 之后这条变成"将来加 GUI 时不许反过来污染内核"）。
- **一切以"能干净撤销"为准绳**：任何外部写入都必须先登记进账本、后执行。
- **staging 必须建在 `<CPI_HOME>` 之下**：只有同一文件系统内的 `rename` 才是原子的，跨文件系统 `rename` 会退化成"拷贝 + 删除"。
- **失败不能留半成品**：安装过程用 rollback 栈，按逆序撤销已完成的步骤。

### 0.3 v2 → v3 变更记录

| 变了什么 | 为什么 |
|---|---|
| 范围：通用装机清单 → **单应用安装器** | 真实需求是"把我自己这一个 Tauri 应用装好"，不是"给陌生人装一堆软件" |
| 输入：版本源 + 镜像 + store → **一个 zip** | 没有服务端、没有清单仓库，最简的可靠分发就是文件 |
| 安装根：`~/.cpi` → **`~/ad`** | `~/ad` 既是应用家目录又是 `CPI_HOME`；卸载 = 删一个目录 |
| cpi 的来源：单独分发 → **zip 自带** | 用户不需要先装一个新东西才能装东西 |
| 校验：store 内容寻址 → **`SHA256SUMS`** | 内容寻址的前提是一整套 store，单应用场景下是过度设计 |
| 图形界面：Wails 四屏 → **暂缓** | 首版是"解压 + 运行脚本"，GUI 不是它的前置条件 |
| 硬链接：`store` ↔ `lib` 之间 → **当前唯一用法是合成 `.app` 外壳** | store 没了，硬链接只剩一处 |

### 0.3.1 v3 → v3.1 变更记录

| 变了什么 | 为什么 |
|---|---|
| Windows PATH：**未实现** → 直写 `HKCU\Environment` 的 `Path` | 见 §2.6。关键是**读出什么类型就写回什么类型**，否则会把用户的 `REG_EXPAND_SZ` 压成 `REG_SZ` |
| Windows 图形入口：**未实现** → 原生 COM 建开始菜单 `.lnk` | 见 §2.7。刻意**不起 PowerShell 子进程** |
| Linux 图形入口：已写未验证 → 已在 ubuntu runner 上验证 | 见 §2.13。测试里会调 `desktop-file-validate` |
| 打包：`tools/fixture/make.sh` 那套 shell → **`cpi pack` 子命令** | 见 §2.15。Windows 的 Git Bash 连 `zip` 都没有，`sha256sum` 也不保证有，而 .app 里的符号链接普通 zip 会展开——这三件事在 Go 里一次写完，三平台一致 |
| PATH 往返保真：**装一次再卸一次必须逐字节还原** | 见 §2.6。Windows 的 PATH 里空条目是有含义的（表示"当前目录"），顺手 Trim 掉就等于改了用户的语义 |
| CI：无 → cpi-go 三平台测试 + 六平台交叉编译；ai-desk 三平台打包 | 见 §2.16。本机没有容器与虚拟机，多平台验证只能这么做 |

**作废**：D1–D5（清单来源/落地方式/默认范围/网络/交付物）、D10 的镜像部分、D11（离线模式）、D12、D13 的 GUI 部分、D14 的"默认清单"、D16 的多应用 Profile。**保留**：D7（PATH 集成）、D8（账本/卸载）、D9（不提权）、D15–D18（启动器/应用形态）。逐条对照见 §7。

### 0.4 待确认假设

| # | 假设 | 影响 |
|---|---|---|
| A1 | **~~Windows 侧的 PATH 集成与开始菜单 `.lnk` 尚未实现~~** —— v3.1 已实现并在真 Windows runner 上验证（§2.6、§2.7、§2.13） | 已消解 |
| A5 | **cpi 在 Windows 的 PATH 上只摘除自己加的那一条**，其余（含结尾的空条目）逐字节保持原样 | 这条现在有 `TestWindowsPathRoundTripIsExact` 与真注册表往返测试守着（§2.6） |
| A2 | 没有 `SHA256SUMS` 时只警告不拒绝 | 供应链可信度换可用性；如需更严，可要求清单里给 `sha256` 并强制比对 |
| A3 | 一次只装一个应用（`manifest.yaml` 里只有一个 `id`） | 要装第二个产品时，再跑一次另一个 zip 即可，账本天然支持多包 |
| A4 | 图标暂不处理 | 合成 `.app` 外壳时没有 `Resources/`、`.desktop` 里没有 `Icon=`；上游给了 `.icns`/`.ico` 时才补 |

---

## 1. 需求

### 1.1 一句话

把"我的应用怎么装到别人（或另一台）机器上"这件事，压缩成**下载一个 zip → 解压 → 运行一个脚本**，装完之后**图标能点、终端能敲、能干净卸掉**。

### 1.2 用户与场景

- **主要用户**：想在自己/同事机器上使用该应用的人。他可能并不熟悉命令行，但"解压一个压缩包、双击里面的脚本"是他能完成的动作。
- **次要用户**：要批量、无人值守地部署的人（CI、内网运维）——他需要的是 `--yes` 这类不交互开关。
- **明确不是**：完全的陌生人（那种场景的正解是 `.dmg` / NSIS `.exe` / AppImage，见 §2.12）。

核心场景：

| # | 场景 | 期望 |
|---|---|---|
| S1 | 解压后运行 `install.sh` / `install.cmd` | 装完，桌面/启动台里出现应用图标 |
| S2 | 新开一个终端，敲应用名 | 应用启动；`ad 某个文件` 这类带参数的用法也成立 |
| S3 | 装到另一个目录（U 盘、`D:\ad`） | `--dir` / `$CPI_HOME` 一改就行，其余行为不变 |
| S4 | 装到一半失败 | 没有半成品残留，可以重跑 |
| S5 | 不想要了 | `cpi uninstall <id>` 之后，目录、图标、终端命令、PATH 条目全部消失 |
| S6 | 无人值守（CI / 内网） | `cpi install ./x.zip --yes` 不提问、不交互 |
| S7 | 用户不想被改 shell 配置 | 拒绝之后应用照样装好、图标照样能点，只是终端里敲不出来 |

### 1.3 功能需求

| # | 需求 |
|---|---|
| FR-1 | 接受一个目录或一个 zip 作为输入 |
| FR-2 | 从 `manifest.yaml` 里读出 `id` / `name` / `version` / `entry` / `launch`，并校验 |
| FR-3 | 按当前 `GOOS`/`GOARCH` 选择入口；清单没有当前平台的入口时明确报错 |
| FR-4 | 用 `SHA256SUMS` 校验 `payload/` 下每个文件；不一致拒绝安装 |
| FR-5 | 把 `payload/` 整体落到 `<CPI_HOME>/lib/<id>_<version>_<os>_<arch>/` |
| FR-6 | 在 `<CPI_HOME>/bin/<cmd>` 生成终端启动器（`0755`），参数逐个透传 |
| FR-7 | 建立图形入口：macOS `~/Applications/<Name>.app`、Linux `~/.local/share/applications/<id>.desktop` |
| FR-8 | 把 `<CPI_HOME>/bin` 挂到 PATH 上；写之前请求许可，写入幂等、可撤销 |
| FR-9 | 把 cpi 自己复制到 `<CPI_HOME>/bin/cpi` |
| FR-10 | 所有外部副作用记入 `state.json` |
| FR-11 | `list` / `where` / `uninstall` 能读出并回放账本 |
| FR-12 | 失败时按逆序回滚，不留半成品 |
| FR-13 | 重装同一个 `id` 时覆盖式替换（先删旧、再装新） |
| FR-14 | 非交互环境（无 TTY）不提问，直接跳过 PATH 集成并打印手工命令 |
| FR-15 | 发布者侧：`cpi pack <装配目录>` 就地补齐 `install.sh`/`install.cmd`/`SHA256SUMS` 与 cpi 二进制并打成 zip（§2.15） |

### 1.4 非功能需求

| # | 需求 |
|---|---|
| NFR-1 | 全程不提权；不需要管理员 |
| NFR-2 | 中断安全：任何时刻断电/杀进程，重启后 `~/ad` 不处于半成品状态 |
| NFR-3 | 卸载后除"用户自己放进去的东西"外无残留；cpi 自己创建的空文件也一并删掉 |
| NFR-4 | 幂等：同一个包连装多次，结果一致（PATH 块不叠加） |
| NFR-5 | 只依赖 Go 标准库 + `gopkg.in/yaml.v3` |
| NFR-6 | 不修改被装应用自身的配置，不往用户可见的文档目录写文件 |
| NFR-7 | 报错必须给出**可执行的下一步**（哪个文件、哪个字段、哪个 URL） |

### 1.5 非目标

- 不做纯命令行工具与语言运行时的安装（`go` / `node` / `rg` / `fzf` … 当初 v1 的目标，现已出局）。
- 不做字体、配置、数据等独立资源包。
- 不做应用商店式的多应用目录、镜像表、版本发现、静默升级。
- 不做 `.dmg` / NSIS / `.deb` / `.rpm` / AppImage **安装器**的驱动（§2.12）。
- 首版不做图形界面。

### 1.6 硬约束（已探明）

| # | 约束 |
|---|---|
| C1 | macOS 上 GUI 应用的身份由 `.app` 目录承载；裸 Mach-O 会被 LaunchServices 当成"用终端打开的文档" |
| C2 | zip 过浏览器/邮件会带 `com.apple.quarantine`（macOS）与 MOTW（Windows），这正是"签名与公证是发布阻塞项"的根源 |
| C3 | Windows 上 `[Environment]::SetEnvironmentVariable(..., "User")` 有已知缺陷：会把 `REG_EXPAND_SZ` 写成 `REG_SZ`，用户原有的 `%USERPROFILE%` 之类不再展开 → 必须直接操作注册表原值 |
| C4 | Windows 默认 `ExecutionPolicy Restricted`，右键"使用 PowerShell 运行"常直接报"在此系统上禁止运行脚本" → 脚本优先 `.cmd`/`.bat` |
| C5 | Windows 资源管理器解压 zip 会丢 Unix 权限位 → `install.sh` 不能依赖 `+x` |
| C6 | macOS 的 `.app` 里可能含符号链接，`zip` 必须加 `-y`；更稳的是 `ditto -c -k --keepParent` |
| C7 | Windows 的 GUI 子系统 exe 从终端启动会**立即返回、不输出、不阻塞**——这是系统设计，不是缺陷 |

---

## 2. 设计

### 2.1 总体架构

```
        用户视角                          内核视角
  ┌────────────────────┐
  │ 解压 zip           │
  │  ├ install.sh      │──┐
  │  ├ install.cmd     │  │  只做三行 bootstrap：
  │  ├ cpi             │  │  cd "$(dirname "$0")"
  │  ├ manifest.yaml   │  │  chmod +x ./cpi
  │  ├ payload/        │  └─ exec ./cpi install . --dir "${CPI_HOME:-$HOME/ad}"
  │  └ SHA256SUMS      │
  └────────────────────┘
                                    │
                       ┌────────────▼─────────────┐
                       │  cpi（同一份内核）        │
                       │  home → manifest → stage  │
                       │  → install → integrate    │
                       │  → ledger                 │
                       └────────────┬─────────────┘
                                    │
                    ~/ad/{bin,lib,staging,state.json}
                    ~/Applications/AI Desk.app
                    ~/.zprofile / .zshrc / .profile
```

一条铁律：**`internal/` 不知道 GUI 存在**。将来加图形壳（若加）时，它和 `cmd/cpi` 走同一条 `install.Install`。

### 2.2 包布局

```
cmd/cpi/            CLI 入口：参数解析、子命令分发、中文输出
internal/home/      安装根解析（--dir > $CPI_HOME > ~/ad）与各子目录
internal/manifest/  manifest.yaml 的解析与平台校验
internal/stage/     解包（目录或 zip）与 sha256 校验
internal/pack/      反向：把装配目录打成可分发的 zip（install.sh/install.cmd/cpi/SHA256SUMS）
internal/integrate/ 外部副作用：终端启动器、图形入口、PATH
internal/ledger/    state.json 的读写
internal/install/   编排 + 回滚 + 卸载回放
tools/fixture/      造测试分发包的脚本（不属于产品；产品路径已由 cpi pack 取代）
```

`internal/integrate/` 内部按平台拆文件：`integrate.go`（启动器与 PATH 标记块，全平台）、`entry.go`（图形入口路径与 `.desktop` 内容，纯函数）、`pathwin.go`（Windows PATH 的字符串处理，纯函数、可测）、`registry_windows.go` + `registry_stub.go`（注册表读写）、`shortcut_windows.go` + `shortcut_stub.go`（`.lnk`）。**平台无关的部分一律做成纯函数**，这样 Windows 的逻辑也能在 macOS 上被测试覆盖。

依赖方向是单向的：`install` → {`home`, `manifest`, `stage`, `integrate`, `ledger`}，`integrate` → `ledger`，`pack` → {`manifest`, `stage`}，其余互相不依赖。

### 2.3 磁盘布局

```
<CPI_HOME>/                     默认 ~/ad
├── bin/
│   ├── cpi                     cpi 自己的副本（D29）
│   └── <cmd>                   终端启动器（D25）
├── lib/
│   └── <id>_<version>_<os>_<arch>/
│       └── <Name>.app          仅 macOS：上游给的或 cpi 合成的
├── staging/                    解包中转；每次安装一个 unpack-<纳秒> 目录，defer 删除
└── state.json                  账本
```

- **没有 `store/`**：v2 的内容寻址存储在单应用场景下是过度设计（§0.3）。
- **`staging/` 必须在 `<CPI_HOME>` 之下**：只有同一文件系统内的 `rename` 才是原子的。
- `state.json` 用 **临时文件 + rename** 写入，保证不会读到写坏的账本。
- `~/ad` 下**除上述四样之外不放任何东西**，这样"用户自己放的东西"是可辨认的，卸载时不必猜。

### 2.4 清单模型

```yaml
id: ai-desk            # 必填，^[a-z0-9][a-z0-9._-]*$
name: AI Desk          # 必填，显示名
version: 1.0.0         # 必填，^[A-Za-z0-9][A-Za-z0-9._+-]*$
entry:
  darwin: { bundle: AI Desk.app }   # 二选一：bundle（.app 目录）或 exe（可执行文件）
  linux:  { exe: ad }
  windows: { exe: ad.exe }
launch:
  cmd: ad              # 终端命令名；^[A-Za-z0-9][A-Za-z0-9._-]*$，禁路径分隔符与 ..
  mode: activate       # activate（默认）| direct
```

三条规则：

1. `entry.<goos>` 必须**恰好**给出 `bundle:` 或 `exe:` 之一，两者同时写或都不写都是错误。
2. 路径**相对 `payload/`**，同时也就相对安装后的包目录；禁绝对路径、盘符、`~`、`..`。
3. `launch.cmd` 缺省等于 `id`。

**只校验当前平台**：清单可以带好几个平台，cpi 只看自己跑在哪个上；当前平台没有对应条目就报错退出。

### 2.5 安装流水线

```
0. 解析 --dir / $CPI_HOME / ~/ad，建好骨架              ← Ensure()
1. 解包到 <CPI_HOME>/staging/unpack-<纳秒>              ← stage.Materialize（目录直接拷，zip 带越界防护）
2. 校验 payload/ 下每个文件                             ← stage.VerifySums
   └ 没有 SHA256SUMS → 警告 + verified=false，继续
   └ 对不上         → 报出「清单 <a>，实际 <b>」，拒绝
3. 读并校验 manifest.yaml                              ← manifest.Load + Validate(goos)
4. payload/ 整体拷进 lib/<id>_<ver>_<os>_<arch>/        ← stage.CopyTree（保留权限位与符号链接）
   └ 同 id 已存在 → 先按卸载流程删旧的（D30）
5. 生成 bin/<cmd>                                      ← integrate.Launcher
6. 建立图形入口                                        ← integrate.AppLink
7. 请求许可后把 bin/ 接进 PATH                          ← integrate.InstallPathBlock
   ├ POSIX   → 写 shell 配置的标记块
   └ Windows → integrate.InstallWindowsPath（改 HKCU\Environment 后广播）
8. 把 cpi 复制到 bin/cpi                               ← installSelf
9. 写 state.json                                       ← ledger.Save
```

每一步成功都把逆操作压进 rollback 栈；任一步失败就**逆序执行**已压入的动作，然后返回错误。第 4 步之后的失败会把整个 `lib/<id>_…/` 目录一起删掉。

**回滚栈与"先登记后执行"的分工**：账本（第 9 步）是**卸载**的依据，rollback 栈是**本次安装失败**的依据。两者不可互相替代——账本写下去的时候，安装已经成功了。

### 2.6 PATH 集成

`<CPI_HOME>/bin` 存在的**唯一理由**就是本需求的核心：让装完的应用在终端里敲得动。

| 平台 | 机制 | 生效时机 |
|---|---|---|
| Windows | 写 **`HKCU\Environment` 的 `Path`**（无需管理员）后广播 `WM_SETTINGCHANGE`；PowerShell 无需额外配置 | 新进程 |
| macOS | **按 `$SHELL`**：zsh → `~/.zprofile`（Terminal.app 每开新窗口都是登录 shell）**并同时写** `~/.zshrc`（非登录的交互 shell）；bash → `~/.bash_profile`；fish → `~/.config/fish/config.fish`。**不论 `$SHELL` 都再写一份 `~/.profile`** 兜底 | 新开终端 |
| Linux | 同 macOS 规则（bash 落 `~/.bashrc`） | 新开终端 |

> **为什么不能只写 `~/.profile`**：macOS 自 Catalina 起默认 shell 是 zsh，而 **zsh 不读 `~/.profile`**（只在被当作 `sh`/`ksh` 调用时才读）。只写 `.profile` 的后果是：软件装好了、图标也在，但新开终端敲 `ad` 依然 `command not found`——而且用 bash 自测时还发现不了。

写入的是一段**幂等守卫**，重复 source 不会叠加 PATH，同一段写进多个文件也安全：

```sh
# >>> cpi >>>
case ":$PATH:" in
  *":$HOME/ad/bin:"*) ;;
  *) PATH="$HOME/ad/bin:$PATH" ;;
esac
export PATH
# <<< cpi <<<
```

- 路径在写进文件时相对化成 `$HOME/ad/bin`，所以整个 `CPI_HOME` 搬走也不会失效。
- 写入前**先摘掉旧块再追加**，所以重复安装不会累积。
- 已经在文件里的 cpi 块与"用户自己早就加过的等价条目"都能识别：前者被替换，后者只提示。
- **写之前必须用人话请求许可**，并逐个列出**实际**要改的文件名（这台机器上是 `~/.zprofile`、`~/.zshrc`、`~/.profile`）。拒绝 → 软件照装、图标照建，只打印一行手工命令，**功能降级而非失败**。
- 非交互环境（管道、CI）不提问，直接跳过。**注意 `isTerminal` 不能只看 `os.ModeCharDevice`**：`/dev/null` 也是字符设备，所以从脚本或双击运行时走的是"提问后立刻 EOF"这条路——那种情况下要明说"没读到你的输入，想让命令能用就重跑一次并加 `--yes`"，不能假装用户回答了"不"。

**Windows 侧的四个具体决定**

- **直写注册表，不用 `[Environment]::SetEnvironmentVariable(..., "User")`**（C3）：那个 API 会把 `REG_EXPAND_SZ` 写成 `REG_SZ`，用户原有的 `%USERPROFILE%` 之类从此不再展开。`syscall` 里没有导出 `RegSetValueEx`，所以用 `syscall.NewLazyDLL("advapi32.dll")` 取原生过程——**依然只用标准库**。
- **读出什么类型就写回什么类型**：值不存在（错误码 2 = `ERROR_FILE_NOT_FOUND`）当作空串 + `REG_EXPAND_SZ`，不算错误。
- **写完广播 `WM_SETTINGCHANGE`**（`SendMessageTimeoutW` + `SMTO_ABORTIFHUNG`，5 秒超时）：不广播的话，已经开着的程序（包括资源管理器）不会重新读环境变量。
- **插入与摘除都必须逐字节保真**：Windows 的 PATH 里**空条目是有含义的**（表示"当前目录"），而用户 PATH 的默认值结尾本来就带一个分号。所以插入时原值一个字节都不动（只在最前面加一条），摘除时按条目切分、只丢掉命中的那一条、空条目原样拼回去。判重则要归一化（大小写、正反斜杠、未展开的 `%VAR%`），否则"已经加过了"会被判成"没加过"，装两次就重复追加。

> 这条保真契约的由来见 §0.3.1：CI 第一次在真 Windows 上跑就抓到了"卸载后用户 PATH 少一个分号"。补的 `TestWindowsPathRoundTripIsExact` 在 macOS 上就能跑——它本来就该在跑到 Windows 之前抓住这个问题。

### 2.7 图形入口集成

| 平台 | 机制 | 落点 | 状态 |
|---|---|---|---|
| macOS | **符号链接**指向 `lib/…/<Name>.app` | `~/Applications/<Name>.app` | 已实测：`open` 能拉起、启动台会索引 |
| Linux | `.desktop` 文件，`Exec=` 指向启动器 | `~/.local/share/applications/<id>.desktop`（**遵守 `$XDG_DATA_HOME`**） | 已实测（ubuntu runner；测试里调 `desktop-file-validate`） |
| Windows | 开始菜单 `.lnk` | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\cpi\<Name>.lnk` | 已实测（windows runner；写入后读回来做往返比对） |

- 目标已存在时**不覆盖**，只打印一条提示（避免把用户自己装的同名应用顶掉）。
- 每条入口都在账本里登记路径与类型，卸载即回放删除。
- **Linux 为什么遵守 `$XDG_DATA_HOME`**：不遵守的话会把 `.desktop` 写进一个用户根本没在看的目录，菜单里永远不出现。写完调一次 `update-desktop-database`，但它不一定装了——没有就静默跳过。
- **Windows 的 `.lnk` 为什么自己写 COM 而不用 PowerShell**：一来不必依赖 `WScript.Shell` 与执行策略（C4），二来"下载来的程序改注册表又拉起 PowerShell"正是杀软/EDR 的敏感行为特征。`.lnk` 建失败时**降级为提示而不是报错**：软件已经装好了，快捷方式建不出来不该判整次安装失败。

**为什么 macOS 上"必须有 `.app`"（本机实测，D24）**

`.app` 不是文件格式，而是一个**目录**；真正可执行的是 `<Name>.app/Contents/MacOS/<Name>`，一个普通 Mach-O。同一个 GUI 程序编成裸 Mach-O **也能跑**（窗口照常、`isActive=1`），但系统不认它是"应用"。macOS 27.0.1 / arm64 上的对照实测：

| 事实 | 裸可执行文件 | `.app` |
|---|---|---|
| UTI（`mdls`） | `public.unix-executable`「Unix可执行文件」 | `com.apple.application-bundle`「应用程序」 |
| Finder 双击 | 当成一份**"交给终端运行"的文档**（LaunchServices 里 `public.unix-executable` 的 `all roles:` 指向终端应用；本机是 Ghostty，stock 机器是 Terminal，装了 iTerm2 又可能变） | 正常启动应用 |
| 进程内 `NSBundle.bundlePath` | 它所在的那个**目录** | 那个 `.app` |
| `bundleIdentifier` / `CFBundleName` | `(null)` / `(null)` | `com.example.cpi.exp` / `CpiExp` |
| 启动时 `NSApp.activationPolicy` | **2（Prohibited）**——得程序自己调 `setActivationPolicy:Regular` 才有 Dock 图标与菜单栏 | **0（Regular）** |
| 启动台/Spotlight 索引、"打开方式"、文件关联、URL scheme、TCC 权限归属、公证 | 全都没有 | 有 |

> 取证口径：表中"双击"一行的依据是 LaunchServices 对 `public.unix-executable` 的绑定与角色声明（`lsregister -dump` 里 `all roles:` 指向终端应用），**不是一次亲眼看到的双击**——实验环境里终端窗口没能被拉起。其余各行都是探针程序自报的身份字段，可直接复现。

**合成外壳的配方（手工实验已跑通，cpi 里尚未实现）**

> ⚠️ 现状：`manifest.Entry` 目前只有 `bundle` / `exe` 两个字段，**没有 `bin`**，也就是说
> "上游只发裸可执行文件"这条路 cpi 现在还走不了。下面的配方是一轮真机实验的结论，
> 已验证可行，等着落成代码（§2.13、§5 R8）。

```
<CPI_HOME>/lib/<id>_<ver>_darwin_<arch>/<Name>.app/Contents/
    Info.plist        # cpi 生成
    MacOS/<exe>       # 指向上游裸可执行文件：硬链接（首选）或拷贝
    Resources/        # 可选，清单给了图标才建
```

- `Info.plist` **最小只要 4 个键**就换来完整身份：`CFBundleExecutable`（必须等于 `MacOS/` 里的文件名）、`CFBundleIdentifier`、`CFBundleName`、`CFBundlePackageType` = `APPL`。
- `CFBundleIdentifier` 缺省 `<id>.cpi.local`——**刻意不冒用上游官方标识**，免得与用户真正装的官方应用在偏好、权限、通知归属上串味。
- **`Contents/MacOS/<exe>` 只能用硬链接或拷贝，绝不能用符号链接**（实测）：符号链接会让进程内的 `NSBundle.mainBundle` 解析到链接目标，于是"外面看着是应用、里面自己不认"——`bundlePath` 变成裸二进制所在目录，`bundleIdentifier` 与 `CFBundleName` 全成 `(null)`。硬链接与拷贝结果都正确；`lib/` 与上游文件同在 `<CPI_HOME>` 之下，所以**硬链接零拷贝、是首选**。
- **外壳没有独立签名**：`codesign -dv` 报出的是内部 Mach-O 的 ad-hoc 身份。所以"合成外壳的应用"与"上游自己发的 `.app`"在 Gatekeeper 面前**不同命**（§5 R2）。

### 2.8 终端启动器集成

GUI 应用默认**不会**在 PATH 里留下任何东西，所以这一步只能由 cpi 主动合成。

| 平台 | 落点 | 内容 |
|---|---|---|
| macOS | `<CPI_HOME>/bin/<cmd>`，`0755` | `mode: activate`（默认）→ `exec open '<lib 绝对路径>/<X>.app' --args "$@"`；`mode: direct` → `exec '<lib 绝对路径>/<X>.app/Contents/MacOS/<X>' "$@"` |
| Windows | `<CPI_HOME>/bin/<cmd>.cmd` | `@echo off` + `"<lib 绝对路径>\<X>.exe" %*`（CRLF 换行） |
| Linux | `<CPI_HOME>/bin/<cmd>`，`0755` | `exec '<lib 绝对路径>/<X>.AppImage' "$@"` |

- **`activate` 与双击完全等价**：`open` 走 LaunchServices，已在运行的实例会被**激活**而不是开第二个。`direct` 绕过 LaunchServices，换来 stdout、退出码与可杀的 `Ctrl+C`。
- **参数一律透传**（`"$@"` / `%*`）：否则 `ad movie.mp4` 这类最常见的用法直接失效。
- **为什么 Linux 用包装脚本而不是符号链接**：AppImage 依赖自身路径（`$APPIMAGE` / `argv[0]`）定位挂载点，软链在部分实现下会解析失败。
- **为什么 Windows 用 `.cmd` 而不是把 exe 硬链进 `bin/`**：Windows 的 exe 常按相对路径加载同目录的 DLL 与资源，搬离自己的目录就损坏；`.cmd` 没这个问题，且 `.CMD` 默认在 `PATHEXT` 里。
- **Windows 的固有行为**：GUI 子系统的 exe 从终端启动会立即返回、不输出、不阻塞（C7）。终端启动的价值在于"不用去开始菜单里翻"。

### 2.9 卸载与反向清理

`uninstall <id>` 严格按账本回放：

```
删 ~/Applications/<Name>.app 或 .desktop   ← 账本 links
删开始菜单的 .lnk（Windows）                ← 账本 links
删 bin/<cmd> 或 bin/<cmd>.cmd              ← 账本 launcher
删 lib/<id>_<version>_<os>_<arch>/         ← 账本 dir
摘除 PATH 条目                             ← 账本 pathEdits
  ├ POSIX   → 逐文件摘掉标记块
  ├ Windows → 从 HKCU\Environment 的 Path 里只摘掉那一条（其余逐字节不动）
  └ 我们自己创建出来的空 shell 配置文件也一并删掉（账本 pathEdits[].created）
删 bin/cpi                                 ← 账本 self（仅当账本里已经没有别的包）
写回 state.json（去掉该包）
```

**验收标准（可测）**：`cpi list` 无该项；`command -v <cmd>` 找不到；`~/Applications` 下链接消失；`lib/` 下无同名目录；shell 配置文件里没有 cpi 标记块（Windows 上是注册表 PATH 里没有 `<CPI_HOME>\bin`）；如果那个文件是 cpi 创建出来的且已经空了，文件本身也不在。

`<CPI_HOME>` 目录**不删**——用户可能往里放了别的东西，删掉是越界。命令会明说"目录还在"。

### 2.10 校验与供应链

| 情况 | 行为 |
|---|---|
| `SHA256SUMS` 存在且全部匹配 | 正常安装，账本 `verified: true` |
| 有文件对不上 | **拒绝**，报出「清单 `<期望>`，实际 `<实得>`」与文件名 |
| 有文件在 `payload/` 里但不在清单里 | 拒绝（清单必须覆盖每个常规文件） |
| 没有 `SHA256SUMS` 这个文件 | 警告后继续，账本 `verified: false`，`cpi list` 显示 `[unverified]` |
| `SHA256SUMS` 存在但是空的 / 格式不对 | **拒绝**（空的清单等于没校验，不能装作校验过了） |
| 符号链接 | 不参与校验（它是链接不是内容），但会被原样保留 |

**不做签名验证**：单应用场景下的信任来自"这个 zip 是从哪来的"，而不是 cpi 能验证什么。`SHA256SUMS` 防的是**传输损坏**与**误改**，不是防恶意——真正的防恶意手段是 §5 R1 的签名公证。

### 2.11 解包与路径安全

`stage.Materialize` 同时接受目录和 zip，两者都要过同一套路径检查：

- 拒绝绝对路径、盘符、含 `..` 的条目——防止 zip slip（一个精心构造的 zip 可以写到 `~/ad` 之外）。
- zip 条目的权限位尽量保留；Windows 造的 zip 没带权限位时按 0755 补齐可执行文件。
- 解包目标永远是 `<CPI_HOME>/staging/unpack-<纳秒>`，并且 `defer os.RemoveAll`。

### 2.12 为什么 cpi 不吃 `.dmg` / NSIS / AppImage 安装器

| 形态 | 技术上能不能 | 为什么不做 |
|---|---|---|
| **AppImage** | 最接近能用：本身就是一个可执行文件，放哪都能跑 | 它不需要"安装"，也就不需要 cpi；而且 AppImage 的正解是直接放到用户想放的地方，cpi 在这里没有附加值。可以作为 `entry.<linux>.exe` 收录，**但不驱动它的安装器** |
| **NSIS `.exe`** | 能：`/S /D=<路径>`（`/D` 必须**放最后且不能加引号**） | 它自己写注册表、自己建卸载器、自己决定装哪；**cpi 的原子性、账本与卸载承诺当场失效**——卸载时 cpi 根本不知道它写了什么 |
| **`.dmg`** | 能：`hdiutil attach -nobrowse -mountpoint …` 挂载后拷出 `.app` | **毫无意义**：dmg 的价值就是拖拽交互；而且从网上下载的 dmg 挂载后拷出的 `.app` 照样带 `com.apple.quarantine`，Gatekeeper 一样拦。签名/公证票据还要**分别** staple 到 dmg 与 `.app` 两处 |

结论：**cpi 只吃"已经摆成 `payload/` 形状"的归档**。这不是能力不足，是刻意的边界——凡是被别人装的，cpi 就没法负责撤销。

### 2.13 三平台现状（诚实交代）

代码层面三条路径都已实现；下面"实测"的意思是**在真的那个操作系统上跑过测试或端到端**，依据是 §2.16 的 CI。

| 能力 | macOS | Linux | Windows |
|---|---|---|---|
| 解包 / 校验 / 落盘 | ✅ 实测（沙箱 HOME，端到端） | ✅ CI | ✅ CI |
| 终端启动器 | ✅ activate 与 direct 都实测过 | ✅ CI（包装脚本） | ✅ CI（`.cmd`） |
| 图形入口 | ✅ 实测（`~/Applications` 软链可被 `open` 拉起） | ✅ CI（`.desktop`，过 `desktop-file-validate`） | ✅ CI（开始菜单 `.lnk`，写入后读回比对） |
| PATH 集成 | ✅ 实测（`.zprofile`/`.zshrc`/`.profile`，连装三次不叠加） | ✅ CI（同一个纯函数，CI 里真写文件） | ✅ CI（真写 `HKCU\Environment` 并向自己广播） |
| 覆盖式重装 / 幂等 / 回滚 / 卸载 | ✅ 实测 | ✅ CI | ✅ CI |
| 上游裸二进制 → 合成 `.app` 外壳 | ❌ **未实现**（配方见 §2.7，需先给 `manifest.Entry` 加 `bin`） | — | — |

> "✅ CI" ≠ "在真机上手动点过一遍"。它等于：**在那个操作系统的 runner 上，测试真的落盘、真的写注册表、真的建 `.lnk` 并读回来**。这三件事里最容易只在真机上暴露的（路径分隔符、可执行位、PATH 的类型与空条目）都已经由 CI 抓过一次或两次（§0.3.1）。

**Windows 上已经踩过并处理掉的坑**（留档，免得改动时踩回去）：

1. 写 `HKCU\Environment` 的 `Path` **别用** `[Environment]::SetEnvironmentVariable(..., "User")`——它会把 `REG_EXPAND_SZ` 写成 `REG_SZ`，用户原有的 `%USERPROFILE%` 等不再展开；直接操作注册表原值才稳（C3、§2.6）。
2. 脚本优先 `.cmd`/`.bat`，不要依赖 PowerShell 的执行策略（C4）。
3. **PATH 的往返必须逐字节保真**，空条目要原样留着（§2.6）——CI 第一轮就抓到了"卸载后少了结尾那个分号"。
4. **`filepath.Separator` 与 `os.UserHomeDir()` 的分隔符不一致**：Windows 上前者给 `\`、后者给 `/`，拿 `homeDir + string(filepath.Separator)` 当前缀去比一定比不中。凡是"把绝对路径相对化成 `$HOME/…`"的地方，两边都要先 `filepath.ToSlash`。
5. 下载物带 MOTW，SmartScreen 会报"未知发布者"；而"下载来的脚本改 PATH + 写注册表"正是杀软/EDR 的敏感行为特征。**这不是能靠代码解决的，是签名问题**（§5 R1）。

### 2.14 CLI

**安装侧：**

```
cpi install <目录或 .zip> [--dir PATH] [--yes] [--no-path] [--skip-verify]
cpi uninstall <id> [--dir PATH]
cpi list [--dir PATH]
cpi where <id> [--dir PATH]
cpi env [--dir PATH]        # 打印把 bin/ 加进 PATH 的 shell 片段
cpi version | cpi help
```

**打包侧（发布者用，见 §2.15）：**

```
cpi pack <装配目录> [--out PATH] [--os OS] [--arch ARCH] [--cpi 可执行文件]
```

- 退出码：`0` 成功；`1` 运行时错误；`2` 用法错误。
- 选项与位置参数**顺序无关**：`cpi install . --dir ~/ad` 与 `cpi install --dir ~/ad .` 等价（标准库 `flag` 遇到第一个位置参数就停止解析，所以入口处把选项重排了一次）。
- 输出针对"看得懂中文但不一定懂 shell 的人"写：出错时给的是**哪个文件、哪个字段、怎么改**。
- `cpi version` 的版本号是编译期注入的（`-ldflags "-X main.version=…"`），所以 `var version` 而不是 `const version`——`const` 注入不进去。

### 2.15 打包：`cpi pack`

**打包这一步在 cpi 里，不在 shell 脚本里。** 装配目录长这样：

```
<装配目录>/
├── manifest.yaml
└── payload/…
```

`cpi pack <装配目录>` 会就地补齐另外三样（`install.sh`、`install.cmd`、`SHA256SUMS`），挑一个 cpi 二进制塞进去，然后产出 `<id>-<version>-<os>-<arch>.zip`。

**为什么从 `tools/fixture/make.sh` 那套 shell 搬进 Go**：

| 问题 | shell 方案 | `cpi pack` |
|---|---|---|
| Windows 上的 Git Bash 常常**没有 `zip`** | 挂 | 标准库 `archive/zip` |
| `sha256sum` 不保证存在（macOS 只有 `shasum`） | 要写分支 | 一处实现 |
| **`.app` 里有符号链接，普通 zip 会把链接展开成实体** | 靠 `zip -y`，容易忘 | `WalkDir` 不跟链接走，按链接原样写 |
| **Unix 权限位**（`install.sh` 要 `0755`） | 靠 `zip` 的默认行为 | `FileHeader.SetMode` —— 只有它会把权限位写进 ExternalAttrs |
| `SHA256SUMS` 的格式（两个空格、按路径排序、符号链接不参与） | 每个仓库各写一遍 | 与 `stage.VerifySums` 同一个约定 |

几条硬规定：

- **先算 `SHA256SUMS`，再建 zip**：清单必须在写进 zip 之前就是确定的字节串。
- **任一步失败就删掉半个 zip**：留一个坏压缩包比什么都不留更糟（人会以为打包成功了）。
- **`payload/` 下每个常规文件都必须在清单里**，符号链接不参与——与安装侧的校验规则逐字一致。
- **Windows 目标时 cpi 改名成 `cpi.exe`**：`install.cmd` 里写的就是 `cpi.exe`。
- 打包用的 manifest 必须**有目标平台的 `entry`**，否则直接报错（"这个包不适用于本机"要在打包时就发现，而不是发出去之后）。

`tools/fixture/make.sh` 还在（造测试用的小包），但**发布路径已经不再经过它**。

cpi 自己的二进制与签名：

| 项 | 现状 |
|---|---|
| 三平台二进制 | `GOOS=… GOARCH=… go build -trimpath -ldflags "-s -w -X main.version=…" ./cmd/cpi`，交叉编译不需要 cgo（`CGO_ENABLED=0`） |
| 发版 | 推 `v*` tag → `.github/workflows/release.yml` 出六个平台的 `cpi-<版本>-<os>-<arch>[.exe]` → `gh release create` |
| 命名 | `cpi-<版本>-<os>-<arch>` 是**给 AI Desk 的 CI 看的契约**（它按这个名字挑对应平台的 cpi 塞进 zip） |
| 签名 | **未做**，是发布阻塞项（§5 R1） |

### 2.16 持续集成

本机没有容器，也没有别的操作系统的虚拟机，所以**多平台验证只能交给 CI**：把平台专属的行为做成"在真 Linux / 真 Windows 上跑起来才成立"的测试，而不是靠读代码相信它。

**cpi-go（`.github/workflows/ci.yml`）**，每次 push / PR：

| job | 内容 |
|---|---|
| `test` × {ubuntu, macos, windows} | `go vet ./...` + `go test ./...`；ubuntu 上先装 `desktop-file-utils`（给 `desktop-file-validate` 用）；**windows 上额外真写一次 `HKCU\Environment`**（`CPI_TEST_REGISTRY=1` 才不 skip） |
| `crosscompile` × {darwin, linux, windows} × {amd64, arm64} | `CGO_ENABLED=0 go build ./...`，确认六个组合都编得出来 |

发版：`.github/workflows/release.yml`，`v*` tag 触发。

**ai-desk（`.github/workflows/release.yml`，一个文件同时兼 CI 与发版）**，每次 push：

- **三个平台各自真打一遍包**：macOS(arm64) / Linux(amd64) / Windows(amd64)。
- cpi **不是下载来的，是就地编译的**：workflow 里 `actions/checkout` 把 cpi-go 检出到 `.cpi-go`，`go build` 出当前平台的 cpi，再交给 `tools/package.sh`。好处是**两个仓库的改动能一起被验证**，也不必先有 cpi 的 release 这里才跑得起来（`workflow_dispatch` 有个 `cpi_ref` 输入，默认 `main`）。
- 版本号只有一个来源：`src-tauri/tauri.conf.json`。CI 用 `node -p` 读它，传给打包脚本，避免"tag 上的版本和产物里的版本不一样"。
- **前端依赖必须锁死**：`npm ci` + `package-lock.json`。第一次 CI 就是在这里翻的车——`package.json` 里写 `"^2"` 又没锁文件，CI 每次解析到最新的 `@tauri-apps/*`，与 `Cargo.lock` 里的 Rust crate 对不上，`tauri build` 直接拒绝构建（§5 R9）。

---

## 3. 测试策略与已验证结果

**隔离原则**：所有测试不得触碰真实 `$HOME`。端到端一律通过覆盖 `HOME` 进行——Unix 上 `os.UserHomeDir()` 返回 `$HOME`，所以改一个环境变量就能把 `~/ad`、`~/Applications`、`.zprofile/.zshrc/.profile` 全部关进沙箱。这也是唯一能安全测 PATH/shell 配置写入的方式。

| 层 | 方法 | 覆盖 |
|---|---|---|
| manifest 校验 | 表驱动：`entry` 缺失、`bundle` 与 `exe` 并存、路径含 `..`、`launch.cmd` 含分隔符、当前平台无入口 | `internal/manifest` |
| 解包路径安全 | 构造含 `../` 与绝对路径的 zip，断言被拒 | `internal/stage` |
| 校验 | 改动一个字节 → 拒绝；删掉 `SHA256SUMS` → 警告 + `unverified`；`--skip-verify` → 放行；**带空格的路径**（`AI Desk.app/…`）要能正确切出文件名 | `internal/stage` |
| 打包 | 打出来的 zip 能原样解回来（含符号链接与 `0755`）；`SHA256SUMS` 覆盖到每个常规文件且不含链接；缺目标平台入口时拒绝；失败不留半个 zip | `internal/pack` |
| 启动器语义 | 断言 `activate` 生成 `open … --args` 而 `direct` 直接 exec；参数逐个透传；生成的文件真的可执行 | `internal/integrate` |
| PATH 块 | **把生成的块真喂给 `zsh -n` / `bash -n` / `sh -n`**；"必须是 `$HOME` 相对形式"；连 source 三次只有一个块且在首位；fish 用 `contains` | `internal/integrate` |
| Windows PATH | 纯函数表驱动（展开 `%VAR%`、判重、只摘自己那一条）+ **往返逐字节保真**；真注册表往返由 CI 的 windows job 跑 | `internal/integrate` |
| Windows `.lnk` | 写进去再读回来，比对 Target/Arguments/WorkingDir/Description；已有同名不覆盖 | `internal/integrate`（windows） |
| Linux `.desktop` | 字段齐全、`Exec=` 指向启动器、遵守 `$XDG_DATA_HOME`；有 `desktop-file-validate` 就过一遍 | `internal/integrate`（linux） |
| 集成副作用 | 隔离 `HOME` 后跑真实安装，断言目录、启动器、图形入口、账本、PATH 块；重复安装不叠加；卸载后逐项消失 | e2e |
| 跨平台 | CI 矩阵 `{ubuntu,macos,windows}` 真跑单测；`{darwin,linux,windows} × {amd64,arm64}` 交叉编译 | §2.16 |
| 静态检查 | `go vet` | 全仓 |

**一条已经兑现的教训**：字符串级断言抓不住"生成的 shell 代码本身是坏的"。PATH 标记块曾经漏掉一个引号（`*":$HOME/ad/bin:*)`），单测全绿，而真 zsh 一读 `~/.zprofile` 就 `unmatched "`——软件装好了、图标也在，终端里却永远 `command not found`。所以那组测试改成**把生成的代码交给真 shell 去解析**。同类：Windows PATH 的往返保真如果只在字符串层面测，也会漏掉"空条目被吃掉"。

**macOS 上已经端到端实测过的（27.0.1 / arm64，沙箱 `HOME`）**：

- `install.sh` → `cpi install . --dir …` 全流程成功，`~/ad` 布局与设计一致。
- 终端启动器 `~/ad/bin/ad` 真的把应用拉起来了（探针自报 `argv0` 在 `~/ad/lib/…/Minimal.app/Contents/MacOS/Minimal`、`bundlePath` 正确、`bundleIdentifier` 非 `(null)`、`activationPolicy = 0`）。
- `~/Applications/AI Desk.app` 符号链接经 `open` 同样能拉起（与双击等价）。
- PATH 标记块写进三个文件；**连装三次每处仍只有一个块**（幂等）。
- 负例：payload 改一个字节 → `sha256 对不上（清单 …，实际 …）` 拒绝；manifest 指向不存在的入口 → 拒绝。两次失败**都不留半成品**。
- 卸载后 0 残留，连 cpi 自己创建出来的空 shell 配置文件都删掉了。

**CI 已经抓出来的四个问题**（都不是靠读代码能发现的）：

| # | 现象 | 根因 |
|---|---|---|
| 1 | Windows 上生成的 PATH 块是绝对路径，没相对化成 `$HOME/…` | `homeRelative` 拿 `filepath.Separator`（`\`）去比 `os.UserHomeDir()`（`/`）→ 前缀永远不匹配（§2.13 第 4 条） |
| 2 | Windows 上报"可执行位丢了：`-rw-rw-rw-`" | Windows 的 `os.Stat` 一律报 `0666`，是**测试**的断言没分平台 |
| 3 | 卸载后用户的 PATH 少了结尾一个分号 | `windowsPathValue` 插值前 `Trim`、`windowsPathRemove` 又只 join 非空条目 → 空条目被吃掉（§2.6） |
| 4 | ai-desk 三平台构建全挂：`Found version mismatched Tauri packages` | `package.json` 写 `"^2"` 且仓库无 lockfile，CI 解析到的 `@tauri-apps/*` 比 `Cargo.lock` 里的 crate 新（§2.16、R9） |

**尚未验证**：`direct` 模式在真实长驻应用上的行为；中断（kill -9）注入；`.app` 外壳合成（代码未实现）；Linux/Windows 上的**人工**体验（CI 覆盖的是测试断言，不是"人对不对得上眼"）。

---

## 4. 里程碑

| 里程碑 | 内容 | 状态 |
|---|---|---|
| **M0 macOS 一条龙** | 解包/校验/落盘/启动器/图形入口/PATH/账本/卸载/回滚；`tools/fixture` 造包 | ✅ **已完成并实测**（提交 `c4fe024`） |
| **M1 Linux** | 包装脚本 + `.desktop`（遵守 `$XDG_DATA_HOME`）；AppImage 作为 `entry.linux.exe` 的收录路径 | ✅ **已完成**，在 ubuntu runner 上验证 |
| **M2 Windows** | `HKCU\Environment` 写 PATH + 广播 `WM_SETTINGCHANGE`；开始菜单 `.lnk`（原生 COM）；`install.cmd` 在真机跑通 | ✅ **已完成**，在 windows runner 上验证 |
| **M3 打包收进内核** | `cpi pack`：`SHA256SUMS` + 权限位 + 符号链接 + 三平台一致 | ✅ **已完成**（提交 `acb0c4a`） |
| **M4 两个仓库的 CI** | cpi-go：三平台单测 + 六平台交叉编译 + tag 发版；ai-desk：三平台真打包 | ✅ **已完成并跑绿**（`242c54e`、`9e33ad7`） |
| **M5 签名与公证** | macOS Developer ID + 公证 + `xcrun stapler`；Windows 代码签名 | ⏸ **用户明确暂缓**（放弃 1，先做 2 和 3）；仍是真正的发布阻塞项（§5 R1） |
| **M6 未做的两件** | 合成 `.app` 外壳（需先给 `manifest.Entry` 加 `bin`）；启动器名字冲突检查（R4） | 待做 |

---

## 5. 风险与开放问题

### 风险

| # | 风险 | 影响 | 对策 |
|---|---|---|---|
| **R1** | **macOS 签名与公证**（Developer ID）、**Windows 代码签名**是发布阻塞项 | zip 过浏览器/邮件会带 `com.apple.quarantine` 与 MOTW，用户双击 `install.sh` 或应用时被 Gatekeeper / SmartScreen 拦。签名在 Mach-O 内部，过 zip 不会丢；彻底解决只能签名 + 公证 + `xcrun stapler staple`（票据订在包上，断网也能验） | 发布前必须解决；内网过渡阶段可在文档里教用户处理，但**不要把 `xattr -dr com.apple.quarantine` 写进 `install.sh`**——那是替用户绕过安全检查 |
| **R2** | **合成外壳没有独立签名**（D24） | 上游只发裸二进制、由 cpi 合成 `.app` 时，外壳本身不是签名包；若下载物带 quarantine，Gatekeeper 可能直接拦 | 与 R1 同源；优先收录上游自己发 `.app` 的软件 |
| **R3** | ~~Windows 侧 PATH 与开始菜单尚未实现~~ | — | **已解决**（§2.13）；CI 的 windows job 每次都会重跑真注册表往返，防止改回去 |
| **R4** | 启动器**没有做名字冲突检查**：`integrate.Launcher` 会直接覆盖 `bin/<cmd>` | 若用户已在 `<CPI_HOME>/bin` 放了同名文件会被静默覆盖 | 应当比照 `AppLink` 的做法：存在且不归 cpi 所有则跳过并告知（当前是**已知缺口**，M6） |
| **R5** | 上游应用改名 / 改目录结构 | `manifest.yaml` 与 `payload/` 对不上，安装失败 | 打包脚本在 CI 里跑，写完就验，不靠人手维护 |
| **R6** | 修改 shell 配置被视为越界 | 用户反感或系统管理员禁止 | 明确请求许可、标记块、可一键撤销、拒绝后功能降级而非失败 |
| **R7** | 用户已有同名应用（自己装过官方版） | `~/Applications` 里两个同名 | 已处理：目标已存在则不覆盖，只提示 |
| **R8** | **`.app` 外壳合成没有实现**（D24 的后半条） | 上游只发裸可执行文件的 macOS 应用，现在只能写 `entry.darwin.bundle`，写不出来就装不了；硬把它当 `exe` 收进来，用户拿到的是一个没有应用身份的东西 | 先给 `manifest.Entry` 加 `bin`，再照 §2.7 的配方实现（`Info.plist` 4 键 + **硬链接** `Contents/MacOS/<exe>`）。在实现之前，清单里**不要写没有 `.app` 的 macOS 应用**（M6） |
| **R9** | **Tauri 前端依赖与 Rust crate 版本漂移** | 这个坑真实发生过：`package.json` 写 `"^2"` + 无 lockfile，CI 解析到比 `Cargo.lock` 更新的 `@tauri-apps/*`，`tauri build` 报 `Found version mismatched Tauri packages` 直接拒绝构建——**本机编得动只是因为 `node_modules` 里还是旧版本** | 已处理：三个 Tauri 包钉死确切版本 + 提交 `package-lock.json` + CI 用 `npm ci`。泛化教训：**凡是"本机能编、CI 编不了"的构建问题，先怀疑锁文件** |

### 开放问题

| # | 问题 | 需要谁定 |
|---|---|---|
| ~~O1~~ | ~~Windows 的 PATH 与 `.lnk` 什么时候做~~ | **已做**（§2.13） |
| O2 | 要不要给应用图标（`.icns` / `.ico`），从哪来 | 用户 |
| O3 | 启动器名字与已有命令冲突时，是跳过、报错、还是让用户改名 | 用户（当前行为见 R4，**尚未实现任何检查**） |
| O4 | 图形安装器（原 Wails 四屏）还做不做 | 用户 |
| O5 | 是否需要"装完自动打开终端 / 自动启动一次应用"的引导 | 设计 |
| O6 | 中断注入测试（kill -9）什么时候补 | 设计 |
| O7 | cpi 自身如何升级（现在只会被新 zip 里的副本覆盖） | 用户 |
| O8 | 要不要做合成 `.app` 外壳（R8）——当前 AI Desk 有真 `.app`，所以不挡路 | 用户 |

---

## 6. 术语

| 术语 | 含义 |
|---|---|
| cpi | 本产品：Cross Platform Installer |
| `CPI_HOME` | 安装根，默认 `~/ad`；`--dir` > `$CPI_HOME` > `~/ad` |
| 分发包 | 那个 zip：`install.sh`/`install.cmd` + `cpi` + `manifest.yaml` + `payload/` + `SHA256SUMS`；由 `cpi pack` 产出（§2.15） |
| 装配目录 | 打包的输入：只有 `manifest.yaml` + `payload/`，其余三样由 `cpi pack` 补齐 |
| `cpi pack` | cpi 的打包子命令（§2.15）；发布路径上唯一被支持的打包方式 |
| manifest | `manifest.yaml`，描述一个应用怎么落地（`entry` + `launch`） |
| payload | 应用的实际内容，原样落到 `lib/<id>_<ver>_<os>_<arch>/` |
| entry | 可执行入口：`bundle:`（macOS `.app`）或 `exe:`（其它平台） |
| 账本 / `state.json` | 装了什么 + 产生了哪些外部副作用的**唯一真相**；卸载按它回放 |
| 集成 / integrate | 让装好的东西"能被用上"：终端启动器、图形入口、PATH |
| 启动器 / launch | `bin/<cmd>` 下的小脚本，让"装完能从终端启动"成立（§2.8） |
| 应用外壳 / shell | cpi 为"上游只发裸可执行文件"的 macOS 应用合成的最小 `.app`（§2.7） |
| 图形入口 | macOS `~/Applications` 链接、Linux `.desktop`、Windows 开始菜单 `.lnk` |
| rollback 栈 | 本次安装失败时逆序撤销的依据（与账本分工不同，§2.5） |

---

## 7. 附录：v1 / v2 已废弃的范围与理由

v1（519 行）与 v2（735 行）描述的是另一个产品：**给不懂命令行的人装一堆常用软件**。它建立在几个后来被推翻的前提上，现整体退出范围。留此存档，免得同一个念头再被重新发明一遍。

### 7.1 为什么"通用装机清单"这条路走不通

- **可收录的软件太窄**。收录规则要求"有官方可搬运二进制 + 支持当前用户级安装 + 三平台"，而 `git` / `curl` / `make` 在 macOS 上**没有官方可搬运二进制**（`git` 只发 `.pkg`），三平台同时满足"不提权"的桌面软件在 Windows 上尤其稀少（官方安装器基本都要 UAC）。最后剩下的是一个又小又偏的目录。
- **真正难的不是"装"，是"下载"**。下载动作本身会把 Gatekeeper / SmartScreen 重新引进来，而解决它需要签名与公证（§5 R1）——那是产品之外的成本。
- **收益与成本不成比例**。为了让陌生人用起来，需要图形界面、错误分级、普通人能懂的文案、WebView 依赖处理、单实例锁……这些工作量的目标是"陌生人"，而真实用户其实就在自己身边（同事、内网、另一台机器），给他们"解压 + 跑脚本"完全够用。

### 7.2 逐条对照

| 原决策 | 内容 | 现在 |
|---|---|---|
| D1 | 清单来源 = 内置 YAML + 外部覆盖 | **作废**：没有"清单"，只有一个 `manifest.yaml` |
| D2 | 全部直连官方二进制，不用包管理器 | **保留精神**：仍然不用 brew/apt/winget |
| D3 | 默认范围 = 核心 CLI + 语言运行时 | **作废**：纯 CLI 工具整体出局 |
| D4 | 网络：代理、自定义源、离线模式 | **作废**：单应用场景下"下载"由用户自己完成 |
| D5 | 交付物 = `docs/DESIGN.md` | **改为**：契约是 `PACKAGE-FORMAT.md`，本文是它的理由 |
| D7 | PATH 集成（标记块、幂等、请求许可） | **保留**，见 §2.6 |
| D8 | 账本 + 反向清理（卸载） | **保留**，见 §2.9 |
| D9 | 只装当前用户，全程不提权 | **保留**，见 §0.2 |
| D10 | 单版本覆盖式 + `install`/`list`/`uninstall` | **保留**（镜像默认值那部分作废） |
| D11 | 离线/内网模式 | **作废**：zip 本身就是离线载体 |
| D13 | 图形界面（Wails 四屏） | **暂缓**（O4） |
| D14 | 范围 = 只收录带 GUI 的应用 | **保留**：只装一个应用，天然成立 |
| D15–D17 | 终端启动器：`bin/<cmd>`、参数透传、`activate`/`direct` | **保留**，见 §2.8 |
| D18 | macOS 应用必须 `.app`，裸二进制由 cpi 合成外壳 | **保留**，见 §2.7——**这是 v2 最有价值的一条**，是花了一轮真机实验换来的（合成那一半代码还没写，R8） |
| — | store（内容寻址）、镜像表、版本源（`github-release`/`url-feed`/`pin`）、计划算法 `plan.Build`、四种安装阶段、`cpi doctor`、`cpi gc`、i18n、便携模式、`expose`（把自带 CLI 暴露成 shim） | **全部作废**（`expose` 单应用场景下用不着——应用自带 CLI 就直接在 `entry` 里给它，不必再暴露一次） |

### 7.3 从 v2 继承下来、仍然值得记住的三条硬知识

1. **macOS 上 `.app` 是必需的，而且是目录不是文件**；`Contents/MacOS/<exe>` 只能硬链接或拷贝，**不能符号链接**（§2.7）。
2. **zsh 不读 `~/.profile`**，只写 `.profile` 会导致"装好了但终端里敲不出来"（§2.6）。
3. **macOS 上 `~/Applications` 会被启动台与 Spotlight 索引**，是用户级图形入口的正解；Windows 侧则必须直写 `HKCU\Environment` 并广播 `WM_SETTINGCHANGE`（§2.6、§2.7）。
