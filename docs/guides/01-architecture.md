# ansible-go 架构详解

> 本文档深入剖析 ansible-go 的五层架构、数据流全景、插件化设计和并发模型，帮助你理解系统是如何工作的。

---

## 一、Ansible 源码架构概览

### 1.1 Python 包结构

在理解 ansible-go 的架构之前，先看看 Ansible 的 Python 包结构：

```
ansible/
├── cli/                    # CLI 入口和子命令
│   ├── __init__.py
│   ├── adhoc.py           # ansible 命令（ad-hoc）
│   ├── playbook.py        # ansible-playbook 命令
│   ├── inventory.py       # ansible-inventory 命令
│   ├── vault.py           # ansible-vault 命令
│   └── galaxy.py          # ansible-galaxy 命令
│
├── executor/               # 执行引擎
│   ├── task_executor.py    # 单任务执行器
│   ├── task_queue_manager.py  # 任务队列管理
│   ├── module_common.py    # 模块公共代码
│   └── play_iterator.py    # Play 迭代器
│
├── inventory/              # Inventory 系统
│   ├── manager.py          # Inventory 管理器
│   ├── host.py             # Host 数据模型
│   ├── group.py            # Group 数据模型
│   └── data.py             # 数据解析
│
├── vars/                   # 变量系统
│   ├── manager.py          # 变量管理器
│   └── hostvars.py         # 主机变量
│
├── plugins/                # 插件系统
│   ├── connection/         # 连接插件
│   │   ├── ssh.py
│   │   └── local.py
│   ├── modules/            # 模块插件
│   │   ├── ping.py
│   │   ├── shell.py
│   │   ├── copy.py
│   │   └── ...
│   ├── callback/           # 回调插件
│   │   ├── default.py
│   │   ├── json.py
│   │   └── yaml.py
│   ├── lookup/             # 查找插件
│   │   ├── file.py
│   │   ├── pipe.py
│   │   └── env.py
│   ├── filter/             # 过滤器插件
│   │   └── core.py
│   ├── test/               # 测试插件
│   │   └── core.py
│   └── strategy/           # 策略插件
│       ├── linear.py
│       └── free.py
│
├── playbook/               # Playbook 解析
│   ├── playbook.py         # Playbook 解析器
│   ├── play.py             # Play 数据模型
│   ├── task.py             # Task 数据模型
│   ├── role/               # Role 处理
│   └── block.py            # Block 数据模型
│
├── template/               # 模板引擎
│   ├── __init__.py
│   └── safe_eval.py
│
├── vault/                  # Vault 加密
│   ├── vault.py
│   └── __init__.py
│
└── galaxy/                 # Galaxy 客户端
    ├── api.py
    └── role.py
```

### 1.2 关键包的职责

| 包 | 职责 | 核心类 |
|---|------|--------|
| `ansible.cli` | 解析命令行参数，分发到对应命令 | `CLI`, `AdHocCLI`, `PlaybookCLI` |
| `ansible.executor` | 执行 Play/Task，管理并发 | `TaskQueueManager`, `TaskExecutor` |
| `ansible.inventory` | 解析和管理主机清单 | `InventoryManager`, `Host`, `Group` |
| `ansible.vars` | 变量加载、合并、作用域管理 | `VariableManager` |
| `ansible.plugins` | 所有插件的实现和注册 | `PluginLoader`, 各种插件类 |
| `ansible.playbook` | Playbook YAML 解析和数据模型 | `Playbook`, `Play`, `Task` |
| `ansible.template` | Jinja2 模板渲染 | `Templar` |
| `ansible.vault` | 文件加密/解密 | `VaultLib` |

### 1.3 Ansible 的执行入口

当你运行 `ansible-playbook site.yml` 时，发生了什么：

```
1. CLI 层
   ansible-playbook 命令 → PlaybookCLI.__init__()
   → 解析命令行参数（-i, --limit, --tags 等）
   → 创建 Options 对象

2. Inventory 加载
   → InventoryManager.parse_inventory(sources)
   → 解析 INI/YAML 文件 → 构建 Host/Group 图
   → 加载 group_vars/, host_vars/

3. Playbook 解析
   → Playbook.load(file_list, variable_manager, loader)
   → 解析 YAML → 构建 Play/Task/Block 数据结构
   → 合并变量

4. 执行引擎
   → PlaybookExecutor.run()
   → 遍历 Plays → 对每个 Play 创建 Strategy
   → Strategy 调度 Tasks 到各主机
   → TaskExecutor 执行单个 Task

5. 结果输出
   → Callback 插件接收事件
   → 默认回调打印到终端
```

---

## 二、五层架构详解

ansible-go 采用五层架构，每一层都有明确的职责边界：

