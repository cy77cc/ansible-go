# 最小可运行路径

> 本文档定义了 go-ansible 的最小可运行目标（MVP），分析关键路径，并提供分步实现指南。目标是尽快让 go-ansible 能在远程主机上执行有意义的操作。

---

## 一、最小可运行目标

### 1.1 什么是"完成"

go-ansible 的最小可运行目标是：

**能在远程主机上执行 Playbook，安装软件包并启动服务。**

具体来说，以下 Playbook 必须能成功执行：

```yaml
# site.yml
- name: Configure web server
  hosts: webservers
  become: true
  gather_facts: true

  vars:
    http_port: 80

  tasks:
    - name: Install nginx
      yum:
        name: nginx
        state: present

    - name: Copy nginx config
      template:
        src: nginx.conf.j2
        dest: /etc/nginx/nginx.conf

    - name: Start nginx
      service:
        name: nginx
        state: started
        enabled: true

    - name: Verify nginx is running
      uri:
        url: "http://localhost:{{ http_port }}"
        status_code: 200
```

### 1.2 最小功能集

为了执行上述 Playbook，go-ansible 必须具备以下最小功能集：

| 功能 | 必需/可选 | 说明 |
|------|----------|------|
| CLI 参数解析 | 必需 | -i, --become, -u 等基本参数 |
| INI Inventory 解析 | 必需 | 解析主机清单文件 |
| SSH 连接 | 必需 | 建立 SSH 连接、执行命令 |
| 变量系统 | 必需 | 至少支持基本变量合并 |
| 模板引擎 | 必需 | text/template + 变量渲染 |
| Playbook YAML 解析 | 必需 | 解析 Play/Task 结构 |
| yum 模块 | 必需 | 安装/卸载软件包 |
| service 模块 | 必需 | 管理系统服务 |
| template 模块 | 必需 | 模板渲染后拷贝文件 |
| uri 模块 | 可选 | HTTP 请求验证 |
| Facts 收集 | 必需 | 收集系统信息用于条件判断 |
| Handler 机制 | 可选 | 通知/触发机制 |
| Block/Rescue | 可选 | 错误处理 |
| Roles 系统 | 可选 | 角色加载 |
| Vault 加密 | 可选 | 敏感数据加密 |
| 回调插件 | 必需 | 至少有默认输出格式 |

### 1.3 不在 MVP 范围内的功能

以下功能可以后续添加，不阻塞 MVP：

- YAML Inventory 格式（先支持 INI）
- Roles 系统（先支持内联 tasks）
- Vault 加密（先用明文变量）
- Galaxy（先手动管理角色）
- 异步任务（先同步执行）
- 查找插件（先硬编码）
- 过滤器/测试插件（先用 Sprig 内置的）
- 多种输出格式（先用默认格式）

---

## 二、关键路径分析

### 2.1 组件依赖图

```
CLI 解析 ──→ Inventory 加载 ──→ Playbook 解析 ──→ 模板渲染 ──→ 模块执行 ──→ 结果输出
   │              │                  │               │            │           │
   │              │                  │               │            │           │
   ▼              ▼                  ▼               ▼            ▼           ▼
 cobra        INI Parser         YAML Parser     text/template  Module    Callback
              Host Pattern       Task 解析        Sprig         Registry  Plugin
              group_vars         Play 解析        变量前缀处理   SSH Exec
              host_vars          变量合并                                  Connection
```

### 2.2 关键路径上的组件

**关键路径**是从"用户输入命令"到"远程命令执行"的最短路径。以下是关键路径上的组件：

```
1. CLI 解析（P0）
   └── 能解析命令行参数，创建 GlobalOptions

2. Inventory 加载（P1）
   └── 能解析 INI 格式的主机清单

3. SSH 连接（P2）
   └── 能建立 SSH 连接、执行命令、获取结果

4. 变量系统（P3）
   └── 能合并变量、支持基本优先级

5. 模板引擎（P3）
   └── 能渲染 text/template 模板

6. Playbook 解析（P5）
   └── 能解析 YAML 格式的 Playbook

7. 模块执行（P4）
   └── 能查找并执行模块

8. 结果输出（P12）
   └── 能将执行结果打印到终端
```

### 2.3 可以 Stub 的组件

以下组件在 MVP 阶段可以简化或 stub：

