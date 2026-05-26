# 模块系统

> 阶段：P4 | 设计文档引用：第六章

模块是 Ansible 的"动词"——每一个模块代表一种要管理的状态：安装一个包、启动一个服务、
写一个文件。Playbook 中的 task 通过调用模块来描述"期望状态"，模块负责判断当前状态与
期望状态的差异，执行必要的变更，然后报告结果。

---

## 1. 模块系统概述

### 1.1 模块是什么

在 Ansible 的世界里，模块（Module）是最小的执行单元。它封装了"管理某个特定资源"
的全部逻辑——参数解析、状态检测、变更执行、结果报告。

```yaml
# 这是一个 task，它调用了 yum 模块
- name: Install nginx
  yum:
    name: nginx
    state: present
```

上面这个 task 的含义是：调用 `yum` 模块，传入参数 `name=nginx, state=present`。
模块会先检查 nginx 是否已经安装，如果已安装则报告 `changed: false`；如果未安装则
执行安装并报告 `changed: true`。

### 1.2 模块的分类

Ansible 拥有数百个模块，按功能可分为以下几大类：

| 类别 | 示例模块 | 职责 |
|------|----------|------|
| 包管理 | `yum`, `apt`, `dnf`, `pip` | 安装/卸载软件包 |
| 文件管理 | `copy`, `template`, `file`, `lineinfile` | 文件操作 |
| 服务管理 | `service`, `systemd` | 启停系统服务 |
| 用户管理 | `user`, `group`, `authorized_key` | 用户/组管理 |
| 命令执行 | `shell`, `command`, `script`, `raw` | 执行命令 |
| 网络 | `uri`, `get_url`, `wait_for` | HTTP 请求、文件下载 |
| 系统 | `hostname`, `cron`, `sysctl`, `setup` | 系统配置 |
| 调试 | `debug`, `assert`, `set_fact` | 调试与断言 |

### 1.3 模块与插件的区别

模块在远程主机上执行（或在本地编排后通过 SSH 发送命令），执行完毕后进程退出。
插件（Plugin）则运行在控制端进程内部，提供连接、回调、查找等扩展能力。

在 ansible-go 的设计中，模块是**本地编排 + SSH 命令**模式：模块在本地根据参数生成
shell 命令，通过 SSH 连接在远程执行，收集输出后解析为统一结果。

---

## 2. Ansible 模块执行模型

### 2.1 原生 Ansible 的执行流程

原生 Ansible 的模块执行是一个复杂的过程：

```
控制节点                          远程主机
─────────                         ────────
1. 模块是 Python 脚本
2. 生成模块执行包裹代码
3. Base64 编码模块代码
4. SSH 连接远程主机
5. 将代码写入临时文件            ├── 创建临时目录
   ~/.ansible/tmp/xxx           │
6. 执行 Python 脚本             ├── python /tmp/xxx
7. 收集 JSON 输出               ├── stdout = JSON 结果
8. 清理临时文件                 ├── 删除临时目录
9. 解析 JSON → Result
```

这个模型有以下特点：

- **模块代码传输**：每个模块都是一个完整的 Python 脚本，需要传输到远程执行
- **临时文件管理**：在远程创建临时目录，用完即删
- **JSON 输出**：模块以 JSON 格式输出结果到 stdout
- **Python 依赖**：远程主机需要 Python 运行时

### 2.2 ansible-go 的执行模型

ansible-go 采用完全不同的策略——**本地编排 + SSH 命令**：

```
ansible-go 控制节点                    远程主机
──────────────────                    ────────
1. 接收模块参数
2. 模块根据参数生成 shell 命令
3. SSH 连接远程主机
4. 直接执行 shell 命令               ├── 执行命令
5. 收集 stdout/stderr/exit code     ├── 返回输出
6. 本地解析结果
7. 关闭连接
```

**核心区别：**

