# gpm — GUI Package Manager 设计文档（v3.5）

> **契约在 [`PACKAGE-FORMAT.md`](PACKAGE-FORMAT.md)，本文讲"为什么这么设计"。**
> 两者冲突时以 `PACKAGE-FORMAT.md` 为准——它是冻结的对外接口，本文是内部推理。

v3 把范围从 v2 的"通用装机清单 + 图形安装器"收窄为**一次装一个产品的安装器**：解压一个 zip、跑里面的脚本，装完终端能敲、图标能点、能干净卸载。

**v3.3 改的是名字与"装到哪儿"**：产品从 `cpi` 改名 **gpm（GUI Package Manager）**；`~/ad` 这个硬编码的安装根被删掉——**装到哪儿是打包方/安装器的决定**（`gpm pack --default-dir` 把它烘进 `install.sh`/`install.cmd`，AI Desk 用的是 `~/ad`），gpm 自己只认 `--dir` > `$GPM_HOME` > 当前目录。顺带补掉一处真实风险：`<bin>/gpm` 已经存在时不再覆盖（原来拿旧包安装会把新版 gpm 悄悄降级）。名字的由来、以及"为什么叫 manager 不算撒谎"见 §0.5。

**v3.4 补的是 v3.3 留下的那个洞（O11）**：删掉 `~/ad` 之后，用户在新终端里敲 `gpm list` 会落到**当前目录**——因为 `GPM_HOME` 只活在 `install.sh` 那一行里，shell 里并没有它。按用户给的布局原则（**带 GUI 的应用放在指定的目录下；没有图形界面的小东西放在它下面的 `bin/`；gpm 自己也一样**），`<家目录>/bin/gpm` 这个位置本身就把家目录说出来了，于是优先级在 `$GPM_HOME` 与"当前目录"之间补了一档 `RootFromSelf()`：**从 gpm 自己在哪儿推断**。不需要新状态、也不需要 shell 帮忙记（见 D21、§2.3、§0.3.4）。

**v3.5 把"装完能用"从 GUI 应用扩到工具链，并把安装根还给工具链自己的家。** 设计对象是随这一版定下的需求文档 [`REQUIREMENTS.md`](REQUIREMENTS.md)（那边记用户原话，本文记理由）：一个包可以在清单里声明 `requires: [cot]` / `[tdp]`，install.sh 就拿 zip 里自带的那份可执行文件跑 `cot i -s <家>`（离线、只装 cot 自身与 activate 框架，不装插件，见 C3/C4）；装完工具链的家（`$COT_HOME` / `$TDP_HOME`）与 GUI 应用的家**合并成同一个目录**——于是 gpm 自造的 `GPM_HOME` 退场，`~/ad` 也不再有任何特殊地位（本机已按旧账本回放清理，见 §3）。同时补上两件拖了很久的事：**终端启动器必须把工具链环境注入给 GUI 应用**（macOS 的 `open` 不给环境，实测见 §2.8 / C1，v1 因此改成直接 exec `Contents/MacOS/<binary>`），**启动器重名检查**从"已知缺口"提为需求（R4 → FR-21）。

v2 里那套"多应用清单 / 镜像表 / 版本源 / 离线 store / Wails 四屏"**全部退出范围**，理由与保留价值记在 §7 附录。

**v3.1 把三平台的后半程补上了**：Windows 的 PATH（直写 `HKCU\Environment`）与开始菜单 `.lnk`（原生 COM）、Linux 的 `.desktop`，都已实现；打包从 shell 脚本搬进 `gpm pack` 子命令；两个仓库都接上 CI，**Linux 与 Windows 的路径由真 Linux / 真 Windows runner 跑测试来验证**。全文里"未实现 / 未在真机验证"的说法已按此更新（§0.3、§2.13、§3）。

**v3.2 补的是一个"手太快"的坑**：gpm 原来会闷头覆盖安装或卸载，而**正在运行的应用被抽掉脚下的文件之后不会自己退出**——它会抓着一份已经被删掉的旧代码继续跑，之后按路径读到的资源、动态库、拉起的子进程却已经是另一份（版本混用）；macOS 上还会留下一个**幽灵进程**（`~/Applications` 的链接一直指着它，用户点图标只会把它唤到前台，永远拿不到新版本）。这条是拿真应用（AI Desk + 真 updater）在这台机器上跑出来的，见 §2.9、§3。现在动手之前先查，正在运行就用人话说明并拒绝，除非给 `--force`。

---

## 0. 决策快照

### 0.1 现行决策（不再重开讨论）

| # | 决策 | 出处 |
|---|---|---|
| **D19** | **范围 = GUI 应用的安装器 + 工具链自举的编排**。一次安装一个 `id`，但**账本是多包的**：同一个家里可以并排装好几个应用，各占自己的 `lib/<id>_…` 与 `bin/<cmd>`（见 A3）。不做多应用清单、不做计划算法、不做镜像表/版本源/断点续传/离线 store，**也不抓取、不升级**（那是 `apt` 的活，见 §0.5）。**图形安装器暂缓**（原 D13 的 Wails 四屏不在首版范围）。 | 本项目 + v3.5 |
| **D20** | **分发形态 = zip 自带 gpm**。产物是一个 `<id>-<version>-<os>-<arch>.zip`，内含 `install.sh`（mac/Linux）/ `install.cmd`（Windows）+ `gpm` + `<简称>-manifest.yaml` + `payload/` + `SHA256SUMS`；声明了 `requires` 时还有 `tools/<os>_<arch>/{cot,tdp}`（D32）。用户解压后运行其中一个，脚本只做三行 bootstrap。 | 本项目（v3.5 改清单名） |
| **D21** | **安装根由安装器决定**：优先级 `--dir` > **按 `requires` 取的那个家**（`${COT_HOME:-$HOME/cot}` / `${TDP_HOME:-$HOME/tdp}`）> **从 gpm 自己的位置推断**（`RootFromSelf()`，v3.4）> **平台数据目录/<简称>**（`requires` 为空、又没有别的东西可依附时的落脚点：macOS `~/Library/Application Support/<简称>`、Windows `%LOCALAPPDATA%\<简称>`、Linux `${XDG_DATA_HOME:-~/.local/share}/<简称>`）> **当前目录**。打包方用 `gpm pack --default-dir "~/cot"` 把默认值烘进 `install.sh`/`install.cmd`。**v3.5 起没有 `GPM_HOME`**：家就是工具链自己的家，GUI 应用落在它的 `lib/` 下，`bin/` 里住 cot / tdp / gpm / 各个 `<简称>`——一个家、一条 PATH。 | 本项目；v3.5 按用户裁决改根（§0.3.5） |
| **D22** | **输入契约**：一个目录或一个 zip，里面有 `<简称>-manifest.yaml` + `payload/` + `SHA256SUMS`（`requires` 非空时还有 `tools/<os>_<arch>/`）。清单文件名由简称决定；给一个目录时 `gpm install` 认唯一的 `*-manifest.yaml`，多于一个就报错。 | 本项目；v3.5 改文件名并加 `requires` |
| **D23** | **校验**：`SHA256SUMS` 必须覆盖 `payload/` 下每一个常规文件；对不上即拒绝安装。**缺失 `SHA256SUMS` 时警告后继续**，并把本次安装记为 `unverified`（`gpm list` 会显示）。`--skip-verify` 只用于调试。 | 沿用 A2 |
| **D24** | **macOS 应用必须以 `.app` 形态落地**：上游给了就用（`entry.darwin.bundle`）；上游只发裸可执行文件时由 gpm 合成最小外壳。 | 沿用 D18。**合成外壳这一半本版尚未实现**，见 §2.7、§2.13 |
| **D25** | **终端启动器**：每个应用在 `<家>/bin/<cmd>` 生成一个启动器，参数透传；`mode: activate`（默认）或 `direct`（要 stdout / 退出码）。**v3.5 起 macOS 上要注入工具链环境时（`requires` 非空）直接 exec 内层可执行文件**，没有环境要注入时不改道（`activate` 仍走 `open`，D33）；名字撞车必须报错、不覆盖（FR-21）。 | 沿用 D15–D17；v3.5 改 macOS 实现 |
| **D26** | **PATH 集成**：把家下面的 `bin/` 接进 PATH——`cot`、`tdp`、`gpm` 与各个 `<简称>` 都住在那里，所以一个块一条就够。按 `$SHELL` 决定落点，写幂等标记块，**写之前必须用人话请求许可**，拒绝则功能降级而非失败。 | 沿用 D7；v3.5 明确"工具链也走同一个块" |
| **D27** | **gpm 不吃 `.dmg` / NSIS `.exe` / AppImage 安装器**，只吃已经摆成 `payload/` 形状的归档。理由见 §2.12。 | 本项目 |
| **D28** | **账本 `state.json` 是所有外部副作用（PATH 块、`~/Applications` 链接、`.desktop`）的唯一真相**，卸载即回放删除。**边界见 D34**：只回放 gpm 自己写下的东西，工具链的资产不记账也不删。 | 沿用 D8 + v3.5 |
| **D29** | **gpm 把自己也装进 `<家>/bin/gpm`**，保证装完之后 `gpm list` / `gpm uninstall` 还找得到它；卸载时一并删掉（仅当账本里已经没有别的包）。**那儿已经有一个 gpm 就不装、不覆盖**（那属于用户，卸载时也不删它）——否则拿旧包安装会把新版 gpm 静默降级。 | 本项目 |
| **D30** | **单版本覆盖式**：同一个 `id` 再装一次，先删旧的包目录与启动器再装新的，不做多版本共存。 | 沿用 D10 |
| **D31** | **动手之前先查那个应用在不在跑**：覆盖安装与卸载都在删东西之前 `proc.Find` 一次；在跑就用人话说明后果并**拒绝**，只有显式给 `--force` 才继续。查不出来（平台不支持 / 没权限）**不拦**，只打印一行提示。 | 本项目。见 §2.9、§3 |
| **D32** | **工具链自举由安装器编排，zip 自带、完全离线**：清单声明 `requires: [cot]`（或 `tdp`）时，install.sh 先跑 zip 里的 `tools/<os>_<arch>/cot`：`cot i -s "${COT_HOME:-$HOME/cot}"`（tdp 按 `$TDP_HOME`）。gpm 不下载、不查版本、不代装插件；那一步失败就**不继续**（FR-20）。 | v3.5，本项目 |
| **D33** | **启动器要把工具链环境注入给 GUI 应用**：`<家>/bin/<cmd>` 启动前 export 本场景的家变量（cot 场景 `COT_HOME`、tdp 场景 `TDP_HOME`）、把 `<家>/bin` 前置到 PATH，并在 `<家>/bin/env-cot.vars` 存在时 source 它。**macOS 上只要这个包声明了 `requires` 就直接 exec `Contents/MacOS/<binary>`**——`open` 不给环境（C1）；没有 `requires` 时不改道（`activate` 仍旧 `open`，双击语义不丢）。改道之后的代价是丢掉 LaunchServices 语义；Finder / 启动台双击的实例拿不到注入（A7、R13）。 | v3.5，本项目 |
| **D34** | **归属边界**：账本只记 gpm 写下的东西（自己的 `lib/<id>_…`、`bin/<cmd>`、图形入口、rc 标记块）。工具链自己的资产（`bin/cot`、`bin/tdp`、`env-cot*`、`activate*`、`lib/` 下的插件）**不进账本、`uninstall` 一律不碰**，那个家目录本身也不删。 | v3.5，本项目。见 §2.9 |

### 0.2 由上述决策推导出的硬性设计约束

- **全程不提权**：不 sudo、不触发 UAC、不写机器级位置。所有落点都在当前用户家目录下。
- **内核不依赖 GUI**：`internal/` 绝不 import 任何 GUI 包（暂缓 GUI 之后这条变成"将来加 GUI 时不许反过来污染内核"）。
- **一切以"能干净撤销"为准绳**：任何外部写入都必须先登记进账本、后执行。
- **staging 必须建在 `<家>` 之下**：只有同一文件系统内的 `rename` 才是原子的，跨文件系统 `rename` 会退化成"拷贝 + 删除"。
- **失败不能留半成品**：安装过程用 rollback 栈，按逆序撤销已完成的步骤。

### 0.3 v2 → v3 变更记录

| 变了什么 | 为什么 |
|---|---|
| 范围：通用装机清单 → **单应用安装器** | 真实需求是"把我自己这一个 Tauri 应用装好"，不是"给陌生人装一堆软件" |
| 输入：版本源 + 镜像 + store → **一个 zip** | 没有服务端、没有清单仓库，最简的可靠分发就是文件 |
| 安装根：`~/.gpm` → **`~/ad`** | `~/ad` 既是应用家目录又是 `GPM_HOME`；卸载 = 删一个目录 |
| gpm 的来源：单独分发 → **zip 自带** | 用户不需要先装一个新东西才能装东西 |
| 校验：store 内容寻址 → **`SHA256SUMS`** | 内容寻址的前提是一整套 store，单应用场景下是过度设计 |
| 图形界面：Wails 四屏 → **暂缓** | 首版是"解压 + 运行脚本"，GUI 不是它的前置条件 |
| 硬链接：`store` ↔ `lib` 之间 → **当前唯一用法是合成 `.app` 外壳** | store 没了，硬链接只剩一处 |

### 0.3.1 v3 → v3.1 变更记录