| 组件 | 简化方案 | 理由 |
|------|---------|------|
| YAML Inventory | 不支持，只支持 INI | INI 更简单，先实现 |
| Roles 系统 | 不支持 | 先用内联 tasks |
| Vault | 不支持 | 先用明文变量 |
| Galaxy | 不支持 | 先手动管理 |
| 异步任务 | 不支持 | 先同步执行 |
| Handler 机制 | 简化实现 | 只支持 notify 基本功能 |
| Block/Rescue | 不支持 | 先不处理复杂错误 |
| 查找插件 | 不支持 | 先硬编码 |
| 过滤器 | 只用 Sprig 内置 | 足够覆盖 MVP 场景 |
| Free 策略 | 不支持 | 先用 Linear 策略 |
| Serial 控制 | 不支持 | 先全量执行 |
| Tags | 不支持 | 先执行所有任务 |
| --limit | 简化实现 | 只支持组名和主机名 |

### 2.4 风险评估

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| SSH 连接复杂度 | 高 | 先用 x/crypto/ssh 的最简配置 |
| 变量合并逻辑 | 高 | 先实现基本合并，不支持深合并 |
| 模板渲染边界情况 | 中 | 先支持最常用场景 |
| 模块执行结果解析 | 中 | 先用 JSON 输出格式 |
| 并发控制 | 中 | 先用简单的 goroutine + WaitGroup |

---

## 三、第一步：SSH + Ping

### 3.1 目标

```bash
go-ansible all -m ping -i inventory/hosts
```

验证：CLI 解析 → Inventory 加载 → SSH 连接 → 模块注册 → 模块执行 → 结果输出

### 3.2 涉及组件

```
CLI 解析 ──→ Inventory 加载 ──→ 主机匹配 ──→ SSH 连接 ──→ Ping 模块 ──→ 结果输出
   │              │                │            │            │            │
   │              │                │            │            │            │
   ▼              ▼                ▼            ▼            ▼            ▼
 cobra        INI Parser      Host Pattern   x/crypto    Module      Callback
              group_vars                     /ssh        Registry    Plugin
```

### 3.3 实现要点

**CLI 层**

```go
// 需要实现的命令结构
go-ansible <host-pattern> -m <module> [-a <args>] [-i <inventory>]

// cobra 配置
cmd := &cobra.Command{
    Use:   "go-ansible <host-pattern>",
    Args:  cobra.ExactArgs(1),
    RunE:  runAdhoc,
}

cmd.Flags().StringP("module-name", "m", "", "module name (required)")
cmd.Flags().StringP("args", "a", "", "module arguments")
cmd.Flags().StringP("inventory", "i", "/etc/ansible/hosts", "inventory path")
```

**Inventory 加载**

```go
// 最小实现：只支持 INI 格式
type INIParser struct{}

func (p *INIParser) Parse(data []byte) (*Inventory, error) {
    // 1. 逐行扫描
    // 2. 识别 [section] 头
    // 3. 解析 host ansible_host=x 行
    // 4. 解析 [group:vars] 变量
    // 5. 解析 [group:children] 子组
    // 6. 返回 Inventory 结构
}

// 主机模式匹配：先只支持 "all" 和组名
func MatchHosts(pattern string, inv *Inventory) []*Host {
    if pattern == "all" || pattern == "*" {
        return inv.AllHosts()
    }
    if group := inv.GetGroup(pattern); group != nil {
        return group.AllHosts()
    }
    return nil
}
```

**SSH 连接**

```go
// 最小实现：只支持密钥认证
type SSHConnection struct {
    client *ssh.Client
}

func (c *SSHConnection) Connect(host Host) error {
    // 1. 读取 SSH 私钥（默认 ~/.ssh/id_rsa）
    // 2. 创建 ssh.Signer
    // 3. 建立 SSH 连接
    // 4. 保存 client
}

func (c *SSHConnection) Exec(cmd string) (string, string, int, error) {
    // 1. 创建 session
    // 2. 设置 stdout/stderr buffers
    // 3. 执行命令
    // 4. 等待完成
    // 5. 获取退出码
    // 6. 返回 stdout, stderr, rc, err
}
```

**Ping 模块**

```go
func init() {
    Register("ping", &PingModule{})
}

type PingModule struct{}

func (m *PingModule) Name() string          { return "ping" }
func (m *PingModule) SupportsCheckMode() bool { return true }

func (m *PingModule) Run(ctx ExecContext) (Result, error) {
    // Ping 模块不需要执行远程命令
    // 只需要验证 SSH 连接可用
    _, _, _, err := ctx.Connection.Exec("echo pong")
    if err != nil {
        return Result{Failed: true, Msg: "ping failed"}, err
    }
    return Result{Changed: false, Msg: "pong"}, nil
}
```

**结果输出**

