# go-ansible 项目总览

> 本文档是 go-ansible 系列教学文档的入口，帮助你理解项目全貌、技术选型和学习路径。

---

## 一、文档导读

### 1.1 本系列文档列表

| 编号 | 文档 | 主题 | 建议阅读阶段 |
|------|------|------|-------------|
| 00 | overview.md（本文） | 项目总览、技术选型、学习路线 | 开始前 |
| 01 | architecture.md | 五层架构、数据流、插件化设计 | P0 完成后 |
| 02 | inventory.md | Inventory 系统原理与实现 | P1 阶段 |
| 03 | connection.md | SSH 连接、认证、文件传输 | P2 阶段 |
| 04 | variables.md | 变量系统、优先级、作用域链 | P3 阶段 |
| 05 | template.md | 模板引擎、text/template + Sprig | P3 阶段 |
| 06 | modules.md | 模块系统、接口、注册机制 | P4 阶段 |
| 07 | engine.md | Playbook 执行引擎 | P5 阶段 |
| 08 | more-modules.md | 扩展模块（包管理、服务、文件） | P6 阶段 |
| 09 | roles.md | Roles 系统、依赖、加载顺序 | P7 阶段 |
| 10 | handlers.md | Handler 机制、Block/Rescue/Always | P8 阶段 |
| 11 | async.md | 异步任务、轮询、超时处理 | P9 阶段 |
| 12 | vault.md | Vault 加密、密钥派生、多密码支持 | P10 阶段 |
| 13 | galaxy.md | Collections、Galaxy CLI、依赖管理 | P11 阶段 |
| 14 | callbacks.md | 回调插件、输出格式化、退出码 | P12 阶段 |
| 15 | filters.md | 过滤器、测试插件、查找插件 | P13 阶段 |
| 16 | minimal-viable-path.md | 最小可运行路径、里程碑检查 | 贯穿全程 |

### 1.2 推荐阅读顺序

```
初学者路径（按实现顺序）：
00 → 01 → 02 → 03 → 04 → 05 → 06 → 07 → 16

深入理解路径（按主题）：
00 → 01 → 16（先建立全局观）
然后按需查阅 02-15 中感兴趣的主题
```

### 1.3 文档约定

- **中文为主**，技术术语保留英文（如 Playbook、Inventory、Handler）
- **Go 代码**只展示 type/interface 签名，不写函数体
- **每个文档**遵循统一模板：原理（Why）→ 机制（How）→ Go 实现要点 → 任务拆解 → 参考资料
- **设计文档**位于 `docs/superpowers/specs/2026-05-25-go-ansible-design.md`，是权威参考

---

## 二、Ansible 是什么

### 2.1 一句话定义

Ansible 是一个**无代理（agentless）**的 IT 自动化工具，通过 SSH 连接到远程主机，以**声明式**的方式描述系统状态，并**幂等**地执行变更。

### 2.2 核心特征

**无代理（Agentless）**

不需要在目标主机上安装任何软件。Ansible 通过 SSH 连接到远程主机，执行命令后退出。这意味着：
- 零部署成本——只要有 SSH 访问权限即可管理
- 零维护成本——不需要升级 agent、不需要管理 agent 生命周期
- 安全性好——不需要开放额外端口，复用 SSH 基础设施

**基于 SSH**

所有通信通过 SSH 进行，支持密码、密钥、SSH Agent 等认证方式。SSH 是 Linux 系统管理员最熟悉的基础协议，无需额外学习。

**声明式（Declarative）**

你描述的是"系统应该是什么样"，而不是"怎么变成那样"。例如：

```yaml
# 声明式（Ansible）
- name: Ensure nginx is installed and running
  yum:
    name: nginx
    state: present
  service:
    name: nginx
    state: started
    enabled: true
```

```bash
# 命令式（Shell 脚本）
if ! rpm -q nginx > /dev/null 2>&1; then
    yum install -y nginx
fi
if ! systemctl is-active nginx > /dev/null 2>&1; then
    systemctl start nginx
fi
systemctl enable nginx
```

声明式的好处：代码更简洁、意图更清晰、更容易审查和维护。

**幂等性（Idempotency）**

同一个 Playbook 执行一次和执行十次的结果完全相同。Ansible 通过检查当前状态来决定是否需要执行变更：