| 维度 | Ansible | ansible-go |
|------|---------|------------|
| 模块语言 | Python 脚本 | Go 函数 |
| 传输方式 | 拷贝脚本到远程 | 不传输，直接执行命令 |
| 远程依赖 | Python | 无（shell 即可） |
| 结果格式 | JSON stdout | 解析 exit code + stdout |
| 扩展方式 | 编写 Python 文件 | 实现 Go 接口并注册 |

这种设计的优势是**不需要远程主机安装任何运行时**，只要有 SSH 和基本的 shell 就能
工作。代价是每个模块都必须能用 shell 命令表达其逻辑。

---

## 3. 模块接口设计

### 3.1 Module 接口

```go
// Module 是所有模块必须实现的核心接口。
type Module interface {
    // Name 返回模块名，必须与 YAML 中使用的名称一致。
    // 例如 "yum", "shell", "copy" 等。
    Name() string

    // Run 执行模块逻辑。
    // ctx 包含执行所需的全部上下文（主机信息、参数、连接等）。
    // 返回 Result 表示执行结果，error 表示框架级错误（非业务失败）。
    Run(ctx ExecContext) (Result, error)

    // SupportsCheckMode 报告模块是否支持干跑模式。
    // 如果返回 false，在 --check 模式下该模块将被跳过。
    SupportsCheckMode() bool
}
```

### 3.2 ModuleArg 结构

```go
// ModuleArg 描述模块接受的参数定义，用于文档生成和参数校验。
type ModuleArg struct {
    Name        string   // 参数名
    Required    bool     // 是否必填
    Default     any      // 默认值
    Type        string   // 类型：str, int, bool, list, dict, path
    Choices     []string // 可选值列表
    Description string   // 描述
}
```

### 3.3 ExecContext 结构

`ExecContext` 是模块执行时的上下文，封装了所有输入信息：

```go
// ExecContext 模块执行上下文，包含执行所需的全部信息。
type ExecContext struct {
    // Host 目标主机信息（名称、端口、变量等）。
    Host *Host

    // Args 模块参数，从 YAML 解析后传入。
    // 键为参数名，值为对应的值（类型可能是 string、int、bool、[]any、map[string]any）。
    Args map[string]any

    // Connection 当前主机的 SSH 或本地连接。
    // 模块通过此接口执行远程命令。
    Connection Connection

    // CheckMode 是否为干跑模式（--check）。
    // 模块应在此模式下只判断状态、不执行变更。
    CheckMode bool

    // Diff 是否显示变更详情（--diff）。
    // 模块可在此模式下返回 DiffResult，展示 before/after 差异。
    Diff bool

    // Variables 当前变量上下文（已合并所有作用域）。
    // 模块可读取变量来辅助执行，但不应修改。
    Variables map[string]any
}
```

**字段说明：**

- **Host**：包含主机名、SSH 端口、主机变量等，模块用它来知道"在谁身上执行"
- **Args**：YAML 中 `module_name:` 下面的键值对，已解析为 Go 类型
- **Connection**：抽象连接接口，模块通过 `Connection.Exec()` 执行命令
- **CheckMode**：布尔值，`true` 时模块只应判断状态、报告 WouldChange，不实际执行
- **Diff**：布尔值，`true` 时模块应返回 DiffResult
- **Variables**：只读的变量映射，包含所有已合并的变量

### 3.4 Result 结构

```go
// Result 模块执行结果，统一的输出格式。
type Result struct {
    // Changed 表示是否发生了实际变更。
    // true = 做了改动，false = 已是期望状态。
    Changed bool

    // Failed 表示是否执行失败。
    // true = 模块遇到错误，Msg 中包含错误信息。
    Failed bool

    // Msg 人类可读的消息。
    // 成功时可为空或描述状态，失败时包含错误描述。
    Msg string

    // Stdout 远程命令的标准输出。
    Stdout string

    // Stderr 远程命令的标准错误。
    Stderr string

    // Rc 远程命令的退出码。
    // 0 = 成功，非 0 = 失败（除非 ignore_errors）。
    Rc int

    // Diff 变更详情（仅在 --diff 模式下填充）。
    Diff *DiffResult

    // Extra 模块特定的额外输出字段。
    // 例如 setup 模块的 ansible_facts，uri 模块的 status_code 等。
    Extra map[string]any
}
```