```
┌─────────────────────────────────────────────────┐
│                   CLI Layer                      │
│         (cobra CLI, 参数解析, 子命令)              │
├─────────────────────────────────────────────────┤
│                Command Layer                     │
│   (ad-hoc / playbook / vault / galaxy 命令)      │
├─────────────────────────────────────────────────┤
│                Engine Layer                      │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────┐ │
│  │ Inventory│ │ Playbook │ │   Variable Mgr   │ │
│  │  Parser  │ │  Engine  │ │ (优先级/合并)     │ │
│  └──────────┘ └──────────┘ └──────────────────┘ │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────┐ │
│  │ Template │ │  Facts   │ │   Handler Mgr    │ │
│  │  Engine  │ │ Collector│ │ (通知/触发)       │ │
│  └──────────┘ └──────────┘ └──────────────────┘ │
├─────────────────────────────────────────────────┤
│               Module Layer                       │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌───────┐ │
│  │ shell│ │ copy │ │ file │ │template│ │ ping │ │
│  └──────┘ └──────┘ └──────┘ └──────┘ └───────┘ │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐           │
│  │ yum  │ │ apt  │ │ service│ │ user │  ...     │
│  └──────┘ └──────┘ └──────┘ └──────┘           │
├─────────────────────────────────────────────────┤
│              Connection Layer                    │
│  ┌──────────┐ ┌──────────┐                      │
│  │   SSH    │ │  Local   │                      │
│  │(x/crypto)│ │          │                      │
│  └──────────┘ └──────────┘                      │
└─────────────────────────────────────────────────┘
```

### 2.1 CLI Layer（命令行层）

**职责：** 解析用户输入的命令行参数，分发到对应的命令处理器。

**核心组件：**

```go
// CLI 层核心结构
type RootCommand struct {
    GlobalOptions GlobalOptions
    SubCommands   map[string]Command
}

type GlobalOptions struct {
    Inventory      string
    User           string
    PrivateKeyFile string
    Become         bool
    BecomeMethod   string
    BecomeUser     string
    Forks          int
    Verbosity      int
    Timeout        int
    Diff           bool
    Check          bool
    Limit          string
    Tags           string
    SkipTags       string
    ExtraVars      []string
}
```

**职责边界：**
- 只负责参数解析和验证
- 不负责业务逻辑
- 将解析后的参数传递给 Command Layer

**设计决策：** 使用 cobra 框架，因为它提供了子命令、标志、帮助生成等标准化能力，是 Go CLI 开发的事实标准。

### 2.2 Command Layer（命令层）

**职责：** 将 CLI 参数转换为 Engine Layer 的调用，协调各组件的初始化和调用顺序。

**核心组件：**

```go
// 命令层接口
type Command interface {
    Execute(args []string) error
}

// Ad-hoc 命令
type AdhocCommand struct {
    HostPattern string
    ModuleName  string
    ModuleArgs  string
    Options     GlobalOptions
}

// Playbook 命令
type PlaybookCommand struct {
    PlaybookFiles []string
    Options       GlobalOptions
}
```

**职责边界：**
- 初始化 Engine Layer 的各组件
- 编排组件之间的调用顺序
- 处理命令级别的错误和退出码

**与 CLI Layer 的区别：**
- CLI Layer 只负责"解析参数"
- Command Layer 负责"执行命令"
- 同一个 Command 可以被不同的 CLI 入口调用（如测试、API）

### 2.3 Engine Layer（引擎层）

**职责：** 核心业务逻辑，包括 Inventory 解析、Playbook 执行、变量管理、模板渲染等。

**核心组件：**

```go
// Inventory 解析器
type InventoryParser interface {
    Parse(data []byte) (*Inventory, error)
    Detect(data []byte) bool
}

// Playbook 引擎
type PlaybookEngine interface {
    Execute(playbook *Playbook, inventory *Inventory, vars *VariableContext) (*PlayStats, error)
}

// 变量管理器
type VariableManager interface {
    GetVars(host *Host, play *Play, task *Task) (map[string]any, error)
    MergeVars(base map[string]any, overlay map[string]any) map[string]any
}

// 模板引擎
type TemplateEngine interface {
    Render(template string, vars map[string]any) (string, error)
}

// Facts 收集器
type FactsCollector interface {
    Collect(host *Host, conn Connection) (map[string]any, error)
}

// Handler 管理器
type HandlerManager interface {
    Notify(handlerName string, host *Host)
    Flush(hosts []*Host) []*Task
}
```

**职责边界：**
- 不关心命令行参数如何解析
- 不关心网络连接如何建立
- 不关心具体模块如何执行
- 专注于"编排逻辑"——什么任务在什么主机上以什么顺序执行

### 2.4 Module Layer（模块层）

**职责：** 实现具体的自动化操作（安装包、管理服务、拷贝文件等）。

**核心接口：**

```go
// 模块接口
type Module interface {
    Name() string
    Args() []ModuleArg
    Run(ctx ExecContext) (Result, error)
    SupportsCheckMode() bool
}

// 执行上下文
type ExecContext struct {
    Host       *Host
    Args       map[string]any
    Connection Connection
    CheckMode  bool
    Diff       bool
    Variables  map[string]any
}

// 执行结果
type Result struct {
    Changed bool
    Failed  bool
    Msg     string
    Stdout  string
    Stderr  string
    Rc      int
    Diff    *DiffResult
    Extra   map[string]any
}
```

**职责边界：**
- 每个模块只关心自己的功能
- 不关心调度、并发、变量优先级
- 通过 ExecContext 获取所需信息
- 通过 Result 返回执行结果

### 2.5 Connection Layer（连接层）

**职责：** 提供与远程主机的通信能力。

**核心接口：**

```go
// 连接接口
type Connection interface {
    Connect(host Host) error
    Exec(cmd string) (stdout, stderr string, rc int, error)
    PutFile(localPath, remotePath string) error
    FetchFile(remotePath, localPath string) error
    Close() error
    Shell() string
}
```

