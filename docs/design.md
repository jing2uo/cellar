# cellar 设计

Wine 应用管理器。能力对标 Bottles，但以**声明式配置**为唯一真相，让人、GUI、AI 改的是同一份文件；
同一套引擎既管本地 prefix，也是 FlatPark 发布的 wine 应用包里的启动器。

## 形态

一份 Go 代码，按 build tag 出两种二进制（Magpie 的做法）：

| 构建 | 内容 | 用途 |
|---|---|---|
| 默认（`!nogui`） | GUI（Wails v3, GTK4/WebKitGTK 6）+ CLI | 管理器，FlatPark 发布为 flatpak |
| `-tags nogui`, `CGO_ENABLED=0` | 纯静态 CLI | 打进每个 wine 应用包当启动器；本地脚本/AI 调用 |

两种二进制共享：schema、配置集、调和引擎、运行与日志格式。发布出去的应用**不依赖 GUI**。

## 两种运行模式

| | 本地模式 | 打包模式 |
|---|---|---|
| 判定 | 默认 | 存在 `/app/share/cellar/cellar.toml` |
| 配置 | `<data>/prefixes/<name>/cellar.toml`（可写） | `/app/share/cellar/cellar.toml`（只读）+ `$XDG_CONFIG_HOME/cellar/override.toml` |
| prefix | `<data>/prefixes/<name>/pfx` | `$XDG_DATA_HOME/cellar/pfx` |
| runner | `<data>/runners/<name>/` 或 `system` | `system`（base app 的 wine） |

`<data>` = `$CELLAR_HOME`，缺省 `$XDG_DATA_HOME/cellar`。

每个 prefix 目录：

```
cellar.toml   声明（真相）
pfx/          WINEPREFIX
state.json    上次 apply 的结果：配置哈希、runner、cellar 管理的注册表值清单
runs/<ts>/    meta.json + wine.log，每次 run 一份，保留最近 20 份
```

## 两种安装环境与 runner

GUI 既能宿主直接安装，也能作为 Flatpak 运行；同一份二进制按 `/.flatpak-info` 判断环境。

| | 宿主安装 | Flatpak（FlatPark 发布） |
|---|---|---|
| `system` runner | 依次找 PATH 上的 wine、`/opt/wine-{staging,devel,stable}`、`/usr` | base app `org.winehq.Wine//stable-26.08` 的 `/app/bin/wine`，**硬依赖，必在** |
| 没有 `system` 时 | 提示装发行版的 wine，或下载社区构建（GUI 一键下载推荐版） | 不会发生（构建不完整才会） |
| 社区构建 | `<data>/runners/`，`cellar runner available / install <source[@ver]>` | 同左，落在应用数据目录 |
| 传统（非 WoW64）构建的前提 | 宿主开 i386 multiarch | 装 `Compat.i386` + `GL32` 扩展（manifest 声明为 no-autodownload） |

- 新建 prefix 默认用 `system`，没有就用最新下载的；`cellar.toml` 的 `runner` 可任选，也可以是绝对目录。
- runner 分 `wow64` / `legacy`（看有没有 `i386-unix` 等 32 位 unix 侧），legacy 缺 32 位 loader
  （`/lib/ld-linux.so.2` 解析不到）时在列表和下载页给出对应环境的修法。
- 下载目录参照 Bottles：Kron4ek（vanilla / staging / staging-tkg，WoW64 与传统版）、bottlesdevs 的
  soda / caffe / vaniglia。Proton 系（GE-Proton 等）等接入 umu 再加。
- GitHub API 匿名配额按 IP 计（共享代理常被耗尽）：先用 `GITHUB_TOKEN` / `GH_TOKEN` / `gh auth token`，
  失败退到 `releases.atom`（最近 10 个 release，按 tag 规则拼 asset，无大小信息），单个仓库失败只记警告。

## FlatPark wine 包（原型：packaging/examples/7zip）