### 3.5 DiffResult 结构

```go
// DiffResult 描述文件或配置的变更详情。
type DiffResult struct {
    Before string // 变更前的内容
    After  string // 变更后的内容
}
```

### 3.6 完整类型关系图

```
Module (interface)
├── Name() string
├── Run(ExecContext) (Result, error)
└── SupportsCheckMode() bool

ExecContext (struct)                Result (struct)
├── Host *Host                     ├── Changed bool
├── Args map[string]any            ├── Failed bool
├── Connection Connection          ├── Msg string
├── CheckMode bool                 ├── Stdout string
├── Diff bool                      ├── Stderr string
└── Variables map[string]any       ├── Rc int
                                   ├── Diff *DiffResult
ModuleArg (struct)                 └── Extra map[string]any
├── Name string
├── Required bool                  DiffResult (struct)
├── Default any                    ├── Before string
├── Type string                    └── After string
├── Choices []string
└── Description string
```

---

## 4. 模块注册机制

### 4.1 注册表设计

模块系统使用**全局注册表**模式：所有模块在程序启动时注册到一个全局映射中，执行时
通过名字查找。

```go
// Registry 模块注册表，线程安全。
// 模块在 init() 中注册，执行时按名查找。
type Registry struct {
    modules map[string]Module
}

// 全局注册表实例
var globalRegistry = &Registry{
    modules: make(map[string]Module),
}

// Register 注册模块到全局注册表。
// 如果模块名为 nil 或已存在同名模块，panic。
func Register(m Module)

// GetModule 按名字查找模块。
// 返回 (Module, true) 如果存在，(nil, false) 如果不存在。
func GetModule(name string) (Module, bool)

// ListModules 返回所有已注册模块的名字列表（排序后）。
func ListModules() []string
```

### 4.2 Go init() 注册模式

Go 语言的 `init()` 函数在包被导入时自动执行，非常适合做注册：

```go
// 文件：modules/ping/ping.go

package ping

import "github.com/project/ansible-go/pkg/module"

// PingModule 实现 ping 模块，用于测试连接。
type PingModule struct{}

func (p *PingModule) Name() string { return "ping" }

func (p *PingModule) SupportsCheckMode() bool { return true }

func (p *PingModule) Run(ctx module.ExecContext) (module.Result, error) {
    return module.Result{
        Changed: false,
        Msg:     "pong",
    }, nil
}

func init() {
    module.Register(&PingModule{})
}
```

### 4.3 驱动注册模式

主程序通过**空白导入**（blank import）驱动所有模块注册：

```go
// 文件：cmd/ansible-go/main.go

package main

import (
    // 空白导入驱动模块注册
    _ "github.com/project/ansible-go/modules/ping"
    _ "github.com/project/ansible-go/modules/shell"
    _ "github.com/project/ansible-go/modules/command"
    _ "github.com/project/ansible-go/modules/copy"
    _ "github.com/project/ansible-go/modules/file"
    _ "github.com/project/ansible-go/modules/yum"
    _ "github.com/project/ansible-go/modules/apt"
    _ "github.com/project/ansible-go/modules/service"
    // ... 更多模块
)
```

### 4.4 模块目录结构

```
modules/
├── ping/
│   └── ping.go
├── shell/
│   └── shell.go
├── command/
│   └── command.go
├── copy/
│   ├── copy.go
│   └── copy_test.go
├── file/
│   ├── file.go
│   └── file_test.go
├── yum/
│   ├── yum.go
│   └── yum_test.go
├── apt/
│   ├── apt.go
│   └── apt_test.go
├── service/
│   ├── service.go
│   └── service_test.go
└── setup/
    ├── setup.go
    ├── facts_linux.go
    └── setup_test.go
```

### 4.5 模块查找流程

```
Playbook 解析 Task
    ↓
提取模块名（如 "yum"）
    ↓
调用 GetModule("yum")
    ↓
在 globalRegistry.modules 中查找
    ├── 找到 → 返回 Module 实例
    └── 未找到 → 报错 "module 'xxx' not found"
```