- 如果 nginx 已经安装，`yum install` 不会再次执行（返回 ok 状态）
- 如果 nginx 已经在运行，`service start` 不会再次执行（返回 ok 状态）
- 只有当实际发生变更时，才返回 changed 状态

幂等性是自动化工具最重要的特性——它让你可以放心地反复执行 Playbook，而不用担心"重复执行会出问题"。

### 2.3 与同类工具对比

| 特性 | Ansible | Puppet | Chef | SaltStack |
|------|---------|--------|------|-----------|
| 架构 | Push（无代理） | Pull（Agent） | Pull（Agent） | Push/Pull（Agent） |
| 通信协议 | SSH | HTTPS | HTTPS | ZeroMQ |
| 配置语言 | YAML | Puppet DSL | Ruby DSL | YAML/Jinja2 |
| 学习曲线 | 低 | 中 | 高 | 中 |
| 部署复杂度 | 极低（只需 SSH） | 高（Master+Agent） | 高（Server+Agent） | 中（Master+Minion） |
| 执行模式 | 顺序执行 | 周期收敛 | 周期收敛 | 事件驱动 |
| 适用规模 | 中小规模 | 大规模 | 大规模 | 大规模 |
| Windows 支持 | 有（WinRM） | 有 | 有 | 有 |

**Ansible 的核心优势**在于其简单性：不需要安装 agent、不需要管理证书、不需要维护基础设施。你只需要一台能 SSH 到目标主机的控制机。

**Ansible 的劣势**在于规模：没有 agent 意味着每次执行都要建立 SSH 连接，在管理数千台主机时效率不如 Puppet/SaltStack 的 agent 模式。

### 2.4 Ansible 的典型使用场景

1. **配置管理**：确保所有服务器的配置文件、软件包、服务状态一致
2. **应用部署**：自动化部署流程，从拉取代码到重启服务
3. **编排协调**：跨多台主机的协调操作，如滚动更新
4. **临时命令**：快速在多台主机上执行命令，如批量查看磁盘使用率
5. **基础设施供应**：配合云模块创建和配置云资源

---

## 三、Ansible 核心设计理念

### 3.1 幂等性：状态收敛 vs 命令式

幂等性是 Ansible 最核心的设计理念。理解幂等性，需要区分两种执行模型：

**命令式执行模型（Shell 脚本）**

```bash
# 每次执行都会追加一行
echo "192.168.1.1 web1" >> /etc/hosts
```

这个脚本执行一次是对的，执行十次就错了——`/etc/hosts` 里会有十行重复记录。

**幂等执行模型（Ansible）**

```yaml
- name: Ensure host entry exists
  lineinfile:
    path: /etc/hosts
    line: "192.168.1.1 web1"
```

这个任务执行一次和执行十次的结果完全相同——如果行已存在，就跳过；如果不存在，就添加。

**状态收敛（State Convergence）**

Ansible 的模块不是"执行一个操作"，而是"确保系统达到某个状态"。这就像恒温器：

- 恒温器不会"开暖气"或"关暖气"，它"确保温度是 22 度"
- 当前温度 20 度 → 开暖气（changed）
- 当前温度 22 度 → 什么都不做（ok）
- 当前温度 24 度 → 开冷气（changed）

每个 Ansible 模块都实现了这个模式：检查当前状态 → 比较目标状态 → 执行必要的变更。

**Go 实现启示**

在 Go 中实现模块时，每个模块的 `Run` 方法必须：
1. 先检查当前状态（通过 SSH 执行命令获取信息）
2. 比较当前状态与目标状态
3. 只在需要时执行变更
4. 返回准确的 `Changed` 标志

### 3.2 声明式 vs 命令式

**命令式编程**关注"怎么做"——你告诉计算机每一步操作。

**声明式编程**关注"要什么"——你告诉计算机期望的结果，由它决定怎么做。

Ansible 的 YAML Playbook 是声明式的。你描述的是期望的系统状态，而不是操作步骤：

```yaml
# 声明式：描述期望状态
- name: Configure web server
  hosts: webservers
  tasks:
    - name: nginx is installed
      yum:
        name: nginx
        state: present

    - name: nginx config is correct
      template:
        src: nginx.conf.j2
        dest: /etc/nginx/nginx.conf
        validate: "nginx -t -c %s"

    - name: nginx is running
      service:
        name: nginx
        state: started
        enabled: true
```