| 变了什么 | 为什么 |
|---|---|
| Windows PATH：**未实现** → 直写 `HKCU\Environment` 的 `Path` | 见 §2.6。关键是**读出什么类型就写回什么类型**，否则会把用户的 `REG_EXPAND_SZ` 压成 `REG_SZ` |
| Windows 图形入口：**未实现** → 原生 COM 建开始菜单 `.lnk` | 见 §2.7。刻意**不起 PowerShell 子进程** |
| Linux 图形入口：已写未验证 → 已在 ubuntu runner 上验证 | 见 §2.13。测试里会调 `desktop-file-validate` |
| 打包：`tools/fixture/make.sh` 那套 shell → **`gpm pack` 子命令** | 见 §2.15。Windows 的 Git Bash 连 `zip` 都没有，`sha256sum` 也不保证有，而 .app 里的符号链接普通 zip 会展开——这三件事在 Go 里一次写完，三平台一致 |
| PATH 往返保真：**装一次再卸一次必须逐字节还原** | 见 §2.6。Windows 的 PATH 里空条目是有含义的（表示"当前目录"），顺手 Trim 掉就等于改了用户的语义 |
| CI：无 → gpm-go 三平台测试 + 六平台交叉编译；ai-desk 三平台打包 | 见 §2.16。本机没有容器与虚拟机，多平台验证只能这么做 |

**作废**：D1–D5（清单来源/落地方式/默认范围/网络/交付物）、D10 的镜像部分、D11（离线模式）、D12、D13 的 GUI 部分、D14 的"默认清单"、D16 的多应用 Profile。**保留**：D7（PATH 集成）、D8（账本/卸载）、D9（不提权）、D15–D18（启动器/应用形态）。逐条对照见 §7。

### 0.3.2 v3.1 → v3.2 变更记录

| 变了什么 | 为什么 |
|---|---|
| 覆盖安装 / 卸载：**闷头就删** → **先查应用在不在跑**（D31） | 见 §2.9。跑着的进程不会因为你删了它的文件就退出，它会抓着一份 unlinked 的旧代码继续跑，而按路径读到的资源/动态库/sidecar 已经是新的——**版本混用**。这条不是推演出来的，是真机上跑出来的（§3） |
| `--force` 选项 | 见 §2.14。拦截必须能被绕过：脚本化批量处理、CI、以及"我就是想现在换掉它"都是正当需求。默认拦、显式放行，与 PATH 集成那道许可（D26）是同一个姿势 |
| 新增 `internal/proc`（三平台） | 见 §2.9。macOS 走 `pgrep -f` 并锚定 argv[0]；Linux 读 `/proc/<pid>/{exe,cmdline}`；Windows 走 Toolhelp32 + `QueryFullProcessImageNameW`（原生 API，**不起 `tasklist`/`wmic` 子进程**，与 §2.7 的 COM 决定同理） |
| **查不出来时不拦** | "查不到"不等于"在跑"。把误判当成拦路理由，会把所有脚本化安装都挡死；宁可漏拦（打印一行提示），不可误拦 |

### 0.3.3 v3.2 → v3.3 变更记录

| 变了什么 | 为什么 |
|---|---|
| 名字：`cpi`（Cross Platform Installer）→ **`gpm`（GUI Package Manager）** | 见 §0.5。名字应当自证：它装的是带 GUI 的应用，做的是"装/卸/列/查/打包"，**不做抓取与升级**——这正是 `dpkg` 与 `apt` 的分工。`cpi` 只说了"跨平台"，没说装什么 |
| 模块路径 `github.com/qiuzhanghua/cpi-go` → **`github.com/qiuzhanghua/gpm-go`**；`cmd/cpi` → `cmd/gpm`；`CPI_HOME` → `GPM_HOME`；`--cpi` → `--gpm` | 全仓 277 处命中、21 个文件。词形普查确认过没有假阳性（不存在 `scpi`/`recipient` 这类词），所以三条替换（`cpi`/`CPI`/`Cpi`）就够了 |
| 安装根：硬编码 `~/ad` → **`--dir` > `$GPM_HOME` > 当前目录**；新增 **`gpm pack --default-dir`** 把这个值烘进生成的 `install.sh`/`install.cmd` | 见 §2.1、§2.15。`~/ad` 是 AI Desk 的缩写，把它写死在通用工具里，等于让每个用户都继承这个项目的私事。装到哪儿是打包方的决定，gpm 只负责执行。**（v3.4 又在 `$GPM_HOME` 与"当前目录"之间补了一档，见 §0.3.4）** |
| `<bin>/gpm`：**无条件覆盖** → **已经有一个就不装、不覆盖**，且不计入账本（卸载时也就不删它） | 见 §2.4。原来 `installSelf` 无条件执行，而全仓没有任何版本比较——拿旧包安装就会把新版 gpm 悄悄降级。用户自己放在那儿的 gpm 更不该被我们删掉 |
| 范围口径：**"单应用安装器"** → **"支持多包，一次装一个"** | 见 A3。代码从来就是按多包写的，文档却一直写着"一次只装一个"——v3.3 把文档改成代码的样子 |

### 0.3.4 v3.3 → v3.4 变更记录

| 变了什么 | 为什么 |
|---|---|
| 安装根优先级：`--dir` > `$GPM_HOME` > 当前目录 → **`--dir` > `$GPM_HOME` > 从 gpm 自己的位置推断 > 当前目录** | 见 D21、§2.3。v3.3 把 `~/ad` 删掉之后留了个洞：装完在新终端里敲 `gpm list` 会落到当前目录报"这里还没装东西"（真机实测，O11）。用户给的布局原则把它一句话解决了 |
| 新增 `home.RootFromSelf()` | 布局规定带 GUI 的应用在 `<家目录>/lib`、非图形界面的小东西（gpm、终端启动器）在 `<家目录>/bin`，于是 `<家目录>/bin/gpm` 自证家目录。三条判据（所在目录叫 `bin`、文件名是 `gpm`、上一级有账本 `state.json`）拦住 `/usr/local/bin/gpm` 这类"看着像但不是"的位置 |
| O11 关闭 | 不用 ⓑ（往 shell 里 `export GPM_HOME`）也不用 ⓒ（另存一处状态）——从自己的位置推断**不引入任何新状态**，与 D21"gpm 不发明默认值"不冲突：它不是发明，是**看出来** |

### 0.3.5 v3.4 → v3.5 变更记录

| 变了什么 | 为什么 |
|---|---|
| 安装根：自造的 `$GPM_HOME` → **工具链自己的家**（`$COT_HOME` / `$TDP_HOME`，默认 `~/cot` / `~/tdp`）；优先级 `--dir` > 按 `requires` 取的家 > `RootFromSelf()` > 当前目录 | 见 D21、§2.1、§2.3。用户裁决："不管 `~/ad`，统一用 COT_HOME/TDP_HOME 就好，不用 GPM_HOME"。GUI 应用落在工具链的 `lib/` 下正合布局：本机 `~/cot/lib` 的 49 个目录里已有 42 个是 `<id>_<version>_<os>_<arch>`，两套东西本来就该共用一个 `bin/` 与一条 PATH |
| 清单文件名 `manifest.yaml` → **`<简称>-manifest.yaml`**；新增 **`requires:`**（`cot` / `tdp`，缺省空）；zip 里新增 `tools/<os>_<arch>/` | 见 D22、D32、§2.4、§2.15。用户要的"具体由配置文件决定"就落在这个字段上：一个包要不要捎带工具链，是打包方写在清单里的决定，gpm 不猜 |
| 范围：**不做 CLI 工具与语言运行时安装** → **不做下载，但编排 zip 自带工具链的自举** | 见 D19、D32、§1.5。这是 v1 那条非目标第一次松缝：gpm 仍然没有一行网络代码（C3 证明 `cot i -s` 全程离线），它只是把 zip 里那个 cot 跑起来；"装哪些工具"仍由 cot 自己决定，干净机器上 `~/cot/lib` 保持空着（用户裁决） |
| macOS 终端启动器：`exec open '<…>.app' --args "$@"` → **注入环境后直接 `exec '<…>.app/Contents/MacOS/<binary>' "$@"`；只在 `requires` 非空（有环境要注入）时改道，没有 `requires` 的包照旧 `open`** | 见 D33、§2.8、C1。实测：GUI 应用由 launchd 启动，环境只有 15 项基线，`open --env K=V` 四种写法**全不生效**。代价是丢掉 LaunchServices 语义（双击等价、单实例激活、Dock 行为），换来"终端里启动的应用看得见 `COT_HOME`"；要两全得等合成 `.app` 外壳（R8、R13） |
| 归属边界写进决策（D34） | 见 §2.9、F9。同一个家里有两种东西：gpm 装的 GUI 应用与 cot 装的工具链。账本只能回放自己写下的东西——否则"卸载一个 GUI 应用"会把用户的 go / java 一起删掉 |
| 启动器重名检查：**已知缺口**（R4）→ **需求**（FR-21） | 见 §2.8。工具链的家与用户自己的 `bin/` 合流之后，`<家>/bin` 里撞名的机会变多，静默覆盖的代价变大 |
| `~/ad`：AI Desk 的默认根 → **普通用户数据，文档与默认值不再引用**；本机已用旧 gpm 的账本回放清理（§3） | 见 §2.17。它是改名前的遗留：`--default-dir` 该烘的是工具链的家（`~/cot`），不是某个应用的缩写 |

**未定**：工具链"已装好"怎么判、失败回滚到哪一步、账本与 cot 共处一室的加锁——三条还在 [`REQUIREMENTS.md`](REQUIREMENTS.md) §7，本文以 O14–O16 跟踪。**已定**（用户裁决）：`requires` 为空时用简称、缺省装到平台数据目录（D21，O12 关闭）；`ac` 只是举例、不单独裁决，重名一律按 FR-21 拒绝（O13 关闭）。

### 0.4 待确认假设

| # | 假设 | 影响 |
|---|---|---|
| A1 | **~~Windows 侧的 PATH 集成与开始菜单 `.lnk` 尚未实现~~** —— v3.1 已实现并在真 Windows runner 上验证（§2.6、§2.7、§2.13） | 已消解 |
| A5 | **gpm 在 Windows 的 PATH 上只摘除自己加的那一条**，其余（含结尾的空条目）逐字节保持原样 | 这条现在有 `TestWindowsPathRoundTripIsExact` 与真注册表往返测试守着（§2.6） |
| A2 | 没有 `SHA256SUMS` 时只警告不拒绝 | 供应链可信度换可用性；如需更严，可要求清单里给 `sha256` 并强制比对 |
| A3 | **一次装一个 `id`，但账本是多包的**：同一个 `<家>` 下可以并排装多个不同 `id` 的应用，各占自己的 `lib/<id>_<version>_<os>_<arch>` 与 `bin/<cmd>`；同一个 `id` 仍然是单版本覆盖式（D30）。 | 代码本来就是这么写的：`ledger.Packages` 是列表，`Put`/`Find`/`Remove` 都按 id 走，删 `bin/gpm` 的条件是"账本空了"——文档过去写着"一次只装一个"，是文档偏离了代码，v3.3 把口径改成代码的样子 |
| A4 | 图标暂不处理 | 合成 `.app` 外壳时没有 `Resources/`、`.desktop` 里没有 `Icon=`；上游给了 `.icns`/`.ico` 时才补 |
| A6 | 干净机器上工具链的 `lib/` 是空的：gpm 只把 `cot` 自己（+ activate 框架）放好，插件由用户自己 `cot install` | 见 §1.5、D32。用户裁决"`~/cot/lib` 让它空着，不用多安装其他插件"——所以 zip 只大 4 MB（cot）/ 7 MB（tdp），也不背"哪些插件该有"这个判断 |
| A7 | macOS v1 只保证**命令行启动器**这条路径注入环境；Finder / 启动台双击启动的实例拿不到 | 见 D33、§2.8。要两全得等合成 `.app` 外壳（R8）；这是 v1 明说的缺口，不是遗漏 |

---

### 0.5 关于这个名字：为什么是 gpm

**gpm = GUI Package Manager。** 这个名字要说清三件事：

1. **装的是带 GUI 的应用。** 纯命令行工具与语言运行时的**下载**不在范围内（§1.5）。v3.5 起它可以把 zip 自带的 cot / tdp 搬进家（D32），但那装的是工具链的"壳"而不是它的插件——干净机器上 `~/cot/lib` 依旧是空的。这是与 `dpkg`/`apt` 这类"什么都能装"的工具最直观的区别，也是当初把它从 `cpi`（Cross Platform Installer）改名的原因——`cpi` 只说了"跨平台"，没说装什么。
2. **它是 `dpkg`，不是 `apt`。** 成熟的分发体系里，"本机这些包"与"从哪弄到新版本"是被刻意分开的两件事：`dpkg` 会装、会卸、会列、会查，**不会去网上找新版本**；`apt` 负责抓取与升级，然后调 `dpkg` 干活。gpm 只做前一半——它**没有 `upgrade` 子命令，也没有一行网络代码**（`grep -rn 'net/http\|http.Get\|net/url' --include='*.go' .` 无命中，见 §2.13）。
3. **叫 "manager" 不算撒谎，前提是别把 `apt` 的活说成自己的。** 大家嘴上管 `dpkg` 叫 package manager，它确实在管理本机的包（安装、卸载、账本、卸载回放）。gpm 同理。缺的那半不是"以后补上"，而是**明确不打算做**：分发者自己决定什么时候出新版本、用户自己把 zip 拿过来，是这套设计的前提（§1.2、§7 R1）。

> 取名时考虑过「分发工具 distribution tool」（盖住 pack + install，但不暗示升级）。最后选了 gpm：更短、更好念，而且和 `~/Applications` 里那些东西是同一类。