---

## 5. 幂等性设计原则

### 5.1 什么是幂等性

幂等性（Idempotency）是 Ansible 最核心的设计原则：**同一个操作执行一次和执行
多次的效果完全相同**。

这意味着：
- 第一次运行：安装 nginx → `changed: true`
- 第二次运行：nginx 已存在 → `changed: false`（不做任何事）
- 第 N 次运行：结果与第二次完全相同

幂等性保证了 Playbook 可以安全地重复执行，不会因为重复运行而产生副作用。

### 5.2 Changed/OK/Failed 三态

每个模块执行后，结果处于以下三种状态之一：

```
┌─────────────────────────────────────────────────────────────┐
│                    模块执行结果                               │
├──────────┬──────────────────┬────────────────────────────────┤
│  OK      │  Changed         │  Failed                        │
│  (绿色)  │  (黄色)          │  (红色)                        │
├──────────┼──────────────────┼────────────────────────────────┤
│ 已是期望 │ 做了变更，达到    │ 执行遇到错误，                  │
│ 状态，不 │ 期望状态          │ 未达到期望状态                  │
│ 做任何事 │                  │                                │
├──────────┼──────────────────┼────────────────────────────────┤
│ Changed: │ Changed: true    │ Failed: true                   │
│ false    │ Failed: false    │ Msg: "error description"       │
│ Failed:  │                  │                                │
│ false    │                  │                                │
└──────────┴──────────────────┴────────────────────────────────┘
```

### 5.3 模块如何判断当前状态

每个模块实现幂等性的方式不同，但基本模式一致：

**模式一：检查文件/资源是否存在**
```yaml
# file 模块 state=directory
1. SSH 执行: stat /path/to/dir
2. 不存在 → mkdir -p /path/to/dir → Changed: true
3. 已存在 → 检查权限是否匹配 → 不匹配则修改 → Changed: true/false
```

**模式二：检查包是否已安装**
```yaml
# yum 模块 state=present
1. SSH 执行: rpm -q nginx
2. 未安装 → yum install -y nginx → Changed: true
3. 已安装 → Changed: false
```

**模式三：检查服务状态**
```yaml
# service 模块 state=started
1. SSH 执行: systemctl is-active nginx
2. 未运行 → systemctl start nginx → Changed: true
3. 已运行 → Changed: false
```

**模式四：命令类模块无法保证幂等**
```yaml
# shell 模块
# shell 模块本身不保证幂等，依赖用户通过条件控制
- name: Run only once
  shell: echo "hello"
  creates: /tmp/hello_done  # 如果文件存在则跳过
```

### 5.4 幂等性检查清单

实现一个模块时，遵循以下检查清单：

1. **执行前检查**：先查询当前状态，再决定是否执行变更
2. **精确比较**：比较期望值与当前值，只在不同时才变更
3. **报告准确**：`Changed` 字段如实反映是否做了变更
4. **错误处理**：执行失败时返回 `Failed: true` 和清晰的错误消息
5. **原子性**：尽量让变更操作原子化，避免部分完成

---

## 6. Check Mode（干跑模式）

### 6.1 什么是 Check Mode

Check Mode（也叫 Dry Run 或干跑模式）允许用户预览 Playbook 将会做什么变更，而
**不实际执行**这些变更。

```bash
# 运行 check mode
ansible-go playbook site.yml --check --diff
```

输出示例：

```
PLAY [Configure webservers] ******************************

TASK [Install nginx] *************************************
changed: [web1]   # 会安装 nginx，但实际未安装
ok: [web2]        # nginx 已安装，无需变更

TASK [Start nginx] ***************************************
ok: [web1]        # nginx 已运行
ok: [web2]        # nginx 已运行
```

### 6.2 模块如何支持 Check Mode

模块通过以下方式支持 Check Mode：

