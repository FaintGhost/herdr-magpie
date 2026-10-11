# Herdr Magpie

[![CI](https://github.com/FaintGhost/herdr-magpie/actions/workflows/ci.yml/badge.svg)](https://github.com/FaintGhost/herdr-magpie/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**在 [Herdr](https://herdr.dev) 里显示 [magpie](https://github.com/yetone/magpie) 网关状态、上游错误风暴、每个会话的模型和成本，并提供跨 agent 的会话启动器。**

[English](README.md)

```text
🟢 ◆ magpie                              顶栏：网关正常
🟡 ◆ magpie ⚠ opencode-go 429×438 +1    顶栏：最严重的活跃风暴，另有一个
🔴 ◇ magpie down                        顶栏：网关已确认离线

claude-sonnet-x +1  $1.75               agent 侧栏（示意）
$3.12                                   工作区侧栏（示意）
```

顶栏从 magpie 的 usage 日志读取风暴信息，因此从不等待 magpie CLI。会话成本来自一次 `magpie sessions` 调用，大约每五分钟一次；agent 状态变化时至多每分钟一次。使用时不需要 gateway 密钥。插件唯一的网络请求是对网关的一次匿名健康探测。

## 显示什么

| 位置 | 内容 | 何时更新 |
| --- | --- | --- |
| 顶栏 | 网关状态，或最严重的活跃错误风暴 | 每 10 秒 |
| agent 侧栏 | `$magpie_model`：该 pane 的会话用过的模型，按花费从高到低。`$magpie_cost`：该会话的成本 | agent 状态变化时（idle、done、blocked 或被检测到），至多每分钟一次；后台约每五分钟一次 |
| 工作区侧栏 | `$magpie_cost`：工作区内打开的会话成本合计 | 同 agent 侧栏 |
| 通知 | 网关离线与恢复；某个提供方风暴开始 | 状态变化时 |
| 弹窗 pane | 会话启动器：列出所有 agent 的 magpie 会话。Enter 聚焦已打开的会话；否则在会话目录所在的工作区新建 tab，没有这样的工作区时为该目录新建工作区，并在其中恢复它 | 按需 |

风暴按提供方统计。风暴指的是：经网关发往某个提供方的近期请求，以相同的状态码失败了多次。它不指向具体 pane，因为经过网关的请求不携带会话 ID。

## 要求

| 要求 | 支持情况 |
| --- | --- |
| 操作系统 | Linux |
| Herdr | 0.9.1 或更高（server） |
| magpie | `PATH` 中的 `magpie` CLI，或 `~/.local/bin/magpie`，也可以设置 `magpie_bin` |
| 构建工具 | 运行 Herdr 服务端的机器上需要 Go 1.26+ 和 Git |
| 运行时 | 单个编译好的二进制，无常驻进程 |

安装时会从源码编译。Go 依赖只有 `golang.org/x/term`（启动器的终端 raw 模式）和 `golang.org/x/text`（东亚字符宽度），安装时会下载锁定的版本。不需要 CGO。

## 安装

```sh
herdr plugin install FaintGhost/herdr-magpie
```

请查看 Herdr 的安装预览。构建命令会生成 `bin/herdr-magpie`；插件 ID 是 **`faintghost.magpie`**。

### 添加顶栏与侧栏 token

打开 setup pane：

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint setup
```

它会打印 TOML 片段，需要你手动合并进 Herdr 配置。打印出的路径都是绝对路径，因此即使 herdr 服务端的 `PATH` 很精简，配置也能生效：

- `tab_bar_right`：一个命令项，每 10 秒运行一次 `status`，并带有防护，二进制缺失时不输出任何内容。
- `[ui.sidebar.agents]` 与 `[ui.sidebar.spaces]`：把 `$magpie_*` token 加入你已经在用的行。
- 可选的 `[[keys.command]]`：默认 `prefix+m`，调用 `open-sessions` action 打开会话弹窗。

侧边栏布局与快捷键读取的是**客户端**的配置。如果你用 `herdr --remote` 连接，请把侧边栏 rows 和快捷键写进你发起连接那台机器的 `config.toml`；状态栏条目仍放在服务器上。

setup 从不修改你的配置。保留已有的条目，不要再新增一个 `[ui]` 表，然后运行 `herdr config check`，并通过 Herdr 菜单或 `herdr server reload-config` 重新加载配置。

### 会话启动器

按 setup 里配置的快捷键（`prefix+m`，默认前缀下即先按 Ctrl+B 再按 M），或运行：

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint sessions
```

弹窗按最近活动从新到旧列出会话。输入文字可按 agent、标题或目录过滤。方向键或 Ctrl-P、Ctrl-N 移动。Enter 执行操作：若会话已打开则聚焦它；否则，若某个工作区中有 agent 位于该会话的目录，就在那个工作区新建一个 tab，没有则为该目录新建一个工作区，并在其中运行该会话的恢复命令。会话没有目录或目录已不存在时，tab 开在当前工作区，弹窗保持打开并在底部显示提示。Esc 或 Ctrl-C 退出。

### doctor 诊断

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint doctor
```

doctor 会检查配置、magpie 与 herdr 二进制、网关探测（包括网关 URL 及其来源）、它发起的那一次 magpie 调用耗时、usage 日志（路径、大小、最新一行距今多久、最近十分钟的网关行数），以及有多少 agent pane 能匹配到 magpie 会话。usage 日志缺失只是警告，风暴通知随之关闭。只有检查失败时才以非零状态退出。输出不包含任何凭据，也不包含会话标题或路径。

## 配置

配置是可选的。用以下命令查看目录：

```sh
herdr plugin config-dir faintghost.magpie
```

在该目录中创建 `config.json`：

```json
{
  "gateway_url": "",
  "magpie_bin": "",
  "herdr_bin": "",
  "notify": true,
  "session_days": 14,
  "usage_path": ""
}
```

| 键 | 默认值 | 含义 |
| --- | --- | --- |
| `gateway_url` | 空 | 网关基础 URL。为空时依次从 `magpie.service` 的环境变量、`MAGPIE_GATEWAY_URL`、`MAGPIE_PUBLIC_URL`、`MAGPIE_ADDR` 中发现，最后回退到 `http://127.0.0.1:3425` |
| `magpie_bin` | 空 | magpie CLI 的绝对路径。为空时查找 `PATH`，再查找 `~/.local/bin/magpie` |
| `herdr_bin` | 空 | herdr CLI 的绝对路径。为空时依次查找 `HERDR_BIN_PATH`、`PATH`，最后是 mise shim |
| `notify` | `true` | 是否发送网关与风暴通知 |
| `session_days` | `14` | 会话列表与会话 token 回溯的天数 |
| `usage_path` | 空 | magpie 的 usage 日志，用于风暴通知。为空时使用 `$XDG_CONFIG_HOME/magpie/usage.jsonl`，未设置 `XDG_CONFIG_HOME` 时使用 `~/.config/magpie/usage.jsonl`。必须是绝对路径 |

未知的键会导致错误，因此拼写错误不会被静默忽略。每次顶栏刷新都会重新读取配置，所以大多数修改在十秒内生效。

## 通知规则

- **网关。** 连续两次探测失败才会确认网关离线。第一次确认离线以及之后的每次状态变化都会发送通知。单次探测失败不会改变任何状态。
- **风暴。** 最近十分钟的网关失败按提供方和状态码分组。同一组达到 5 次失败即构成风暴；单次 402（余额或付款问题）即可构成风暴。499（客户端已取消）和 400（调用方请求有误）不计入，日志行也不计入，日志行来自直接调用提供方的 agent。
- **冷却。** 风暴开始时通知一次，之后同一提供方、同一状态码在 30 分钟内不再重复通知。
- **来源。** 风暴从 magpie 的 usage 日志读取（见 `usage_path`）。日志缺失或无法读取时风暴通知关闭，doctor 会明确报告。插件从不为了风暴而轮询 magpie CLI。

## 隐私

- 从不读取 magpie 的 settings、logins、auth 或密钥文件，也不需要 gateway 密钥。
- 只运行一条 magpie 命令：`magpie sessions --json --limit 100000`，且是只读的。每次 magpie CLI 调用都会改写 magpie 的凭据文件，因此插件大约每五分钟调用一次，事件触发时也至多每分钟一次。
- 每个顶栏刷新周期向网关发送一次未认证的 `GET /v1/models`。自报身份为 magpie 的 401 响应视为在线。
- 只读取 magpie 的 usage 日志（默认 `~/.config/magpie/usage.jsonl`），只读。每次从文件末尾向前读取，最多 4 MiB。该日志是 magpie 的内部网关日志，不是文档化的接口，其格式可能随 magpie 版本变化。插件无法解析的行会被跳过，并由 doctor 计数。
- 通过 `herdr agent list` 和 `herdr workspace list` 读取 Herdr 状态。顶栏与后台刷新只写入插件自己的 metadata token 和通知。launcher 只在你按下 Enter 时写入：新建 tab 或工作区、聚焦 pane 或工作区，并运行恢复命令。
- 状态保存在 Herdr 的插件状态目录中：`status.json`、`refresh.json`、`refresh.log` 以及一个锁文件。它从不修改 Herdr 或 magpie 的配置。
- 通知从不包含会话标题、路径或账号名。风暴通知只显示提供方、状态码、最常见的错误类型以及失败最多的 agent 名称。

## 已知限制

- 网关请求没有会话 ID，因此风暴只能指向提供方，无法指向 pane。
- 模型与成本来自 `magpie sessions`。没有对应 magpie 会话的 agent 所在 pane 不显示 token。
- 每次 magpie CLI 调用耗时一到两秒，机器繁忙时更久。数值在后台刷新，而不是每个顶栏刷新周期都刷新。超过五秒的调用会失败，并在下一次刷新时重试。
- 风暴统计的是 usage 日志中最近十分钟的数据。插件最多读取日志末尾的 4 MiB，在当前流量下远超十分钟；流量大得多的网关可能少计风暴。读取超过 300 毫秒预算时，沿用上一次结果，最长十分钟。
- 成本是 magpie 自己的估算。
- 安装后看到的第一个网关状态从不发送通知。只有两个已确认状态之间的变化才会通知。

## 开发

```sh
git clone https://github.com/FaintGhost/herdr-magpie.git
cd herdr-magpie
go build -trimpath -ldflags='-s -w' -o bin/herdr-magpie .
go vet ./...
go test -race ./...
test -z "$(gofmt -l .)"
```

设置 `HERDR_MAGPIE_DRY_RUN=1` 后，所有 herdr 写操作都只打印命令而不执行。配合 `status` 或 `refresh` 使用，可以在真实数据上查看将会发生什么改变。同时请把 `HERDR_PLUGIN_STATE_DIR` 指向临时目录。

| 文件 | 职责 |
| --- | --- |
| `main.go` | 子命令、参数、herdr 的 dry-run 包装 |
| `config.go` | 路径、严格的 `config.json` 解析、URL 脱敏 |
| `status.go` | 网关防抖、基于 usage 日志的风暴通知、顶栏输出、后台刷新的触发 |
| `refresh.go` | 风暴计算（纯函数）、会话 token 计算与更新 |
| `sessions.go` | 装配会话启动器 |
| `setup.go`、`doctor.go` | 打印的配置片段与诊断 |
| `internal/magpie` | 网关发现与探测、会话、usage CSV、usage 日志读取 |
| `internal/herdr` | agent、工作区、metadata、通知、tab |
| `internal/state` | 带锁、原子写入的 JSON 状态文件 |
| `internal/launcher` | 会话启动器 |

## 更新与卸载

重新运行 `herdr plugin install FaintGhost/herdr-magpie` 即可更新。卸载时，请先从配置中删除插件的 `tab_bar_right` 条目、侧栏行与键位绑定，重新加载 Herdr 配置，然后运行：

```sh
herdr plugin uninstall faintghost.magpie
```

卸载后 Herdr 会保留插件的配置目录与状态目录。如果不想留下任何痕迹，请自行删除。

## 许可证

[MIT](LICENSE)。Bug 报告与贡献请到 [GitHub Issues](https://github.com/FaintGhost/herdr-magpie/issues)。