**职责边界：**
- 只关心"如何连接"和"如何传输"
- 不关心连接上执行什么命令
- 不关心命令的业务含义

### 2.6 层间依赖关系

```
CLI Layer
    │
    ▼
Command Layer
    │
    ▼
Engine Layer
    │
    ├──→ Module Layer
    │        │
    │        ▼
    │    Connection Layer
    │
    └──→ Connection Layer（Facts 收集）
```

**依赖规则：**
- 上层可以依赖下层，下层不能依赖上层
- 同层之间通过接口交互
- 所有依赖都通过接口，不依赖具体实现

---

## 三、数据流全景

### 3.1 完整数据流

以 `ansible-go playbook site.yml -i inventory/hosts` 为例，完整的数据流如下：

```
┌──────────────────────────────────────────────────────────────┐
│                        用户输入                               │
│              ansible-go playbook site.yml -i inventory/hosts  │
└──────────────────────────┬───────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  1. CLI 解析                                                  │
│     cobra 解析参数 → GlobalOptions{                           │
│       Inventory: "inventory/hosts",                           │
│       PlaybookFiles: ["site.yml"],                            │
│       Forks: 5, ...                                           │
│     }                                                         │
└──────────────────────────┬───────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  2. Inventory 加载                                            │
│     读取 inventory/hosts → 检测格式（INI/YAML）               │
│     → 解析为 Inventory 结构                                   │
│     → 加载 group_vars/ 和 host_vars/                          │
│     → 构建 Host/Group 图                                      │
│                                                              │
│     输出：Inventory{                                          │
│       Groups: {"all": ..., "webservers": ..., "dbservers": ...}, │
│       Hosts: {"web1": ..., "web2": ..., "db1": ...}         │
│     }                                                        │
└──────────────────────────┬───────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  3. Playbook 解析                                             │
│     读取 site.yml → 解析 YAML                                 │
│     → 构建 Playbook → Play → Task 数据结构                    │
│     → 合并 vars_files                                         │
│     → 处理 roles 引用                                         │
│                                                              │
│     输出：Playbook{                                           │
│       Plays: [                                               │
│         Play{Name: "Configure webservers", Hosts: "webservers", │
│           Tasks: [Task{Name: "Install nginx", Module: "yum", ...}]} │
│       ]                                                      │
│     }                                                        │
└──────────────────────────┬───────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  4. 遍历 Plays                                                │
│     for each Play in Playbook.Plays:                          │
│       │                                                       │
│       ▼                                                       │
│     4a. 主机匹配                                              │
│       Hosts: "webservers" → 查找 Inventory.Groups["webservers"] │
│       → 获取主机列表 [web1, web2]                             │
│       → 应用 --limit 过滤                                     │
│       │                                                       │
│       ▼                                                       │
│     4b. 变量合并                                              │
│       合并层级：                                               │
│         group_vars/all.yml                                    │
│         + group_vars/webservers.yml                           │
│         + host_vars/web1.yml                                  │
│         + play vars:                                          │
│         + extra-vars                                          │
│       → 每台主机生成独立的变量上下文                            │
│       │                                                       │
│       ▼                                                       │
│     4c. Facts 收集（如果 gather_facts: true）                  │
│       SSH 到每台主机 → 执行 setup 命令                         │
│       → 收集 ansible_os_family, ansible_hostname 等           │
│       → 注入变量上下文                                         │
│       │                                                       │
│       ▼                                                       │
│     4d. 执行 Tasks                                             │
│       （见下方详细流程）                                        │
└──────────────────────────────────────────────────────────────┘
```

### 3.2 Task 执行详细流程

```
┌──────────────────────────────────────────────────────────────┐
│  5. Task 执行                                                 │
│     for each Task in Play.Tasks:                              │
│       │                                                       │
│       ▼                                                       │
│     5a. 模板渲染                                              │
│       Task.Name: "Install {{ package }}" → "Install nginx"   │
│       Task.Args: {"name": "{{ package }}"} → {"name": "nginx"} │
│       Task.When: "{{ ansible_os_family == 'RedHat' }}" → "true" │
│       │                                                       │
│       ▼                                                       │
│     5b. 条件检查                                              │
│       When 条件为 false → 跳过（skipped）                     │
│       When 条件为 true → 继续执行                             │
│       │                                                       │
│       ▼                                                       │
│     5c. 循环展开                                              │
│       Loop: ["nginx", "vim", "curl"]                          │
│       → 展开为 3 个子任务，每个子任务的 item 变量不同          │
│       │                                                       │
│       ▼                                                       │
│     5d. 模块查找                                              │
│       Module: "yum" → 查找模块注册表                          │
│       → 找到 YumModule{}                                      │
│       │                                                       │
│       ▼                                                       │
│     5e. 模块执行                                              │
│       创建 ExecContext{                                        │
│         Host: web1,                                           │
│         Args: {"name": "nginx", "state": "present"},         │
│         Connection: SSHConnection(web1),                      │
│         CheckMode: false,                                     │
│         Variables: mergedVars                                 │
│       }                                                       │
│       → YumModule.Run(ctx)                                    │
│       │                                                       │
│       │   ┌──────────────────────────────────────────────┐    │
│       │   │  YumModule.Run 内部流程：                     │    │
│       │   │  1. 检查是否已安装：rpm -q nginx              │    │
│       │   │  2. 已安装 → 返回 Result{Changed: false}     │    │
│       │   │  3. 未安装 → 执行 yum install -y nginx       │    │
│       │   │  4. 检查退出码                               │    │
│       │   │  5. 返回 Result{Changed: true, Rc: 0}        │    │
│       │   └──────────────────────────────────────────────┘    │
│       │                                                       │
│       ▼                                                       │
│     5f. 结果处理                                              │
│       Result.Changed = true → 通知 Handler（如果有 notify）   │
│       Result.Failed = true → 检查 ignore_errors              │
│       → 更新主机统计（ok/changed/failed/skipped）             │
│       │                                                       │
│       ▼                                                       │
│     5g. 回调通知                                              │
│       CallbackPlugin.OnTaskOk(result) 或                      │
│       CallbackPlugin.OnTaskFailed(result)                     │
│       → 打印到终端                                            │
└──────────────────────────────────────────────────────────────┘
```