**它与 Tauri 的关系**：gpm 不是 Tauri 的插件、包装或管理器，而是 **Tauri 自带分发体系（`.dmg` / NSIS `.exe` / AppImage，外加 `tauri-plugin-updater`）的替代品**——§2.12 讲为什么 gpm 不吃那三种安装器。用 gpm 打包一个 Tauri 程序的完整流程、以及装完之后那些东西是怎么被调用的，见 §2.17。

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
| S3 | 装到另一个目录（U 盘、`D:\ad`） | `--dir` / `$COT_HOME` 一改就行，其余行为不变 |
| S4 | 装到一半失败 | 没有半成品残留，可以重跑 |
| S5 | 不想要了 | `gpm uninstall <id>` 之后，目录、图标、终端命令、PATH 条目全部消失 |
| S6 | 无人值守（CI / 内网） | `gpm install ./x.zip --yes` 不提问、不交互 |
| S7 | 用户不想被改 shell 配置 | 拒绝之后应用照样装好、图标照样能点，只是终端里敲不出来 |

### 1.3 功能需求

| # | 需求 |
|---|---|
| FR-1 | 接受一个目录或一个 zip 作为输入 |
| FR-2 | 从 `<简称>-manifest.yaml` 里读出 `id` / `name` / `version` / `entry` / `launch`，并校验 |
| FR-3 | 按当前 `GOOS`/`GOARCH` 选择入口；清单没有当前平台的入口时明确报错 |
| FR-4 | 用 `SHA256SUMS` 校验 `payload/` 下每个文件；不一致拒绝安装 |
| FR-5 | 把 `payload/` 整体落到 `<家>/lib/<id>_<version>_<os>_<arch>/` |
| FR-6 | 在 `<家>/bin/<cmd>` 生成终端启动器（`0755`），参数逐个透传 |
| FR-7 | 建立图形入口：macOS `~/Applications/<Name>.app`、Linux `~/.local/share/applications/<id>.desktop` |
| FR-8 | 把 `<家>/bin` 挂到 PATH 上；写之前请求许可，写入幂等、可撤销 |
| FR-9 | 把 gpm 自己复制到 `<家>/bin/gpm`；**那儿已经有一个 gpm 就原样留着**，并在收尾时说明这次没覆盖它 |
| FR-10 | 所有外部副作用记入 `state.json` |
| FR-11 | `list` / `where` / `uninstall` 能读出并回放账本 |
| FR-12 | 失败时按逆序回滚，不留半成品 |
| FR-13 | 重装同一个 `id` 时覆盖式替换（先删旧、再装新） |
| FR-14 | 非交互环境（无 TTY）不提问，直接跳过 PATH 集成并打印手工命令 |
| FR-15 | 发布者侧：`gpm pack <装配目录>` 就地补齐 `install.sh`/`install.cmd`/`SHA256SUMS` 与 gpm 二进制并打成 zip（§2.15）；`--default-dir PATH` 把这个包该装到哪儿**烘进**那两个脚本 |
| FR-16 | 覆盖安装或卸载某个 `id` **之前**先查它是否正在运行；正在运行就用人话说明后果并拒绝，除非给了 `--force`（§2.9） |
| FR-17 | 查不出来（平台不支持、或没有权限看进程）时只打印一行提示并**继续**——"查不到"不等于"在跑"，不能因此把安装拦下来 |
| FR-18 | 清单可以声明 `requires: [cot]` / `[tdp]`；安装时先跑工具链自举（FR-19）再装应用；`--with cot[,tdp]` 只作覆盖 |
| FR-19 | 自举只用 zip 自带的 `tools/<os>_<arch>/{cot,tdp}`：`cot i -s "${COT_HOME:-$HOME/cot}"`（tdp 按 `$TDP_HOME`），全程不联网、不查版本、不代下载 |
| FR-20 | 工具链步骤失败就**不继续**：整体非 0 退出，并按 rollback 栈撤销本次已做的一切（不写 rc 块、不建启动器、不建软链、不记账本） |
| FR-21 | 生成 `<家>/bin/<cmd>` 之前必须检查该名字是否已被非 gpm 的文件占用，占用则报错、不覆盖（补 R4） |
| FR-22 | macOS 的终端启动器必须把工具链环境注入给应用进程（至少 `COT_HOME` 与 `PATH`），不再走 `open`（D33） |

### 1.4 非功能需求

| # | 需求 |
|---|---|
| NFR-1 | 全程不提权；不需要管理员 |
| NFR-2 | 中断安全：任何时刻断电/杀进程，重启后 `<家>` 不处于半成品状态 |
| NFR-3 | 卸载后除"用户自己放进去的东西"外无残留；gpm 自己创建的空文件也一并删掉 |
| NFR-4 | 幂等：同一个包连装多次，结果一致（PATH 块不叠加） |
| NFR-5 | 只依赖 Go 标准库 + `gopkg.in/yaml.v3` |
| NFR-6 | 不修改被装应用自身的配置，不往用户可见的文档目录写文件 |
| NFR-7 | 报错必须给出**可执行的下一步**（哪个文件、哪个字段、哪个 URL） |

### 1.5 非目标

- 不做纯命令行工具与语言运行时的**下载**（`go` / `node` / `rg` / `fzf` … 当初 v1 的目标，现已出局）。**v3.5 松了一条缝**：install.sh 可以跑 zip 自带的 `cot i -s <家>` 把 cot 自己连同 activate 框架装好，但那是**离线搬运**、不是下载，装完 `lib/` 里一个插件都没有（D32、C4）。
- 不做字体、配置、数据等独立资源包。
- 不做应用商店式的多应用目录、镜像表、版本发现、静默升级。
- 不做 `.dmg` / NSIS / `.deb` / `.rpm` / AppImage **安装器**的驱动（§2.12）。
- 首版不做图形界面。
- **不做"关掉正在运行的应用"**：gpm 只查、只说、只拒绝（或按 `--force` 放行），**绝不替用户 kill**。杀别人的进程比换掉它的文件更越界。

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
| C8 | macOS 上**枚举进程不能依赖 `/bin/ps`**：它是 setuid root（`-rwsr-xr-x root wheel`），在受限/沙箱化的宿主里 exec 直接被拒（`Operation not permitted`）。`/usr/bin/pgrep`、`/usr/bin/lsof`、`/usr/bin/lsappinfo` 都不是 setuid，可以放心用 |
| C9 | **删掉一个正在运行的进程脚下的文件，它不会退出**。进程抓着 inode 继续跑，`/proc/<pid>/exe`（Linux）或 `lsof` 的 `txt` 行（macOS）会显示一个已经不存在的路径、或带 `" (deleted)"` 后缀——所以"这个路径还在不在"不能拿来判断进程活没活，判断必须**纯字符串比对**，绝不能 `stat` |

---

## 2. 设计

### 2.1 总体架构

```
        用户视角                          内核视角
  ┌────────────────────┐
  │ 解压 zip           │
  │  ├ install.sh      │──┐
  │  ├ install.cmd     │  │  只做三行 bootstrap：
  │  ├ gpm             │  │  cd "$(dirname "$0")"
  │  ├ ad-manifest.yaml│  │  chmod +x ./gpm
  │  ├ payload/        │  └─ exec ./gpm install . --dir "${COT_HOME:-…}"
  │  ├ tools/          │  （requires 非空时才有）
  │  └ SHA256SUMS      │
  └────────────────────┘
                                    │
                       ┌────────────▼─────────────┐
                       │  gpm（同一份内核）        │
                       │  home → manifest → stage  │
                       │  → install → integrate    │
                       │  → ledger                 │
                       └────────────┬─────────────┘
                                    │
                    <家>/{bin,lib,staging,state.json}   （家 = $COT_HOME，默认 ~/cot）
                    ~/Applications/AI Desk.app
                    ~/.zprofile / .zshrc / .profile
```

那个 `…` 就是 `gpm pack --default-dir` 烘进来的家（AI Desk 从 v3.5 起烘的是 `~` 展开后的 `$HOME/cot`）；打包方没指定时它就是 `.`，也就是"解压出来那个目录"。**gpm 只在"没有工具链的家可依附"时才自己挑一个落脚点**——它认 `--dir` > 按 `requires` 取的那个家（`$COT_HOME` / `$TDP_HOME`）> 从自己的位置推断 > 平台数据目录/<简称> > 当前目录这个顺序（§2.3）。

**装完之后还有一档**：`install.sh` 只在装的那一次把 `--dir` 传进来，之后用户在新终端里敲 `gpm list` 时 `$COT_HOME` 并不在环境里（除非他先 source 过 activate）。于是 gpm 看一眼**自己现在在哪儿**——`<家>/bin/gpm` 这个位置本身就说明了家目录在哪（`home.RootFromSelf()`，§2.3）。

一条铁律：**`internal/` 不知道 GUI 存在**。将来加图形壳（若加）时，它和 `cmd/gpm` 走同一条 `install.Install`。

**这条铁律在 v3.x 被一个真实用户需求印证了**：他描述这套东西的用法是"一方面方便在后面打包应用程序，另一方面可以在前端被安装程序调用"——那个"前端安装程序"就是 `gpm pack` 生成的 `install.sh`/`install.cmd`，它和 `cmd/gpm` 一样只是 `install.Install` 的一个壳（§2.17）。

### 2.2 包布局

```
cmd/gpm/            CLI 入口：参数解析、子命令分发、中文输出
internal/home/      家的解析（--dir > 按 requires 取的家 > 从自己的位置推断 > 平台数据目录/<简称> > 当前目录）与各子目录
internal/manifest/  <简称>-manifest.yaml 的解析与平台校验（含 requires）
internal/stage/     解包（目录或 zip）与 sha256 校验
internal/pack/      反向：把装配目录打成可分发的 zip（install.sh/install.cmd/gpm/清单/SHA256SUMS/tools/）
internal/integrate/ 外部副作用：终端启动器、图形入口、PATH
internal/ledger/    state.json 的读写
internal/proc/      查某个包目录底下有没有活着的进程（§2.9.1）
internal/install/   编排 + 回滚 + 卸载回放 + 工具链自举（§2.5.1）
tools/fixture/      造测试分发包的脚本（不属于产品；产品路径已由 gpm pack 取代）
```

`internal/proc/` 内部同样按平台拆文件：`proc.go`（路径前缀比对的纯函数，全平台）、`find_darwin.go`（`pgrep -f`）、`find_linux.go`（`/proc`）、`find_windows.go`（Toolhelp32）、`find_other.go`（返回"还不支持"，调用方打印提示后继续）。

`internal/integrate/` 内部按平台拆文件：`integrate.go`（启动器与 PATH 标记块，全平台）、`entry.go`（图形入口路径与 `.desktop` 内容，纯函数）、`pathwin.go`（Windows PATH 的字符串处理，纯函数、可测）、`registry_windows.go` + `registry_stub.go`（注册表读写）、`shortcut_windows.go` + `shortcut_stub.go`（`.lnk`）。**平台无关的部分一律做成纯函数**，这样 Windows 的逻辑也能在 macOS 上被测试覆盖。

依赖方向是单向的：`install` → {`home`, `manifest`, `stage`, `integrate`, `ledger`, `proc`}，`integrate` → `ledger`，`pack` → {`manifest`, `stage`}，其余互相不依赖。

### 2.3 磁盘布局

```
<家>                            装到哪儿由安装器传进来（D21）；默认 $COT_HOME（~/cot）或 $TDP_HOME（~/tdp）
├── bin/                        没有图形界面的小东西
│   ├── gpm                     gpm 自己的副本（D29）
│   ├── cot / tdp               工具链自举的产物（D32；gpm 不记账、卸载不碰，D34）
│   ├── activate* / env-cot*    cot 的 activate 框架，同上
│   └── <cmd>                   终端启动器（D25）
├── lib/                        带 GUI 的应用 + 工具链自己的插件
│   ├── <id>_<version>_<os>_<arch>/
│   │   └── <Name>.app          仅 macOS：上游给的或 gpm 合成的（账本管这一支）
│   └── go_1.27.1_darwin_arm64/ …       cot 装的插件（账本不管，v3.5）
├── staging/                    解包中转；每次安装一个 unpack-<纳秒> 目录，defer 删除
└── state.json                  账本
```

- **`bin/` 与 `lib/` 的分界是"有没有图形界面"，不是"是不是可执行文件"**：带 GUI 的应用住在 `lib/<id>_<version>_<os>_<arch>/` 里（用户指定的目录之下），没有图形界面的小东西——终端启动器与 gpm 自己——住在 `bin/` 下。这条分界不只是整洁，它还是 **`RootFromSelf()` 的依据**（见下）。
- **`RootFromSelf()`：gpm 从"自己在哪儿"反推家目录**（v3.4）。`install.sh` 只在安装那一次把 `--dir` 传进来；之后用户在新终端里敲 `gpm list` 时 `$COT_HOME` 并不在环境里（工具链没被激活过），于是落到"当前目录"——`gpm list` 会报"这里还没装东西"（O11，真机实测过）。可既然布局把 gpm 自己固定放在 `<家>/bin/gpm`，**这个位置本身就在说家目录是它的上一级**。判据三条，缺一不可：① 可执行文件所在目录正好叫 `bin`；② 文件名正好是 `gpm`（Windows 上 `gpm.exe`）；③ 上一级里有账本 `state.json`。第三条是专门用来挡住 `/usr/local/bin/gpm` 这类"恰好也叫 bin、但上一级不是 gpm 的家"的位置的。
- **`requires` 为空时才有"gpm 自己挑一个家"这回事**（v3.5，用户裁决）：没有工具链的家可依附，就落到**平台数据目录 + 简称**——macOS `~/Library/Application Support/<简称>`、Windows `%LOCALAPPDATA%\<简称>`、Linux `${XDG_DATA_HOME:-~/.local/share}/<简称>`。这个目录是 gpm 自己建的，所以只有"第一次装、且装失败"时才把它收回去（`home.DropIfEmpty()`，把建出来的空骨架逐个删掉）；装成功之后它就和别的家一样——卸载只按账本回放，不删整个目录。
- **这不违反 D21。** D21 里"不发明默认值"说的是**有工具链的家可依附时**（`$COT_HOME` / `$TDP_HOME`，缺省 `~/cot` / `~/tdp`）gpm 不另造一个 `GPM_HOME`；从自己的位置推断也不是发明，是**看出来**：它不引入任何新状态（不像把根另存一个文件），也不要求 shell 帮忙记着（不像往 PATH 标记块里塞 `export COT_HOME`）。真正决定装到哪儿的仍然是安装器，推断只在"用户装完之后随手敲 gpm"这个场景里替他把根找回来。
- **只有"一份真正的家"会被推断出来**：包里那个 `./gpm` 解压在任意目录（不在 `bin/` 下）时推断不出来，也不该推断——它的活是照着 `--dir` 干活。
- **没有 `store/`**：v2 的内容寻址存储在单应用场景下是过度设计（§0.3）。
- **`staging/` 必须在 `<家>` 之下**：只有同一文件系统内的 `rename` 才是原子的。
- `state.json` 用 **临时文件 + rename** 写入，保证不会读到写坏的账本。
- 这个家**不是 gpm 独占的**（v3.5）：`bin/` 与 `lib/` 里还会有 cot / tdp 自己放的东西（D34）。所以卸载**只按账本回放**，绝不按"目录里多了什么"去猜。

