# gpm 需求文档

> §1–§2 由我手工填写，一字未改；§3–§8 是讨论后的草稿。全文已落进 [`DESIGN.md`](DESIGN.md) 并**实现完成**（M10–M13，`go vet` + `go test ./...` 全绿，本机跑通端到端）；§7 里原来的三条设计问题（7-2 已装好判据、7-3 回滚范围、7-5 的加锁）已由用户裁决、v3.8 落地，新增 7-6。
>
> **v3.6 改了一处落地位置**（用户裁决，DESIGN D35）：GUI 应用的入口落**家目录顶层**（`<根>/AI Desk.app`），不再进 `lib/<id>_<版本>_<平台>/`——那句话原话是"GUI 程序不放到命令行程序中类似的位置"。`payload/` 根下只许有入口一个条目；入口名不许撞家骨架（`bin` / `lib` / `staging` / `state.json` / `log`）；家里已有别人的同名东西时拒绝、`--force` 才放行。下文凡出现 `lib/<id>_…` 的地方，都已按此改过（历史上 v3.5 是这样，理由见 DESIGN §0.3.6）。
>
> **v3.7 给账本改了名字**（用户裁决，DESIGN D36）：`<根>/<家目录名>-state.json`（`~/cot/cot-state.json`）。用户原话是"state.json 和 manifest 一样改名字"——manifest 那份附件用简称，账本这份用家目录名（一个家里可能装好几个应用，用简称会让同一家冒出好几本账）。旧 `state.json` 仍可读、写回时迁移，不需要手工搬；下文凡提账本的地方都已按此改过。
>
> **v3.8 定了三件事**（[`DESIGN.md`](DESIGN.md) D37/D38、§0.3.8）：① **工具链已经装好就跳过**——`<根>/bin/cot`（或 `tdp`）已经是文件时不再跑包里自带的那一份，`--force` 才重铺；跳过不影响 PATH 集成，"`~/cot` 已经好了但 `.profile` 没改好"就把 rc 补好（用户原话："本机已经安装好，并且修改了 .profile 之类的文件，就不用再安装；如果 ~/cot 之类的地方已经安装好了，但是 .profile 之类的文件没有修改，则修改好"）；② **失败回滚只回滚 GUI 那部分**，命令行部分（cot 等）不回滚（"说不定以前就安装好了"）；③ **账本写回之前比对原文**（不引锁文件）——命名解决的是归属与撞名，不解决并发写；被别人动过就让这次操作失败、提示重跑。

## 1. 背景与动机
我已经开发了cot_cli与tdp-cli，都是给程序员用的开发和管理工具。现在要给非开发人员准备带GUI的Desktop工具。
为了复用以前的能力。非开发人员安装程序时，可能自动安装cot/tdp。具体由配置文件决定。

## 2. 用户与场景
每个GUI程序会有一个简称，如AI Desk简称为ad, AI Code简称为ac，该简称在安装后能从terminal/cmd调用。

用gpm打包好的GUI应用，用户解压后，运行其中的install.cmd或者install.sh进行安装。

如果参数是cot,则可以调用`cot i -s ~/cot` 来自动化安装好命令行开发的工具；
并保证将激活cot的命令写到.profile, .zshrc, Windows .Profile等位置，如果不清楚，问我。
(会暴露出COT_HOME环境变量)
如果以前已经安装好了，则跳过此步。

如果参数是tdp,则可以调用`tdp i -s ~/tdp` 来自动化安装好命令行开发的工具；(先不测)

GUI的程序安装到~/cot目录下而非~/cot/bin下。

## 3. 功能需求