```go
// 最简化的回调
type DefaultCallback struct{}

func (c *DefaultCallback) OnTaskOk(result TaskResult) {
    if result.Result.Changed {
        fmt.Printf("changed: [%s]\n", result.Host.Name)
    } else {
        fmt.Printf("ok: [%s]\n", result.Host.Name)
    }
}

func (c *DefaultCallback) OnTaskFailed(result TaskResult, ignored bool) {
    fmt.Printf("failed: [%s] => %s\n", result.Host.Name, result.Result.Msg)
}
```

### 3.4 测试用例

```bash
# 基本测试
go-ansible all -m ping -i testdata/hosts.ini

# 预期输出
web1 | SUCCESS => pong
web2 | SUCCESS => pong

# 错误测试
go-ansible all -m ping -i nonexistent.ini
# 预期：错误信息 "inventory file not found: nonexistent.ini"
```

### 3.5 里程碑检查

- [ ] `go-ansible --help` 显示正确的帮助信息
- [ ] `go-ansible all -m ping -i hosts.ini` 能成功连接并返回 pong
- [ ] 错误处理：无法连接时显示有意义的错误信息
- [ ] 退出码：成功时返回 0，失败时返回非 0

---

## 四、第二步：Ad-hoc 命令

### 4.1 目标

```bash
go-ansible all -m shell -a "uptime" -i inventory/hosts
go-ansible webservers -m command -a "df -h" -i inventory/hosts
```

验证：命令执行、结果解析、错误处理

### 4.2 涉及组件

```
第一步的所有组件
    └── 新增：shell 模块、command 模块
    └── 新增：结果解析（stdout/stderr/rc）
    └── 新增：组名匹配
```

### 4.3 实现要点

**Shell 模块**

```go
func init() {
    Register("shell", &ShellModule{})
}

type ShellModule struct{}

func (m *ShellModule) Name() string          { return "shell" }
func (m *ShellModule) SupportsCheckMode() bool { return false }

func (m *ShellModule) Run(ctx ExecContext) (Result, error) {
    // 1. 获取命令参数
    cmd, ok := ctx.Args["cmd"].(string)
    if !ok {
        return Result{Failed: true, Msg: "cmd parameter is required"}, nil
    }

    // 2. 通过 SSH 执行命令
    stdout, stderr, rc, err := ctx.Connection.Exec(cmd)
    if err != nil {
        return Result{Failed: true, Msg: err.Error(), Stderr: stderr}, nil
    }

    // 3. 根据退出码判断成功/失败
    if rc != 0 {
        return Result{
            Failed: true,
            Msg:    fmt.Sprintf("non-zero return code: %d", rc),
            Stdout: stdout,
            Stderr: stderr,
            Rc:     rc,
        }, nil
    }

    return Result{
        Changed: true,  // shell 命令总是假设 changed
        Msg:     "Command executed successfully",
        Stdout:  stdout,
        Stderr:  stderr,
        Rc:      rc,
    }, nil
}
```

**Command 模块**

Command 模块与 Shell 模块类似，但不通过 shell 执行（不支持管道、重定向等）：

```go
func (m *CommandModule) Run(ctx ExecContext) (Result, error) {
    cmd, _ := ctx.Args["cmd"].(string)

    // command 模块直接执行命令，不经 shell
    // 实际实现中，这通常意味着不使用 "sh -c" 包装
    stdout, stderr, rc, err := ctx.Connection.Exec(cmd)
    // ... 类似 shell 模块的处理
}
```

**结果解析**

```go
// 统一的结果解析
func parseResult(stdout, stderr string, rc int) Result {
    result := Result{
        Stdout: stdout,
        Stderr: stderr,
        Rc:     rc,
    }

    if rc != 0 {
        result.Failed = true
        result.Msg = fmt.Sprintf("Command failed with return code %d", rc)
    } else {
        result.Changed = true
        result.Msg = strings.TrimSpace(stdout)
    }

    return result
}
```

### 4.4 测试用例

```bash
# Shell 命令
go-ansible all -m shell -a "uptime" -i hosts.ini
# 预期输出
web1 | CHANGED | rc=0 >>  10:30:00 up 30 days,  1:23,  1 user,  load average: 0.01, 0.02, 0.00
web2 | CHANGED | rc=0 >>  10:30:00 up 15 days,  5:45,  2 users, load average: 0.05, 0.03, 0.01

# Command 命令
go-ansible webservers -m command -a "df -h" -i hosts.ini
# 预期输出
web1 | CHANGED | rc=0 >> Filesystem      Size  Used Avail Use% Mounted on
web2 | CHANGED | rc=0 >> Filesystem      Size  Used Avail Use% Mounted on

# 命令失败
go-ansible all -m shell -a "ls /nonexistent" -i hosts.ini
# 预期输出
web1 | FAILED | rc=2 >> ls: cannot access '/nonexistent': No such file or directory
```

### 4.5 里程碑检查