注意你不需要关心：
- nginx 是否已经安装（如果已安装就跳过）
- 配置文件是否已经正确（如果已经正确就跳过）
- 服务是否已经在运行（如果已在运行就跳过）
- 配置变更后是否需要重启服务（通过 Handler 机制处理）

**Go 实现启示**

声明式的核心在于模块需要自己判断"需不需要执行"。在 Go 中，这意味着模块实现要比命令式脚本更复杂——你需要先检查状态，再决定是否执行。

### 3.3 Push 架构

Ansible 使用 Push 架构——控制机主动将配置推送到目标主机，而不是目标主机定期拉取配置。

**Push vs Pull 对比：**

| 特性 | Push（Ansible） | Pull（Puppet/Chef） |
|------|----------------|---------------------|
| 触发方式 | 手动或 CI/CD 触发 | Agent 定期拉取 |
| 实时性 | 立即生效 | 取决于拉取间隔 |
| 控制力 | 完全控制执行时机 | Agent 自主执行 |
| 基础设施 | 只需控制机 | 需要 Master 服务器 |
| 离线场景 | 不适用 | Agent 可缓存配置 |

Push 架构的优势：
- 简单——不需要维护 Master 服务器和 Agent 之间的通信
- 可控——你决定什么时候执行、执行什么
- 安全——不需要目标主机主动连接外部服务器

**Go 实现启示**

Push 架构意味着 go-ansible 需要管理大量的 SSH 连接。Go 的 goroutine 模型非常适合这个场景——每个主机一个 goroutine，通过 channel 协调结果。

### 3.4 YAML 作为用户界面

Ansible 选择 YAML 作为配置语言，这是一个深思熟虑的设计决策：

**YAML 的优势：**
- 人类可读——比 JSON、XML 更容易阅读和编写
- 支持注释——可以用 `#` 添加注释，JSON 不行
- 层次结构——通过缩进表达嵌套关系，直观清晰
- 广泛支持——几乎所有编程语言都有 YAML 解析库

**YAML 的劣势：**
- 缩进敏感——缩进错误会导致解析失败或语义错误
- 类型歧义——`yes` 会被解析为布尔值 `true`，`1.20` 会被解析为浮点数
- 错误信息不友好——YAML 解析器的错误信息往往难以理解

**Ansible 的 YAML 扩展：**

Ansible 在标准 YAML 基础上添加了 Jinja2 模板语法：

```yaml
# 标准 YAML
- name: Install packages
  yum:
    name: nginx
    state: present

# Jinja2 模板扩展
- name: Install packages
  yum:
    name: "{{ package_name }}"
    state: "{{ package_state | default('present') }}"
```

`{{ }}` 是 Jinja2 的变量引用语法，`|` 是过滤器。这些表达式在 YAML 解析之后、任务执行之前被渲染。

**Go 实现启示**

go-ansible 使用 Go 的 `text/template` + Sprig 代替 Jinja2。这意味着：
- 语法不兼容——`{{ foo | upper }}`（Jinja2）vs `{{ .foo | upper }}`（Go template）
- 需要变量前缀预处理——将 `{{ foo }}` 自动转换为 `{{ .foo }}`
- 功能覆盖——Sprig 提供了大部分 Jinja2 过滤器的等价实现

### 3.5 插件化扩展

Ansible 的几乎所有功能都是插件：

| 插件类型 | 作用 | 示例 |
|----------|------|------|
| Connection | 连接方式 | ssh, local, winrm |
| Module | 执行操作 | shell, copy, yum, service |
| Callback | 输出格式 | default, json, yaml |
| Lookup | 数据查找 | file, pipe, env, password |
| Filter | 数据转换 | upper, default, combine |
| Test | 条件判断 | defined, match, version |
| Strategy | 执行策略 | linear, free |

插件化的优势：
- **可扩展**——用户可以编写自己的插件
- **可组合**——不同插件可以自由组合
- **可测试**——每个插件独立测试
- **松耦合**——插件之间通过接口交互

**Go 实现启示**

在 Go 中实现插件化，使用 `init()` 函数注册模式：

```go
// internal/modules/ping.go
func init() {
    modules.Register("ping", &PingModule{})
}

type PingModule struct{}

func (m *PingModule) Name() string { return "ping" }
func (m *PingModule) Run(ctx ExecContext) (Result, error) {
    // ping 模块总是成功
    return Result{Changed: false}, nil
}
```