### 2.4 清单模型

文件名是 **`<简称>-manifest.yaml`**（`ad-manifest.yaml`、`sag-manifest.yaml`；v3.5 起，简称就是 `launch.cmd`）。

```yaml
id: ai-desk            # 必填，^[a-z0-9][a-z0-9._-]*$
name: AI Desk          # 必填，显示名
version: 1.0.0         # 必填，^[A-Za-z0-9][A-Za-z0-9._+-]*$
requires: [cot]        # 选填，缺省 []；取值 cot | tdp，决定安装根（D21）与自举（D32）
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
3. `launch.cmd` 缺省等于**简称**（清单文件名里那一段）；两者不一致就报错，安装前就拦住。
4. `requires` 缺省空 = 不装工具链、也不改 PATH；声明了就从 zip 的 `tools/<os>_<arch>/` 取对应可执行文件（缺了是**打包错误**，不是运行时静默跳过）。

**只校验当前平台**：清单可以带好几个平台，gpm 只看自己跑在哪个上；当前平台没有对应条目就报错退出。

### 2.5 安装流水线

```
0. 先读清单（目录里的或 zip 根层的 <简称>-manifest.yaml）拿到简称与 requires  ← manifest.Peek
0.2 定家：--dir > 按 requires 取的家 > 从自己的位置推断 > 平台数据目录/<简称> > 当前目录（§2.3）
    └ 同时记一笔"这个家原来存不存在"：本次建出来的空骨架，装不成就收回去
0.4 建好骨架 bin/ lib/ staging/                              ← home.Ensure
1. 解包到 <家>/staging/unpack-<纳秒>              ← stage.Materialize（目录直接拷，zip 带越界防护）
2. 校验 payload/ 与 tools/ 下每个文件                   ← stage.VerifySums
   └ 没有 SHA256SUMS → 警告 + verified=false，继续
   └ 对不上         → 报出「清单 <a>，实际 <b>」，拒绝
3. 读并校验 <简称>-manifest.yaml                       ← manifest.Load + Validate(goos)
3.5 启动器重名检查（FR-21）                 ← integrate.OwnedByGpm + ledger.FindByCmd
    └ 名字是 gpm 自己用的 / 属于别的包 / 是别人放的文件 → 拒绝，只有 --force 放行
3.6 工具链自举（requires 非空时，§2.5.1）          ← install.installToolchains
    └ 失败 → 不继续：逆序回滚，连刚建出来的空家也收回去
4. payload/ 整体拷进 lib/<id>_<ver>_<os>_<arch>/        ← stage.CopyTree（保留权限位与符号链接）
   ├ 同 id 已存在 → 先查那个应用在不在跑（D31）          ← proc.Find（纯字符串比对，绝不 stat）
   │  ├ 在跑且没给 --force → 说明后果后拒绝，就此打住（一个字都还没动）
   │  └ 放行后按卸载流程删旧的（D30）
   └ 拷进去
5. 生成 bin/<cmd>                                      ← integrate.Launcher
6. 建立图形入口                                        ← integrate.AppLink
7. 请求许可后把 bin/ 接进 PATH                          ← integrate.InstallPathBlock
   ├ POSIX   → 写 shell 配置的标记块
   └ Windows → integrate.InstallWindowsPath（改 HKCU\Environment 后广播）
8. 把 gpm 复制到 bin/gpm                               ← installSelf
9. 写 state.json                                       ← ledger.Save
```

每一步成功都把逆操作压进 rollback 栈；任一步失败就**逆序执行**已压入的动作，然后返回错误。第 4 步之后的失败会把整个 `lib/<id>_…/` 目录一起删掉。

**回滚栈与"先登记后执行"的分工**：账本（第 9 步）是**卸载**的依据，rollback 栈是**本次安装失败**的依据。两者不可互相替代——账本写下去的时候，安装已经成功了。

**工具链自举（3.6）留下的东西不进 rollback 栈**（D34）：那是 `cot` 自己写的，gpm 只做两件事——它失败时不继续往下走，以及本次若是自己新建的空家、就把那个空骨架收回去。已经存在的家里被 cot 改了什么是 cot 的事，gpm 不猜也不删。

#### 2.5.1 工具链自举（v3.5）

清单里声明了 `requires: [cot]`（或 `tdp`）时，流水线第 0.5 步做这些事，**全程离线**：

| 步 | 动作 |
|---|---|
| 1 | 从 `<包>/tools/<os>_<arch>/cot` 取 zip 自带的那份可执行文件（Windows 上是 `cot.exe`）；没有就报错——这是**打包错误** |
| 2 | 目标家（第 0.2 步已经定好）= `--dir` > `$COT_HOME`（tdp 则 `$TDP_HOME`）> 平台数据目录/<简称> |
| 3 | 跑 `cot i -s "<目标家>"`（Windows：`cot.exe install -s "%COT_HOME%"`），stdout / stderr 原样透传给用户 |
| 4 | 非 0 退出 → **不继续**：按 rollback 栈撤销本次已做的一切，返回错误（FR-20） |

- **为什么由 install.sh 调、不写进 gpm 内核**：解析工具链、决定装哪些插件是 cot 的活（D32）。gpm 只负责"把 zip 里那个东西跑起来，跑失败就别往下走"。
- **"已经装好了"不需要 gpm 发明判据**：`cot i -s` 的语义（见 §1.6 C3/C4）是目录不存在就建目录 + `install_self`，已存在就走 `do_upgrade` 做版本比较、相同或更新即打印一句并**以 0 退出**。所以重复安装不叠加，两个包都声明 `requires: [cot]` 也不会打两次架。
- **不进账本**：自举产生的一切（`bin/cot`、`env-cot*`、`lib/` 下的插件）都不登记，`gpm uninstall` 一律不碰（D34）。
- **PATH 只需要一条**：工具链的家就是 GUI 应用的家，`<家>/bin` 接上去之后 `cot`、`tdp` 与 `<简称>` 一起到位（D26）。

### 2.6 PATH 集成

`<家>/bin` 存在的**唯一理由**就是本需求的核心：让装完的应用在终端里敲得动。

| 平台 | 机制 | 生效时机 |
|---|---|---|
| Windows | 写 **`HKCU\Environment` 的 `Path`**（无需管理员）后广播 `WM_SETTINGCHANGE`；PowerShell 无需额外配置 | 新进程 |
| macOS | **按 `$SHELL`**：zsh → `~/.zprofile`（Terminal.app 每开新窗口都是登录 shell）**并同时写** `~/.zshrc`（非登录的交互 shell）；bash → `~/.bash_profile`；fish → `~/.config/fish/config.fish`。**不论 `$SHELL` 都再写一份 `~/.profile`** 兜底 | 新开终端 |
| Linux | 同 macOS 规则（bash 落 `~/.bashrc`） | 新开终端 |

> **为什么不能只写 `~/.profile`**：macOS 自 Catalina 起默认 shell 是 zsh，而 **zsh 不读 `~/.profile`**（只在被当作 `sh`/`ksh` 调用时才读）。只写 `.profile` 的后果是：软件装好了、图标也在，但新开终端敲 `ad` 依然 `command not found`——而且用 bash 自测时还发现不了。

写入的是一段**幂等守卫**，重复 source 不会叠加 PATH，同一段写进多个文件也安全：

```sh
# >>> gpm >>>
case ":$PATH:" in
  *":$HOME/cot/bin:"*) ;;
  *) PATH="$HOME/cot/bin:$PATH" ;;
esac
export PATH
# <<< gpm <<<
```

- 上面这个例子里家是 `$HOME/cot`（AI Desk 用 `--default-dir "~/cot"` 烘进脚本的值）；换一个家，块里的路径就跟着换。
- **只要 `<家>` 在 `$HOME` 之下，写进文件时就相对化成 `$HOME/…`**（`internal/integrate/integrate.go` 里那句 `"$HOME/" + strings.TrimPrefix(p, home+"/")`），于是整个家目录搬走、或者用户名变了，PATH 条目都还成立；不在 `$HOME` 之下则老老实实写绝对路径。
- 写入前**先摘掉旧块再追加**，所以重复安装不会累积。
- 已经在文件里的 gpm 块与"用户自己早就加过的等价条目"都能识别：前者被替换，后者只提示。
- **写之前必须用人话请求许可**，并逐个列出**实际**要改的文件名（这台机器上是 `~/.zprofile`、`~/.zshrc`、`~/.profile`）。拒绝 → 软件照装、图标照建，只打印一行手工命令，**功能降级而非失败**。
- 非交互环境（管道、CI）不提问，直接跳过。**注意 `isTerminal` 不能只看 `os.ModeCharDevice`**：`/dev/null` 也是字符设备，所以从脚本或双击运行时走的是"提问后立刻 EOF"这条路——那种情况下要明说"没读到你的输入，想让命令能用就重跑一次并加 `--yes`"，不能假装用户回答了"不"。

- **工具链与 GUI 应用共用这一个块**（v3.5）：家就是 `$COT_HOME`，块里的路径是 `<家>/bin`，`cot` / `tdp` / `<简称>` / `gpm` 都在里面，所以不需要第二块（D26）；工具链的资产不进账本，但 PATH 块是 gpm 写的，仍按账本回放（D34）。

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
| Windows | 开始菜单 `.lnk` | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\gpm\<Name>.lnk` | 已实测（windows runner；写入后读回来做往返比对） |

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
| `bundleIdentifier` / `CFBundleName` | `(null)` / `(null)` | `com.example.gpm.exp` / `GpmExp` |
| 启动时 `NSApp.activationPolicy` | **2（Prohibited）**——得程序自己调 `setActivationPolicy:Regular` 才有 Dock 图标与菜单栏 | **0（Regular）** |
| 启动台/Spotlight 索引、"打开方式"、文件关联、URL scheme、TCC 权限归属、公证 | 全都没有 | 有 |

> 取证口径：表中"双击"一行的依据是 LaunchServices 对 `public.unix-executable` 的绑定与角色声明（`lsregister -dump` 里 `all roles:` 指向终端应用），**不是一次亲眼看到的双击**——实验环境里终端窗口没能被拉起。其余各行都是探针程序自报的身份字段，可直接复现。

**合成外壳的配方（手工实验已跑通，gpm 里尚未实现）**

> ⚠️ 现状：`manifest.Entry` 目前只有 `bundle` / `exe` 两个字段，**没有 `bin`**，也就是说
> "上游只发裸可执行文件"这条路 gpm 现在还走不了。下面的配方是一轮真机实验的结论，
> 已验证可行，等着落成代码（§2.13、§5 R8）。

```
<家>/lib/<id>_<ver>_darwin_<arch>/<Name>.app/Contents/
    Info.plist        # gpm 生成
    MacOS/<exe>       # 指向上游裸可执行文件：硬链接（首选）或拷贝
    Resources/        # 可选，清单给了图标才建
```

- `Info.plist` **最小只要 4 个键**就换来完整身份：`CFBundleExecutable`（必须等于 `MacOS/` 里的文件名）、`CFBundleIdentifier`、`CFBundleName`、`CFBundlePackageType` = `APPL`。
- `CFBundleIdentifier` 缺省 `<id>.gpm.local`——**刻意不冒用上游官方标识**，免得与用户真正装的官方应用在偏好、权限、通知归属上串味。
- **`Contents/MacOS/<exe>` 只能用硬链接或拷贝，绝不能用符号链接**（实测）：符号链接会让进程内的 `NSBundle.mainBundle` 解析到链接目标，于是"外面看着是应用、里面自己不认"——`bundlePath` 变成裸二进制所在目录，`bundleIdentifier` 与 `CFBundleName` 全成 `(null)`。硬链接与拷贝结果都正确；`lib/` 与上游文件同在 `<家>` 之下，所以**硬链接零拷贝、是首选**。
- **外壳没有独立签名**：`codesign -dv` 报出的是内部 Mach-O 的 ad-hoc 身份。所以"合成外壳的应用"与"上游自己发的 `.app`"在 Gatekeeper 面前**不同命**（§5 R2）。

### 2.8 终端启动器集成

GUI 应用默认**不会**在 PATH 里留下任何东西，所以这一步只能由 gpm 主动合成。

| 平台 | 落点 | 内容 |
|---|---|---|
| macOS | `<家>/bin/<cmd>`，`0755` | 有 `requires` 时先注入工具链环境（见下），再 `exec '<lib 绝对路径>/<X>.app/Contents/MacOS/<X>' "$@"`；没有 `requires` 要注入时 `activate` 照旧 `exec open '<…>.app' --args "$@"`（v3.5） |
| Windows | `<家>/bin/<cmd>.cmd` | `@echo off` + 先 `set` 工具链变量与 PATH + `"<lib 绝对路径>\<X>.exe" %*`（CRLF 换行） |
| Linux | `<家>/bin/<cmd>`，`0755` | 先注入工具链环境，再 `exec '<lib 绝对路径>/<X>.AppImage' "$@"` |