- [ ] `go-ansible all -m shell -a "uptime"` 成功执行并显示输出
- [ ] `go-ansible webservers -m shell -a "uptime"` 只在 webservers 组执行
- [ ] 命令失败时显示 stderr 和退出码
- [ ] `--limit` 能限制执行范围

---

## 五、第三步：Playbook 最小集

### 5.1 目标

```bash
go-ansible playbook site.yml -i inventory/hosts
```

验证：YAML 解析、Play 遍历、Task 顺序执行

### 5.2 最小 Playbook

```yaml
# site.yml
- name: Simple test
  hosts: all
  tasks:
    - name: Show hostname
      shell: hostname

    - name: Show uptime
      shell: uptime
```

### 5.3 涉及组件

```
第一步 + 第二步的所有组件
    └── 新增：Playbook YAML 解析
    └── 新增：Play 遍历
    └── 新增：Task 顺序执行
```

### 5.4 实现要点

**Playbook YAML 解析**

```go
// Play 数据结构
type Play struct {
    Name       string         `yaml:"name"`
    Hosts      string         `yaml:"hosts"`
    Become     bool           `yaml:"become"`
    GatherFacts bool          `yaml:"gather_facts"`
    Vars       map[string]any `yaml:"vars"`
    Tasks      []Task         `yaml:"tasks"`
}

// Task 数据结构
type Task struct {
    Name   string         `yaml:"name"`
    Module string         // 从 YAML 中动态解析
    Args   map[string]any // 模块参数
    When   string         `yaml:"when"`
    Loop   any            `yaml:"loop"`
    Tags   []string       `yaml:"tags"`
}

// Playbook 解析
func ParsePlaybook(data []byte) ([]Play, error) {
    var plays []Play
    err := yaml.Unmarshal(data, &plays)
    if err != nil {
        return nil, err
    }

    // 后处理：从 YAML 中提取模块名和参数
    for i := range plays {
        for j := range plays[i].Tasks {
            // Task 的模块名和参数需要特殊解析
            // 因为 YAML 结构是：
            // - name: xxx
            //   yum:           <-- 这是模块名
            //     name: nginx  <-- 这是模块参数
            //     state: present
            plays[i].Tasks[j] = parseTaskModule(plays[i].Tasks[j])
        }
    }

    return plays, nil
}
```

**Task 模块名解析**

这是 Playbook 解析中最复杂的部分。YAML 中 Task 的模块名和参数是动态的：

```yaml
# 这个 Task 的模块名是 "yum"，参数是 {name: nginx, state: present}
- name: Install nginx
  yum:
    name: nginx
    state: present

# 这个 Task 的模块名是 "shell"，参数是 {cmd: uptime}
- name: Show uptime
  shell: uptime
```

解析策略：

```go
func parseTaskModule(raw map[string]any) Task {
    task := Task{}

    // 提取已知字段
    if name, ok := raw["name"].(string); ok {
        task.Name = name
        delete(raw, "name")
    }
    if when, ok := raw["when"].(string); ok {
        task.When = when
        delete(raw, "when")
    }
    // ... 其他已知字段

    // 剩余的键中，第一个非控制字段就是模块名
    for key, value := range raw {
        if isControlField(key) {
            continue
        }
        task.Module = key
        if args, ok := value.(map[string]any); ok {
            task.Args = args
        } else {
            // 简单值，如 shell: uptime
            task.Args = map[string]any{"cmd": value}
        }
        break
    }

    return task
}
```

**Play 遍历和 Task 执行**

```go
func (e *PlaybookEngine) Execute(playbook []Play, inventory *Inventory) error {
    for _, play := range playbook {
        // 1. 匹配主机
        hosts := MatchHosts(play.Hosts, inventory)
        if len(hosts) == 0 {
            return fmt.Errorf("no hosts matched: %s", play.Hosts)
        }

        // 2. 打印 Play 标题
        e.callback.OnPlayStart(play, hostNames(hosts))

        // 3. 顺序执行 Tasks
        for _, task := range play.Tasks {
            e.callback.OnTaskStart(task, false)

            // 4. 对每台主机执行 Task
            for _, host := range hosts {
                result := e.executeTask(host, task, play)
                if result.Failed {
                    e.callback.OnTaskFailed(TaskResult{Host: host, Task: task, Result: result}, false)
                } else {
                    e.callback.OnTaskOk(TaskResult{Host: host, Task: task, Result: result})
                }
            }
        }
    }

    return nil
}
```

### 5.5 测试用例