**安装根（术语）**：不再有 `GPM_HOME` 这个概念。安装根就是工具链自己的家，**按 `requires` 里"是哪一家"来取**：`requires: [cot]` → `${COT_HOME:-$HOME/cot}`；`requires: [tdp]` → `${TDP_HOME:-$HOME/tdp}`；两个都要时以 `COT_HOME` 作为**应用**的根（两个工具链各装各家）。解析顺序：`--dir` > 上面这条 > **从 gpm 自己的位置推断** > **`requires` 为空时：平台数据目录 + 简称**（macOS `~/Library/Application Support/<简称>`、Windows `%LOCALAPPDATA%\<简称>`、Linux `${XDG_DATA_HOME:-~/.local/share}/<简称>`）> 当前目录（用户裁决："requires 为空的，用简名安装，缺省安装到类似 local/appdata 之类的地方"）。打包方仍可用 `--default-dir` 把默认值烘进脚本——那是位子最明确的一条路。注意 `~/cot/bin/activate` 里的 `TDP_HOME=${COT_HOME}` 只是**兼容性**设置，不代表 tdp 的家就是 cot（C11）。

**F1 zip 输入契约（扩展 D22）**：包内文件名由 `manifest.yaml` 改为 `<简称>-manifest.yaml`（`ad-manifest.yaml`、`sag-manifest.yaml`）。`gpm pack` 生成该文件名并把它烘进 install.sh / install.cmd；`gpm install <目录>` 在目录里识别唯一的 `*-manifest.yaml`，多于一个则报错。

**F2 工具链依赖声明（新增字段）**：`<简称>-manifest.yaml` 新增 `requires:` 列表，取值限 `cot` / `tdp`，缺省为空 = 不装工具链，安装根退到平台数据目录 + 简称；**PATH 集成照做**（简称启动器要能在终端里敲，那是 §2 的原话）。**以字段为准**；install.sh 可接受 `--with cot[,tdp]` 作为覆盖（对应 §2"如果参数是 cot"的写法）。该字段同时决定安装根（见上）。

**F3 安装顺序与原子性**：顺序固定为 解析 manifest → 装工具链（F4/F5）→ 装 GUI 应用（F8）→ PATH 集成（F6）。工具链步骤失败则**不继续**，整体以非 0 退出，并回滚到运行前状态：不写 rc 块、不建启动器、不建软链、不记账本。**v3.8 明确回滚范围**（用户裁决）：只回滚 GUI 那部分；工具链自己已经写下的东西（`bin/cot`、`env-cot*` 等）一律留着——"说不定以前就安装好了"（[`DESIGN.md`](DESIGN.md) D34/D37、FR-25）。

**F4 工具链自举（zip 自带、离线）**：zip 自带对应平台的可执行文件（放 `<包>/tools/<os>_<arch>/{cot,tdp}`）。install.sh 按 `requires` 分派：`cot` → `cot i -s "${COT_HOME:-$HOME/cot}"`；`tdp` → `tdp i -s "${TDP_HOME:-$HOME/tdp}"`（Windows 用 `%COT_HOME%` / `%TDP_HOME%`）。gpm 不代为下载、不查询版本、不联网。落地的资产见 C3。

**F5 已装则跳过（v3.8 改写）**：判据就是"`<根>/bin/cot`（tdp 则 `<根>/bin/tdp`；Windows 加 `.exe`）在不在"——在就**不跑**包里自带的那一份，只打印一行"已经装好 …，跳过"，重复安装天然幂等；`--force` 才照包里那份重铺。**只查"在不在"，不做版本比较**（清单里没有版本约束字段，见 7-6）：原来"交给 `cot i -s` 自己比较版本"的方案（C3/C5）被用户裁决取代——"就不用再安装"。**跳过自举不等于跳过 PATH 集成**：rc 里没有 `# >>> gpm >>>` 块就补上（F6）。

**F6 PATH 集成（沿用 D26）**：把 `<根>/bin` 加入 PATH —— 该目录同时住着 `cot`、`tdp`、`<简称>` 启动器与 `gpm`。写之前仍必须用人话请求许可，拒绝则降级；沿用 `# >>> gpm >>>` 幂等块与 D26 的文件集合（macOS 按 `$SHELL` 写 `~/.zprofile` + `~/.zshrc` + `~/.profile`，Windows 写 `HKCU\Environment` 的 `Path`）。