- **macOS 上"有工具链要注入"的包为什么不再用 `open`**（v3.5）：`open` 走 LaunchServices，**不给环境**（C1 实测：GUI 应用只有 15 项 launchd 基线，`open --env K=V` 四种写法全不生效）。要"终端里启动的应用看得见 `COT_HOME`"就只能直接 exec 内层可执行文件。代价是丢掉双击等价、单实例激活与 Dock 行为（R13）——要两全得等合成 `.app` 外壳（R8）。
- **Finder / 启动台双击不注入**：那条路径不经过启动器，走的是 LaunchServices（A7、X7）。
- **`activate` 与 `direct` 的分工在 v3.5 变了**：从前 `activate` 走 `open`（等价双击、能激活已运行实例），`direct` 直接 exec 内层可执行文件（换来 stdout、退出码与可杀的 `Ctrl+C`）。现在 macOS 上**声明了 `requires` 的包**两者都直接 exec（有环境要注入）；没有 `requires` 的包分工照旧——`activate` 仍旧 `open`。Linux / Windows 不受影响。
- **参数一律透传**（`"$@"` / `%*`）：否则 `ad movie.mp4` 这类最常见的用法直接失效。
- **为什么 Linux 用包装脚本而不是符号链接**：AppImage 依赖自身路径（`$APPIMAGE` / `argv[0]`）定位挂载点，软链在部分实现下会解析失败。
- **为什么 Windows 用 `.cmd` 而不是把 exe 硬链进 `bin/`**：Windows 的 exe 常按相对路径加载同目录的 DLL 与资源，搬离自己的目录就损坏；`.cmd` 没这个问题，且 `.CMD` 默认在 `PATHEXT` 里。
- **Windows 的固有行为**：GUI 子系统的 exe 从终端启动会立即返回、不输出、不阻塞（C7）。终端启动的价值在于"不用去开始菜单里翻"。

### 2.9 卸载与反向清理

卸载与"覆盖安装时先删旧的"走的是同一条回放路径。**动手之前先过一道闸**（§2.9.1）。

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
删 bin/gpm                                 ← 账本 self（仅当账本里已经没有别的包）
写回 state.json（去掉该包）
```

**验收标准（可测）**：`gpm list` 无该项；`command -v <cmd>` 找不到；`~/Applications` 下链接消失；`lib/` 下无同名目录；shell 配置文件里没有 gpm 标记块（Windows 上是注册表 PATH 里没有 `<家>\bin`）；如果那个文件是 gpm 创建出来的且已经空了，文件本身也不在。

`<家>` 目录**不删**——用户可能往里放了别的东西，删掉是越界。命令会明说"目录还在"。

**只回放自己写的东西**（v3.5、D34）：`bin/cot`、`bin/tdp`、`env-cot*`、`activate*` 与 `lib/` 下的插件都不在账本里，卸载一律不碰。这也是家目录不删的同一个理由——用户的东西与工具链的东西都住在里面。

#### 2.9.1 动手之前：应用在不在跑（D31）

**为什么必须要这一道闸。** 删掉一个正在运行的进程脚下的文件，它**不会退出**。进程抓着内核里的 inode 继续执行那份已经被 unlink 的旧代码，而此时同名路径下已经是新拷进去的另一份：

| 会发生什么 | 后果 |
|---|---|
| 覆盖安装：先 `RemoveAll` 再拷新的 | 跑着的进程执行**旧**代码，但按路径 `dlopen`、读 `NSBundle` 资源、拉起 sidecar 时拿到的是**新**文件——**版本混用**。这次实测没崩，只是因为 Tauri 的 release 构建把前端资源编进了二进制；有 sidecar 或动态库的应用中招概率大得多 |
| macOS 上更新 `.app` | 更糟：LaunchServices 把 `~/Applications/<Name>.app` 这条路径一直绑在那个旧 pid 上。**用户之后点图标只会把幽灵唤到前台，永远拿不到新版本**——除非手动 kill |
| 卸载 | 同样的混用，外加"用户以为已经卸干净了，其实还有个进程在跑" |

所以判断的依据是"**包目录底下有没有活着的进程**"，不是"文件在不在"。

**怎么查（逐个平台）。** 都只做**纯字符串比对**：把进程报出来的可执行路径（以及 argv[0]）与账本里的 `dir` 前缀比，**绝不 `stat`**——幽灵进程的路径早就不存在了，`stat` 一下就会把最该抓到的情况漏掉。前缀同时收"账本里原样那个路径"和"`EvalSymlinks` 解析后那个路径"两份（macOS 上 `/var/...` 与 `/private/var/...` 是同一个地方的两个写法，进程报哪一种都可能）。

| 平台 | 手段 | 注意 |
|---|---|---|
| macOS | `pgrep -f '^(<pkg>/|<pkg2>/…)'`，即**锚定在行首的 argv[0]** | 用 `pgrep` 而不是 `ps`：`/bin/ps` 是 setuid root，在受限环境里 exec 会被直接拒（C8）。`pgrep` 退出码 1 表示"没找到"，**不是**故障。锚定 `^` 是刻意的：另一个进程的命令行里"提到"这个路径不算命中（实测过），而幽灵进程的 argv[0] 仍是原路径，照样认得出 |
| Linux | 读 `/proc/<pid>/exe`（readlink）与 `/proc/<pid>/cmdline` 的 argv[0] | 两个候选都要收：AppImage 自解压后 `exe` 指向 `/tmp/.mount_xxxx`，只有 argv[0] 认得原位置 |
| Windows | Toolhelp32 快照 + `QueryFullProcessImageNameW` 取**完整路径** | 查完整路径而不是进程名（同名的 exe 可以有好几份）；与 §2.7 的 COM 决定同理，**不起 `tasklist`/`wmic` 子进程** |
| 其它 | 直接返回"这个平台还不支持查进程" | 调用方打印提示后继续（FR-17） |

**拦的时候说什么。** 不吓唬人、不甩术语，四句话说清"现在动它会怎样、你该做什么、不想听劝怎么办"：

```
<名字> 正在运行（进程 1844，/Users/q/ad/lib/ai-desk_0.2.0_darwin_arm64/…），现在动它，它脚下的文件会被换掉或抽走。
它自己不会马上退出，但之后读到的资源、动态库、拉起的子进程都可能是另一份（版本混用）；
在 macOS 上还会留下一个幽灵进程：那个位置一直指着这个旧进程，你下次点图标只会把它唤到前台，拿不到新的。
请先退出 <名字>，再重新运行一次 gpm install。
确定不在乎（比如在脚本里批量处理），加 --force。
```

**查不出来就不拦。** 平台不支持、没权限、命令不存在——一律只打印一行 `提示：没能确认 … 是不是正在运行（…），这次不拦。` 然后照常继续。宁可漏拦（用户自己会看到后果），不可误拦（那会把所有脚本化安装都挡死）。同理，**gpm 绝不替用户 kill**（§1.5）。

### 2.10 校验与供应链

| 情况 | 行为 |
|---|---|
| `SHA256SUMS` 存在且全部匹配 | 正常安装，账本 `verified: true` |
| 有文件对不上 | **拒绝**，报出「清单 `<期望>`，实际 `<实得>`」与文件名 |
| 有文件在 `payload/` 里但不在清单里 | 拒绝（清单必须覆盖每个常规文件） |
| 没有 `SHA256SUMS` 这个文件 | 警告后继续，账本 `verified: false`，`gpm list` 显示 `[unverified]` |
| `SHA256SUMS` 存在但是空的 / 格式不对 | **拒绝**（空的清单等于没校验，不能装作校验过了） |
| 符号链接 | 不参与校验（它是链接不是内容），但会被原样保留 |

**不做签名验证**：单应用场景下的信任来自"这个 zip 是从哪来的"，而不是 gpm 能验证什么。`SHA256SUMS` 防的是**传输损坏**与**误改**，不是防恶意——真正的防恶意手段是 §5 R1 的签名公证。

### 2.11 解包与路径安全

`stage.Materialize` 同时接受目录和 zip，两者都要过同一套路径检查：

- 拒绝绝对路径、盘符、含 `..` 的条目——防止 zip slip（一个精心构造的 zip 可以写到 `<家>` 之外）。
- zip 条目的权限位尽量保留；Windows 造的 zip 没带权限位时按 0755 补齐可执行文件。
- 解包目标永远是 `<家>/staging/unpack-<纳秒>`，并且 `defer os.RemoveAll`。

### 2.12 为什么 gpm 不吃 `.dmg` / NSIS / AppImage 安装器

| 形态 | 技术上能不能 | 为什么不做 |
|---|---|---|
| **AppImage** | 最接近能用：本身就是一个可执行文件，放哪都能跑 | 它不需要"安装"，也就不需要 gpm；而且 AppImage 的正解是直接放到用户想放的地方，gpm 在这里没有附加值。可以作为 `entry.<linux>.exe` 收录，**但不驱动它的安装器** |
| **NSIS `.exe`** | 能：`/S /D=<路径>`（`/D` 必须**放最后且不能加引号**） | 它自己写注册表、自己建卸载器、自己决定装哪；**gpm 的原子性、账本与卸载承诺当场失效**——卸载时 gpm 根本不知道它写了什么 |
| **`.dmg`** | 能：`hdiutil attach -nobrowse -mountpoint …` 挂载后拷出 `.app` | **毫无意义**：dmg 的价值就是拖拽交互；而且从网上下载的 dmg 挂载后拷出的 `.app` 照样带 `com.apple.quarantine`，Gatekeeper 一样拦。签名/公证票据还要**分别** staple 到 dmg 与 `.app` 两处 |

结论：**gpm 只吃"已经摆成 `payload/` 形状"的归档**。这不是能力不足，是刻意的边界——凡是被别人装的，gpm 就没法负责撤销。

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
| 工具链自举（`requires: [cot]`） | ✅ **已实现并实测**（v3.5，FR-18–FR-20；本机用假 cot 跑通 e2e，§3） | ✅ CI（`internal/install` 的假 cot 用例） | ✅ CI（同一批用例；Windows 上跳过 shell 夹具） |
| 启动器注入工具链环境（D33） | ✅ **已实现并实测**（v3.5，FR-22；C1 的环境实验见 §2.8） | ✅ CI（生成的块过 `sh -n`） | ✅ CI |

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
gpm install <目录或 .zip> [--dir PATH] [--with cot[,tdp]] [--yes] [--no-path] [--skip-verify] [--force]
gpm uninstall <id> [--dir PATH] [--force]
gpm list [--dir PATH]
gpm where <id> [--dir PATH]
gpm env [--dir PATH]        # 打印把 bin/ 加进 PATH 的 shell 片段
gpm version | gpm help
```

**打包侧（发布者用，见 §2.15）：**

```
gpm pack <装配目录> [--out PATH] [--os OS] [--arch ARCH] [--gpm 可执行文件] [--default-dir PATH]
```

- 退出码：`0` 成功；`1` 运行时错误；`2` 用法错误。
- 选项与位置参数**顺序无关**：`gpm install . --dir ~/cot` 与 `gpm install --dir ~/cot .` 等价（标准库 `flag` 遇到第一个位置参数就停止解析，所以入口处把选项重排了一次）。
- 输出针对"看得懂中文但不一定懂 shell 的人"写：出错时给的是**哪个文件、哪个字段、怎么改**。
- `gpm version` 的版本号是编译期注入的（`-ldflags "-X main.version=…"`），所以 `var version` 而不是 `const version`——`const` 注入不进去。
- `--force`（install 与 uninstall 都有）绕过的是**同一道闸**：那个应用正在运行时照做。它**不是**"忽略所有错误"的通用开关——校验、路径安全、PATH 许可这些一概不变。默认拦、显式放行：绝大多数人是手滑点到了正在用的应用，少数人是脚本里明知故犯。（§2.9.1）
- `--with` 覆盖清单里的 `requires`（很少用：清单说什么就装什么；打包方调试时拿来试"不装工具链"或"装另一家"）。

### 2.15 打包：`gpm pack`

**打包这一步在 gpm 里，不在 shell 脚本里。** 装配目录长这样：

```
<装配目录>/
├── ad-manifest.yaml              # 文件名 = <简称>-manifest.yaml（v3.5）
├── tools/<os>_<arch>/{cot,tdp}   # 选填；requires 声明了才需要（D32）
└── payload/…
```

`gpm pack <装配目录>` 会就地补齐另外几样（`install.sh`、`install.cmd`、`SHA256SUMS`），挑一个 gpm 二进制塞进去，把 `tools/` 一并打进 zip **并纳入 `SHA256SUMS`**（坏掉的 cot 必须在动手之前就被发现，FR-20），然后产出 `<id>-<version>-<os>-<arch>.zip`。

**`--default-dir` 决定这个包默认装到哪儿。** 它把路径烘进生成的 `install.sh` / `install.cmd`：

```sh
exec ./gpm install . --dir "${COT_HOME:-$HOME/cot}"
```

- 留空时脚本里写的是 `.`，也就是**解压出来那个目录**——gpm 自己永远不会凭空挑一个家目录（D21）。
- 开头是 `~` 时在脚本里换成 `$HOME`（POSIX）或 `%USERPROFILE%`（Windows）：双引号里的 `~` 不会展开，这两个变量会。
- 用户始终可以用 `COT_HOME` / `TDP_HOME` 或 `gpm install --dir` 覆盖它——烘进去的只是**默认值**。

