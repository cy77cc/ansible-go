# 回调插件与输出系统

> 阶段：P12 | 设计文档引用：第十三章

本文件描述 ansible-go 中回调插件（Callback Plugin）的设计、输出格式、退出码处理、颜色终端支持以及日志系统。

---

## 目录

1. [回调插件机制](#1-回调插件机制)
2. [Default Callback — 标准输出格式](#2-default-callback--标准输出格式)
3. [其他回调插件](#3-其他回调插件)
4. [退出码处理](#4-退出码处理)
5. [颜色与终端](#5-颜色与终端)
6. [Go 实现要点](#6-go-实现要点)
7. [日志系统](#7-日志系统)
8. [任务拆解](#8-任务拆解)

---

## 1. 回调插件机制

### 1.1 事件驱动模型

回调插件是 ansible-go 输出系统的核心。引擎在执行生命周期的关键节点触发事件，所有注册的回调插件收到通知并执行各自的输出逻辑。

这种事件驱动设计实现了**关注点分离**：引擎只负责调度和执行，输出格式完全由回调插件决定。用户可以通过配置切换不同的输出格式，而无需修改引擎代码。

### 1.2 回调触发时机

引擎在以下生命周期节点触发回调事件：

```
Playbook 执行开始
  │
  ├── OnPlaybookStart(playbook string)
  │     触发时机：Playbook 文件开始执行时
  │     参数：playbook 文件路径
  │
  ├── Play 开始
  │     ├── OnPlayStart(play Play, hosts []string)
  │     │     触发时机：每个 Play 开始执行时
  │     │     参数：Play 定义、涉及的主机列表
  │     │
  │     ├── Task 开始
  │     │     ├── OnTaskStart(task Task, isHandler bool)
  │     │     │     触发时机：每个 Task 开始执行前
  │     │     │     参数：Task 定义、是否为 Handler
  │     │     │
  │     │     ├── Task 执行结果（三选一）
  │     │     │     ├── OnTaskOk(result TaskResult)
  │     │     │     │     触发时机：Task 执行成功（ok 或 changed）
  │     │     │     │
  │     │     │     ├── OnTaskFailed(result TaskResult, ignored bool)
  │     │     │     │     触发时机：Task 执行失败
  │     │     │     │     参数：ignored=true 表示 ignore_errors 生效
  │     │     │     │
  │     │     │     └── OnTaskSkipped(result TaskResult)
  │     │     │           触发时机：Task 被跳过（when 条件不满足）
  │     │     │
  │     │     └── OnTaskUnreachable(host string, result TaskResult)
  │     │           触发时机：主机不可达（SSH 连接失败等）
  │     │
  │     └── ... 更多 Task ...
  │
  └── OnPlaybookStats(stats PlayStats)
        触发时机：所有 Play 执行完毕后
        参数：汇总统计信息
```

### 1.3 事件与结果状态

每个 Task 执行完毕后，引擎根据结果确定状态并触发对应事件：

| 状态 | 触发的回调方法 | 说明 |
|------|---------------|------|
| `ok` | `OnTaskOk` | 任务成功，无变更 |
| `changed` | `OnTaskOk` | 任务成功，有变更 |
| `failed` | `OnTaskFailed` | 任务失败 |
| `unreachable` | `OnTaskUnreachable` | 主机不可达 |
| `skipped` | `OnTaskSkipped` | 任务被跳过 |

注意：`ok` 和 `changed` 共用 `OnTaskOk` 方法，回调插件通过检查 `TaskResult.Changed` 字段区分。

### 1.4 多回调插件共存

ansible-go 支持同时注册多个回调插件。引擎维护一个回调列表，事件触发时遍历调用。典型场景：

- `default` — 标准终端输出
- `timer` — 在后台记录耗时
- 日志回调 — 写入文件

配置方式：

```ini
# ansible.cfg
[defaults]
stdout_callback = default
callback_whitelist = timer, log_plays
```

---

## 2. Default Callback — 标准输出格式

Default Callback 是 ansible-go 的默认输出格式，忠实复刻 Ansible 的终端输出风格。

### 2.1 PLAY 横幅

每个 Play 开始时打印横幅行：

```
PLAY [Configure webservers] *****************************************************
```

规则：
- `PLAY` 关键字使用青色（cyan）
- Play 名称使用粗体
- 星号 `*` 填充至终端宽度（默认 79 列）
- 如果终端宽度无法获取，使用默认宽度

### 2.2 TASK 横幅

每个 Task 开始时打印横幅行：

```
TASK [Install nginx] ************************************************************
```

规则：
- `TASK` 关键字使用青色
- Handler 任务显示为 `RUNNING HANDLER` 而非 `TASK`

```
RUNNING HANDLER [restart nginx] **************************************************
```

### 2.3 任务结果行

每个主机的 Task 结果打印一行：

```
ok: [web1]
changed: [web1]
failed: [web1] => {"msg": "non-zero return code"}
skipped: [web1]
fatal: [web1] => {"msg": "UNREACHABLE!"}
```

规则：
- 状态关键字使用对应颜色（见 2.4 颜色方案）
- 主机名用方括号包裹
- `failed` 和 `unreachable` 结果附加 JSON 详情（`=> {...}`）
- `ignore_errors: true` 的失败显示为 `...ignoring`

### 2.4 颜色方案

| 状态 | 颜色 | ANSI 码 |
|------|------|---------|
| `ok` | 绿色 | `\033[32m` |
| `changed` | 黄色 | `\033[33m` |
| `failed` | 红色 | `\033[31m` |
| `skipped` | 青色 | `\033[36m` |
| `unreachable` | 红色 | `\033[31m` |
| PLAY/TASK 横幅 | 青色 | `\033[36m` |

### 2.5 RECAP 表

所有 Play 执行完毕后打印汇总表：

```
PLAY RECAP ***********************************************************************
web1     : ok=4  changed=3  unreachable=0  failed=0  skipped=0
web2     : ok=2  changed=0  unreachable=0  failed=0  skipped=0
```

规则：
- `PLAY RECAP` 使用青色
- 每行显示一台主机的统计
- 统计项：`ok`、`changed`、`unreachable`、`failed`、`skipped`
- `failed > 0` 时该行显示红色
- 星号填充至终端宽度

### 2.6 完整输出示例

```
PLAY [Configure webservers] *****************************************************

TASK [Gathering Facts] **********************************************************
ok: [web1]
ok: [web2]

TASK [Install nginx] ************************************************************
changed: [web1]
ok: [web2]

TASK [Start nginx] **************************************************************
ok: [web1]
skipping: [web2]

RUNNING HANDLER [restart nginx] **************************************************
changed: [web1]

PLAY RECAP ***********************************************************************
web1     : ok=4  changed=3  unreachable=0  failed=0  skipped=0
web2     : ok=2  changed=0  unreachable=0  failed=0  skipped=0
```

---

## 3. 其他回调插件

### 3.1 Minimal Callback

一行一个结果，适合脚本解析和 CI 管道：

```
web1 | OK => {"changed": false, "ping": "pong"}
web1 | CHANGED => {"changed": true, ...}
web2 | FAILED => {"msg": "non-zero return code"}
web1 | SKIPPED
```

格式规则：
- 每行格式：`主机名 | 状态 => JSON详情`
- 状态关键字大写
- 始终附加 JSON 结果（ok/changed 时也显示）
- 无横幅、无 RECAP

### 3.2 JSON Callback

输出完整 JSON，适合程序化处理和 CI/CD 集成：

```json
{
  "plays": [
    {
      "play": {"name": "Configure webservers", "id": "uuid"},
      "tasks": [
        {
          "task": {"name": "Install nginx", "id": "uuid"},
          "hosts": {
            "web1": {
              "status": "changed",
              "result": {"changed": true, ...}
            },
            "web2": {
              "status": "ok",
              "result": {"changed": false, ...}
            }
          }
        }
      ]
    }
  ],
  "stats": {
    "web1": {"ok": 4, "changed": 3, "unreachable": 0, "failed": 0, "skipped": 0},
    "web2": {"ok": 2, "changed": 0, "unreachable": 0, "failed": 0, "skipped": 0}
  }
}
```

实现要点：
- 在内存中累积所有事件数据
- `OnPlaybookStats` 触发时序列化输出
- 使用 `json.MarshalIndent` 格式化

### 3.3 YAML Callback

人类可读的 YAML 格式输出：

```yaml
- play: Configure webservers
  tasks:
    - task: Install nginx
      hosts:
        web1:
          status: changed
          result:
            changed: true
        web2:
          status: ok
          result:
            changed: false
  recap:
    web1: {ok: 4, changed: 3, unreachable: 0, failed: 0, skipped: 0}
    web2: {ok: 2, changed: 0, unreachable: 0, failed: 0, skipped: 0}
```

### 3.4 Timer Callback

在标准输出基础上追加耗时信息：

```
Playbook executed in 0:02:34.567
```

实现方式：
- `OnPlaybookStart` 记录开始时间
- `OnPlaybookStats` 计算耗时并输出
- 格式：`HH:MM:SS.mmm`

---

## 4. 退出码处理

### 4.1 退出码映射

ansible-go 的退出码基于 Playbook 执行的聚合结果：

| 条件 | 退出码 | 说明 |
|------|--------|------|
| 所有主机所有任务成功 | 0 | 完全成功 |
| 其他错误（配置/解析等） | 1 | 通用错误 |
| 至少一台主机有任务失败 | 2 | 主机失败 |
| 至少一台主机不可达 | 4 | 连接失败 |

### 4.2 退出码计算逻辑

退出码在 `OnPlaybookStats` 回调触发后计算：

```
1. 收集 PlayStats 中所有主机的统计
2. 如果任何主机 failed > 0 → 退出码 2
3. 如果任何主机 unreachable > 0 → 退出码 4
4. 否则 → 退出码 0
5. 配置/解析等致命错误 → 退出码 1（在引擎层面处理，不经过回调）
```

### 4.3 退出码与 ignore_errors

- `ignore_errors: true` 的任务失败**不**计入 `failed` 统计
- 因此不会影响退出码
- 但结果中会标记 `ignored: true`

### 4.4 退出码与 any_errors_fatal

- `any_errors_fatal: true` 时，任何主机失败立即中止所有主机
- 退出码仍为 2（主机失败）

---

## 5. 颜色与终端

### 5.1 ANSI 颜色码

ansible-go 使用标准 ANSI 转义序列实现终端颜色：

| 颜色 | 前景色码 | 重置码 |
|------|---------|--------|
| 红色 | `\033[31m` | `\033[0m` |
| 绿色 | `\033[32m` | `\033[0m` |
| 黄色 | `\033[33m` | `\033[0m` |
| 青色 | `\033[36m` | `\033[0m` |

粗体文本：`\033[1m` ... `\033[0m`

### 5.2 TTY 检测

ansible-go 在启动时检测 stdout 是否为 TTY（终端设备）：

```go
// 使用 github.com/mattn/go-isatty
isatty.IsTerminal(os.Stdout.Fd())   // 检查是否为终端
isatty.IsCygwinTerminal(os.Stdout.Fd()) // 检查 Cygwin 终端
```

### 5.3 非 TTY 禁用颜色

以下情况自动禁用颜色输出：

1. stdout 不是 TTY（管道、重定向到文件）
2. `NO_COLOR` 环境变量存在（无论值是什么）
3. `TERM=dumb`
4. 用户通过 `--no-color` 命令行标志禁用
5. `ANSIBLE_FORCE_COLOR` 未设置且非 TTY

禁用颜色时，所有 ANSI 转义序列不输出，仅保留纯文本。

### 5.4 颜色强制启用

即使 stdout 不是 TTY，以下情况仍强制启用颜色：

1. `ANSIBLE_FORCE_COLOR=true` 环境变量
2. `--force-color` 命令行标志

这在 CI/CD 环境中捕获带颜色的输出时有用。

### 5.5 终端宽度

横幅行的星号填充依赖终端宽度检测：

```go
// 优先级：
// 1. COLUMNS 环境变量
// 2. ioctl TIOCGWINSZ 系统调用
// 3. 默认值 79
```

---

## 6. Go 实现要点

### 6.1 CallbackPlugin 接口

回调插件的核心接口定义：

```go
// CallbackPlugin 定义了所有回调插件必须实现的接口。
// 引擎在执行生命周期的关键节点调用这些方法。
type CallbackPlugin interface {
    // OnPlaybookStart 在 Playbook 开始执行时调用。
    OnPlaybookStart(playbook string)

    // OnPlayStart 在每个 Play 开始执行时调用。
    OnPlayStart(play Play, hosts []string)

    // OnTaskStart 在每个 Task 开始执行时调用。
    // isHandler 为 true 表示这是一个 Handler 任务。
    OnTaskStart(task Task, isHandler bool)

    // OnTaskOk 在 Task 执行成功时调用（包括 ok 和 changed 状态）。
    // 调用方应检查 result.Changed 区分 ok 和 changed。
    OnTaskOk(result TaskResult)

    // OnTaskFailed 在 Task 执行失败时调用。
    // ignored 为 true 表示 ignore_errors 已生效，失败被忽略。
    OnTaskFailed(result TaskResult, ignored bool)

    // OnTaskSkipped 在 Task 被跳过时调用（when 条件不满足）。
    OnTaskSkipped(result TaskResult)

    // OnTaskUnreachable 在主机不可达时调用（SSH 连接失败等）。
    OnTaskUnreachable(host string, result TaskResult)

    // OnPlaybookStats 在所有 Play 执行完毕后调用，传入汇总统计。
    OnPlaybookStats(stats PlayStats)
}
```

### 6.2 TaskResult 类型

```go
// TaskResult 表示单个 Task 在单个主机上的执行结果。
type TaskResult struct {
    Host       string         // 主机名
    TaskName   string         // 任务名称
    Status     TaskStatus     // ok/changed/failed/skipped/unreachable
    Changed    bool           // 是否有变更
    Result     map[string]any // 模块返回的结果数据
    Msg        string         // 错误消息（失败时）
    Ignored    bool           // 是否被 ignore_errors 忽略
    Duration   time.Duration  // 任务执行耗时
}
```

### 6.3 TaskStatus 类型

```go
// TaskStatus 表示任务执行状态。
type TaskStatus int

const (
    TaskStatusOk          TaskStatus = iota // 成功无变更
    TaskStatusChanged                       // 成功有变更
    TaskStatusFailed                        // 执行失败
    TaskStatusSkipped                       // 被跳过
    TaskStatusUnreachable                   // 主机不可达
)
```

### 6.4 PlayStats 类型

```go
// PlayStats 汇总所有主机的执行统计。
type PlayStats struct {
    // HostStats 按主机名索引的统计信息。
    HostStats map[string]HostStat
}

// HostStat 表示单台主机的执行统计。
type HostStat struct {
    Ok          int // 成功（无变更）的任务数
    Changed     int // 成功（有变更）的任务数
    Unreachable int // 不可达次数
    Failed      int // 失败的任务数
    Skipped     int // 跳过的任务数
}
```

### 6.5 DefaultCallback 类型签名

```go
// DefaultCallback 实现 Ansible 风格的标准终端输出。
type DefaultCallback struct {
    // writer 是输出目标，通常为 os.Stdout。
    writer io.Writer

    // colorEnabled 控制是否输出 ANSI 颜色。
    colorEnabled bool

    // terminalWidth 终端宽度（列数），用于横幅填充。
    terminalWidth int

    // stats 聚合的主机统计信息。
    stats PlayStats
}

// 确保 DefaultCallback 实现了 CallbackPlugin 接口。
var _ CallbackPlugin = (*DefaultCallback)(nil)
```

### 6.6 回调注册表

```go
// CallbackRegistry 管理回调插件的注册和查找。
type CallbackRegistry struct {
    factories map[string]CallbackFactory
}

// CallbackFactory 是回调插件的工厂函数类型。
type CallbackFactory func(config CallbackConfig) CallbackPlugin

// Register 注册一个回调插件工厂。
func (r *CallbackRegistry) Register(name string, factory CallbackFactory)

// Get 根据名称获取回调插件实例。
func (r *CallbackRegistry) Get(name string) (CallbackPlugin, error)
```

### 6.7 辅助类型

```go
// Play 表示一个 Play 的基本信息（供回调使用）。
type Play struct {
    ID    string   // Play 的唯一标识
    Name  string   // Play 名称
    Hosts []string // 目标主机模式
}

// Task 表示一个 Task 的基本信息（供回调使用）。
type Task struct {
    ID       string // Task 的唯一标识
    Name     string // Task 名称
    Module   string // 使用的模块名
    IsHandler bool  // 是否为 Handler
}
```

---

## 7. 日志系统

### 7.1 日志级别

ansible-go 使用分层日志系统，每个级别有对应的详细程度：

| 级别 | CLI 标志 | 输出内容 | 何时使用 |
|------|---------|---------|---------|
| ERROR | 默认 | 错误信息 | 任务失败、连接断开 |
| WARNING | 默认 | 警告信息 | 废弃语法、未使用变量 |
| INFO | `-v` | 任务结果、Play 进度 | 正常运行状态 |
| DEBUG | `-vv` | 变量值、模板渲染结果 | 调试变量和模板 |
| TRACE | `-vvv` | SSH 连接细节、命令执行 | 调试连接问题 |
| DEBUG2 | `-vvvv` | 原始 SSH 输出、完整数据 | 深度调试 |

### 7.2 -v 标志映射

```
无 -v    → WARNING + ERROR
-v       → INFO + WARNING + ERROR
-vv      → DEBUG + INFO + WARNING + ERROR
-vvv     → TRACE + DEBUG + INFO + WARNING + ERROR
-vvvv    → DEBUG2 + TRACE + DEBUG + INFO + WARNING + ERROR
```

### 7.3 日志输出目标

日志输出与回调输出分离：

- **回调输出** → stdout（面向用户的标准输出）
- **日志输出** → stderr 或日志文件

日志文件配置：

```ini
# ansible.cfg
[defaults]
log_path = /var/log/ansible-go.log
```

未配置 `log_path` 时，日志输出到 stderr。

### 7.4 日志格式

```
2026-05-25 10:30:45 INFO  [engine] Playing 'Configure webservers'
2026-05-25 10:30:45 DEBUG [template] Rendering template: {{ nginx_port }}
2026-05-25 10:30:46 ERROR [connection] SSH connection failed: dial tcp 10.0.0.1:22: connect: connection refused
```

格式：`时间戳 级别 [组件] 消息`

### 7.5 Go 日志实现要点

```go
// Logger 定义日志接口。
type Logger interface {
    Error(msg string, args ...any)
    Warning(msg string, args ...any)
    Info(msg string, args ...any)
    Debug(msg string, args ...any)
    Trace(msg string, args ...any)
    Debug2(msg string, args ...any)

    // SetLevel 设置日志级别。
    SetLevel(level LogLevel)

    // IsEnabled 检查指定级别是否启用。
    IsEnabled(level LogLevel) bool
}

// LogLevel 表示日志级别。
type LogLevel int

const (
    LogLevelError   LogLevel = iota
    LogLevelWarning
    LogLevelInfo
    LogLevelDebug
    LogLevelTrace
    LogLevelDebug2
)
```

### 7.6 组件日志

每个子系统使用独立的组件名，便于过滤：

| 组件名 | 说明 |
|--------|------|
| `engine` | 执行引擎 |
| `inventory` | Inventory 解析 |
| `variables` | 变量系统 |
| `template` | 模板引擎 |
| `connection` | SSH 连接 |
| `module` | 模块执行 |
| `callback` | 回调插件 |
| `vault` | Vault 加解密 |

---

## 8. 任务拆解

### T12.1 回调插件接口与默认输出

**目标**：实现回调插件系统的核心接口和默认输出格式。

**子任务**：

| 编号 | 任务 | 说明 | 预估 |
|------|------|------|------|
| T12.1.1 | 定义 CallbackPlugin 接口 | 8 个事件方法 + 辅助类型 | 0.5d |
| T12.1.2 | 实现 DefaultCallback | 标准 Ansible 输出格式 | 1d |
| T12.1.3 | 实现颜色系统 | ANSI 码、TTY 检测、NO_COLOR 支持 | 0.5d |
| T12.1.4 | 实现 RECAP 表输出 | 横幅填充、统计汇总 | 0.5d |
| T12.1.5 | 实现 MinimalCallback | 一行一结果格式 | 0.5d |
| T12.1.6 | 实现 JSONCallback | 完整 JSON 输出 | 0.5d |
| T12.1.7 | 实现 YAMLCallback | YAML 格式输出 | 0.5d |
| T12.1.8 | 实现 TimerCallback | 耗时统计 | 0.25d |
| T12.1.9 | 退出码处理 | 基于 PlayStats 的退出码计算 | 0.25d |
| T12.1.10 | 日志系统 | Logger 接口、级别过滤、组件日志 | 1d |
| T12.1.11 | 回调注册表 | 注册/查找机制、配置集成 | 0.5d |
| T12.1.12 | 单元测试 | 所有回调插件的测试 | 1d |

**总预估**：7 天

**验收标准**：

- [ ] DefaultCallback 输出与 Ansible 完全一致
- [ ] 颜色在非 TTY 环境自动禁用
- [ ] RECAP 表统计正确
- [ ] 退出码映射正确
- [ ] 日志级别过滤正确
- [ ] 所有回调插件测试通过

---

*上一篇：[12-vault-and-secrets.md](12-vault-and-secrets.md) | 下一篇：[14-filters-tests-lookups.md](14-filters-tests-lookups.md)*