**F7 启动器注入工具链环境**：`<根>/bin/<简称>` 在启动应用前注入工具链环境：按场景 export 自己的家变量（cot 场景 `COT_HOME=<根>`、tdp 场景 `TDP_HOME=<根>`，与 C11 一致 —— 不替 cot 造 `TDP_HOME=${COT_HOME}` 这个兼容值），把 `<根>/bin` 前置到 `PATH`，并在 `<根>/bin/env-cot.vars` 存在时 source 它（只含变量与 PATH，不执行插件 `cmd` 行，与 cot 自己的信任边界一致）。干净机器上该文件里没有插件变量，能保证的就是家变量与 PATH（C4、X2）。Linux 由 exec 继承；Windows 写用户级环境变量；**macOS 上只有"有工具链要注入"（即 `requires` 非空）的包才改用"直接 exec `.../Contents/MacOS/<binary>`"**（决策 ①，理由见 C1；代价是丢 LaunchServices 语义，见 DESIGN R13）；没有 `requires` 时不改道，`activate` 照旧 `exec open … --args`，双击语义不丢。这一条必须真机验证 AI Desk 是否有回归，有回归则升级为 R8 的合成 `.app` 外壳。v1 只保证**命令行启动器**这条路径注入环境，Finder 双击启动的实例不注入（见 X7）。

**F8 落地位置（细化 D21/D24/D35）**：payload 里那**一个**入口 → `<根>/<入口顶层名>`（macOS 就是 `<根>/AI Desk.app`，与 `bin/`、`lib/`、账本平级；v3.6 以前是 `<根>/lib/<id>_<version>_<os>_<arch>/`）；macOS 入口以 `.app` 落地；简称启动器 → `<根>/bin/<cmd>`；图形入口软链 → `~/Applications/<name>.app`。**不存在"GUI 程序装进 bin/"的形态**；`lib/<id>_<版本>_<平台>/` 那套命名留给命令行插件（cot 在用）。

**F9 归属与卸载边界（细化 D28）**：gpm 只回放自己写的东西（自己的入口——v3.6 起是 `<根>/<入口顶层名>`，v3.5 是 `lib/` 下自己的目录——、`bin/<cmd>`、软链、rc 里的 `# >>> gpm >>>` 块）。`<根>/bin/cot`、`env-cot*`、`lib/` 下 cot 自己的包都不在账本里，`gpm uninstall` 一律不碰；`~/cot` 目录本身也不删。

**F10 简称规则与重名检查**：简称必须三平台可用（`ac` 撞 macOS `/usr/sbin/ac`，见 C8 与 §7-4）；生成启动器前必须检查 `<根>/bin/<cmd>` 是否已被非 gpm 的文件占用（补 R4/O3），占用则报错，不静默覆盖；只有 `--force` 放行。**v3.6 起还要查入口本身**：`<根>/<入口顶层名>` 已被非 gpm 的东西占着时同样报错、`--force` 才放行；入口名撞家骨架（`bin` / `lib` / `staging` / `state.json` / `log`）或 `payload/` 根下多出一个条目，则连 `--force` 也拒（DESIGN D35、FR-23）。同一个家里这个命令名若已被账本里**别的包**占着，或 `<cmd>` 就是 `gpm` 自己，同样报错。

**F11 tdp 同机制**：`tdp i -s "<根>"`；本次不测。

## 4. 非功能需求