```bash
# 简单 Playbook
go-ansible playbook site.yml -i hosts.ini

# 预期输出
PLAY [Simple test] *************************************************************

TASK [Gathering Facts] *********************************************************
ok: [web1]
ok: [web2]

TASK [Show hostname] ***********************************************************
changed: [web1]
changed: [web2]

TASK [Show uptime] *************************************************************
changed: [web1]
changed: [web2]

PLAY RECAP *********************************************************************
web1     : ok=3  changed=2  unreachable=0  failed=0
web2     : ok=3  changed=2  unreachable=0  failed=0
```

### 5.6 里程碑检查

- [ ] Playbook YAML 解析正确
- [ ] 多个 Play 依次执行
- [ ] 每个 Play 内的 Task 顺序执行
- [ ] PLAY RECAP 统计信息正确
- [ ] 错误处理：Task 失败后继续执行下一个 Task（可配置）

---

## 六、第四步：添加条件和循环

### 6.1 目标

支持 `when` 条件和 `loop` 循环：

```yaml
- name: Install packages
  yum:
    name: "{{ item }}"
    state: present
  loop:
    - nginx
    - vim
    - curl

- name: Only on RedHat
  yum:
    name: httpd
    state: present
  when: ansible_os_family == "RedHat"
```

### 6.2 涉及组件

```
前三步的所有组件
    └── 新增：Facts 收集（setup 模块）
    └── 新增：变量系统（基本合并）
    └── 新增：模板引擎（text/template）
    └── 新增：when 条件评估
    └── 新增：loop 循环展开
```

### 6.3 实现要点

**Facts 收集**

```go
// setup 模块：通过 SSH 收集系统信息
func (m *SetupModule) Run(ctx ExecContext) (Result, error) {
    facts := map[string]any{}

    // 收集 OS 信息
    stdout, _, _, _ := ctx.Connection.Exec("cat /etc/os-release")
    facts["ansible_distribution"] = parseOSRelease(stdout)

    // 收集主机名
    stdout, _, _, _ = ctx.Connection.Exec("hostname")
    facts["ansible_hostname"] = strings.TrimSpace(stdout)

    // 收集 IP 地址
    stdout, _, _, _ = ctx.Connection.Exec("hostname -I")
    facts["ansible_all_ipv4_addresses"] = strings.Fields(stdout)

    // ... 更多 facts

    return Result{
        Changed: false,
        Extra:   map[string]any{"ansible_facts": facts},
    }, nil
}
```

**变量系统**

```go
// 变量合并：基本的 map 合并
func MergeVariables(base map[string]any, overlay map[string]any) map[string]any {
    result := make(map[string]any)

    // 复制 base
    for k, v := range base {
        result[k] = v
    }

    // overlay 覆盖 base
    for k, v := range overlay {
        if baseMap, ok := result[k].(map[string]any); ok {
            if overlayMap, ok := v.(map[string]any); ok {
                // dict 递归合并
                result[k] = MergeVariables(baseMap, overlayMap)
                continue
            }
        }
        result[k] = v
    }

    return result
}
```

**模板引擎**

```go
// Go text/template + Sprig
import (
    "text/template"
    "github.com/Masterminds/sprig/v3"
)

type TemplateEngine struct {
    funcs template.FuncMap
}

func NewTemplateEngine() *TemplateEngine {
    // 使用 Sprig 函数 + 自定义函数
    funcs := sprig.TxtFuncMap()
    // 添加自定义函数...

    return &TemplateEngine{funcs: funcs}
}

func (e *TemplateEngine) Render(templateStr string, vars map[string]any) (string, error) {
    // 1. 预处理：将 {{ foo }} 转换为 {{ .foo }}
    processed := preprocess(templateStr)

    // 2. 解析模板
    tmpl, err := template.New("").Funcs(e.funcs).Parse(processed)
    if err != nil {
        return "", fmt.Errorf("template parse error: %w", err)
    }

    // 3. 渲染
    var buf strings.Builder
    err = tmpl.Execute(&buf, vars)
    if err != nil {
        return "", fmt.Errorf("template render error: %w", err)
    }

    return buf.String(), nil
}

// 变量前缀预处理
func preprocess(s string) string {
    // 将 {{ foo }} 转换为 {{ .foo }}
    // 将 {{ foo.bar }} 转换为 {{ .foo.bar }}
    // 保留已经是 {{ .foo }} 形式的
    // 处理 {{ func(...) }} 形式（不加前缀）
    // ...
}
```

**when 条件评估**

```go
// when 条件评估
func evaluateWhen(when string, vars map[string]any) (bool, error) {
    if when == "" {
        return true, nil
    }

    // 渲染 when 表达式
    result, err := templateEngine.Render("{{ " + when + " }}", vars)
    if err != nil {
        return false, fmt.Errorf("when evaluation error: %w", err)
    }

    // 解析结果
    result = strings.TrimSpace(result)
    switch result {
    case "true", "1", "yes":
        return true, nil
    case "false", "0", "no", "":
        return false, nil
    default:
        return false, fmt.Errorf("unexpected when result: %s", result)
    }
}
```