### 3.3 完整数据流 ASCII 图

```
用户输入
  │
  ▼
┌─────────┐    ┌──────────────┐    ┌──────────────┐
│   CLI   │───▶│   Command    │───▶│   Engine     │
│ (cobra) │    │ (adhoc/      │    │ (inventory/  │
│         │    │  playbook)   │    │  playbook/   │
└─────────┘    └──────────────┘    │  variables)  │
                                   └──────┬───────┘
                                          │
                          ┌───────────────┼───────────────┐
                          │               │               │
                          ▼               ▼               ▼
                   ┌────────────┐  ┌────────────┐  ┌────────────┐
                   │  Template  │  │   Facts    │  │  Handler   │
                   │  Engine    │  │  Collector │  │  Manager   │
                   └────────────┘  └──────┬─────┘  └────────────┘
                                          │
                                          ▼
                                   ┌────────────┐
                                   │  Strategy  │
                                   │ (linear/   │
                                   │  free)     │
                                   └──────┬─────┘
                                          │
                          ┌───────────────┼───────────────┐
                          │               │               │
                          ▼               ▼               ▼
                   ┌────────────┐  ┌────────────┐  ┌────────────┐
                   │  Module    │  │  Module    │  │  Module    │
                   │  (yum)     │  │  (service) │  │  (copy)    │
                   └──────┬─────┘  └──────┬─────┘  └──────┬─────┘
                          │               │               │
                          └───────────────┼───────────────┘
                                          │
                                          ▼
                                   ┌────────────┐
                                   │ Connection │
                                   │ (SSH/Local)│
                                   └──────┬─────┘
                                          │
                                          ▼
                                   ┌────────────┐
                                   │  远程主机   │
                                   │ (Linux)    │
                                   └────────────┘
```

---

## 四、插件化架构

### 4.1 插件类型总览

ansible-go 有 7 种插件类型，每种都有明确的接口定义：

| 插件类型 | 接口 | 注册方式 | 生命周期 |
|----------|------|---------|---------|
| Connection | `Connection` | `connection.Register()` | 每个主机一个实例 |
| Module | `Module` | `modules.Register()` | 全局单例 |
| Callback | `CallbackPlugin` | `callback.Register()` | 全局单例 |
| Lookup | `LookupPlugin` | `lookup.Register()` | 按需创建 |
| Filter | `FilterFunc` | `filter.Register()` | 函数注册 |
| Test | `TestFunc` | `test.Register()` | 函数注册 |
| Strategy | `Strategy` | `strategy.Register()` | 每个 Play 一个实例 |

### 4.2 Connection 插件

```go
// 连接插件接口
type Connection interface {
    // Connect 建立到远程主机的连接
    Connect(host Host) error

    // Exec 在远程主机上执行命令
    Exec(cmd string) (stdout, stderr string, rc int, err error)

    // PutFile 将本地文件传输到远程主机
    PutFile(localPath, remotePath string) error

    // FetchFile 从远程主机拉取文件
    FetchFile(remotePath, localPath string) error

    // Close 关闭连接
    Close() error

    // Shell 返回远程主机的默认 shell
    Shell() string
}
```

**内置实现：**
- `SSHConnection`——基于 `x/crypto/ssh` 的 SSH 连接
- `LocalConnection`——基于 `os/exec` 的本地连接

### 4.3 Module 插件

```go
// 模块插件接口
type Module interface {
    // Name 返回模块名称
    Name() string

    // Args 返回模块参数定义
    Args() []ModuleArg

    // Run 执行模块
    Run(ctx ExecContext) (Result, error)

    // SupportsCheckMode 返回是否支持干跑模式
    SupportsCheckMode() bool
}

// 模块参数定义
type ModuleArg struct {
    Name     string
    Type     string  // "str", "int", "bool", "list", "dict"
    Required bool
    Default  any
    Choices  []any
}
```

**内置模块分类：**

```
文件管理类：copy, template, file, stat, find, lineinfile, blockinfile
包管理类：  yum, apt, dnf, pip
服务管理类：service, systemd
命令执行类：shell, command, script, raw, expect
用户管理类：user, group, authorized_key
网络类：    uri, get_url, wait_for, wait_for_connection
系统类：    hostname, cron, sysctl, setup, debug, assert
异步类：    async_status
```