```go
func (m *YumModule) Run(ctx module.ExecContext) (module.Result, error) {
    name := ctx.Args["name"].(string)

    // 先检查包是否已安装
    installed, err := m.isInstalled(ctx.Connection, name)
    if err != nil {
        return module.Result{Failed: true, Msg: err.Error()}, nil
    }

    if installed {
        // 已安装，不需要变更
        return module.Result{Changed: false}, nil
    }

    // 未安装，需要变更
    if ctx.CheckMode {
        // Check Mode：报告会变更，但不实际执行
        return module.Result{
            Changed: true,
            Msg:     fmt.Sprintf("Would install %s", name),
        }, nil
    }

    // 正常模式：实际安装
    err = m.doInstall(ctx.Connection, name)
    if err != nil {
        return module.Result{Failed: true, Msg: err.Error()}, nil
    }

    return module.Result{Changed: true}, nil
}
```

### 6.3 Check Mode 的局限

某些模块在 Check Mode 下的行为受限：

| 模块 | Check Mode 行为 |
|------|-----------------|
| `shell` | 默认跳过（除非 `creates`/`removes` 可判断） |
| `command` | 同 shell |
| `copy` | 报告会复制，但不实际复制 |
| `yum` | 报告会安装/卸载，但不实际执行 |
| `service` | 报告会启停，但不实际操作 |
| `debug` | 正常输出（不受 check mode 影响） |

### 6.4 SupportsCheckMode() 的含义

```go
// SupportsCheckMode 返回 true 表示模块能正确处理 check mode。
// 返回 false 表示模块在 check mode 下会被跳过。
func (m *SomeModule) SupportsCheckMode() bool {
    return true  // 或 false
}
```

当 `SupportsCheckMode()` 返回 `false` 且处于 Check Mode 时，执行引擎会跳过该模块
并输出提示：

```
skipping: [web1]  Module 'xxx' does not support check mode
```

---

## 7. 模块参数处理

### 7.1 参数来源

模块参数有三种来源格式：

**格式一：key=value 字符串（ad-hoc 模式）**
```bash
ansible-go all -m copy -a "src=/local/file dest=/remote/file mode=0644"
```

**格式二：YAML dict（Playbook 模式）**
```yaml
- name: Copy file
  copy:
    src: /local/file
    dest: /remote/file
    mode: "0644"
```

**格式三：_raw_params（命令类模块）**
```yaml
- name: Run command
  command: ls -la /tmp
  # 等价于
- name: Run command
  command:
    _raw_params: ls -la /tmp
```

### 7.2 key=value 解析

ad-hoc 模式下的参数字符串需要解析为 `map[string]any`：

```
输入: "src=/local/file dest=/remote/file mode=0644"

解析规则:
1. 按空格分割（但要处理引号内的空格）
2. 每个 token 按第一个 = 分割为 key/value
3. 处理引号: key="value with spaces"
4. 处理特殊值: true/false/yes/no → bool, 纯数字 → int

输出: map[string]any{
    "src":  "/local/file",
    "dest": "/remote/file",
    "mode": "0644",
}
```

### 7.3 _raw_params 处理

对于 `shell`、`command` 等命令类模块，整个参数字符串就是命令本身：

```yaml
- name: Run complex command
  shell: echo "hello world" | grep hello > /tmp/out.txt
```

此时参数解析为：
```go
map[string]any{
    "_raw_params": `echo "hello world" | grep hello > /tmp/out.txt`,
}
```

### 7.4 类型校验

参数在传入模块前应进行类型校验：

```go
// 参数校验函数
func ValidateArgs(module Module, args map[string]any) error

// 校验规则:
// 1. 必填参数检查 — Required=true 的参数必须存在
// 2. 类型检查 — 值的类型必须匹配 ModuleArg.Type
// 3. 选项检查 — 有 Choices 的参数值必须在 Choices 中
// 4. 默认值填充 — 缺失的可选参数用 Default 填充
```

### 7.5 参数类型转换

从 YAML 解析出来的类型可能与模块期望的不一致，需要自动转换：