**loop 循环展开**

```go
// loop 循环展开
func expandLoop(task Task, vars map[string]any) ([]Task, error) {
    if task.Loop == nil {
        return []Task{task}, nil
    }

    // 将 loop 转换为列表
    var items []any
    switch v := task.Loop.(type) {
    case []any:
        items = v
    case []string:
        for _, s := range v {
            items = append(items, s)
        }
    default:
        return nil, fmt.Errorf("unsupported loop type: %T", task.Loop)
    }

    // 为每个 item 创建一个子任务
    var tasks []Task
    for _, item := range items {
        subTask := task // 复制 task

        // 设置 item 变量
        subTask.Vars = make(map[string]any)
        for k, v := range vars {
            subTask.Vars[k] = v
        }
        subTask.Vars["item"] = item

        tasks = append(tasks, subTask)
    }

    return tasks, nil
}
```

### 6.4 测试用例

```bash
# when 条件
go-ansible playbook conditional.yml -i hosts.ini

# conditional.yml
- name: Conditional test
  hosts: all
  gather_facts: true
  tasks:
    - name: Only on RedHat
      shell: echo "RedHat family"
      when: ansible_os_family == "RedHat"

    - name: Only on Debian
      shell: echo "Debian family"
      when: ansible_os_family == "Debian"

# loop 循环
go-ansible playbook loop.yml -i hosts.ini

# loop.yml
- name: Loop test
  hosts: all
  tasks:
    - name: Create users
      shell: "useradd {{ item }}"
      loop:
        - alice
        - bob
        - charlie
```

### 6.5 里程碑检查

- [ ] `gather_facts: true` 能收集系统信息
- [ ] `when` 条件正确评估
- [ ] `loop` 循环正确展开
- [ ] 模板渲染支持 `{{ }}` 语法
- [ ] 变量合并基本正确

---

## 七、第五步：添加更多模块

### 7.1 目标

实现核心模块，让 go-ansible 能执行实际的服务器配置任务：

```bash
go-ansible playbook full.yml -i inventory/hosts
```

### 7.2 核心模块清单

**文件管理类**

```go
// copy 模块：拷贝文件到远程
type CopyModule struct{}

func (m *CopyModule) Run(ctx ExecContext) (Result, error) {
    // 1. 获取参数：src, dest, owner, group, mode
    // 2. 检查远程文件是否存在，比较内容
    // 3. 如果不同，通过 SFTP 传输文件
    // 4. 设置权限
    // 5. 返回 Changed/Ok
}

// template 模块：模板渲染后拷贝
type TemplateModule struct{}

func (m *TemplateModule) Run(ctx ExecContext) (Result, error) {
    // 1. 获取参数：src, dest, owner, group, mode
    // 2. 读取本地模板文件
    // 3. 渲染模板（使用变量）
    // 4. 检查远程文件是否与渲染结果相同
    // 5. 如果不同，通过 SFTP 传输
    // 6. 设置权限
    // 7. 返回 Changed/Ok
}

// file 模块：文件/目录操作
type FileModule struct{}

func (m *FileModule) Run(ctx ExecContext) (Result, error) {
    // 支持 state: absent, directory, file, touch, link
    // 1. 获取参数：path, state, mode, owner, group
    // 2. 根据 state 执行操作
    // 3. 返回 Changed/Ok
}
```

**包管理类**

```go
// yum 模块：RPM 包管理
type YumModule struct{}

func (m *YumModule) Run(ctx ExecContext) (Result, error) {
    // 1. 获取参数：name, state (present, absent, latest)
    // 2. 检查包是否已安装：rpm -q <name>
    // 3. 如果 state=present 且未安装：yum install -y <name>
    // 4. 如果 state=absent 且已安装：yum remove -y <name>
    // 5. 返回 Changed/Ok
}

// apt 模块：Debian 包管理
type AptModule struct{}

func (m *AptModule) Run(ctx ExecContext) (Result, error) {
    // 类似 yum，但使用 apt-get
    // 1. 检查：dpkg -l <name>
    // 2. 安装：apt-get install -y <name>
    // 3. 卸载：apt-get remove -y <name>
}
```

**服务管理类**