每个插件包通过 `init()` 自动注册到全局注册表，主程序通过 `import _ "internal/modules"` 触发注册。

---

## 四、为什么用 Go 重写

### 4.1 Python 依赖痛点

Ansible 是用 Python 编写的，这带来了几个实际问题：

**Python 版本混乱**

```
系统 Python 2.7（CentOS 7 默认）
    ├── Ansible 2.9 需要 Python 2.7 或 3.6+
    ├── Ansible 2.12+ 需要 Python 3.8+
    └── 目标主机需要 Python 2.6+（用于模块执行）
```

在实际运维中，你经常遇到这样的问题：
- 控制机的 Python 版本和 Ansible 要求的版本不匹配
- 目标主机的 Python 版本太旧，模块执行失败
- 不同项目需要不同版本的 Ansible，但它们依赖不同的 Python 版本

**pip 依赖地狱**

```bash
$ pip install ansible
# 安装了 40+ 个依赖包
$ pip list | wc -l
67
```

Ansible 的 pip 安装会拉入大量依赖包，这些依赖包之间可能存在版本冲突。在生产环境中，这经常导致"我机器上能跑，你机器上跑不了"的问题。

**虚拟环境的尴尬**

```bash
# 每个 Ansible 版本需要独立的虚拟环境
python3 -m venv ~/.venvs/ansible-2.9
source ~/.venvs/ansible-2.9/bin/activate
pip install ansible==2.9.*

python3 -m venv ~/.venvs/ansible-6.0
source ~/.venvs/ansible-6.0/bin/activate
pip install ansible-core==2.13.*
```

虚拟环境解决了版本隔离问题，但增加了使用复杂度。每次使用 Ansible 都需要先激活正确的虚拟环境。

### 4.2 单一二进制的优势

Go 编译后生成单一静态链接二进制文件，解决了 Python 的所有依赖问题：

```
Python 版 Ansible：
    ansible
    ├── Python 3.10+
    ├── 40+ pip 包
    ├── 系统库依赖
    └── 虚拟环境管理

Go 版 go-ansible：
    go-ansible（单个文件，~15MB）
    └── 无任何外部依赖
```

**部署对比：**

```bash
# Python Ansible
sudo apt install python3 python3-pip
pip3 install ansible
ansible --version  # 确认版本和路径

# go-ansible
curl -L https://github.com/.../go-ansible-linux-amd64 -o /usr/local/bin/go-ansible
chmod +x /usr/local/bin/go-ansible
go-ansible --version  # 直接可用
```

**版本管理对比：**

```bash
# Python：需要 pyenv + virtualenv
pyenv install 3.10.0
pyenv virtualenv 3.10.0 ansible-env
pyenv activate ansible-env
pip install ansible==6.0.0

# Go：直接下载对应版本
curl -L .../go-ansible-v1.0.0-linux-amd64 -o /usr/local/bin/go-ansible
```

### 4.3 goroutine 并发 vs GIL

**Python 的 GIL 问题**

CPython 有全局解释器锁（GIL），同一时刻只有一个线程能执行 Python 字节码。这意味着：

```python
# Python 多线程并不真正并行
import threading

def run_on_host(host):
    # 这里的代码在多线程中并不会真正并行执行
    result = ssh_exec(host, "uptime")
    return result

# 创建 10 个线程，但 CPU 密集部分是串行的
threads = [threading.Thread(target=run_on_host, args=(h,)) for h in hosts]
```

Ansible 解决 GIL 问题的方式是**多进程**——每个主机 fork 一个子进程。这有效但开销大：

```
Ansible fork 模型：
主进程
├── fork 子进程 1（web1）  ~10MB 内存
├── fork 子进程 2（web2）  ~10MB 内存
├── fork 子进程 3（web3）  ~10MB 内存
└── ...（最多 5 个并发，默认）
```

每个 fork 的子进程都有独立的 Python 解释器和内存空间，启动和通信开销较大。

**Go 的 goroutine 模型**

Go 没有 GIL，goroutine 是真正的轻量级线程：