```
YAML 解析        →    模块期望类型     →    转换规则
─────────────────────────────────────────────────────
string "80"      →    int             →    strconv.Atoi
string "true"    →    bool            →    strconv.ParseBool
string "yes"     →    bool            →    true
string "no"      →    bool            →    false
int 80           →    string          →    strconv.Itoa
```

---

## 8. Go 实现要点

### 8.1 接口定义

模块系统的核心接口和类型定义集中在 `pkg/module/` 包中：

```go
// pkg/module/module.go

package module

// Module 所有模块必须实现的接口。
type Module interface {
    Name() string
    Run(ctx ExecContext) (Result, error)
    SupportsCheckMode() bool
}

// ExecContext 模块执行上下文。
type ExecContext struct {
    Host       *Host
    Args       map[string]any
    Connection Connection
    CheckMode  bool
    Diff       bool
    Variables  map[string]any
}

// Result 模块执行结果。
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

// DiffResult 变更详情。
type DiffResult struct {
    Before string
    After  string
}
```

### 8.2 Registry 实现

```go
// pkg/module/registry.go

package module

import (
    "fmt"
    "sort"
    "sync"
)

// registry 全局模块注册表。
type registry struct {
    mu      sync.RWMutex
    modules map[string]Module
}

var globalRegistry = &registry{
    modules: make(map[string]Module),
}

// Register 注册模块。
func Register(m Module) {
    globalRegistry.mu.Lock()
    defer globalRegistry.mu.Unlock()

    name := m.Name()
    if name == "" {
        panic("module: empty module name")
    }
    if _, exists := globalRegistry.modules[name]; exists {
        panic(fmt.Sprintf("module: duplicate registration: %s", name))
    }
    globalRegistry.modules[name] = m
}

// GetModule 按名字查找模块。
func GetModule(name string) (Module, bool) {
    globalRegistry.mu.RLock()
    defer globalRegistry.mu.RUnlock()

    m, ok := globalRegistry.modules[name]
    return m, ok
}

// ListModules 返回所有已注册模块名（排序后）。
func ListModules() []string {
    globalRegistry.mu.RLock()
    defer globalRegistry.mu.RUnlock()

    names := make([]string, 0, len(globalRegistry.modules))
    for name := range globalRegistry.modules {
        names = append(names, name)
    }
    sort.Strings(names)
    return names
}
```

### 8.3 参数解析器

```go
// pkg/module/args.go

package module

// ParseArgs 解析 key=value 格式的参数字符串。
// 支持引号包裹的值、转义字符。
func ParseArgs(raw string) (map[string]any, error)

// ParseYAMLArgs 将 YAML dict 转换为模块参数。
// 处理 _raw_params 特殊字段。
func ParseYAMLArgs(yamlArgs map[string]any) map[string]any

// ValidateArgs 校验参数是否符合模块定义。
func ValidateArgs(m Module, args map[string]any) error

// SetDefaults 填充默认值。
func SetDefaults(argDefs []ModuleArg, args map[string]any) map[string]any
```

### 8.4 辅助函数

模块实现中常用的辅助函数：

```go
// pkg/module/util.go

package module

// GetStringArg 从 args 中获取字符串参数。
func GetStringArg(args map[string]any, key string) (string, error)

// GetStringArgWithDefault 获取字符串参数，支持默认值。
func GetStringArgWithDefault(args map[string]any, key, defaultVal string) string

// GetIntArg 从 args 中获取整数参数。
func GetIntArg(args map[string]any, key string) (int, error)

// GetBoolArg 从 args 中获取布尔参数。
func GetBoolArg(args map[string]any, key string) (bool, error)

// GetListArg 从 args 中获取列表参数。
func GetListArg(args map[string]any, key string) ([]any, error)
```

---

## 9. 任务拆解

### T4.1 模块接口与注册

**目标：** 建立模块系统的基础设施。

