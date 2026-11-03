# FLClash TUI

一个功能完整的 **mihomo (Clash.Meta) 终端客户端**。基于对 [FLClash](https://github.com/chen08209/FLClash) 源码的研究构建：复刻了它的订阅（Profile）数据模型、配置生成/覆写管线和全部页面能力，以纯终端形式呈现，编译为单个二进制文件。

**内核已内置**（8 个平台），也提供**覆盖全部功能的非交互式 CLI**——同一个二进制，无参数是 TUI，带参数就是脚本友好的命令行工具。

```
FLClash TUI (v1.19.13)   1仪表盘  2代理  3订阅  4连接  5规则  6日志  7设置    规则 · 内核运行中
╭────────────────────────────────────────────────────────╮╭─────────────────────────────────────────────╮
│ 状态                                                   ││ 代理                                        │
│  内核         内核运行中                               ││  模式         规则                          │
│  版本         v1.19.13                                 ││  混合端口     7890                          │
│  运行时长     1h2m3s                                   ││  系统代理     开启                          │
│  控制器       127.0.0.1:9090                           ││  TUN          关闭                          │
╰────────────────────────────────────────────────────────╯╰─────────────────────────────────────────────╯
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ 实时流量                                                                                                 │
│  ↓ 2.3 MB/s        内存: 48.2 MB                                                                        │
│  ↑ 156.0 KB/s      连接: 37                                                                             │
│ ↓ ▁▁▂▂▃▅▆▇█▇▆▅▃▂▂▁▁▁▂▃▃▄▅▆▇██▇▆▄▃▂                                                                     │
│ ↑ ▁▁▁▁▁▂▁▁▁▂▁▁▁▁▁▁▁▁▁▁▁▂▃▂▁▁▁▁▁▁▁▁▁                                                                    │
│ 本次运行累计 ↑1.2 GB ↓15.8 GB                                                                          │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────╯
                                                                          ℹ 已切换 GLOBAL → 香港02
                                                                          ✓ 模式已切换: 全局
                                                                          ✗ 添加订阅: 经代理重试仍失败(HTTP 502)
 [m]切换模式 [r]重启内核 [c]关闭全部连接 [f]清空FakeIP  全局: 1-7换页 · ?帮助 · Q退出
```

右下角事件通知栈（操作结果 + 内核重要错误日志，可在设置中关闭）。手写配置编辑器（订阅页按 `n` 新建 / `e` 编辑源）——粘贴 JSON，保存即转为规范 YAML：

```
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│  编辑 · 手写JSON   保存后自动应用                                                                                │
│ ┃   1 proxies:                                                                                                   │
│ ┃   2     - name: JP-01                                                                                          │
│ ┃   3       type: ss                                                                                             │
│ ┃   5       port: 8388                                                                                           │
│ ┃   8 rules:                                                                                                     │
│ ┃   9     - DOMAIN-SUFFIX,example.net,JP-01                                                                      │
│ ┃  10     - DOMAIN-SUFFIX,example.org,DIRECT                                                                     │
│ ┃  12     - MATCH,JP-01                                                                                          │
│  ctrl+s 保存并应用  esc 取消   支持 YAML / JSON                                                                  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

## 功能

| 页面 | 能力 |
|---|---|
| **1 仪表盘** | 实时上下行速率图（WebSocket 流）、内存、连接数、内核版本/运行时长、累计流量、订阅流量余量；快捷操作：切模式、重启内核、关闭全部连接、清空 FakeIP |
| **2 代理** | 策略组 + 节点双栏视图；节点切换（Selector/Fallback）；单节点测速（`t`）、整组并发测速（`T`）；按延迟排序；Providers 视图（更新 / 健康检查） |
| **3 订阅** | URL 订阅 / 本地 YAML 导入 / **手写配置**；自动解析 `subscription-userinfo`（流量）与 `content-disposition`/`filename*`（订阅名）；一键更新 / 全部更新 / 定时自动更新；激活即重载内核；流量进度条与到期时间 |
| **4 连接** | 实时连接表（主机/规则/链路/速率/累计流量/进程）；按主机、进程、规则、链路过滤；关闭单个 / 全部连接；按时间或流量排序 |
| **5 规则** | 全量规则浏览 + 关键词过滤 |
| **6 日志** | 内核日志实时流；级别过滤（info/warning/error）、内容过滤、暂停跟随、清空 |
| **7 设置** | 内核路径、控制器地址/Secret、端口、模式、日志等级、TUN（栈）、IPv6、统一延迟、TCP 并发、测速 URL/超时、订阅自动更新、系统代理（macOS）；操作：应用并重启、更新 Geo 数据库、清空 FakeIP、在线升级内核 |

附加能力：

- **手写配置 / 直接编辑**：不依赖订阅，`n` 在全屏编辑器里直接编写配置；**可以整体粘贴 JSON**，保存时自动规范化为块状 YAML（保留注释与键序，引号仅按语义保留）；`e` 随时编辑当前订阅的原始配置，保存后若是激活订阅会自动重载内核。非法 YAML/JSON 会被拒绝并保持编辑器打开。
- **右下角事件通知栈**：操作结果与内核重要错误日志在右下角一列堆叠显示（错误停留更久），可在设置中关闭。
- **订阅拉取失败自动走代理**：直连失败（网络错误/非 200）时自动经专用代理端口重试——配置生成时会给内核添加一个绑定到当前主出口（MATCH 出口组）的 `listeners` 入站，重试请求强制走"正在使用的代理"，不受规则影响（与 Clash Verge Rev 的"使用 Clash 代理更新"同类能力，但路由更精准）。可在设置中关闭。
- **两种内核模式**：`managed`（TUI 托管 mihomo 生命周期，退出自动清理）与 `attach`（连接任意已运行的 external controller，例如别的机器或已开的 FLClash）。
- **FLClash 同款配置管线**：订阅原始 YAML + 应用设置 → 强制 patch（端口/TUN `dns-hijack any:53` + `auto-route`、DNS 基线 fake-ip + DoH、`store-selected: false`、关闭多余 controller 形态）→ 写入内核工作目录。
- 选择状态由 TUI 自行持久化（不依赖内核 cache），切换订阅/重启不丢失。
- 全局快捷键、鼠标滚轮、事件通知、确认弹窗、帮助浮层（`?`）。
- **内置 mihomo 内核**：二进制里直接打包了 8 个平台的 mihomo（darwin/arm64+amd64、linux/amd64+arm64+armv7+386、windows/amd64+arm64），首次运行自动释放到数据目录并校验 sha256，不需要单独下载内核。内核资产不进 git 仓库，构建时按 `assets.lock.json` 拉取并校验。
- **全功能非交互式 CLI**：所有 TUI 能力都有对应子命令（见下），统一 `--json` 输出与退出码，可直接写进脚本 / CI / cron。

## 安装 / 构建

需要 Go 1.22+ 与 Python 3（拉取内核资产用）：

```bash
git clone https://github.com/yuuuuukiu/flclash-tui && cd flclash-tui
make build        # = 拉取内核资产 + 编译
./flclash-tui
```

或者不用 make：

```bash
python3 internal/coreasset/gen.py    # 先拉取内核资产（必需，否则编译会缺 assets/）
go build -trimpath -ldflags "-s -w" -o flclash-tui .
```

**内核已内置，但不进仓库**：每个平台的 mihomo 约 11MB，8 个平台共 85MB；如果提交到
git，每次升级内核都会在历史里再留一份。所以改为**构建期拉取**：

- `internal/coreasset/assets.lock.json` 提交到仓库，用 sha256 钉死每个平台的上游包与内核二进制
- `python3 internal/coreasset/gen.py` 按 lock 下载并校验，生成 `assets/*.gz` 与 `asset_<os>_<arch>.go`
- 目录已被 `.gitignore` 忽略；上游包缓存在 `$TMPDIR/coreassets`，重复构建不重复下载

这两级校验都是围着**上游原始文件**和**解压后的内核**做的，不依赖重新压缩后的字节：

```bash
python3 internal/coreasset/gen.py --verify   # 只校验（CI 用）
python3 internal/coreasset/gen.py --clean    # 清掉生成的资产
```

首次运行会把内核释放到 `core/bin/mihomo`，用内置 `embeddedSum` 校验，已存在且一致就跳过；
文件被损坏或版本不符时会自动重新释放。

未打包的平台（如 freebsd）会编译成 `coreasset.Available() == false`，此时回退到 PATH 里的
mihomo，也可在设置页/`settings set corePath` 手动指定。

### 常用 make 目标

```bash
make build          # 拉资产 + 编译
make test           # go test
make test-all       # 单测 + 两套 e2e
make release-local  # 交叉编译 8 个平台到 dist/
make clean          # 清掉产物与内核资产
```

### 发布

推送 `v*` 形式的 tag 会自动跑测试、交叉编译 8 个平台并创建 GitHub Release
（`.github/workflows/release.yml`）：

```bash
git tag v1.20.0 && git push origin v1.20.0
```

Release 里每个产物都自带目标平台的内核，另附 `checksums.txt`。工作流会缓存内核上游包，
并校验产物 >15MB 且 `go list -f '{{.EmbedFiles}}'` 确实指向本平台的资产，避免发出没带内核的包。

### 升级内核版本

```bash
# 1. 改 lock 里所有 version/upstream/source，以及 internal/coreasset/coreasset.go 的 Version
# 2. 重新算哈希并重建资产
python3 internal/coreasset/gen.py --clean
python3 internal/coreasset/gen.py
make test-all
```

## 快速开始

```bash
./flclash-tui
```

1. 首次运行会在 `~/.config/flclash-tui/` 生成目录结构，并从程序内释放内置 mihomo 内核；
2. 按 `3` 进入订阅页：按 `a` 粘贴订阅链接、`f` 导入本地 YAML，或按 `n` 直接手写配置（可整体粘贴 JSON，自动转为 YAML）；
3. 回车激活订阅，内核自动重载生效；
4. 按 `2` 选择节点，`T` 测速；
5. 系统代理：设置页选择「系统代理」回车切换（macOS 走 networksetup）。

不想用 TUI？同样的能力全都能用子命令完成，见下面的[非交互式调用](#非交互式调用cli)。

数据目录：

```
~/.config/flclash-tui/
├── settings.json    应用设置
├── profiles.json    订阅元数据
├── profiles/        订阅原始 YAML（与 FLClash 相同的存储方式）
└── core/            mihomo 工作目录（config.yaml、geo 数据）
    ├── bin/mihomo   从程序内释放的内核
    ├── config.yaml  生成的最终配置
    └── core.pid     后台内核的 pid（core start --daemon 时）
```

可用 `FLCLASH_TUI_HOME` 环境变量重定向数据目录。

## 快捷键

**全局**：`1-7` 换页 · `Tab`/`Shift+Tab` 循环 · `m` 切模式 · `?` 帮助 · `q`/`Q` 退出 · `j/k` 或方向键移动 · `g/G` 首尾

| 页面 | 按键 |
|---|---|
| 代理 | `enter` 选节点 · `t`/`T` 测速 · `s` 延迟排序 · `p` Providers · `r` 刷新 |
| 订阅 | `a` 添加 · `n` 手写配置（YAML/JSON） · `e` 编辑源 · `f` 导入文件 · `enter` 激活 · `u`/`U` 更新 · `d` 删除 · `r` 重命名 |
| 连接 | `/` 过滤 · `x` 关闭 · `X` 关闭全部 · `s` 切排序 |
| 规则/日志 | `/` 过滤 · 日志页 `l` 级别 · 空格 暂停跟随 · `c` 清空 |
| 设置 | `enter` 编辑/切换/执行 · `s` 保存 · `R` 应用并重启 |

## 非交互式调用（CLI）

带上任何参数就不会启动 TUI，而是执行对应子命令。全部子命令都支持 `--json`、`--quiet`
与 `--home <dir>`；退出码：`0` 成功 / `1` 运行时错误 / `2` 用法错误 / `3` 控制器不可达。

```bash
flclash-tui help                   # 全部子命令
flclash-tui help profile           # 单个子命令的用法
```

| 子命令 | 作用 |
|---|---|
| `core start\|stop\|restart\|status\|logs\|path\|upgrade` | 内核生命周期；`start --daemon` 后台常驻、`logs -f` 跟踪日志 |
| `profile list\|add\|create\|edit\|set\|show\|update\|activate\|rename\|remove` | 订阅管理 |
| `proxy groups\|list\|select\|delay\|provider` | 策略组 / 节点切换 / 测速 / Provider |
| `conn list\|close\|close-all` | 连接表与断连 |
| `rule list\|update` | 规则浏览 / rule-provider 更新 |
| `log tail` | 内核日志流（`--level` `--filter` `--duration`） |
| `config show\|path\|mode\|patch\|reload\|flush-fakeip\|update-geo` | 运行中内核的配置 |
| `settings list\|get\|set\|reset\|apply` | 应用设置（`--apply` 立即生效） |
| `sysproxy status\|on\|off` | 系统代理（macOS） |
| `version` | 版本 / 内置内核信息 |

### 手写配置：直接进 vim / vi

`profile edit` 会把配置写到临时 `.yaml` 文件，然后用 **`$EDITOR`** 打开它——就是你熟悉的
vim / vi / nano（依次读 `--editor` → `$FLCLASH_TUI_EDITOR` → `$VISUAL` → `$EDITOR` → `vi`）：

```bash
flclash-tui profile edit            # 编辑当前激活订阅
flclash-tui profile edit <id>       # 编辑指定订阅
flclash-tui profile edit --editor "code -w"   # 指定编辑器（支持带参数）
flclash-tui profile edit            # 没有任何订阅时 = 新建
```

保存退出后的行为：

- 内容没变 → 什么都不做（不会重载内核）
- 内容变了 → 规范化（**YAML / JSON 都行，JSON 自动转块状 YAML**）→ 落盘 → 若是激活订阅则热重载内核
- YAML 非法 → 拒绝写入，原内容保留在 `*.rejected.yaml` 并报错退出（退出码 1）
- 编辑器非零退出（如 vim 的 `:cq`）→ 视为取消，不修改任何东西

脚本/CI 场景不需要编辑器，用 stdin 更直接：

```bash
flclash-tui profile create --label 我的配置 --activate < sub.yaml
cat conf.json | flclash-tui profile set <id>       # 粘贴 JSON 也一样
flclash-tui profile show --raw > backup.yaml
```

`profile edit` 在非终端环境（管道 / CI）下会直接报错并提示改用 `profile set`。

### 常见组合

```bash
# 后台长驻内核
flclash-tui core start --daemon
flclash-tui core status --json

# 加订阅并切成全局模式
flclash-tui profile add 'https://example.com/sub?token=x' --activate
flclash-tui config mode global

# 选延迟最低的节点（组测速 + jq 排序）
flclash-tui proxy delay --group '🚀 节点选择' --json \
  | jq -r '.results | map(select(.delay>0)) | sort_by(.delay)[0].name' \
  | xargs -I{} flclash-tui proxy select '🚀 节点选择' {}

# 列出当前连接的主机
flclash-tui conn list --json | jq -r '.connections[].host'

# 用内置内核的版本固定数据目录跑一次性任务
flclash-tui --home /tmp/sandbox profile create --label t --activate < conf.yaml
flclash-tui --home /tmp/sandbox core start --daemon
flclash-tui --home /tmp/sandbox core logs -n 20
flclash-tui --home /tmp/sandbox core stop
```

### 后台内核与 TUI 的关系

`core start --daemon` 起的进程写在 `core/core.pid`，TUI 不会去抢它：TUI 检测到控制器已被
占用时会走「原地热重载」而不是重启进程，因此 CLI 起的常驻内核不会被 TUI 退出带走。
反过来，TUI 托管的内核没有 pid 文件，`core stop` 不会误杀它（会提示用 TUI 管理）。

## 架构

```
┌────────────────────────────── flclash-tui（单二进制 Go） ──────────────────────────────┐
│  bubbletea UI（7 页面 + 弹窗/吐司）        │  cli（子命令树 + --json）                  │
│      ↑↓ 消息流（tea.Msg）                  │      │                                    │
│  engine    两路共用：内置内核释放 / 配置生成 / 热重载                                   │
│  clashapi   REST + WebSocket 客户端（/proxies /configs /connections /traffic /logs …） │
│  coremgr    mihomo 进程生命周期（managed）/ 就绪探测 / 崩溃日志尾部                      │
│  corecfg    配置生成（复刻 FLClash lib/common/task.dart 的合并与强制覆写逻辑）           │
│  coreasset  go:embed 的内核资产 + sha256 校验释放                                       │
│  profiles   订阅模型与下载（userinfo/filename* 解析、自动更新调度）                      │
│  osshare    系统代理（macOS networksetup）                                              │
└────────────────────────────────────────────────────────────────────────────────────────┘
                    │ exec + 外部工作目录                     │ HTTP/WS 127.0.0.1:9090
                    ▼                                        ▼
         内置 mihomo（释放到 core/bin）  ◄──── config.yaml（订阅 YAML + FLClash 式 patch）
```

## 测试

```bash
# 单元测试（含内置内核释放/校验/损坏修复）
go test ./...

# TUI 端到端冒烟测试（pty 驱动 + 内置内核，隔离数据目录与端口）
python3 test/smoke_test.py

# 非交互式 CLI 全链路测试（不依赖 TUI，覆盖全部子命令）
python3 test/cli_test.py
```

`smoke_test.py`（42 项，完全不依赖公网：目标站点也是本地起的）：内核拉起/退出清理、七个页面渲染、组测速与节点切换、订阅添加（含
`subscription-userinfo`/`filename*` 解析）、手写配置（JSON 粘贴→规范化落盘→激活→规则生效→
编辑源→保存重载→取消）、订阅拉取双通道回退、事件通知栈堆叠与开关、设置编辑→持久化→应用重启→
新端口代理验证、模式切换、帮助浮层。

`cli_test.py`（81 项）：内置内核释放与校验、`core` 全部子命令与后台 pid 生命周期、订阅
增/删/改/查/更新/激活/重命名、JSON 规范化、$EDITOR 集成（保存 / 取消 / 非法内容 / 非 tty）、
`proxy`/`conn`/`rule`/`config`/`log`/`settings`/`sysproxy` 的读写与 `--json` 输出、退出码约定。

## 说明

- TUN 模式需要 root/管理员权限运行（macOS 建议 `sudo ./flclash-tui`，或保持关闭仅用系统代理/手动代理）。
- 本项目与 FLClash 官方无隶属关系，仅为其终端用户场景的独立实现，遵循 GPL-3.0（参考了 FLClash 的行为设计）。