- **N1 离线**：全流程不联网（保持 DESIGN §0.5 的身份宣言）。体积预算：每平台额外 +cot ≈ 4 MB、+tdp ≈ 7 MB（未压缩）。
- **N2 幂等**：重复执行 install.sh，rc 文件、启动器、软链、账本结果不变。
- **N3 原子性**：任何一步失败都不留半成品（F3）。
- **N4 许可与降级**：改 shell 配置 / 注册表前必须请求许可（D26 不变），拒绝则降级并记账。
- **N5 不静默覆盖**：冲突必须报错（F10、D31）。
- **N6 可测**：每条功能需求在 §8 有可执行的验收。
- **N7 跨平台**：macOS / Linux / Windows × zsh / bash / fish / PowerShell / cmd。
- **N8 不越界**：不修改用户 rc 的其它内容；不删用户文件；不动 `<根>/lib` 里非 gpm 的目录。

## 5. 明确不做（非目标）

- **X1** 不做应用商店、不做自动更新、不做网络抓取（保持"没有一行网络代码"）。
- **X2** 不安装 cot 的插件（go / java / fzf 等，本机 49 个包）—— zip 只带 cot 自身，**干净机器上 `~/cot/lib` 保持空着**（v3.6 起 gpm 装的 GUI 应用也落在家的顶层，不占 `lib/`），工具链内容仍由用户自己 `cot install` 决定（用户决策）。
- **X3** 不管理 `<根>/lib` 下 cot 自己的包：不清理、不升级、不记账。
- **X4** 不引入第二个"家"：不新增 `GPM_HOME` 之类的变量，统一以 `COT_HOME` / `TDP_HOME` 为根（用户决策）。**去掉 `~/ad`**：新版不做兼容、不读旧账本、不迁移；旧安装用旧 gpm 自己回放清理（`~/ad/bin/gpm uninstall ai-desk`，它会把三份 rc 里的 `$HOME/ad/bin` 块与 `~/Applications/AI Desk.app` 软链一起收掉），然后删掉 `~/ad`；不接受直接 `rm -rf ~/ad` 留下悬空的 PATH 块。
- **X5** 不做 .dmg / NSIS / AppImage 分发，仍是 zip + 脚本。
- **X6** 不做强杀：应用在跑时拒绝安装/覆盖（D31）。
- **X7** v1 不保证 Finder/资源管理器双击启动的进程拿到工具链环境（要注入需要在 macOS 上加 R8 的合成 `.app` 外壳）。

## 6. 硬约束（已探明的事实，不可协商）

- **C1 macOS 的 `open` 不传递环境变量**（决定性，F7 的 ①）。GUI 应用由 launchd 启动，环境只有基线 15 项（`HOME` / `TMPDIR` / `PATH=/usr/bin:/bin:/usr/sbin:/sbin` …）。本机 macOS 27.0.1 实测：`open --env K=V`（usage 里确实列了这个选项）四种写法**全不生效**；直接 exec `.../Contents/MacOS/<binary>` 才继承调用者环境。
- **C2** 应用已在运行时 `open` 只激活旧实例 → 任何"启动时注入"对已存在的进程无效。
- **C3 `cot i -s <目录>` 的语义**（`rust/cot_cli/src/cmd/install/{mod,self_upgrade}.rs` 确认）：目录已存在 → `do_upgrade`（先比较版本）；不存在 → 建目录 + `install_self`。`install_self` = 把 `current_exe()` 拷成 `<目录>/bin/cot`，再把 9 个内嵌资产展开到 `<目录>/bin/`：`activate`、`activate.bat`、`activate.fish`、`activate.ps1`、`env-cot`、`env-cot.bat`、`env-cot.fish`、`env-cot.ps1`、`log4rs.yml`；其中 8 个（PROCESS_NEEDED）里的 `ToBeDefined` 被替换成 `<目录>` 的绝对路径。**全程零网络。**
- **C4** `cot i -s` 只装 cot 自身 + activate 框架，**不装任何插件**。
- **C5** 已装版本 ≥ 自带版本时 `do_upgrade` 打印 already-latest 并返回 Ok（exit 0）；`--force` 才覆盖。
- **C6** Windows 上把 exe 拷进 `%COT_HOME%\bin\cot.exe` 时 `CopyFileEx` 可能被拒（源码注释记录实测 `os error 5`），实现必须走"临时文件 + 改名"回退。
- **C7 根就是 `$COT_HOME` / `$TDP_HOME`**：因此 `<根>/bin` 就是 `~/cot/bin`，里面同时住着 `cot`、`tdp`、`gpm`（D29 自装）与 `<简称>` 启动器；PATH 只需一条；`RootFromSelf()` 的三条判据（目录叫 bin、文件叫 gpm、上一级有账本——v3.7 的 `<家目录名>-state.json`，v3.6 及以前的 `state.json` 也认）在此仍然成立。
- **C8** `ac` 已被占用：`/usr/sbin/ac`（macOS 登录记账）。
- **C9** 账本字段固定为 `schemaVersion` / `self` / `packages[…]`，卸载靠它回放（D28）；**v3.7 起它落在 `<根>/<家目录名>-state.json`**（`~/cot/cot-state.json`、`~/Library/Application Support/ad/ad-state.json`）。v3.6 及以前的 `<根>/state.json` 仍可读，写回时迁到新名字并删掉旧文件（D36）。**v3.8 起写回之前会比对这次读进来的原文**：被别人改过 / 挪走 / 期间被别人建出来就拒绝写、让本次操作失败（D38、R16）。
- **C10** 不得依赖网络（N1）。
- **C11** 本机 `~/cot/bin/activate` 里 `TDP_HOME=${COT_HOME}` 是**兼容性**设置（用户明确），不是"tdp 的家"：cot 场景认 `COT_HOME`、tdp 场景认 `TDP_HOME`（默认 `~/tdp`），启动器注入环境时照此办理（F7）。