```go
// Go goroutine 真正并行
func runOnHost(host *Host) Result {
    result := sshExec(host, "uptime")
    return result
}

// 启动 100 个 goroutine，真正并行执行
var wg sync.WaitGroup
for _, host := range hosts {
    wg.Add(1)
    go func(h *Host) {
        defer wg.Done()
        result := runOnHost(h)
        results <- result
    }(host)
}
```

**性能对比：**

| 指标 | Ansible fork | Go goroutine |
|------|-------------|-------------|
| 内存开销 | ~10MB/进程 | ~2KB/goroutine |
| 启动时间 | ~50ms/进程 | ~1μs/goroutine |
| 最大并发 | 默认 5 | 数千个 |
| 通信方式 | Pipe/Queue | Channel |
| 错误处理 | 进程退出码 | error 接口 |

Go 的 goroutine 模型让 go-ansible 可以轻松管理数百台主机的并发执行，而不需要担心进程开销。

### 4.4 学习价值

用 Go 重写 Ansible 不仅仅是一个工程项目，更是一个深入理解 Ansible 原理的学习过程：

**通过实现理解原理**

- 你不只是知道 Ansible "能做什么"，而是理解它"怎么做到的"
- 每一个功能的实现都对应一个 Ansible 内部机制的理解
- 遇到问题时，你知道该去哪里找原因

**Go 语言学习**

- 接口驱动设计——Go 的 interface 是最优雅的抽象机制
- 并发编程——goroutine + channel 是 Go 的核心竞争力
- 标准库运用——text/template、os/exec、crypto/ssh 等
- 项目组织——internal/ 包可见性控制、依赖注入

**系统编程能力**

- SSH 协议理解——认证、通道、SFTP
- 进程管理——远程命令执行、信号处理
- 加密算法——PBKDF2、AES-CTR、HMAC
- 网络编程——连接池、超时控制、重试机制

---

## 五、go-ansible 项目定位

### 5.1 不是 Fork，是重写

go-ansible 不是 Ansible 的 fork，而是一个从零开始的 Go 语言重写：

| 维度 | Ansible | go-ansible |
|------|---------|-----------|
| 语言 | Python 3 | Go 1.21+ |
| 代码基础 | 从零编写 | 从零编写 |
| YAML 格式 | 兼容 | 兼容 |
| Inventory 格式 | 标准 | 兼容 |
| Jinja2 模板 | 原生支持 | 不兼容（用 text/template） |
| Python 模块 | 支持 | 不支持（用 Go 模块） |
| Galaxy 集合 | 完整支持 | 计划支持 |

**兼容的部分：**
- Playbook YAML 结构
- Inventory 文件格式（INI 和 YAML）
- 命令行参数（-i, -m, -a, --become 等）
- Roles 目录结构
- Vault 文件格式
- Handler 机制

**不兼容的部分：**
- Jinja2 模板语法（用 Go text/template 替代）
- Python 模块（用 Go 模块替代）
- 某些 Jinja2 过滤器（用 Sprig 替代）
- Windows 管理（不支持，仅 Linux）

### 5.2 目标平台

go-ansible 的目标平台是 **Linux**：

- **控制机**：Linux（amd64/arm64）
- **目标主机**：仅 Linux 服务器
- **不支持**：Windows、macOS（作为目标主机）、网络设备

这个限制是刻意的——聚焦于 Linux 服务器管理场景，避免过度工程化。

### 5.3 设计原则

1. **接口驱动**——所有核心组件通过接口交互，方便测试和替换
2. **不可变数据**——变量上下文通过深拷贝传递，避免并发竞争
3. **插件化**——连接、模块、回调、查找都是插件，通过注册机制扩展
4. **渐进式实现**——从最小可用版本开始，逐步添加功能
5. **测试驱动**——每个功能先写测试，再写实现

---

## 六、技术选型决策

### 6.1 模板引擎：text/template + Sprig

**选择：** Go 标准库 `text/template` + Sprig 函数库

**理由：**
- 纯 Go 实现，无 cgo 依赖，编译后单一二进制
- Sprig 提供了 70+ 个模板函数，覆盖大部分 Jinja2 过滤器
- Helm 同款技术栈，社区验证充分
- 性能优秀，编译时优化

**不选择 Jinja2 的原因：**
- Python 的 Jinja2 无法在 Go 中直接使用
- 即使通过 cgo 调用 Python，也会破坏单一二进制的优势
- Go 的 text/template 生态已经足够成熟

**语法差异示例：**