### 4.4 Callback 插件

```go
// 回调插件接口
type CallbackPlugin interface {
    // OnPlaybookStart playbook 开始执行时调用
    OnPlaybookStart(playbook string)

    // OnPlayStart play 开始执行时调用
    OnPlayStart(play Play, hosts []string)

    // OnTaskStart task 开始执行时调用
    OnTaskStart(task Task, isHandler bool)

    // OnTaskOk task 执行成功时调用
    OnTaskOk(result TaskResult)

    // OnTaskFailed task 执行失败时调用
    OnTaskFailed(result TaskResult, ignored bool)

    // OnTaskSkipped task 被跳过时调用
    OnTaskSkipped(result TaskResult)

    // OnTaskUnreachable 主机不可达时调用
    OnTaskUnreachable(host string, result TaskResult)

    // OnPlaybookStats playbook 执行完毕时调用
    OnPlaybookStats(stats PlayStats)
}
```

**内置回调：**
- `DefaultCallback`——标准输出格式（彩色）
- `MinimalCallback`——一行一个结果
- `JSONCallback`——完整 JSON 输出
- `YAMLCallback`——人类可读 YAML 格式

### 4.5 Lookup 插件

```go
// 查找插件接口
type LookupPlugin interface {
    // Name 返回插件名称
    Name() string

    // Run 执行查找，返回结果列表
    Run(terms []string, variables map[string]any) ([]string, error)
}
```

**内置查找插件：**
- `file`——读取文件内容
- `pipe`——执行命令获取输出
- `env`——读取环境变量
- `password`——生成/读取密码
- `template`——渲染模板返回字符串

### 4.6 Filter 和 Test 插件

```go
// 过滤器函数签名
type FilterFunc func(input any, args ...any) (any, error)

// 测试函数签名
type TestFunc func(input any, args ...any) (bool, error)
```

Filter 和 Test 是函数级别的插件，直接注册到模板引擎中使用。

### 4.7 Strategy 插件

```go
// 策略插件接口
type Strategy interface {
    // Run 执行 Play 的所有 Tasks
    Run(
        play *Play,
        hosts []*Host,
        tasks []*Task,
        iterator TaskIterator,
        resultCallback func(host *Host, result Result),
    ) error

    // Name 返回策略名称
    Name() string
}
```

**内置策略：**
- `Linear`——每个 task 等所有主机完成后再进入下一个（默认）
- `Free`——每台主机独立推进，互不等待

### 4.8 Go 的 init() 注册模式

Go 的 `init()` 函数在包被导入时自动执行，这为插件注册提供了天然的机制：

```go
// internal/modules/ping.go
package modules

func init() {
    Register("ping", &PingModule{})
}

type PingModule struct{}

func (m *PingModule) Name() string          { return "ping" }
func (m *PingModule) SupportsCheckMode() bool { return true }

func (m *PingModule) Run(ctx ExecContext) (Result, error) {
    return Result{
        Changed: false,
        Msg:     "pong",
    }, nil
}
```

```go
// internal/modules/registry.go
package modules

var registry = map[string]Module{}

func Register(name string, module Module) {
    registry[name] = module
}

func Get(name string) (Module, bool) {
    m, ok := registry[name]
    return m, ok
}
```

```go
// cmd/ansible-go/main.go
package main

// 这个 import 触发所有模块的 init() 注册
import _ "github.com/yourname/ansible-go/internal/modules"
```

**这种模式的优势：**
- 插件自注册——不需要在某个中心文件中手动添加
- 编译时链接——未使用的插件不会被编译进去
- 测试友好——测试时可以只导入需要的插件

---

## 五、并发模型对比

### 5.1 Ansible 的 fork 进程池

Ansible 使用 Python 的 `multiprocessing` 模块实现并发。每个主机 fork 一个子进程：

```
主进程（ansible-playbook）
│
├── fork → 子进程 1（web1）
│   ├── 建立 SSH 连接
│   ├── 执行 Task 1
│   ├── 执行 Task 2
│   ├── ...
│   └── 退出
│
├── fork → 子进程 2（web2）
│   ├── 建立 SSH 连接
│   ├── 执行 Task 1
│   ├── 执行 Task 2
│   ├── ...
│   └── 退出
│
└── fork → 子进程 3（web3）
    ├── ...
    └── 退出
```

**fork 模型的特点：**

```
优点：
- 进程隔离——一个子进程崩溃不影响其他
- 无 GIL——每个进程独立运行
- 简单可靠——成熟的进程管理机制

缺点：
- 内存开销大——每个进程 ~10MB
- 启动开销大——fork 一个进程 ~50ms
- 通信复杂——进程间通信需要 Pipe/Queue
- 并发数受限——默认只有 5 个 fork
```

**Ansible 的执行策略：**

```
Linear 策略（默认）：
  Task 1: web1 ──┐
                  ├── 等待全部完成 → Task 2: web1 ──┐
  Task 1: web2 ──┘                                   ├── 等待全部完成 → ...
                  Task 1: web3 ──┘                   │
                                                     Task 2: web2 ──┘
                                    Task 2: web3 ──┘

Free 策略：
  web1: Task 1 → Task 2 → Task 3 → ...
  web2: Task 1 → Task 2 → Task 3 → ...  （各自独立推进）
  web3: Task 1 → Task 2 → Task 3 → ...
```