```go
// service 模块：系统服务管理
type ServiceModule struct{}

func (m *ServiceModule) Run(ctx ExecContext) (Result, error) {
    // 1. 获取参数：name, state (started, stopped, restarted), enabled
    // 2. 检查服务状态：systemctl is-active <name>
    // 3. 如果 state=started 且未运行：systemctl start <name>
    // 4. 如果 state=stopped 且在运行：systemctl stop <name>
    // 5. 如果 enabled=true：systemctl enable <name>
    // 6. 返回 Changed/Ok
}
```

**系统类**

```go
// debug 模块：调试输出
type DebugModule struct{}

func (m *DebugModule) Run(ctx ExecContext) (Result, error) {
    // 支持 msg 和 var 参数
    if msg, ok := ctx.Args["msg"].(string); ok {
        return Result{Msg: msg}, nil
    }
    if varName, ok := ctx.Args["var"].(string); ok {
        value := ctx.Variables[varName]
        return Result{Msg: fmt.Sprintf("%s: %v", varName, value)}, nil
    }
    return Result{Msg: "Hello world!"}, nil
}

// assert 模块：断言检查
type AssertModule struct{}

func (m *AssertModule) Run(ctx ExecContext) (Result, error) {
    // 1. 获取 that 参数（条件列表）
    // 2. 评估每个条件
    // 3. 如果任何条件为 false，返回 Failed
    // 4. 否则返回 Ok
}
```

### 7.3 模块执行流程（以 yum 为例）

```
1. 获取参数
   ctx.Args = {"name": "nginx", "state": "present"}

2. 检查当前状态
   ctx.Connection.Exec("rpm -q nginx")
   ├── 返回 "nginx is not installed" + rc=1 → 需要安装
   └── 返回 "nginx-1.24.0-1.el9.x86_64" + rc=0 → 已安装

3. 决定操作
   state=present + 未安装 → 执行安装
   state=present + 已安装 → 跳过
   state=absent + 已安装 → 执行卸载
   state=absent + 未安装 → 跳过

4. 执行操作（如果需要）
   ctx.Connection.Exec("yum install -y nginx")
   ├── rc=0 → 安装成功
   └── rc!=0 → 安装失败

5. 返回结果
   安装成功 → Result{Changed: true, Msg: "nginx installed"}
   已存在 → Result{Changed: false, Msg: "nginx already installed"}
   安装失败 → Result{Failed: true, Msg: "yum install failed", Stderr: stderr}
```

### 7.4 完整 Playbook 示例

```yaml
# full.yml - 完整的 Web 服务器配置
- name: Configure web servers
  hosts: webservers
  become: true
  gather_facts: true

  vars:
    http_port: 80
    server_name: example.com

  tasks:
    - name: Install nginx
      yum:
        name: nginx
        state: present

    - name: Create web root
      file:
        path: /var/www/html
        state: directory
        owner: nginx
        group: nginx
        mode: "0755"

    - name: Copy index page
      copy:
        src: files/index.html
        dest: /var/www/html/index.html
        owner: nginx
        group: nginx
        mode: "0644"

    - name: Configure nginx
      template:
        src: templates/nginx.conf.j2
        dest: /etc/nginx/nginx.conf
        validate: "nginx -t -c %s"
      notify: restart nginx

    - name: Start nginx
      service:
        name: nginx
        state: started
        enabled: true

    - name: Verify nginx is running
      shell: "curl -s -o /dev/null -w '%{http_code}' http://localhost:{{ http_port }}"
      register: health_check

    - name: Check health
      assert:
        that:
          - health_check.stdout == "200"
        fail_msg: "Nginx health check failed"

  handlers:
    - name: restart nginx
      service:
        name: nginx
        state: restarted
```

### 7.5 里程碑检查

- [ ] `copy` 模块能传输文件到远程
- [ ] `template` 模块能渲染模板并传输
- [ ] `file` 模块能创建目录、设置权限
- [ ] `yum` 模块能安装/卸载软件包
- [ ] `apt` 模块能安装/卸载软件包（Debian 系）
- [ ] `service` 模块能启动/停止/重启服务
- [ ] `debug` 模块能输出变量值
- [ ] `assert` 模块能进行断言检查
- [ ] 完整 Playbook 能成功执行

---

## 八、里程碑检查清单

### 8.1 总体里程碑

| 里程碑 | 验收标准 | 依赖 |
|--------|---------|------|
| M1: CLI 可用 | `go-ansible --help` 和 `--version` 正常工作 | P0 |
| M2: Ping 可用 | `go-ansible all -m ping` 成功执行 | P0+P1+P2+P4 |
| M3: Ad-hoc 可用 | `go-ansible all -m shell -a "uptime"` 成功执行 | M2 |
| M4: Playbook 最小集 | 简单 Playbook 能顺序执行 | M3+P5 |
| M5: 条件和循环 | `when` 和 `loop` 功能可用 | M4+P3 |
| M6: 核心模块 | copy/template/yum/service 模块可用 | M4+P6 |
| M7: MVP 完成 | 完整 Playbook 能成功执行 | M5+M6 |