```yaml
# Ansible (Jinja2)
msg: "Hello {{ name | upper }}, you have {{ items | length }} items"

# go-ansible (text/template + Sprig)
msg: "Hello {{ .name | upper }}, you have {{ .items | len }} items"
```

**变量前缀预处理：**

go-ansible 需要一个预处理器，将 Ansible 风格的 `{{ foo }}` 自动转换为 Go 风格的 `{{ .foo }}`。这个预处理器需要处理边界情况：
- `{{ foo }}` → `{{ .foo }}`
- `{{ foo.bar }}` → `{{ .foo.bar }}`
- `{{ foo | upper }}` → `{{ .foo | upper }}`
- `{{ lookup('file', '/etc/hosts') }}` → `{{ lookup "file" "/etc/hosts" }}`

### 6.2 SSH 库：golang.org/x/crypto/ssh

**选择：** Go 官方扩展库 `golang.org/x/crypto/ssh`

**理由：**
- 纯 Go 实现，无 cgo 依赖
- Go 官方维护，质量有保障
- 支持所有 SSH 认证方式（密钥、密码、Agent）
- 支持 SFTP 文件传输
- 跨平台（虽然 go-ansible 只支持 Linux 控制机）

**关键能力：**

```go
// SSH 客户端连接
type Client struct {
    // 连接到远程主机
    Connect(addr string, config *ClientConfig) (*Client, error)
    // 执行命令
    Exec(cmd string) ([]byte, []byte, int, error)
    // SFTP 文件传输
    SFTP() (*sftp.Client, error)
    // 关闭连接
    Close() error
}
```

### 6.3 CLI 框架：cobra

**选择：** `github.com/spf13/cobra`

**理由：**
- Go 生态事实标准 CLI 框架
- kubectl、docker、hugo 等知名项目都使用 cobra
- 支持子命令、标志、自动补全、帮助生成
- 社区活跃，文档完善

**命令结构设计：**

```
go-ansible
├── <host-pattern>       # ad-hoc 命令
├── playbook             # playbook 执行
├── inventory            # inventory 管理
│   ├── list
│   ├── host
│   └── graph
├── vault                # 加密管理
│   ├── encrypt
│   ├── decrypt
│   ├── view
│   └── rekey
├── galaxy               # 角色/集合管理
│   ├── install
│   ├── list
│   └── remove
└── config               # 配置管理
    ├── list
    └── dump
```

### 6.4 其他依赖

| 依赖 | 用途 | 选择理由 |
|------|------|---------|
| `gopkg.in/yaml.v3` | YAML 解析 | Go 生态标准 YAML 库，支持自定义类型 |
| `github.com/Masterminds/sprig` | 模板函数 | 70+ 函数，Helm 同款，覆盖常用场景 |
| `github.com/fatih/color` | 终端颜色 | 简单易用，跨平台颜色输出 |
| `github.com/mattn/go-isatty` | TTY 检测 | 判断是否在终端中运行，决定是否输出颜色 |
| `github.com/google/uuid` | UUID 生成 | 用于异步任务 ID、临时文件名等 |

---

## 七、整体功能清单

### 7.1 核心功能

| 功能 | 描述 | 优先级 |
|------|------|--------|
| Inventory 解析 | INI/YAML 格式、主机模式匹配 | P0 |
| SSH 连接 | 认证、命令执行、文件传输 | P0 |
| Ad-hoc 命令 | `go-ansible all -m ping` | P0 |
| Playbook 执行 | YAML 解析、顺序执行 | P0 |
| 模块系统 | shell, ping, copy, file 等 | P0 |
| 变量系统 | 16 级优先级、深合并 | P0 |
| 模板引擎 | text/template + Sprig | P0 |

### 7.2 高级功能

| 功能 | 描述 | 优先级 |
|------|------|--------|
| Roles 系统 | 目录结构、依赖管理、加载顺序 | P1 |
| Handlers | 通知机制、Play 结束后触发 | P1 |
| Block/Rescue/Always | 错误处理、恢复逻辑 | P1 |
| 条件与循环 | when 条件、loop/with_items 循环 | P1 |
| Tags | 任务标记、选择性执行 | P1 |
| 异步任务 | async/poll 模式、后台执行 | P1 |
| Vault 加密 | AES-256-CTR、多密码支持 | P1 |
| Galaxy | 角色/集合安装、依赖解析 | P1 |
| 回调插件 | default/json/yaml 输出格式 | P1 |
| 过滤器 | Ansible 特有过滤器（ipaddr, combine 等） | P1 |
| 查找插件 | file, pipe, env, password 等 | P1 |
| 策略插件 | linear（默认）、free 策略 | P1 |
| 配置系统 | ansible.cfg 兼容、环境变量 | P1 |