## 7. 待确认

> 已定（用户裁决）：cot 场景认 `COT_HOME`、tdp 场景认 `TDP_HOME`（C11）；`~/cot/lib` 保持空着、不多装插件（X2）；去掉 `~/ad`（X4）；macOS 注入走直接 exec（F7）；**`requires` 为空时用简称、缺省装到平台数据目录**（7-1）；**`ac` 只是举例、重名一律按 F10 拒绝**（7-4）；**v3.8 把原来的三条待确认也定了**：7-2（已装好判据，F5/D37）、7-3（回滚范围，F3/D37）、7-5 的加锁部分（不做锁，改原文比对，D38）；7-6 是新增的待确认（清单要不要能表达版本约束）。前两条已实现并验收（A15、A16）。

- ~~**7-1** `requires` 为空的 GUI 应用装到哪个根？~~ **已定（用户）**：用简称、缺省装到平台数据目录（macOS `~/Library/Application Support/<简称>`、Windows `%LOCALAPPDATA%\<简称>`、Linux `${XDG_DATA_HOME:-~/.local/share}/<简称>`）；这时 gpm 是那个目录的建立者，装失败要把它收回去（`home.DropIfEmpty()`）。见 DESIGN D21、§2.3。
- ~~**7-2（阻塞 F5 验收）**"已装好则跳过"的判定~~ **已定（用户）**：`<根>/bin/cot`（Windows `cot.exe`）在就跳过自举、`--force` 才重铺；不发明版本判据（F5、[`DESIGN.md`](DESIGN.md) D37、O14 关闭）。用户原话："本机已经安装好，并且修改了 .profile 之类的文件，就不用再安装"。
- ~~**7-3**"不继续"时回滚的范围~~ **已定（用户）**：GUI 部分全部回滚，命令行部分（cot 等）不回滚；工具链写下的东西留着，只有本次新建的空家才收回去（F3、[`DESIGN.md`](DESIGN.md) D34/D37、O15 关闭）。用户原话："GUI 部分全部回滚，但是命令行部分如 cot，不回滚，说不定以前就安装好了"。
- ~~**7-4** 撞名简称（`ac`）怎么处理：拒绝安装 / 自动改名 / 仅警告？~~ **已定（用户）**：`ac` 只是举例、不单独裁决；重名一律报错拒绝、`--force` 才覆盖（F10，DESIGN FR-21、R4、O3 关闭）。
- ~~**7-5** `<根>/state.json` 与 cot 共享 `~/cot` 目录：是否需要给账本改名或加锁？~~ **两条都已定**：改名（v3.7、D36）账本按家命名 `<根>/<家目录名>-state.json`，不再用放之四海皆可的 `state.json`；**加锁不做**（v3.8、D38）——命名解决归属与撞名、不解决并发写，改成"写回前比对原文、被别人动过就失败重跑"（[`DESIGN.md`](DESIGN.md) R16）。用户问的原话："State.json 还需要考虑加锁吗？靠命名的改变是不是已经解决了这个问题"。
- **7-6（新增）** 清单要不要能表达**版本约束**（比如 `requires: {cot: ">=2.1"}`）：现在只有"在不在"这一条判据，已经装在家里的 cot 比包里旧也不会被换掉（F5、D37）。真要"至少 2.1"就得给清单加字段，同时定"谁来判断版本"——gpm 去解析 `cot --version`，还是交给 `cot i -s` 自己比较（[`DESIGN.md`](DESIGN.md) O17）。