### 8.2 每个里程碑的详细检查

**M1: CLI 可用**

```bash
# 检查点
go-ansible --help          # 显示帮助信息
go-ansible --version       # 显示版本信息
go-ansible inventory --help  # 显示 inventory 子命令帮助
go-ansible playbook --help   # 显示 playbook 子命令帮助
```

- [ ] 帮助信息格式正确
- [ ] 版本信息包含 build time 和 commit
- [ ] 所有全局标志都有描述
- [ ] 所有子命令都能显示帮助

**M2: Ping 可用**

```bash
# 检查点
go-ansible all -m ping -i testdata/hosts.ini
go-ansible webservers -m ping -i testdata/hosts.ini
go-ansible all -m ping -i nonexistent.ini  # 应该报错
```

- [ ] 能连接到所有主机并返回 pong
- [ ] 能按组过滤主机
- [ ] Inventory 文件不存在时有明确错误
- [ ] SSH 认证失败时有明确错误
- [ ] 退出码正确（成功=0，失败=非0）

**M3: Ad-hoc 可用**

```bash
# 检查点
go-ansible all -m shell -a "uptime" -i hosts.ini
go-ansible all -m command -a "df -h" -i hosts.ini
go-ansible all -m shell -a "ls /nonexistent" -i hosts.ini  # 应该报错
```

- [ ] shell 命令能执行并显示输出
- [ ] command 命令能执行并显示输出
- [ ] 命令失败时显示 stderr 和退出码
- [ ] 输出格式清晰可读

**M4: Playbook 最小集**

```bash
# 检查点
go-ansible playbook simple.yml -i hosts.ini
```

- [ ] Playbook YAML 解析正确
- [ ] 多个 Play 依次执行
- [ ] 每个 Play 内的 Task 顺序执行
- [ ] PLAY RECAP 统计信息正确

**M5: 条件和循环**

```bash
# 检查点
go-ansible playbook conditional.yml -i hosts.ini
go-ansible playbook loop.yml -i hosts.ini
```

- [ ] `gather_facts: true` 能收集系统信息
- [ ] `when` 条件正确评估
- [ ] `loop` 循环正确展开
- [ ] 模板渲染支持 `{{ }}` 语法

**M6: 核心模块**

```bash
# 检查点
go-ansible playbook full.yml -i hosts.ini
```

- [ ] copy 模块能传输文件
- [ ] template 模块能渲染并传输
- [ ] file 模块能创建目录
- [ ] yum/apt 模块能安装包
- [ ] service 模块能管理服务
- [ ] debug 模块能输出信息
- [ ] assert 模块能进行断言

**M7: MVP 完成**

```bash
# 检查点：执行完整的服务器配置
go-ansible playbook site.yml -i inventory/production

# site.yml 包含：
# - 安装软件包
# - 拷贝配置文件
# - 启动服务
# - 健康检查
```

- [ ] 完整 Playbook 能成功执行
- [ ] 错误处理合理（不会因为单个 Task 失败而停止整个 Playbook）
- [ ] 退出码正确反映执行结果
- [ ] 输出信息足够调试问题

### 8.3 质量检查

在达到 MVP 之前，还需要确保以下质量标准：

**测试覆盖**

```bash
make test-coverage
# 目标：核心模块覆盖率 >= 80%
```

- [ ] inventory 包覆盖率 >= 90%
- [ ] connection 包覆盖率 >= 80%（使用 mock）
- [ ] modules 包覆盖率 >= 80%
- [ ] engine 包覆盖率 >= 85%

**代码质量**

```bash
make lint
make vet
```

- [ ] golangci-lint 无警告
- [ ] go vet 无警告
- [ ] 无 race condition（`go test -race`）

**文档**

- [ ] README.md 包含安装和使用说明
- [ ] 每个包都有 package doc comment
- [ ] 公共函数都有 doc comment

---

## 参考资料

- [设计文档](../superpowers/specs/2026-05-25-go-ansible-design.md)——完整的技术规范
- [实现计划](../superpowers/plans/2026-05-25-go-ansible-implementation.md)——详细的实现步骤
- [00-overview.md](./00-overview.md)——项目总览
- [01-architecture.md](./01-architecture.md)——架构详解
- [Ansible Ad-hoc 命令](https://docs.ansible.com/ansible/latest/command_guide/intro_adhoc.html)
- [Ansible Playbook](https://docs.ansible.com/ansible/latest/playbook_guide/index.html)