### 7.3 不支持的功能

| 功能 | 原因 |
|------|------|
| Windows 管理 | 目标平台聚焦 Linux |
| Python 模块 | Go 模块替代 |
| Jinja2 语法 | text/template 替代 |
| Network 模块 | 不在网络设备管理范围内 |
| Cloud 模块 | 不在核心范围内 |
| Callback 插件自定义 | 计划支持，但不是优先级 |

---

## 八、学习路线图

### 8.1 阶段与文档对应关系

```
P0: 项目骨架 + CLI
    └── 文档：00-overview（本文）+ 01-architecture
    └── 产出：go-ansible --help 正常工作

P1: Inventory 系统
    └── 文档：02-inventory
    └── 产出：go-ansible inventory list 正常工作

P2: 连接层 (SSH/Local)
    └── 文档：03-connection
    └── 产出：SSH 连接测试通过

P3: 变量系统 + 模板引擎
    └── 文档：04-variables + 05-template
    └── 产出：变量渲染验证通过

P4: 核心模块
    └── 文档：06-modules
    └── 产出：go-ansible all -m ping 正常工作

P5: Playbook 引擎
    └── 文档：07-engine + 16-minimal-viable-path
    └── 产出：go-ansible playbook site.yml 执行成功

P6: 更多模块
    └── 文档：08-more-modules
    └── 产出：copy/file/yum/apt/service 模块可用

P7: Roles 系统
    └── 文档：09-roles
    └── 产出：roles 目录加载执行正常

P8: Handlers + 错误处理
    └── 文档：10-handlers
    └── 产出：block/rescue/handlers 功能可用

P9: 异步任务
    └── 文档：11-async
    └── 产出：async/poll 执行正常

P10: Vault 加密
    └── 文档：12-vault
    └── 产出：vault encrypt/decrypt 正常工作

P11: Collections + Galaxy
    └── 文档：13-galaxy
    └── 产出：galaxy install 正常工作

P12: 回调插件 + 输出格式化
    └── 文档：14-callbacks
    └── 产出：多种输出格式可用

P13: 过滤器/测试/查找插件
    └── 文档：15-filters
    └── 产出：完整模板能力

P14: E2E 测试 + 文档
    └── 文档：全部
    └── 产出：完整可用工具
```

### 8.2 阶段依赖图

```
P0 ──→ P1 ──→ P2 ──→ P3 ──→ P4 ──→ P5
                                    │
                    ┌───────────────┼───────────────┐
                    │               │               │
                    ▼               ▼               ▼
                   P6              P8              P9
                    │
                    ▼
                   P7
                    │
        ┌──────────┼──────────┐
        │          │          │
        ▼          ▼          ▼
      P10        P11        P12
        │          │          │
        └──────────┼──────────┘
                   │
                   ▼
                  P13
                   │
                   ▼
                  P14
```

**关键路径：** P0 → P1 → P2 → P3 → P4 → P5

这条路径是 go-ansible 能够执行最小 Playbook 的最短路径。其他阶段可以并行或后续添加。

### 8.3 学习建议

1. **先通读 00 和 01**，建立全局理解
2. **按 P0-P5 顺序实现**，这是最小可用路径
3. **每个阶段先读对应文档**，理解原理后再动手
4. **测试驱动**，先写测试再写实现
5. **不要跳过基础阶段**，后续阶段都依赖前面的基础

---

## 参考资料

- [Ansible 官方文档](https://docs.ansible.com/)
- [Ansible 源码](https://github.com/ansible/ansible)
- [Go text/template 文档](https://pkg.go.dev/text/template)
- [Sprig 模板函数库](https://masterminds.github.io/sprig/)
- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh)
- [cobra CLI 框架](https://cobra.dev/)
- [设计文档](../superpowers/specs/2026-05-25-go-ansible-design.md)
- [实现计划](../superpowers/plans/2026-05-25-go-ansible-implementation.md)