- runtime `org.freedesktop.Platform//26.08` + base `org.winehq.Wine//stable-26.08`，启动器是 nogui 静态 cellar
  （`command: cellar`），配置在 `/app/share/cellar/cellar.toml`（只读），用户覆盖在 `~/.var/app/<id>/config/cellar/override.toml`。
- 厂商安装包原样作为 extra-data，`[install]` 声明预制安装动作：
  - `extract`：`apply_extra` 执行 `cellar extract`，用 base 自带的 7z 离线解到 `/app/extra/cellar-install`，删掉原文件；
    运行时每个用户的 prefix 把 `target`（如 `C:\Program Files\7-Zip`）软链过去。程序文件只读、共享，包更新原地替换，链接不变。
    **只适合不往自己安装目录写数据的程序**（通达信这类把数据写进程序目录的要用 run）。
  - `run`：首次启动在用户 prefix 里用 `args`（静默参数）跑安装程序；按安装包内容指纹（sha256，Flatpak 部署文件 mtime 固定）
    判断，包更新换了安装包就重跑。安装过程同样记录在 runs/。
- 每次启动都 plan 一次（约 0.1s），有变更才 apply：新版 cellar 或包更新改变了期望状态、链接/字体/盘符漂移都会被修正。
- desktop：由 Flatpak 在构建期导出（`Exec=cellar %f`，Flatpak 自动改写成 `flatpak run --command=cellar --file-forwarding`），
  打包模式下 cellar 拒绝写 `~/.local/share/applications`；winemenubuilder 在 base 配置集里禁用。
  `cellar [files...]`（非子命令）= 用这些文件运行默认应用；宿主路径经已有盘符（如 `H: = ~`）或 `Y:` 转成 Windows 路径。
- 默认带 `cjk` 配置集（只管 CJK 字体，不设区域）：界面语言跟随用户自己的 LANG，`flatpak override --env=LANG=zh_CN.UTF-8 <id>` 切中文。
- 本地测试：bundle 装不了 extra-data 应用（"Extra data missing in detached metadata"），要把构建出的 repo 加成
  `--no-gpg-verify` 的 user remote 再装。

## GUI 安装流程（对标 Bottles）

1. **新建 prefix**：默认 runner、默认 `zh-cn` 配置集（中文区域 + Noto CJK 字体复制、登记、替换），创建后立即后台 apply。
2. **设置**：Windows 版本、虚拟桌面、DPI、DLL 覆盖、依赖（winetricks verb，常用的有中文说明），
   保存只改 `cellar.toml` 里对应的行（注释保留），然后后台 apply；进度和日志在任务面板。
3. **运行安装程序**：系统文件选择框选 .exe / .msi（msi 走 `msiexec /i`）。
4. **添加应用**：扫描开始菜单 `.lnk`（解析 MS-SHLLINK）和 drive_c 里的 exe（含 `C:\zd_xxx` 这种自定义目录；
   同层按体积排序，主程序通常最大），也可以在应用内浏览 drive_c 选择；添加时从 exe 提取图标（PNG 或 1–32 位 DIB），
   写入 `[[apps]]` 并生成菜单快捷方式。
5. 以上每一步都有对应 CLI：`new` / `apply` / `run --exe` / `app discover|add|rm` / `desktop`，GUI 与 AI 走同一套代码。