### 5.2 Go 的 goroutine Worker Pool

ansible-go 使用 goroutine 实现并发，采用 Worker Pool 模式：

```
主 goroutine
│
├── 创建 Task Queue（channel）
├── 创建 Result Queue（channel）
│
├── 启动 Worker Pool（N 个 worker goroutine）
│   ├── Worker 1: 从 Task Queue 取任务 → 执行 → 结果送入 Result Queue
│   ├── Worker 2: 从 Task Queue 取任务 → 执行 → 结果送入 Result Queue
│   ├── Worker 3: 从 Task Queue 取任务 → 执行 → 结果送入 Result Queue
│   └── ...（N = forks，默认 5）
│
├── 调度器 goroutine: 分发任务到 Task Queue
│   ├── Linear 策略: 等待当前 Task 所有主机完成后再分发下一个
│   └── Free 策略: 每个主机独立分发任务
│
└── 结果收集器 goroutine: 从 Result Queue 收集结果
    ├── 更新统计信息
    ├── 通知回调插件
    └── 检查是否需要停止
```

**Go 并发模型的特点：**

```
优点：
- 内存开销极小——每个 goroutine ~2KB
- 启动开销极小——创建 goroutine ~1μs
- 通信简单——channel 是一等公民
- 并发数高——可以轻松管理数千个 goroutine
- 无 GIL——真正的并行执行

缺点：
- 无进程隔离——一个 goroutine panic 可能影响整个程序
- 需要 careful error handling——必须 recover panic
- 调试复杂——并发代码调试比串行代码难
```

### 5.3 并发模型对比总结

| 指标 | Ansible fork | Go goroutine |
|------|-------------|-------------|
| 内存开销 | ~10MB/进程 | ~2KB/goroutine |
| 启动时间 | ~50ms/进程 | ~1μs/goroutine |
| 最大并发 | 默认 5 | 数千个 |
| 通信方式 | Pipe/Queue | Channel |
| 错误隔离 | 进程级别 | 需要 recover |
| 调试难度 | 中等 | 较高 |
| 资源回收 | 进程退出自动回收 | GC 自动回收 |

### 5.4 ansible-go 的并发实现要点

```go
// Worker Pool 实现要点

// 1. 任务队列
type TaskJob struct {
    Host *Host
    Task *Task
    Vars map[string]any
}

// 2. 结果队列
type TaskResult struct {
    Host   *Host
    Task   *Task
    Result Result
    Error  error
}

// 3. Worker Pool
type WorkerPool struct {
    workers   int
    taskQueue chan TaskJob
    resultQueue chan TaskResult
    done      chan struct{}
}

// 4. 策略接口
type Strategy interface {
    // Run 执行策略，管理任务分发和结果收集
    Run(pool *WorkerPool, play *Play, hosts []*Host, tasks []*Task) error
}
```

**关键设计决策：**

1. **Channel 作为任务队列**——天然的线程安全队列
2. **WaitGroup 等待所有主机完成**——确保 Linear 策略的同步点
3. **Context 取消**——支持 Ctrl+C 中断执行
4. **Recover panic**——防止一个 goroutine 的 panic 影响整个程序

---

## 六、ansible-go 项目结构映射

### 6.1 目录树