**为什么从 `tools/fixture/make.sh` 那套 shell 搬进 Go**：

| 问题 | shell 方案 | `gpm pack` |
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
- **Windows 目标时 gpm 改名成 `gpm.exe`**：`install.cmd` 里写的就是 `gpm.exe`。
- 打包用的 manifest 必须**有目标平台的 `entry`**，否则直接报错（"这个包不适用于本机"要在打包时就发现，而不是发出去之后）。

`tools/fixture/make.sh` 还在（造测试用的小包），但**发布路径已经不再经过它**。

gpm 自己的二进制与签名：

| 项 | 现状 |
|---|---|
| 三平台二进制 | `GOOS=… GOARCH=… go build -trimpath -ldflags "-s -w -X main.version=…" ./cmd/gpm`，交叉编译不需要 cgo（`CGO_ENABLED=0`） |
| 发版 | 推 `v*` tag → `.github/workflows/release.yml` 出六个平台的 `gpm-<版本>-<os>-<arch>[.exe]` → `gh release create` |
| 命名 | `gpm-<版本>-<os>-<arch>` 是**给 AI Desk 的 CI 看的契约**（它按这个名字挑对应平台的 gpm 塞进 zip） |
| 签名 | **未做**，是发布阻塞项（§5 R1） |

### 2.16 持续集成

本机没有容器，也没有别的操作系统的虚拟机，所以**多平台验证只能交给 CI**：把平台专属的行为做成"在真 Linux / 真 Windows 上跑起来才成立"的测试，而不是靠读代码相信它。

**gpm-go（`.github/workflows/ci.yml`）**，每次 push / PR：

| job | 内容 |
|---|---|
| `test` × {ubuntu, macos, windows} | `go vet ./...` + `go test ./...`；ubuntu 上先装 `desktop-file-utils`（给 `desktop-file-validate` 用）；**windows 上额外真写一次 `HKCU\Environment`**（`GPM_TEST_REGISTRY=1` 才不 skip） |
| `crosscompile` × {darwin, linux, windows} × {amd64, arm64} | `CGO_ENABLED=0 go build ./...`，确认六个组合都编得出来 |

发版：`.github/workflows/release.yml`，`v*` tag 触发。

**ai-desk（`.github/workflows/release.yml`，一个文件同时兼 CI 与发版）**，每次 push：

- **三个平台各自真打一遍包**：macOS(arm64) / Linux(amd64) / Windows(amd64)。
- gpm **不是下载来的，是就地编译的**：workflow 里 `actions/checkout` 把 gpm-go 检出到 `.gpm-go`，`go build` 出当前平台的 gpm，再交给 `tools/package.sh`。好处是**两个仓库的改动能一起被验证**，也不必先有 gpm 的 release 这里才跑得起来（`workflow_dispatch` 有个 `gpm_ref` 输入，默认 `main`）。
- 版本号只有一个来源：`src-tauri/tauri.conf.json`。CI 用 `node -p` 读它，传给打包脚本，避免"tag 上的版本和产物里的版本不一样"。
- **前端依赖必须锁死**：`npm ci` + `package-lock.json`。第一次 CI 就是在这里翻的车——`package.json` 里写 `"^2"` 又没锁文件，CI 每次解析到最新的 `@tauri-apps/*`，与 `Cargo.lock` 里的 Rust crate 对不上，`tauri build` 直接拒绝构建（§5 R9）。

### 2.17 用它打包一个 Tauri 程序：流程与"后续谁调用谁"

**先说结论**：gpm 与 Tauri 的构建系统**没有代码耦合**——Tauri 负责把程序编出来，gpm 负责把编出来的东西装到用户机器上。两边的接缝只有**一个装配目录**（`<简称>-manifest.yaml` + `payload/`）。下面讲四件事：产物怎么变成装配目录、`gpm pack` 产出什么、**装完之后哪些东西会被谁调用**、以及和 `tauri-plugin-updater` 的关系。

#### ① Tauri 构建产物 → 装配目录

`tauri build` 给出的是：

| 平台 | 产物 |
|---|---|
| macOS | `src-tauri/target/release/bundle/macos/<Name>.app`（一个**目录**） |
| Linux | `src-tauri/target/release/<binary>`（裸 ELF），或自己打的 AppImage |
| Windows | `src-tauri/target/release/<binary>.exe`（裸 exe），或 NSIS `.exe` / `.msi` |

装配目录要的只是把上面那个产物原样搬进去：

```
<装配目录>/
├── ad-manifest.yaml     # id / name / version / requires / entry / launch
└── payload/
    └── <Name>.app 或 <binary>[.exe]
```

ai-desk 的 `tools/package.sh` 干的就这一件事：读 `src-tauri/tauri.conf.json` 拿 `productName` 与版本号，把构建产物拷进 `payload/`，按平台写好 `entry`，然后调

```sh
gpm pack <装配目录> --gpm <当前平台的 gpm> --default-dir "~/cot"
```

产出 `ai-desk-<version>-<os>-<arch>.zip`。

**注意 gpm 不吃 Tauri 自己产的 `.dmg` / NSIS `.exe` / AppImage 安装器**（§2.12），它要的是"已经摆成 `payload/` 形状"的那个目录。在 CI 里这反而更简单：macOS 上用 `tauri build --bundles app`（跳过打 `.dmg`），Linux / Windows 上直接拿 `target/release/` 里那个二进制。

#### ② `gpm pack` 产出的 zip

```
<id>-<version>-<os>-<arch>.zip
├── install.sh        # macOS / Linux，由 pack 生成
├── install.cmd       # Windows，由 pack 生成
├── gpm               # 打包时那个 gpm（Windows 目标时叫 gpm.exe）
├── ad-manifest.yaml  # 文件名 = <简称>-manifest.yaml（v3.5）
├── tools/…           # 只在 requires 非空时（D32）
├── payload/…
└── SHA256SUMS
```

两个入口脚本都只做 bootstrap（§2.1）。**"装到哪儿"就写在这一行里**：

```sh
exec ./gpm install . --dir "${COT_HOME:-$HOME/cot}"
```

那个 `$HOME/cot` 是 `gpm pack --default-dir "~/cot"` 烘进来的（`~` 在脚本里展开成 `$HOME`，§2.15）；没给 `--default-dir` 时它是 `.`，也就是解压出来那个目录。

**那个 `"~/cot"` 的引号不能省。** 不引的话是 shell 先把 `~` 展开成本机绝对路径（CI 上就是 `/home/runner/...`），再交给 `gpm pack`——于是发布产物里被写死了一个**打包机的家目录**，别人装到哪儿就全错了。引起来才轮到 `gpm pack` 去展开成 `$HOME` / `%USERPROFILE%`（§2.15 的 `scriptDir`）。

#### ③ 装完之后：谁调用谁

这张表是本节的正题——**Tauri 那边到此为止**。

| 谁 | 什么时候、被谁调用 | 备注 |
|---|---|---|
| `install.sh` / `install.cmd` | 用户双击、或解压后在终端里跑一次 | 只做三件事：切到自己的目录、补 `+x`、把活交给 `./gpm` |
| 包里的 `./gpm` | 被 `install.sh` 调这一次 | **用户机器上事先不需要有 gpm**——这就是 D20"zip 自带 gpm"的全部意思 |
| `<家>/bin/gpm` | 用户之后敲 `gpm list` / `gpm where` / `gpm uninstall` | 装的时候把自己拷过去的那一份（D29）。那儿本来就有 gpm 则**不覆盖**，也不计入账本 |
| `<家>/bin/<cmd>` 终端启动器 | 用户在终端敲应用名（`ad movie.mp4`） | 先注入工具链环境（D33），再直接 exec 内层可执行文件（v3.5 起 macOS 不再用 `open`，因为它不给环境） |
| `~/Applications/<Name>.app`（macOS） | 用户点图标、Spotlight、启动台 | 它是指向 `<家>/lib/<id>_<ver>_<os>_<arch>/<Name>.app` 的**符号链接**，不是拷贝 |
| `.desktop`（Linux）/ 开始菜单 `.lnk`（Windows） | 桌面菜单 | Linux 的 `Exec=` 指向启动器；Windows 的 `.lnk` 指向应用本体（§2.7） |
| PATH 标记块 | 每次开 shell 时被 source | `~/.zprofile` / `~/.zshrc` / `~/.profile`；Windows 上是 `HKCU\Environment` 的 `Path` |
| `state.json` | **不被人调用**，只被 gpm 自己读写 | 卸载按它逐条回放（D28） |
| `~/.ai-desk-update.log` 之类应用自己的东西 | 应用自己 | gpm 不碰（NFR-6） |

**要出新版本时**：再 `gpm pack` 一个 zip，用户再跑一次 `install.sh`。同一个 `id` 是**覆盖式**安装（D30）：先按卸载流程删旧的（动手前先查应用在不在跑，D31/§2.9.1），再落新的，装完账本、目录名、启动器、图形入口、PATH 全部重新自洽。

#### ④ 与 `tauri-plugin-updater` 的关系：二选一

gpm **没有网络代码、没有 `upgrade` 子命令**（§0.5），所以它不会去"抓"新版本。如果同时启用了 `tauri-plugin-updater`，两套东西会各干各的，而且在三个平台上结果不一致：

| 平台 | `tauri-plugin-updater` 的表现 | gpm 的路子 |
|---|---|---|
| macOS | 能工作：minisign 签名校验后**原地 `rename` 换掉整个 `.app`**（AI Desk 上实测过，含自动重启） | 跑新的 `install.sh` |
| Linux | 只认 AppImage 载荷；裸二进制可以覆盖写，但 `.tar.gz` 载荷会报 `BinaryNotFoundInArchive` | 同上 |
| Windows | **裸 exe 装不了**：它强制要求 NSIS `setup.exe` 或 `.msi`，还得 `ShellExecuteW` 拉起安装器 | 同上 |

而且 updater 换完之后**账本与目录名会脱节**：目录还叫 `ai-desk_0.1.0_darwin_arm64`，里面已经是 0.2.0，`gpm list` 照旧报 0.1.0（O10）。用 gpm 分发就别同时开 updater——**要么让用户重跑 `install.sh`（gpm 的路子），要么走 Tauri 自己的安装器体系（那就别用 gpm 分发）**。

---

## 3. 测试策略与已验证结果

**隔离原则**：所有测试不得触碰真实 `$HOME`。端到端一律通过覆盖 `HOME` 进行——Unix 上 `os.UserHomeDir()` 返回 `$HOME`，所以改一个环境变量就能把 `<家>`、`~/Applications`、`.zprofile/.zshrc/.profile` 全部关进沙箱。这也是唯一能安全测 PATH/shell 配置写入的方式。

| 层 | 方法 | 覆盖 |
|---|---|---|
| manifest 校验 | 表驱动：`entry` 缺失、`bundle` 与 `exe` 并存、路径含 `..`、`launch.cmd` 含分隔符、当前平台无入口 | `internal/manifest` |
| 解包路径安全 | 构造含 `../` 与绝对路径的 zip，断言被拒 | `internal/stage` |
| 校验 | 改动一个字节 → 拒绝；删掉 `SHA256SUMS` → 警告 + `unverified`；`--skip-verify` → 放行；**带空格的路径**（`AI Desk.app/…`）要能正确切出文件名 | `internal/stage` |
| 打包 | 打出来的 zip 能原样解回来（含符号链接与 `0755`）；`SHA256SUMS` 覆盖到每个常规文件且不含链接；缺目标平台入口时拒绝；失败不留半个 zip | `internal/pack` |
| 启动器语义 | 断言**没有 `requires` 时** `activate` 生成 `open … --args`、**有 `requires` 或 `direct` 时**直接 exec 内层可执行文件；注入的环境块过 `sh -n`；参数逐个透传；生成的文件真的可执行 | `internal/integrate` |
| PATH 块 | **把生成的块真喂给 `zsh -n` / `bash -n` / `sh -n`**；"必须是 `$HOME` 相对形式"；连 source 三次只有一个块且在首位；fish 用 `contains` | `internal/integrate` |
| Windows PATH | 纯函数表驱动（展开 `%VAR%`、判重、只摘自己那一条）+ **往返逐字节保真**；真注册表往返由 CI 的 windows job 跑 | `internal/integrate` |
| Windows `.lnk` | 写进去再读回来，比对 Target/Arguments/WorkingDir/Description；已有同名不覆盖 | `internal/integrate`（windows） |
| Linux `.desktop` | 字段齐全、`Exec=` 指向启动器、遵守 `$XDG_DATA_HOME`；有 `desktop-file-validate` 就过一遍 | `internal/integrate`（linux） |
| 集成副作用 | 隔离 `HOME` 后跑真实安装，断言目录、启动器、图形入口、账本、PATH 块；重复安装不叠加；卸载后逐项消失 | e2e |
| 运行中检查 | 纯函数表驱动：`/a/b` 不匹配 `/a/bc`、只认路径边界、`" (deleted)"` 幽灵照认、符号链接两种写法都收、空 `dir` 不匹配任何东西；darwin 的 `pgrep` 模式**恰好**锚在 argv[0]；Linux 用假 `/proc` 造四种 pid（正主 / 只有 argv[0] 的 AppImage / 幽灵 / 无关）；真起一个进程、**把它删掉**，断言仍认得出 | `internal/proc`（三平台） |
| 拦截语义 | 隔离 `HOME` 后真装一次、真把假应用跑起来，再装第二次 → 必须拒绝且**账本与包目录一个字没动**；`--force` → 放行且有警告；卸载同理；应用不在跑时不许误拦 | `internal/install` |
| 安装根与 `--default-dir` | `home.Resolve` / `home.ResolveInstall` 的五档优先级（`--dir` > **按 `requires` 取的家** > **从自己的位置推断** > **平台数据目录/<简称>** > 当前目录）；`RootFromSelf()` 的三条判据各自能拦住一种"看着像但不是"的位置（目录不叫 `bin` / 文件不叫 `gpm` / 上一级没有 `state.json` / 连自己在哪都不知道）；生成的脚本里 `~` 展开成 `$HOME` / `%USERPROFILE%`，没给 `--default-dir` 时脚本干脆**不传 `--dir`**（交给 gpm 按上面那条链自己定）；`<bin>/gpm` 已存在时**不覆盖**且不进账本 | `internal/home`、`internal/pack`、`internal/install` |
| 工具链自举 | 用一个会写日志的假 cot（脚本）：`requires` 非空时它被调用一次、收到的家参数正确；返回非 0 时整次安装失败且**一个字都没落下**；`requires` 为空时它**根本不被调用**（A1、A11） | `internal/install` |
| 启动器环境注入 | 生成的 macOS 启动器里含 `export COT_HOME=…` 与 PATH 前置，交给 `zsh -n` 解析；真机上从终端启动一次，进程环境里能看到 `COT_HOME`（A12） | `internal/integrate` |
| 跨平台 | CI 矩阵 `{ubuntu,macos,windows}` 真跑单测；`{darwin,linux,windows} × {amd64,arm64}` 交叉编译 | §2.16 |
| 静态检查 | `go vet` | 全仓 |