- **没有 Z: 时 wine 无法按 unix 路径启动程序**（直接段错误）。`run` 把宿主路径转成 Windows 路径：drive_c 内转 `C:\`，
  已有盘符覆盖的用该盘符，否则把专用的 `Y:` 指到文件所在目录（整个目录，多文件安装包才能找到同目录的数据）。
- 安装状态以 prefix 里 winetricks 自己写的 `winetricks.log` 为准；Flatpak 的 Wine base 自带 winetricks / cabextract / 7z，
  宿主没有 winetricks 时下载一份到 `<data>/tools/`。

## 配置合并

`内置 base 配置集` → `profiles` 里列的配置集（按序）→ `cellar.toml` → `override.toml`。
表做深合并，`apps` 按 `id` 合并，后者覆盖前者。`cellar inspect` 输出合并后的生效配置。

## 调和（plan / apply）

声明 → 期望状态（注册表值、字体链接、盘符），与 `state.json` 记录的「上次由 cellar 写入的」对比：

- 新增或变化的值：写入；
- 上次由 cellar 写入、这次不再声明的值：删除；
- 用户或程序自己写的注册表值：cellar 不碰（只管自己声明过的）。

prefix 不存在则 `wineboot -i`；runner 变了则 `wineboot -u`。
`run` 前若配置哈希与 `state.json` 不一致，自动 apply —— 所以改完配置直接 run 即可，
打包模式下应用更新后也会自动重新调和。

## 给 AI 的接口

所有命令支持 `--json`。工作流：改 TOML → `plan` → `apply` → `run` → `logs` / `doctor`。
GUI 只是这套 CLI 的另一个前端，GUI 监听配置文件，外部修改实时反映。

## 实测结论（2026-10-10 首轮验证）

- **runner 不需要宿主 i386。** new-WoW64 构建（Kron4ek `-wow64`、Flathub Wine stable-26.08）的 unix 侧只有
  x86_64；宿主未开 i386 时 64/32 位 PE 都能跑。但 wine 不是自包含的：gnutls、freetype、GL/vulkan、
  pulse、X11 等约 20 个库靠宿主或 runtime 提供 —— 这是 GUI 走 flatpak 的理由。
- **flatpak 里跑 32 位 PE 必须 `--allow=multiarch`。** 不需要 Compat.i386 的库，但不开它 Flatpak 的 seccomp
  会让每个 32 位程序在 `NtCreateUserProcess` 失败（`--allow=devel` 无效）。FlatPark 的 wine 应用包同理。
- **Wine 走 X11 驱动，finish-args 要 `--socket=x11`**，不能只给 fallback-x11。
- **字体复制进 prefix，不软链。** 宿主是 `/usr/share/fonts`，沙箱是 `/run/host/fonts`，软链会让同一 prefix
  在两边来回翻转。按大小判同（btrfs 上 `io.Copy` 走 copy_file_range，即 reflink）。
- **字体解析用 `fc-list`，不用 `fc-match`。** Debian 的 fontconfig 规则会把 "Noto Sans CJK SC" 别名到
  "Noto Sans"，fc-match 跟着走。
- **Wails v3 + GTK4/WebKitGTK 6 在 GNOME 51 flatpak 里正常**：设 `Linux.ApplicationID` = flatpak id 后无
  SIGABRT，中文由 WebKit 走 fontconfig 直接正确。
- **GNOME 51 runtime + `base: org.winehq.Wine//stable-26.08` 可行**（与 Bottles 同配）：沙箱里 `system` = wine-11.0
  WoW64，64/32 位都过。传统构建（Kron4ek staging 非 wow64）在装了 Compat.i386 时也过，**不需要**
  Bottles manifest 里那条 `LD_LIBRARY_PATH`（runtime 已把 `/lib/i386-linux-gnu` 链到 `/app/lib/i386-linux-gnu`）。
- 判断 32 位支持不能看 `/usr/lib/i386-linux-gnu` 非空：它是到 `/app/lib/i386-linux-gnu` 的链接，manifest
  建的 `GL` 挂载点总在。看 `/lib/ld-linux.so.2` 能否解析。
- **控制台程序输出是 OEM 代码页**（zh_CN 下 GBK），原始日志不改，`logs` 显示时非 UTF-8 行按 GB18030 解码。

## 构建

- `make cli`：宿主直接编，`CGO_ENABLED=0 -tags nogui`，纯静态。
- `make image && make gui`：GUI 在 podman 里编（debian:sid + GTK4/WebKit6 -dev），宿主 Go 工具链只读挂进去。
- 本地 flatpak：`flatpak run org.flatpak.Builder --repo=.cache/repo .cache/flatpak-build packaging/flatpak/org.flatpark.Cellar.yml`。