```
ansible-go/
├── cmd/
│   └── ansible-go/
│       └── main.go                    # 程序入口
│
├── internal/                          # 私有包（不可被外部导入）
│   ├── cli/                           # CLI 层
│   │   ├── root.go                    # 根命令 + 全局标志
│   │   ├── adhoc.go                   # ad-hoc 命令
│   │   ├── playbook.go                # playbook 命令
│   │   ├── inventory.go               # inventory 命令
│   │   ├── vault.go                   # vault 命令
│   │   ├── galaxy.go                  # galaxy 命令
│   │   └── config.go                  # config 命令
│   │
│   ├── engine/                        # 执行引擎
│   │   ├── engine.go                  # 引擎核心
│   │   ├── playbook.go                # Playbook 执行
│   │   ├── play.go                    # Play 执行
│   │   ├── task.go                    # Task 执行
│   │   ├── facts.go                   # Facts 收集
│   │   └── handler.go                 # Handler 管理
│   │
│   ├── strategy/                      # 策略插件
│   │   ├── strategy.go                # 策略接口和注册
│   │   ├── linear.go                  # Linear 策略
│   │   └── free.go                    # Free 策略
│   │
│   ├── inventory/                     # Inventory 系统
│   │   ├── inventory.go               # 数据模型
│   │   ├── ini_parser.go              # INI 解析器
│   │   ├── yaml_parser.go             # YAML 解析器
│   │   ├── host_pattern.go            # 主机模式匹配
│   │   └── loader.go                  # Inventory 加载器
│   │
│   ├── variables/                     # 变量系统
│   │   ├── manager.go                 # 变量管理器
│   │   ├── context.go                 # 变量上下文
│   │   ├── precedence.go              # 优先级合并
│   │   └── magic.go                   # 内置变量
│   │
│   ├── template/                      # 模板引擎
│   │   ├── engine.go                  # 模板引擎核心
│   │   ├── preprocessor.go            # 变量前缀预处理
│   │   └── filters.go                 # Ansible 特有过滤器
│   │
│   ├── connection/                    # 连接层
│   │   ├── connection.go              # 连接接口和注册
│   │   ├── ssh.go                     # SSH 连接实现
│   │   ├── local.go                   # 本地连接实现
│   │   └── pool.go                    # 连接池
│   │
│   ├── modules/                       # 模块系统
│   │   ├── registry.go                # 模块注册表
│   │   ├── ping.go                    # ping 模块
│   │   ├── shell.go                   # shell 模块
│   │   ├── command.go                 # command 模块
│   │   ├── copy.go                    # copy 模块
│   │   ├── file.go                    # file 模块
│   │   ├── template.go                # template 模块
│   │   ├── yum.go                     # yum 模块
│   │   ├── apt.go                     # apt 模块
│   │   ├── service.go                 # service 模块
│   │   ├── setup.go                   # setup 模块（Facts 收集）
│   │   └── debug.go                   # debug 模块
│   │
│   ├── plugins/                       # 其他插件
│   │   ├── callback/
│   │   │   ├── callback.go            # 回调接口和注册
│   │   │   ├── default.go             # 默认回调
│   │   │   ├── json.go                # JSON 回调
│   │   │   └── yaml.go                # YAML 回调
│   │   ├── lookup/
│   │   │   ├── lookup.go              # 查找接口和注册
│   │   │   ├── file.go                # file 查找
│   │   │   └── pipe.go                # pipe 查找
│   │   ├── filter/
│   │   │   ├── filter.go              # 过滤器注册
│   │   │   └── core.go                # 核心过滤器
│   │   └── test/
│   │       ├── test.go                # 测试注册
│   │       └── core.go                # 核心测试
│   │
│   ├── vault/                         # Vault 加密
│   │   ├── vault.go                   # Vault 核心
│   │   ├── crypto.go                  # 加密算法
│   │   └── password.go                # 密码管理
│   │
│   ├── galaxy/                        # Galaxy 客户端
│   │   ├── client.go                  # Galaxy API 客户端
│   │   └── installer.go               # 角色/集合安装
│   │
│   ├── roles/                         # Roles 系统
│   │   ├── role.go                    # Role 数据模型
│   │   ├── loader.go                  # Role 加载器
│   │   └── dependency.go              # 依赖解析
│   │
│   ├── config/                        # 配置系统
│   │   ├── config.go                  # 配置加载
│   │   └── defaults.go                # 默认值
│   │
│   └── logging/                       # 日志系统
│       └── logger.go                  # 日志实现
│
├── pkg/                               # 公共包（可被外部导入）
│   ├── types/                         # 公共类型
│   │   ├── host.go                    # Host 类型
│   │   ├── group.go                   # Group 类型
│   │   └── result.go                  # Result 类型
│   └── utils/                         # 工具函数
│       ├── hash.go                    # 哈希工具
│       └── file.go                    # 文件工具
│
├── testdata/                          # 测试数据
│   ├── hosts.ini
│   ├── hosts.yml
│   ├── site.yml
│   └── roles/
│
├── docs/                              # 文档
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### 6.2 与 Ansible Python 包的对应关系

| ansible-go 目录 | Ansible Python 包 | 说明 |
|----------------|-------------------|------|
| `internal/cli/` | `ansible/cli/` | CLI 入口和子命令 |
| `internal/engine/` | `ansible/executor/` | 执行引擎 |
| `internal/inventory/` | `ansible/inventory/` | Inventory 系统 |
| `internal/variables/` | `ansible/vars/` | 变量系统 |
| `internal/template/` | `ansible/template/` | 模板引擎 |
| `internal/connection/` | `ansible/plugins/connection/` | 连接插件 |
| `internal/modules/` | `ansible/plugins/modules/` | 模块插件 |
| `internal/plugins/callback/` | `ansible/plugins/callback/` | 回调插件 |
| `internal/plugins/lookup/` | `ansible/plugins/lookup/` | 查找插件 |
| `internal/plugins/filter/` | `ansible/plugins/filter/` | 过滤器插件 |
| `internal/plugins/test/` | `ansible/plugins/test/` | 测试插件 |
| `internal/strategy/` | `ansible/plugins/strategy/` | 策略插件 |
| `internal/vault/` | `ansible/vault/` | Vault 加密 |
| `internal/galaxy/` | `ansible/galaxy/` | Galaxy 客户端 |
| `internal/roles/` | `ansible/playbook/role/` | Roles 系统 |
| `internal/config/` | `ansible/config/` | 配置系统 |

### 6.3 internal vs pkg 的选择

ansible-go 使用 Go 的 `internal/` 包可见性规则：

```
internal/    → 只能被 ansible-go 项目内部导入
pkg/         → 可以被外部项目导入
```

**放在 internal/ 的理由：**
- 所有核心实现都不需要被外部导入
- 防止外部项目依赖 ansible-go 的内部实现
- 便于重构——内部实现可以自由修改而不影响外部

**放在 pkg/ 的理由：**
- 公共类型（Host, Group, Result）可能被外部使用
- 工具函数可能被外部使用
- 作为 ansible-go 的"公共 API"

---

## 七、接口驱动设计

### 7.1 为什么使用接口

ansible-go 的所有核心组件都通过接口交互，这是刻意的设计决策：

**可测试性**

```go
// 使用接口，可以轻松 mock
type Connection interface {
    Exec(cmd string) (stdout, stderr string, rc int, err error)
}