**一条已经兑现的教训**：字符串级断言抓不住"生成的 shell 代码本身是坏的"。PATH 标记块曾经漏掉一个引号（`*":$HOME/ad/bin:*)`），单测全绿，而真 zsh 一读 `~/.zprofile` 就 `unmatched "`——软件装好了、图标也在，终端里却永远 `command not found`。所以那组测试改成**把生成的代码交给真 shell 去解析**。同类：Windows PATH 的往返保真如果只在字符串层面测，也会漏掉"空条目被吃掉"。

**macOS 上已经端到端实测过的（27.0.1 / arm64，沙箱 `HOME`）**：

- `install.sh` → `gpm install . --dir …` 全流程成功，`~/ad` 布局与设计一致。
- 终端启动器 `~/ad/bin/ad` 真的把应用拉起来了（探针自报 `argv0` 在 `~/ad/lib/…/Minimal.app/Contents/MacOS/Minimal`、`bundlePath` 正确、`bundleIdentifier` 非 `(null)`、`activationPolicy = 0`）。
- `~/Applications/AI Desk.app` 符号链接经 `open` 同样能拉起（与双击等价）。
- PATH 标记块写进三个文件；**连装三次每处仍只有一个块**（幂等）。
- 负例：payload 改一个字节 → `sha256 对不上（清单 …，实际 …）` 拒绝；manifest 指向不存在的入口 → 拒绝。两次失败**都不留半成品**。
- 卸载后 0 残留，连 gpm 自己创建出来的空 shell 配置文件都删掉了。

**"正在运行就拦"在真机上实测过（同一个 AI Desk，`~/ad` 里装着 0.2.0，用 `~/ad/bin/ad` 拉起来，pid 1844）**：

- `gpm install <zip> --dir ~/ad --yes` → 打印那四行人话后拒绝，退出码 1；`gpm uninstall ai-desk` → 同样拒绝。
- 被拦之后 `gpm list` 仍报 0.2.0、`~/Applications/AI Desk.app` 符号链接原样——**确实一个字都没动**。
- 加 `--force` → 先打一句警告再完整重装，退出码 0。
- **假阳性测试**：起一个命令行里只是"提到"那个路径的 shell 循环，不加 force 的安装**照常成功**——证明 `^` 锚定 argv[0] 是有效的。
- **幽灵进程确实存在**：更新后旧进程还活着，`lsof` 显示它的 `txt` 指向
  `/private/var/folders/…/T/tauri_current_appKElfRw/current_app/Contents/MacOS/ai-desk`，而这个目录**已经被删掉**了（`ls` 报 `No such file or directory`）；`lsappinfo list` 里它仍占着 `bundle path="…/AI Desk.app"`、`Version="0.1.0"`，再 `open` 那个路径只会把它唤到前台。**这就是 D31 存在的全部理由。**

**CI 已经抓出来的四个问题**（都不是靠读代码能发现的）：