## 8. 验收标准

- **A1** 干净机器 + 断网 + `./install.sh`：`command -v cot` 有输出；`~/cot/bin/cot --version` 可运行；`command -v ad` 有输出；`~/Applications/AI Desk.app` 存在；**`~/cot/AI Desk.app` 存在**（v3.6；不再有 `~/cot/lib/ai-desk_0.2.0_darwin_arm64/`）。
- **A2** `~/.zprofile`、`~/.zshrc`、`~/.profile` 各恰有一个 `# >>> gpm >>>` 块，块内含 `$HOME/cot/bin`（或等价绝对路径）。
- **A3** 幂等：连跑两次 install.sh，上述三个 rc 文件、`~/cot/bin/ad`、软链、账本（`~/cot/cot-state.json`）的 packages 段不变。
- **A4** `gpm list` 含 ai-desk；账本（`<根>/<家目录名>-state.json`）entry 指向 `.app`、`cmd=ad`、`verified=true`。
- **A18** 账本改名与迁移（v3.7）：装完账本是 `<根>/<家目录名>-state.json`；把账本手工改回旧名 `state.json` 后 `gpm list` 仍认得这个家、也读得到里面的包，再 `gpm uninstall` 一次 → 新名字写出来、旧文件消失（`internal/ledger/ledger_test.go` 四条用例 + 真机验证见 DESIGN §3）。
- **A5** 跳过（v3.8 改写）：预置一个可用的 `~/cot/bin/cot`（含 `env-cot` 等资产）后再装：输出含"已经装好 cot（…），跳过。"；包里那份假 cot **没有被调用**（标记文件不出现）；`~/cot/bin/cot` 的内容与 mtime 不变；GUI 那部分照常装 / 覆盖；加 `--force` 才重铺工具链（`internal/install/toolchain_test.go` 三条用例，真机四场景见 DESIGN §3）。
- **A6** 原子性：把 zip 里的 cot 换成不可执行的坏文件后跑 install.sh：退出码非 0；`~/cot/bin/ad`、`~/Applications/AI Desk.app`、rc 里的 `# >>> gpm >>>` 块、账本里的该包**都不存在**。
- **A7** 许可降级：对 PATH 集成回答"拒绝"：安装成功；rc 文件字节不变；`gpm list` 有该包；安装输出里给出了手动 `export PATH=…` 的办法（`gpm env` 也能打印同一行）。
- **A8** 卸载：`gpm uninstall ai-desk` 后 `<根>/AI Desk.app`（v3.5 装的老包则是 `<根>/lib/ai-desk_*`）、`<根>/bin/ad`、`~/Applications/AI Desk.app`、rc 块全部消失/还原；`~/cot/bin/cot`、`env-cot*`、`lib/` 下 cot 的包仍在；`command -v ad` 找不到。
- **A9** 重名：先手工放一个非 gpm 的 `~/cot/bin/ad`，安装必须报错且不覆盖该文件。
- **A10** 简称冲突：拿一个已被账本里别的包占着的 `<cmd>` 再装一个包 → 报错、账本不变；`--force` 才覆盖（`internal/install` 的 `TestInstallRefusesCmdOwnedByAnotherPackage`）。`ac` 本身不再单独裁决（只是举例）。
- **A11** `requires` 为空的包：即使 zip 里带了 cot 也不跑它、`~/cot` 一个字节都不新增；安装落在平台数据目录 + 简称（`~/Library/Application Support/ad`），PATH 块指向那里的 `bin/`，`command -v ad` 能找到。
- **A12** 环境注入：从命令行执行 `ad` 启动后，AI Desk 进程的环境里能看到 `COT_HOME`（macOS 按决策 ① 验收；同时记录 Finder 双击启动时不注入 —— X7）。
- **A13** 干净机器上装完，`~/cot/lib` 里**没有**任何 cot 插件目录，也**没有** `ai-desk_*`（v3.6：GUI 应用落在家目录顶层）；`cot list` 为空。
- **A14** 清理旧 `~/ad` 后：三份 rc 里不再有 `$HOME/ad/bin` 的 gpm 块，`~/ad` 目录被删除，`~/ad/state.json` 不存在；若已在 `~/cot` 下重装，`command -v ad` 仍能通过新启动器找到。
  - 本机已于 2026-10-09 执行完毕：`~/ad/bin/gpm uninstall ai-desk`（旧 v34 回放）→ `rm -rf ~/ad`。实测输出为 6 条删除 + 3 条"已摘除 PATH 标记块"，`~/Applications/AI Desk.app` 软链一并消失；三份 rc 里 gpm 行数归 0，`~/.zshrc:141` 摘块留下的 3 个连续空行已收敛为 1；`alias c` 与 token 导出未受影响。