// 测试时使用 mock
type MockConnection struct {
    Responses map[string]MockResponse
}

func (m *MockConnection) Exec(cmd string) (string, string, int, error) {
    resp := m.Responses[cmd]
    return resp.Stdout, resp.Stderr, resp.Rc, resp.Err
}
```

**可替换性**

```go
// 运行时可以替换实现
var conn Connection

if opts.Connection == "ssh" {
    conn = NewSSHConnection()
} else {
    conn = NewLocalConnection()
}

// 使用方不关心具体实现
result, err := module.Run(ExecContext{Connection: conn})
```

**单一职责**

```go
// 每个接口只关心一件事
type InventoryParser interface {
    Parse(data []byte) (*Inventory, error)  // 只关心解析
}

type InventoryLoader interface {
    Load(path string) (*Inventory, error)   // 只关心加载
}

type HostMatcher interface {
    Match(pattern string, inv *Inventory) []*Host  // 只关心匹配
}
```

### 7.2 核心接口定义

**Connection 接口——连接层的核心抽象**

```go
type Connection interface {
    Connect(host Host) error
    Exec(cmd string) (stdout, stderr string, rc int, err error)
    PutFile(localPath, remotePath string) error
    FetchFile(remotePath, localPath string) error
    Close() error
    Shell() string
}
```

这个接口抽象了所有连接方式的共同行为。无论是 SSH、Local 还是未来可能的 WinRM，都通过这个接口交互。

**Module 接口——模块层的核心抽象**

```go
type Module interface {
    Name() string
    Args() []ModuleArg
    Run(ctx ExecContext) (Result, error)
    SupportsCheckMode() bool
}
```

每个模块都实现这个接口。Engine Layer 通过这个接口调用模块，不需要关心模块的具体实现。

**CallbackPlugin 接口——回调插件的核心抽象**

```go
type CallbackPlugin interface {
    OnPlaybookStart(playbook string)
    OnPlayStart(play Play, hosts []string)
    OnTaskStart(task Task, isHandler bool)
    OnTaskOk(result TaskResult)
    OnTaskFailed(result TaskResult, ignored bool)
    OnTaskSkipped(result TaskResult)
    OnTaskUnreachable(host string, result TaskResult)
    OnPlaybookStats(stats PlayStats)
}
```

回调插件通过事件驱动模式接收执行过程中的各种事件。默认回调打印到终端，JSON 回调输出 JSON 格式，用户也可以实现自己的回调。

**LookupPlugin 接口——查找插件的核心抽象**

```go
type LookupPlugin interface {
    Name() string
    Run(terms []string, variables map[string]any) ([]string, error)
}
```

查找插件用于在模板渲染时获取外部数据（文件内容、环境变量、命令输出等）。

**Strategy 接口——策略插件的核心抽象**

```go
type Strategy interface {
    Name() string
    Run(
        play *Play,
        hosts []*Host,
        tasks []*Task,
        hostConn map[string]Connection,
        callback CallbackPlugin,
    ) (*PlayStats, error)
}
```

策略插件决定了任务如何在多台主机上调度执行。Linear 策略保证所有主机同步推进，Free 策略允许各主机独立执行。

### 7.3 接口设计原则

**接口隔离原则（ISP）**

```go
// 好：小接口，每个接口只做一件事
type Executor interface {
    Execute(ctx ExecContext) (Result, error)
}

type Validator interface {
    Validate(args map[string]any) error
}

type Checker interface {
    SupportsCheckMode() bool
}

// 坏：大接口，包含太多方法
type Module interface {
    Name() string
    Args() []ModuleArg
    Validate(args map[string]any) error
    Execute(ctx ExecContext) (Result, error)
    SupportsCheckMode() bool
    // ... 更多方法
}
```

ansible-go 的 Module 接口虽然包含多个方法，但每个方法都有明确的职责，且都是模块必须具备的核心能力。

**依赖倒置原则（DIP）**

```go
// Engine 依赖接口，不依赖具体实现
type PlaybookEngine struct {
    inventoryParser InventoryParser  // 接口
    templateEngine  TemplateEngine   // 接口
    moduleRegistry  ModuleRegistry   // 接口
    connFactory     ConnectionFactory // 接口
    callback        CallbackPlugin   // 接口
}

// 具体实现通过依赖注入
engine := &PlaybookEngine{
    inventoryParser: inventory.NewINIParser(),
    templateEngine:  template.NewEngine(),
    moduleRegistry:  modules.DefaultRegistry(),
    connFactory:     connection.NewSSHFactory(),
    callback:        callback.NewDefaultCallback(),
}
```

---

## 参考资料

- [Ansible 源码结构](https://github.com/ansible/ansible/tree/devel/lib/ansible)
- [Go 接口设计](https://go.dev/doc/effective_go#interfaces)
- [Go 并发模式](https://go.dev/doc/effective_go#concurrency)
- [cobra 文档](https://cobra.dev/)
- [x/crypto/ssh 文档](https://pkg.go.dev/golang.org/x/crypto/ssh)
- [设计文档](../superpowers/specs/2026-05-25-ansible-go-design.md)
- [实现计划](../superpowers/plans/2026-05-25-ansible-go-implementation.md)