| # | 现象 | 根因 |
|---|---|---|
| 1 | Windows 上生成的 PATH 块是绝对路径，没相对化成 `$HOME/…` | `homeRelative` 拿 `filepath.Separator`（`\`）去比 `os.UserHomeDir()`（`/`）→ 前缀永远不匹配（§2.13 第 4 条） |
| 2 | Windows 上报"可执行位丢了：`-rw-rw-rw-`" | Windows 的 `os.Stat` 一律报 `0666`，是**测试**的断言没分平台 |
| 3 | 卸载后用户的 PATH 少了结尾一个分号 | `windowsPathValue` 插值前 `Trim`、`windowsPathRemove` 又只 join 非空条目 → 空条目被吃掉（§2.6） |
| 4 | ai-desk 三平台构建全挂：`Found version mismatched Tauri packages` | `package.json` 写 `"^2"` 且仓库无 lockfile，CI 解析到的 `@tauri-apps/*` 比 `Cargo.lock` 里的 crate 新（§2.16、R9） |

**尚未验证**：`direct` 模式在真实长驻应用上的行为；中断（kill -9）注入；`.app` 外壳合成（代码未实现）；Linux/Windows 上的**人工**体验（CI 覆盖的是测试断言，不是"人对不对得上眼"）；`internal/proc` 的 Windows 与 Linux 实现只在 CI 的真 runner 上证过，本机没跑过真进程（macOS 那条是真跑了的）。

**v3.3（改名 + 安装根解耦）的真机验证（2026-10-09，macOS）**：用 `gpm 0.2.0-local` 重新打包 AI Desk，真机上依次跑过——① 用改名前的 `cpi` 卸载干净（`~/ad` 只剩骨架、三处标记块归零）；② 用新包自带的 gpm 从零装上 0.2.0（`bin/gpm`、`bin/ad`、`~/Applications` 软链、三处标记块、账本 `self` 都自洽）；③ `~/ad/bin/ad` 真的把 GUI 拉起来了（应用日志 `version=0.2.0`）；④ **故意拿一个内嵌旧 gpm 的 0.1.0 包覆盖安装**：覆盖流程正常走完，并打出「`<bin>/gpm` 已经有一个 gpm，这次没覆盖它」，`~/ad/bin/gpm` 的 sha256 前后一致——**D29 没有把新 gpm 降级**；⑤ 包里的 `install.sh` 也在沙箱根里单独跑通过一次。仍然没验证的：Linux / Windows 的真机（只到 CI 断言）。

**v3.4（从自己的位置推断家目录）的真机验证（macOS）**：把 `gpm 0.2.0-local` 放进 `~/ad/bin/gpm`（那儿本来就有），**不带 `--dir`、也不设 `GPM_HOME`** 直接敲 `~/ad/bin/gpm list` —— 它自己找回了 `~/ad`，列出 AI Desk 0.2.0，不再报"这里还没装东西"（这正是 v3.3 留给 O11 的那个洞）。`gpm where ai-desk` 同样正常。另外确认过：把同一个二进制放到一个不叫 `bin` 的目录里（`/tmp/gpm-dist/gpm`）时它不会乱认家，仍旧回落到当前目录。

**v3.5 的第 0 步：清掉 `~/ad`（2026-10-09，macOS）**。用旧安装自带的 `~/ad/bin/gpm uninstall ai-desk` 按账本回放：删掉 `~/Applications/AI Desk.app` 软链、`~/ad/bin/ad`、`~/ad/lib/ai-desk_0.2.0_darwin_arm64` 与三份 rc（`.zprofile` / `.zshrc` / `.profile`）里的 `$HOME/ad/bin` 标记块，最后删掉自装的 `~/ad/bin/gpm`，然后 `rm -rf ~/ad`。核验：三份 rc 里 `gpm` 与 `/ad/bin` 的命中都归 0、`command -v ad` 找不到、`alias c` 与四个 token 导出未受影响。**教训：先回放账本、再删目录。** 反过来（直接 `rm -rf ~/ad`）会在三个 shell 配置里留下三个悬空的 PATH 条目，此后每个新 shell 都要为它多算一次——账本存在的意义正在于此。

**macOS `open` 不给环境（v3.5，C1）** 在同一天用探针 `.app` 实测过：`open` 起来的进程环境只有 15 项 launchd 基线（`PATH=/usr/bin:/bin:/usr/sbin:/sbin` 等），既没有 shell 的 PATH 也没有 `COT_HOME`；`open --env GPM_x=y` 的四种写法（`-a` 形式、选项在路径后、`--env` 在 `-a` 前、配 `--args`，含 `-n`）**全部不生效**；直接执行 `Contents/MacOS/run` 则环境完整继承。这就是 D33 改成直接 exec 的全部依据。另外记一笔：受限/沙箱宿主里 `/bin/ps` 会被拒（`Operation not permitted`），读 GUI 进程环境**只能用"自 dump 的最小 .app"这类夹具**，不能用 `ps eww`。

---

## 4. 里程碑

| 里程碑 | 内容 | 状态 |
|---|---|---|
| **M0 macOS 一条龙** | 解包/校验/落盘/启动器/图形入口/PATH/账本/卸载/回滚；`tools/fixture` 造包 | ✅ **已完成并实测**（提交 `c4fe024`） |
| **M1 Linux** | 包装脚本 + `.desktop`（遵守 `$XDG_DATA_HOME`）；AppImage 作为 `entry.linux.exe` 的收录路径 | ✅ **已完成**，在 ubuntu runner 上验证 |
| **M2 Windows** | `HKCU\Environment` 写 PATH + 广播 `WM_SETTINGCHANGE`；开始菜单 `.lnk`（原生 COM）；`install.cmd` 在真机跑通 | ✅ **已完成**，在 windows runner 上验证 |
| **M3 打包收进内核** | `gpm pack`：`SHA256SUMS` + 权限位 + 符号链接 + 三平台一致 | ✅ **已完成**（提交 `acb0c4a`） |
| **M4 两个仓库的 CI** | gpm-go：三平台单测 + 六平台交叉编译 + tag 发版；ai-desk：三平台真打包 | ✅ **已完成并跑绿**（`242c54e`、`9e33ad7`） |
| **M5 签名与公证** | macOS Developer ID + 公证 + `xcrun stapler`；Windows 代码签名 | ⏸ **用户明确暂缓**（放弃 1，先做 2 和 3）；仍是真正的发布阻塞项（§5 R1） |
| **M6 未做的两件** | 合成 `.app` 外壳（需先给 `manifest.Entry` 加 `bin`）；启动器名字冲突检查（R4） | 待做 |
| **M7 运行中检查** | `internal/proc`（macOS/Linux/Windows 三平台）+ install/uninstall 的拦截闸 + `--force`；真机上拿 AI Desk 验过（拒绝、`--force`、无假阳性、幽灵进程仍在） | ✅ **已完成并实测** |
| **M8 改名 + 装到哪儿解耦** | 全仓 `cpi`→`gpm`（模块路径、`GPM_HOME`、`cmd/gpm`、CI、tools）；`home.Resolve` 删掉 `~/ad` 硬编码；`gpm pack --default-dir`；`<bin>/gpm` 已存在不覆盖；文档同步（§0.5、§0.3.3、§2.17） | ✅ **已完成**（v3.3） |
| **M9 从自己的位置推断家目录** | `home.RootFromSelf()`（三条判据）+ `Resolve` 多一档 + 四条新测试（含三种"看着像但不是"的反例）；真机验证不带 `--dir` 的 `gpm list`；文档同步（§0.3.4、§2.3、D21、O11 关闭） | ✅ **已完成**（v3.4） |
| **M10 工具链自举 + 家合并（v3.5）** | `<简称>-manifest.yaml` 与 `requires:`；install.sh 跑 zip 自带的 `cot i -s <家>`（失败不继续）；家改成 `$COT_HOME` / `$TDP_HOME`、`GPM_HOME` 退场；`requires` 为空时落平台数据目录/<简称>；macOS 启动器注入环境（有 `requires` 才改成直接 exec）；启动器重名检查（FR-21）；文档同步（§0.3.5、§2.5.1） | ✅ **已完成**（v3.5）：`go vet` + `go test ./...` 全绿，并在本机用假 cot 跑通端到端（§3） |

---

## 5. 风险与开放问题

### 风险

| # | 风险 | 影响 | 对策 |
|---|---|---|---|
| **R1** | **macOS 签名与公证**（Developer ID）、**Windows 代码签名**是发布阻塞项 | zip 过浏览器/邮件会带 `com.apple.quarantine` 与 MOTW，用户双击 `install.sh` 或应用时被 Gatekeeper / SmartScreen 拦。签名在 Mach-O 内部，过 zip 不会丢；彻底解决只能签名 + 公证 + `xcrun stapler staple`（票据订在包上，断网也能验） | 发布前必须解决；内网过渡阶段可在文档里教用户处理，但**不要把 `xattr -dr com.apple.quarantine` 写进 `install.sh`**——那是替用户绕过安全检查 |
| **R2** | **合成外壳没有独立签名**（D24） | 上游只发裸二进制、由 gpm 合成 `.app` 时，外壳本身不是签名包；若下载物带 quarantine，Gatekeeper 可能直接拦 | 与 R1 同源；优先收录上游自己发 `.app` 的软件 |
| **R3** | ~~Windows 侧 PATH 与开始菜单尚未实现~~ | — | **已解决**（§2.13）；CI 的 windows job 每次都会重跑真注册表往返，防止改回去 |
| **R4** | ~~启动器没有做名字冲突检查~~ `integrate.Launcher` 会直接覆盖 `bin/<cmd>` | 若用户已在 `<家>/bin` 放了同名文件会被静默覆盖；v3.5 把工具链的家与用户的 `bin/` 合流之后，撞名的机会更多（`ac` 还会撞 macOS 的 `/usr/sbin/ac`） | **已解决**（v3.5，FR-21）：`integrate.OwnedByGpm` 认出"不是 gpm 写的"同名文件、`ledger.FindByCmd` 拦住"别的包占着这个名字"，两者都报错拒绝、只有 `--force` 放行，撞上时账本与包目录一个字都不动（`internal/install` 三个用例守着） |
| **R5** | 上游应用改名 / 改目录结构 | `<简称>-manifest.yaml` 与 `payload/` 对不上，安装失败 | 打包脚本在 CI 里跑，写完就验，不靠人手维护 |
| **R6** | 修改 shell 配置被视为越界 | 用户反感或系统管理员禁止 | 明确请求许可、标记块、可一键撤销、拒绝后功能降级而非失败 |
| **R7** | 用户已有同名应用（自己装过官方版） | `~/Applications` 里两个同名 | 已处理：目标已存在则不覆盖，只提示 |
| **R8** | **`.app` 外壳合成没有实现**（D24 的后半条） | 上游只发裸可执行文件的 macOS 应用，现在只能写 `entry.darwin.bundle`，写不出来就装不了；硬把它当 `exe` 收进来，用户拿到的是一个没有应用身份的东西 | 先给 `manifest.Entry` 加 `bin`，再照 §2.7 的配方实现（`Info.plist` 4 键 + **硬链接** `Contents/MacOS/<exe>`）。在实现之前，清单里**不要写没有 `.app` 的 macOS 应用**（M6） |
| **R9** | **Tauri 前端依赖与 Rust crate 版本漂移** | 这个坑真实发生过：`package.json` 写 `"^2"` + 无 lockfile，CI 解析到比 `Cargo.lock` 更新的 `@tauri-apps/*`，`tauri build` 报 `Found version mismatched Tauri packages` 直接拒绝构建——**本机编得动只是因为 `node_modules` 里还是旧版本** | 已处理：三个 Tauri 包钉死确切版本 + 提交 `package-lock.json` + CI 用 `npm ci`。泛化教训：**凡是"本机能编、CI 编不了"的构建问题，先怀疑锁文件** |
| **R10** | **进程探测会漏拦**（D31） | 三种漏法：① 进程的可执行文件不在包目录下（比如它自己 `chdir` 走了、或某个壳反过来启动）② 平台不支持枚举进程（`find_other.go`）③ 没权限看别人的进程。漏拦的后果回到 v3.1 的老样子（版本混用 / 幽灵进程），**但用户至少看到过一行提示** | 已接受：**宁可漏拦不可误拦**（FR-17）。真要补，方向是让应用自己上报（单实例锁 / pid 文件），但那要求应用配合，且把 gpm 从"看文件系统"拖进"看运行时协议"——不属于本版 |
| **R11** | 同名/同目录的**另一个**进程被误判 | 比如用户从同一份 `lib/<id>_…/` 目录里手动跑了两次、或开发者拿这个目录当工作目录跑了个 shell | 前缀比对是**双向的**（`<pkg>/` 开头才算），实机测过"命令行里只是提到这个路径"不命中。真要更严只能校验进程的 `exe` 而不是 `argv[0]`——但那样会漏掉 AppImage 与 macOS 的 `open` 路径，得不偿失 |
| **R12** | 改名会打断**仓库外**的调用方 | `cpi`→`gpm` 之后，`~/space/rust/ai-desk/tools/package.sh` 里的 `--cpi` 与 `CPI_BIN` 立刻报 `flag provided but not defined`。这类调用方不在本仓的 `git grep` 范围里，改名时**看不见** | 已处理：同步改了那个脚本（改成 `--gpm` / `GPM_BIN`，并让它把 `--default-dir "~/ad"` 传给 `gpm pack`）。泛化教训：**改 CLI 名字之前，先 grep 一遍仓库外谁在调它**——这是 `docs/DESIGN.md` 自己教不会的那一类知识 |
| **R13** | macOS 启动器改直接 exec 之后，**丢掉 LaunchServices 语义**（双击等价、单实例激活、Dock / 最近使用） | 从终端启动可能开出第二个实例；Finder / 启动台那条路走 LaunchServices，拿不到注入的环境（A7） | 已接受为 v3.5 的代价：环境注入是刚需（C1）。两全的解是合成 `.app` 外壳（R8），在它落地之前两条路各有一个缺口 |
| **R14** | 家合并之后，gpm 的账本与工具链的东西**同住一个目录** | `gpm uninstall` 的边界一旦写错就会删到用户的 go / java（D34 正是为此存在）；另外 `~/cot/bin` 里 gpm 自己装的 `gpm` 与 cot 的 `cot` 是邻居 | 账本只记自己写的东西，卸载按账本逐条回放、**不扫目录**；可能的加锁 / 改名留作 O16 |

### 开放问题

| # | 问题 | 需要谁定 |
|---|---|---|
| ~~O1~~ | ~~Windows 的 PATH 与 `.lnk` 什么时候做~~ | **已做**（§2.13） |
| O2 | 要不要给应用图标（`.icns` / `.ico`），从哪来 | 用户 |
| ~~O3~~ | ~~启动器名字与已有命令冲突时，是跳过、报错、还是让用户改名~~ | **已定并实现**（v3.5）：一律报错拒绝、`--force` 才覆盖（R4、FR-21）。注意 gpm 只看**同一个家**里有没有重名；`<家>/bin` 之外的既有命令（如 macOS `/usr/sbin/ac`）谁赢由 PATH 顺序决定，gpm 不代答 |
| O4 | 图形安装器（原 Wails 四屏）还做不做 | 用户 |
| O5 | 是否需要"装完自动打开终端 / 自动启动一次应用"的引导 | 设计 |
| O6 | 中断注入测试（kill -9）什么时候补 | 设计 |
| O7 | gpm 自身如何升级（D29 之后，装新包**既不会覆盖** `<bin>/gpm`，gpm 自己也没有升级命令——想升级得手工换掉那个文件） | 用户 |
| O8 | 要不要做合成 `.app` 外壳（R8）——当前 AI Desk 有真 `.app`，所以不挡路 | 用户 |
| O9 | 拦下来之后，要不要顺手提供"我来帮你退出它"（发 SIGTERM 再重试）？当前只说不做（§1.5） | 用户 |
| O10 | 自动更新把 `.app` 换掉之后，**账本里的版本号与目录名会过期**（目录仍叫 `…_0.1.0_…`，里面已是 0.2.0）。要不要让 `gpm list` 从 `Info.plist` 现读一次真实版本 | 用户 |
| O11 | ~~**装完之后，`gpm` 自己不知道家在哪**~~ —— **已解决（v3.4）**：`GPM_HOME` 只活在 `install.sh` 那一行里，用户在新终端里敲 `gpm list` 时会落到**当前目录**报"这里还没装东西"（真机实测过）。用户的裁决：**带 GUI 的程序放到用户指定的目录下，非 GUI 的程序放到它下面的 `bin/`，gpm 自己也一样在 `bin/` 下，所以不用给它指定 HOME** | 已定（用户）：不认命（ⓐ）、也不往 shell 里塞 `export GPM_HOME`（ⓑ）、更不另存一处状态（ⓒ），而是从 `<家目录>/bin/gpm` 这个位置**看出来**——`RootFromSelf()`，见 §2.3、D21 |
| ~~O12~~ | ~~`requires` 为空的包装到哪个家~~ | **已定（用户）**：用简称、缺省装到平台数据目录（macOS `~/Library/Application Support/<简称>`、Windows `%LOCALAPPDATA%\<简称>`、Linux `${XDG_DATA_HOME:-~/.local/share}/<简称>`）——见 D21、§2.3；用户原话见 `REQUIREMENTS.md` §7-1 |
| ~~O13~~ | ~~简称撞名（`ac` 撞 macOS `/usr/sbin/ac`）与启动器重名怎么裁决~~ | **已定（用户）**：`ac` 只是举例、不单独裁决；重名一律按 FR-21 拒绝安装、`--force` 才放行（§7-4） |
| **O14** | "工具链已经装好"的判据：只看 `<家>/bin/cot` 存在，还是要求 activate 资产齐全 / 走一次版本比较 | 设计（§7-2） |
| **O15** | 工具链自举失败时的回滚范围：是否连 gpm 已写下的 rc 块、软链一起撤销（本版按"全部撤销、不留半成品"起草） | 设计（§7-3） |
| **O16** | 账本 `state.json` 与 cot 共处一个家：要不要给账本改名或加锁 | 设计（§7-5） |

---

## 6. 术语

| 术语 | 含义 |
|---|---|
| gpm | 本产品：GUI Package Manager（§0.5）。名字里的 manager 对标 `dpkg`：装、卸、列、查、打包，**不抓取、不升级** |
| 家 / `$COT_HOME` / `$TDP_HOME` | 安装根，也是工具链自己的家。**v3.5 起没有 `GPM_HOME`**：`--dir` > 按 `requires` 取的那个家 > 从 gpm 自己的位置推断 > 当前目录；打包方可以用 `gpm pack --default-dir` 把默认值烘进 `install.sh`/`install.cmd`（AI Desk 烘的是 `~/cot`） |
| `RootFromSelf` | 从 gpm 自己所在的位置反推安装根：`<家目录>/bin/gpm` 说明家目录是它的上一级（§2.3）。装完之后用户在新终端里随手敲 `gpm list` 靠的就是它 |
| 分发包 | 那个 zip：`install.sh`/`install.cmd` + `gpm` + `<简称>-manifest.yaml` + `payload/` + `SHA256SUMS`（+ 需要时的 `tools/`）；由 `gpm pack` 产出（§2.15） |
| 装配目录 | 打包的输入：`<简称>-manifest.yaml` + `payload/`（要自举工具链再加 `tools/`），其余由 `gpm pack` 补齐 |
| `gpm pack` | gpm 的打包子命令（§2.15）；发布路径上唯一被支持的打包方式 |
| manifest | `<简称>-manifest.yaml`，描述一个应用怎么落地（`entry` + `launch`），以及要不要捎带工具链（`requires`） |
| 工具链自举 | install.sh 跑 zip 自带的 cot / tdp 把它自己装进家（D32、§2.5.1）：离线，只装壳与 activate 框架，不装插件 |
| `requires` | 清单里声明要哪家工具链（`cot` / `tdp`）的字段；它同时决定安装根与要不要走自举（D21、D32） |
| payload | 应用的实际内容，原样落到 `lib/<id>_<ver>_<os>_<arch>/` |
| entry | 可执行入口：`bundle:`（macOS `.app`）或 `exe:`（其它平台） |
| 账本 / `state.json` | 装了什么 + 产生了哪些外部副作用的**唯一真相**；卸载按它回放 |
| 集成 / integrate | 让装好的东西"能被用上"：终端启动器、图形入口、PATH |
| 启动器 / launch | `bin/<cmd>` 下的小脚本，让"装完能从终端启动"成立（§2.8） |
| 应用外壳 / shell | gpm 为"上游只发裸可执行文件"的 macOS 应用合成的最小 `.app`（§2.7） |
| 图形入口 | macOS `~/Applications` 链接、Linux `.desktop`、Windows 开始菜单 `.lnk` |
| rollback 栈 | 本次安装失败时逆序撤销的依据（与账本分工不同，§2.5） |
| 正在运行检查 / `proc.Find` | 覆盖安装或卸载之前，查包目录底下有没有活着的进程（§2.9.1）。只做**纯字符串**路径前缀比对，绝不 `stat`——幽灵进程的路径早就没了 |
| 幽灵进程 / ghost | 文件被删或换掉之后仍抓着旧 inode 继续跑的进程。在 macOS 上更麻烦：LaunchServices 把 `.app` 路径一直绑在它身上，用户点图标只会把它唤到前台 |

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
| D1 | 清单来源 = 内置 YAML + 外部覆盖 | **作废**：没有"清单"，只有一个 `<简称>-manifest.yaml`（v3.5 前叫 `manifest.yaml`） |
| D2 | 全部直连官方二进制，不用包管理器 | **保留精神**：仍然不用 brew/apt/winget |
| D3 | 默认范围 = 核心 CLI + 语言运行时 | **仍作废**：纯 CLI 工具的**插件**整体出局；v3.5 只允许"把 zip 自带的 cot / tdp 本身搬进家"这一条离线通道（D32） |
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
| D18 | macOS 应用必须 `.app`，裸二进制由 gpm 合成外壳 | **保留**，见 §2.7——**这是 v2 最有价值的一条**，是花了一轮真机实验换来的（合成那一半代码还没写，R8） |
| — | store（内容寻址）、镜像表、版本源（`github-release`/`url-feed`/`pin`）、计划算法 `plan.Build`、四种安装阶段、`gpm doctor`、`gpm gc`、i18n、便携模式、`expose`（把自带 CLI 暴露成 shim） | **全部作废**（`expose` 单应用场景下用不着——应用自带 CLI 就直接在 `entry` 里给它，不必再暴露一次） |

### 7.3 从 v2 继承下来、仍然值得记住的三条硬知识

1. **macOS 上 `.app` 是必需的，而且是目录不是文件**；`Contents/MacOS/<exe>` 只能硬链接或拷贝，**不能符号链接**（§2.7）。
2. **zsh 不读 `~/.profile`**，只写 `.profile` 会导致"装好了但终端里敲不出来"（§2.6）。
3. **macOS 上 `~/Applications` 会被启动台与 Spotlight 索引**，是用户级图形入口的正解；Windows 侧则必须直写 `HKCU\Environment` 并广播 `WM_SETTINGCHANGE`（§2.6、§2.7）。