- **A17** 入口落点与三道闸（v3.6）：装完 `<根>/AI Desk.app` 存在且 `<根>/lib` 下没有 `ai-desk_*`；`payload/` 根下多放一个文件 → 安装报错且不留痕；家里先手工放一个非 gpm 的 `~/cot/ad` → 报错、`--force` 才覆盖；`entry` 名字取 `bin` / `lib` / `staging` / 账本名（`cot-state.json`，以及还没迁移的旧名 `state.json`）→ 即使 `--force` 也拒（`internal/install/layout_test.go` 四条用例）。
- **A19** 工具链已装好但 rc 没改（v3.8）：手工预放 `~/cot/bin/cot`、三份 rc 里都没有 gpm 块 → 装完打印"已经装好 …，跳过"，**而三份 rc 各补上一个 `# >>> gpm >>>` 块**，`command -v ad` 能找到（真机 C 场景，DESIGN §3）。
- **A20** `--force` 重铺工具链（v3.8）：同样预置后再装并加 `--force` → 包里那份假 cot 被调用（标记文件出现）。
- **A21** 回滚范围（v3.8）：让工具链成功、后面的 GUI 步骤失败（家里预放别人的同名入口）→ 整次安装非 0 退出、账本里没有该包、没有启动器，但 `~/cot/bin/cot` 与别人放的东西都还在（`TestInstallKeepsToolchainWhenGuiPartFails`）。
- **A22** 账本并发写（v3.8）：`gpm list` 读完账本之后、`gpm install` 写账本之前手工改一次账本 → 安装失败、提示"请重跑一次"，磁盘上账本仍是别人那份（`internal/ledger/ledger_test.go` 的 `TestSaveRefusesWhenLedgerChangedUnderneath` / `TestSaveRefusesWhenLedgerAppeared`）。