**交付物：**
- `pkg/module/module.go` — Module 接口定义
- `pkg/module/context.go` — ExecContext 结构体
- `pkg/module/result.go` — Result / DiffResult 结构体
- `pkg/module/registry.go` — 全局注册表（Register / GetModule / ListModules）
- `pkg/module/args.go` — 参数解析（ParseArgs / ParseYAMLArgs / ValidateArgs）
- `pkg/module/util.go` — 辅助函数（GetStringArg / GetIntArg / GetBoolArg 等）

**验收标准：**
- [ ] Module 接口定义清晰，包含 Name / Run / SupportsCheckMode
- [ ] ExecContext 包含 Host / Args / Connection / CheckMode / Diff / Variables
- [ ] Result 包含 Changed / Failed / Msg / Stdout / Stderr / Rc / Diff / Extra
- [ ] Registry 线程安全，支持 Register / GetModule / ListModules
- [ ] 参数解析支持 key=value 格式和 YAML dict 格式
- [ ] 单元测试覆盖注册、查找、参数解析

### T4.2 ping 模块

**目标：** 实现第一个模块，验证模块系统工作正常。

**交付物：**
- `modules/ping/ping.go` — ping 模块实现

**模块行为：**
- 返回 `Changed: false, Msg: "pong"`
- 支持 Check Mode

**验收标准：**
- [ ] ping 模块正确注册到全局注册表
- [ ] 通过 ad-hoc 命令可执行：`ansible-go all -m ping`
- [ ] Check Mode 下正常工作
- [ ] 单元测试通过

### T4.3 shell / command 模块

**目标：** 实现命令执行模块，这是最常用的模块之一。

**交付物：**
- `modules/shell/shell.go` — shell 模块（通过 shell 执行命令）
- `modules/command/command.go` — command 模块（直接执行，不经 shell）

**模块行为：**

| 参数 | 必填 | 说明 |
|------|------|------|
| `_raw_params` | 是 | 要执行的命令 |
| `chdir` | 否 | 执行前先 cd 到此目录 |
| `creates` | 否 | 如果此文件存在则跳过 |
| `removes` | 否 | 如果此文件不存在则跳过 |
| `stdin` | 否 | 标准输入内容 |

**关键差异：**

```
shell:     执行时使用 /bin/sh -c "command"
command:   直接执行，不经过 shell 解释
```

**验收标准：**
- [ ] shell 模块通过 SSH 执行远程命令
- [ ] command 模块直接执行，不解释 shell 特殊字符
- [ ] `creates` / `removes` 条件正确实现幂等
- [ ] `chdir` 参数正确切换工作目录
- [ ] exit code 正确映射到 Result.Rc
- [ ] 非零 exit code 设置 Failed: true
- [ ] Check Mode 下跳过执行，报告 Would Execute
- [ ] 单元测试覆盖各种场景

---

## 附录：模块开发速查表

### 新模块文件模板

```go
package mymodule

import "github.com/project/ansible-go/pkg/module"

type MyModule struct{}

func (m *MyModule) Name() string { return "mymodule" }

func (m *MyModule) SupportsCheckMode() bool { return true }

func (m *MyModule) Run(ctx module.ExecContext) (module.Result, error) {
    // 1. 解析参数
    // 2. 检查当前状态
    // 3. 如果 CheckMode，返回 WouldChange
    // 4. 执行变更
    // 5. 返回结果
    return module.Result{}, nil
}

func init() {
    module.Register(&MyModule{})
}
```

### 测试模板

```go
package mymodule

import (
    "testing"
    "github.com/project/ansible-go/pkg/module"
)

func TestMyModule_Run(t *testing.T) {
    m := &MyModule{}

    // 测试名称
    if m.Name() != "mymodule" {
        t.Errorf("expected name 'mymodule', got '%s'", m.Name())
    }

    // 测试 Check Mode 支持
    if !m.SupportsCheckMode() {
        t.Error("expected SupportsCheckMode to be true")
    }

    // 测试正常执行
    ctx := module.ExecContext{
        Args: map[string]any{
            "key": "value",
        },
        // Mock Connection 等
    }

    result, err := m.Run(ctx)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result.Failed {
        t.Errorf("unexpected failure: %s", result.Msg)
    }
}
```
