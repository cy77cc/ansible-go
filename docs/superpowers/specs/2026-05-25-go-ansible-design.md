# ansible-go 设计文档

> 用 Go 语言实现的完整 Ansible 工具，目标是 1:1 复刻 Ansible 核心功能。

## 项目定位

**实用工具**——可在实际环境中用来管理 Linux 服务器。架构设计上具备完整扩展性。

## 技术决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 模板引擎 | Go text/template + Sprig (Helm 风格) | 纯 Go，无 cgo 依赖，不兼容 Jinja2 |
| SSH 库 | golang.org/x/crypto/ssh | 纯 Go 实现，跨平台 |
| 目标平台 | Linux | 仅管理 Linux 服务器 |
| 模块执行 | 本地编排 + SSH 命令 | 不拷贝模块脚本到远程，通过 SSH 直接执行命令 |
| 格式兼容 | 核心格式兼容 | playbook YAML、inventory 格式兼容 Ansible |
| CLI 框架 | cobra | Go 生态标准 CLI 框架 |

---

## 一、整体架构

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

**数据流：**

```
CLI 参数 → 解析 Inventory → 加载 Variables → 解析 Playbook/Ad-hoc
    → 模板渲染 → 模块执行 → SSH/Local → 收集结果 → 回调输出
```

**核心设计原则：**

1. **插件化架构** — 连接、模块、回调、查找都是插件，通过注册机制扩展
2. **并发执行** — 利用 Go goroutine 实现多主机并行（Ansible 的 fork 机制）
3. **不可变数据** — 变量上下文通过深拷贝传递，避免并发竞争
4. **接口驱动** — 所有核心组件通过接口交互，方便测试和替换

---

## 二、CLI 层设计

基于 `cobra` 库，顶层命令结构：

```
ansible-go
├── <host-pattern>  # ad-hoc 命令（位置参数即主机模式，需 -m 指定模块）
├── playbook        # 执行 playbook 文件
├── inventory       # 管理 inventory
│   ├── list     # 列出所有主机/组
│   ├── host     # 查看单个主机变量
│   └── graph    # 显示主机关系图
├── vault        # 加密/解密
│   ├── encrypt
│   ├── decrypt
│   ├── view
│   └── rekey
├── galaxy       # 角色/集合管理
│   ├── install
│   ├── list
│   └── remove
├── config       # 查看/管理配置
│   ├── list
│   └── dump
└── playbook     # 执行 playbook
    └── --syntax-check  # 语法检查模式
```

**全局标志（所有命令共享）：**

| 标志 | 短写 | 说明 | 默认值 |
|------|------|------|--------|
| `--inventory` | `-i` | inventory 文件/目录路径 | `/etc/ansible/hosts` |
| `--user` | `-u` | SSH 用户名 | 当前用户 |
| `--private-key` | `--key-file` | SSH 私钥路径 | `~/.ssh/id_rsa` |
| `--become` | | 提权（sudo） | false |
| `--become-method` | | 提权方式 | sudo |
| `--become-user` | | 提权目标用户 | root |
| `--forks` | `-f` | 并发数 | 5 |
| `--verbosity` | `-v` | 日志级别（-v 到 -vvvv） | 0 |
| `--timeout` | | SSH 超时秒数 | 10 |
| `--diff` | | 显示文件变更 diff | false |
| `--check` | | 干跑模式（不实际执行） | false |
| `--limit` | | 限制执行的主机 | |
| `--tags` | | 只执行指定 tag | |
| `--skip-tags` | | 跳过指定 tag | |
| `--extra-vars` | `-e` | 额外变量（JSON 或 key=value） | |

**Ad-hoc 命令用法：**

```bash
ansible-go <host-pattern> -m <module> -a "<args>" [flags]
# 示例
ansible-go all -m shell -a "uptime"
ansible-go webservers -m copy -a "src=/local/file dest=/remote/file"
ansible-go db -m service -a "name=mysql state=restarted" --become
```

**Playbook 命令用法：**

```bash
ansible-go playbook site.yml [flags]
# 示例
ansible-go playbook site.yml -i inventory/production
ansible-go playbook deploy.yml --limit webservers --tags deploy
ansible-go playbook site.yml --check --diff
```

---

## 三、Inventory 系统

### 3.1 数据模型

```
Inventory
├── Host          # 单台主机
│   ├── Name      # 主机名/IP
│   ├── Port      # SSH 端口（默认22）
│   ├── Variables # 主机级变量
│   └── Groups    # 所属组列表
│
├── Group         # 主机组
│   ├── Name      # 组名
│   ├── Hosts     # 包含的主机
│   ├── Children  # 子组
│   ├── Variables # 组级变量
│   └── Parent    # 父组
│
└── InventorySource  # 来源
    ├── File      # 文件路径
    ├── Format    # ini / yaml / dynamic
    └── Parsed    # 解析结果
```

### 3.2 支持的格式

**INI 格式：**

```ini
[webservers]
web1 ansible_host=192.168.1.10 ansible_port=22
web2 ansible_host=192.168.1.11

[dbservers]
db1 ansible_host=192.168.1.20
db2 ansible_host=192.168.1.21

[production:children]
webservers
dbservers

[all:vars]
ansible_user=deploy
ansible_ssh_private_key_file=~/.ssh/deploy_key

[webservers:vars]
http_port=80
```

**YAML 格式：**

```yaml
all:
  vars:
    ansible_user: deploy
  children:
    webservers:
      hosts:
        web1:
          ansible_host: 192.168.1.10
          http_port: 80
        web2:
          ansible_host: 192.168.1.11
      vars:
        nginx_version: 1.24
    dbservers:
      hosts:
        db1:
          ansible_host: 192.168.1.20
        db2:
          ansible_host: 192.168.1.21
    production:
      children:
        webservers:
        dbservers:
```

**目录格式：**

```
inventory/
├── hosts.ini          # 主文件
├── host_vars/         # 主机级变量目录
│   ├── web1.yml
│   └── db1.yml
└── group_vars/        # 组级变量目录
    ├── webservers.yml
    ├── dbservers.yml
    └── all.yml
```

### 3.3 变量优先级（从低到高）

```
1. group_vars/all.yml
2. 父组变量
3. 子组变量
4. inventory 文件中的组变量 [group:vars]
5. inventory 文件中的主机变量 host ansible_var=x
6. host_vars/<hostname>.yml
7. playbook 中的 vars:
8. task 中的 vars:
9. --extra-vars (-e)  （最高优先级）
```

### 3.4 主机模式匹配

| 模式 | 含义 | 示例 |
|------|------|------|
| `all` / `*` | 所有主机 | `all` |
| 组名 | 指定组 | `webservers` |
| `:` | 并集 | `webservers:dbservers` |
| `:&` | 交集 | `webservers:&staging` |
| `:!` | 差集 | `all:!dbservers` |
| 通配符 | 模式匹配 | `web*.example.com` |
| 索引 | 主机切片 | `webservers[0]` |
| 正则 | 正则匹配 | `~web[0-9]+\.example\.com` |

---

## 四、Playbook YAML 格式规范

### 4.1 Playbook 结构

```yaml
- name: Configure webservers
  hosts: webservers
  become: true
  gather_facts: true
  vars:
    http_port: 80
  vars_files:
    - vars/common.yml
  roles:
    - common
    - nginx
  tasks:
    - name: Install nginx
      yum:
        name: nginx
        state: present
      tags: [install]

    - name: Start nginx
      service:
        name: nginx
        state: started
      notify: restart nginx
      when: ansible_os_family == "RedHat"

  handlers:
    - name: restart nginx
      service:
        name: nginx
        state: restarted
```

### 4.2 Task 完整字段

```yaml
- name: <string>            # 任务描述（必填）
  <module>:                  # 模块名（必填）
    <param>: <value>         # 模块参数

  # 控制流
  when: <condition>
  loop: <list>
  loop_control:
    loop_var: item
    index_var: idx
    label: "{{ item.name }}"
  register: <var_name>
  ignore_errors: true
  failed_when: <condition>
  changed_when: <condition>
  check_mode: true/false

  # 执行控制
  async: <seconds>
  poll: <seconds>
  retries: <n>
  delay: <seconds>
  until: <condition>
  throttle: <n>

  # 标记与过滤
  tags: [tag1, tag2]
  delegate_to: <host>
  run_once: true

  # 错误处理
  rescue:
  always:
```

### 4.3 Block / Rescue / Always

```yaml
tasks:
  - block:
      - name: Try this
        shell: /usr/bin/might_fail
    rescue:
      - name: Handle failure
        debug:
          msg: "Task failed, running recovery"
    always:
      - name: Always run
        debug:
          msg: "This always runs"
```

### 4.4 条件与循环

```yaml
# 条件
- name: Only on RedHat
  yum:
    name: httpd
    state: present
  when: ansible_os_family == "RedHat"

# 循环
- name: Install packages
  yum:
    name: "{{ item }}"
    state: present
  loop:
    - nginx
    - vim
    - curl
```

---

## 五、Playbook 执行引擎

### 5.1 执行流程

```
Playbook YAML
    │
    ▼
1. 加载 & 解析      读取 YAML，合并 vars_files
    │
    ▼
2. 变量渲染         用模板引擎渲染所有 {{ }} 表达式
    │
    ▼
3. 主机匹配         根据 hosts: 模式选择目标主机
    │
    ▼
4. Facts 收集       SSH 到每台主机收集系统信息
    │
    ▼
5. 策略执行         按策略（linear/free）并发执行 task
    │
    ▼
6. 结果收集         聚合每台主机的执行结果
    │
    ▼
7. 回调输出         通知回调插件显示/记录结果
```

### 5.2 核心数据结构

```
Playbook
├── Plays []Play

Play
├── Name          string
├── Hosts         string
├── Become        bool
├── GatherFacts   bool
├── Vars          map[string]any
├── VarsFiles     []string
├── Roles         []string
├── PreTasks      []Task
├── Tasks         []Task
├── PostTasks     []Task
├── Handlers      []Task

Task
├── Name          string
├── Module        string
├── Args          map[string]any
├── When          string
├── Loop          any
├── Register      string
├── Tags          []string
├── Notify        []string
├── DelegateTo    string
├── RunOnce       bool
├── Async         int
├── Poll          int
├── Block         *Block

Block
├── Tasks         []Task
├── Rescue        []Task
├── Always        []Task
```

### 5.3 执行策略

**Linear 策略**（默认）：每个 task 等所有主机完成后再进入下一个。

**Free 策略**：每台主机独立推进，互不等待。

### 5.4 变量作用域

```
GlobalContext          # 全局（extra-vars, config）
  └── PlayContext      # Play 级
       └── RoleContext  # Role 级
            └── TaskContext  # Task 级
                 └── HostContext  # 主机级
```

**合并规则：**
- 同名 dict：深度合并（递归）
- 同名标量：子作用域覆盖父作用域
- 不可变：每次合并产生新对象

### 5.5 Handler 机制

```
Task 执行成功且 changed=true → notify handler name → 加入 pending 队列
→ Play 所有 tasks 完成后 → 执行匹配的 handler（每个 handler 只执行一次）
```

### 5.6 错误处理流程

```
Task 执行
├── success → 继续
├── failure → ignore_errors? → 继续（标记 failed）
│           → block 中? → rescue → always → 继续
│           → 默认 → always → 该主机停止
└── changed → 触发 notify handler
```

### 5.7 异步任务

- `async > 0`：启动 goroutine，返回 job_id
- `poll > 0`：主 goroutine 轮询等待结果
- `poll = 0`：立即返回，后续通过 async_status 查询

---

## 六、模块系统

### 6.1 执行模型

```
ansible-go (本地)
    ├── 1. 模块根据参数生成 shell 命令
    ├── 2. 通过 SSH 在远程主机执行命令
    ├── 3. 收集 stdout/stderr/exit code
    └── 4. 解析为统一结果格式
```

### 6.2 模块接口

```
Module
├── Name() string
├── Args() []ModuleArg
├── Run(ctx ExecContext) (Result, error)
├── SupportsCheckMode() bool
```

**ExecContext：**
```
ExecContext
├── Host          *Host
├── Args          map[string]any
├── Connection    Connection
├── CheckMode     bool
├── Diff          bool
├── Variables     map[string]any
```

**Result：**
```
Result
├── Changed   bool
├── Failed    bool
├── Msg       string
├── Stdout    string
├── Stderr    string
├── Rc        int
├── Diff      *DiffResult
├── Extra     map[string]any
```

### 6.3 核心模块清单

**文件管理类：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `copy` | 拷贝文件到远程 | `src`, `dest`, `owner`, `group`, `mode`, `backup` |
| `template` | 模板渲染后拷贝 | `src`, `dest`, `owner`, `group`, `mode` |
| `file` | 文件/目录操作 | `path`, `state`, `mode`, `recurse` |
| `stat` | 获取文件信息 | `path`, `checksum_algorithm` |
| `find` | 查找文件 | `paths`, `patterns`, `file_type`, `age`, `size` |
| `lineinfile` | 确保某行存在/不存在 | `path`, `line`, `regexp`, `state` |
| `blockinfile` | 插入/更新文本块 | `path`, `block`, `marker` |
| `synchronize` | rsync 同步 | `src`, `dest`, `mode` |
| `fetch` | 从远程拉取文件 | `src`, `dest`, `flat` |
| `unarchive` | 解压文件 | `src`, `dest`, `remote_src` |

**包管理类：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `yum` | RPM 包管理 | `name`, `state`, `enablerepo` |
| `apt` | Debian 包管理 | `name`, `state`, `update_cache` |
| `dnf` | DNF 包管理 | 同 yum |
| `pip` | Python 包管理 | `name`, `state`, `virtualenv` |

**服务管理类：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `service` | 系统服务管理 | `name`, `state`, `enabled` |
| `systemd` | systemd 管理 | `name`, `state`, `enabled`, `daemon_reload` |

**命令执行类：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `shell` | 执行 shell 命令 | `cmd`, `chdir`, `creates`, `removes` |
| `command` | 执行命令（不经 shell） | 同 shell |
| `script` | 传输并执行本地脚本 | `cmd`, `chdir` |
| `raw` | 原始 SSH 命令 | 参数即命令 |
| `expect` | 交互式命令 | `command`, `responses`, `timeout` |

**用户管理类：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `user` | 用户管理 | `name`, `state`, `uid`, `groups`, `shell` |
| `group` | 用户组管理 | `name`, `state`, `gid` |
| `authorized_key` | SSH 公钥管理 | `user`, `key`, `state` |

**网络类：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `uri` | HTTP 请求 | `url`, `method`, `body`, `headers` |
| `get_url` | 下载文件 | `url`, `dest`, `checksum` |
| `wait_for` | 等待条件 | `host`, `port`, `state`, `timeout` |
| `wait_for_connection` | 等待连接可用 | `timeout`, `delay` |

**系统类：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `hostname` | 设置主机名 | `name` |
| `cron` | 定时任务 | `name`, `minute`, `hour`, `job` |
| `sysctl` | 内核参数 | `name`, `value`, `state` |
| `setup` | 收集 facts | `filter`, `gather_subset` |
| `debug` | 调试输出 | `msg`, `var`, `verbosity` |
| `assert` | 断言检查 | `that`, `fail_msg` |
| `pause` | 暂停 | `seconds`, `prompt` |
| `set_fact` | 设置变量 | 键值对参数 |
| `meta` | 元操作 | `flush_handlers`, `end_play` |

**异步相关：**

| 模块 | 功能 | 关键参数 |
|------|------|----------|
| `async_status` | 查询异步任务状态 | `jid` |

### 6.4 模块注册机制

所有模块在启动时注册到全局注册表，执行时通过名字查找。

### 6.5 Facts 收集（setup 模块）

通过 SSH 执行预定义 shell 命令收集系统信息：

| 类别 | 收集内容 |
|------|----------|
| `hardware` | CPU、内存、架构、设备 |
| `network` | IP 地址、接口、网关、DNS |
| `virtual` | 虚拟化类型 |
| `distribution` | OS 发行版、版本号 |
| `user` | 当前用户、UID、GID |
| `date_time` | 系统时间、时区 |

---

## 七、连接层

### 7.1 连接接口

```
Connection
├── Connect(host Host) error
├── Exec(cmd string) (stdout, stderr string, rc int, error)
├── PutFile(localPath, remotePath string) error
├── FetchFile(remotePath, localPath string) error
├── Close() error
├── Shell() string
```

### 7.2 SSH 连接实现

基于 `golang.org/x/crypto/ssh`：

**认证方式（按优先级）：**
1. SSH Key（ed25519 / rsa / ecdsa，支持 passphrase）
2. SSH Agent（连接 SSH_AUTH_SOCK）
3. Password（ansible_ssh_pass 变量）

**SSH 变量清单：**

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `ansible_host` | SSH 目标地址 | 主机名 |
| `ansible_port` | SSH 端口 | 22 |
| `ansible_user` | SSH 用户 | 当前用户 |
| `ansible_ssh_pass` | SSH 密码 | |
| `ansible_ssh_private_key_file` | 私钥路径 | ~/.ssh/id_rsa |
| `ansible_ssh_common_args` | 额外 SSH 参数 | |
| `ansible_ssh_pipelining` | 是否启用管道 | false |
| `ansible_timeout` | 连接超时(秒) | 10 |

### 7.3 本地连接

用于 `connection: local` 场景，通过 `os/exec` 执行命令。

### 7.4 提权机制（Become）

```bash
# 原始命令
cat /etc/shadow

# become 后 (sudo)
sudo -H -S -n -u root /bin/sh -c 'cat /etc/shadow'
```

支持 `sudo`、`su`、`pbrun`、`pfexec` 等提权方式。

### 7.5 文件传输

通过 SFTP 子系统传输文件：建立 SFTP 会话 → 创建远程目录 → 写入文件 → 设置权限。

### 7.6 连接池

每个 `host:port` 维护一个连接，同一主机的多个 task 串行复用连接，`forks` 控制不同主机间的并发数。

---

## 八、模板引擎

### 8.1 技术选型

Go `text/template` + Sprig 函数库（Helm 同款），不兼容 Jinja2。

### 8.2 语法对照

| Jinja2 语法 | Go template 语法 |
|-------------|-----------------|
| `{{ foo \| upper }}` | `{{ .foo \| upper }}` |
| `{{ foo \| default('bar') }}` | `{{ .foo \| default "bar" }}` |
| `{{ items \| length }}` | `{{ .items \| len }}` |
| `{{ foo \| to_json }}` | `{{ .foo \| toJson }}` |
| `{% if x %}...{% endif %}` | `{{ if .x }}...{{ end }}` |
| `{% for i in items %}...{% endfor %}` | `{{ range .items }}...{{ end }}` |

### 8.3 渲染时机

- Playbook 加载时：vars_files 路径、hosts 字段、vars 值
- Task 执行前：task name、module args、when 条件、loop 数据
- 模块内部：template 模块渲染模板文件

### 8.4 变量前缀预处理

引擎自动将裸变量引用 `{{ foo }}` 转换为 `{{ .foo }}`，处理边界情况（已有前缀、函数调用等）。

---

## 九、变量系统

### 9.1 完整优先级（从低到高）

```
 1. role defaults                 (roles/x/defaults/main.yml)
 2. inventory file vars           ([group:vars])
 3. inventory group_vars/         (group_vars/all.yml → group_vars/<group>.yml)
 4. inventory host_vars/          (host_vars/<hostname>.yml)
 5. inventory host vars           (host ansible_var=x)
 6. play vars                     (playbook vars:)
 7. play vars_files               (playbook vars_files:)
 8. play vars_prompt              (交互式输入)
 9. role vars                     (roles/x/vars/main.yml)
10. block vars
11. task vars
12. include_vars
13. set_facts / registered vars
14. role parameters
15. include parameters
16. extra-vars (-e)               （最高优先级）
```

### 9.2 合并规则

- dict + dict → 递归合并
- list + list → 后者覆盖
- scalar + scalar → 后者覆盖
- 不可变：每次合并产生新对象

### 9.3 内置变量（Magic Variables）

| 变量 | 说明 |
|------|------|
| `inventory_hostname` | 当前主机名 |
| `inventory_hostname_short` | 主机名短形式 |
| `inventory_file` | inventory 文件路径 |
| `inventory_dir` | inventory 文件所在目录 |
| `group_names` | 当前主机所属的所有组 |
| `groups` | 所有组及组内主机映射 |
| `hostvars` | 所有主机的变量（可跨主机访问） |
| `ansible_check_mode` | 是否干跑模式 |
| `ansible_diff` | 是否 diff 模式 |
| `ansible_forks` | 并发数 |
| `ansible_play_hosts` | 当前 play 中所有主机 |
| `playbook_dir` | playbook 文件所在目录 |
| `role_name` | 当前角色名 |
| `role_path` | 当前角色路径 |

### 9.4 Facts 注入

Play 开始时（`gather_facts: true`），SSH 到每台主机执行 setup 模块，收集系统信息并注入变量上下文，可通过 `ansible_*` 变量访问。

`gather_subset` 可控制收集类别（`hardware`、`network`、`distribution` 等）。

---

## 十、Roles 系统

### 10.1 目录结构

```
roles/nginx/
├── defaults/main.yml    # 默认变量（最低优先级）
├── vars/main.yml        # 角色变量（高优先级）
├── tasks/main.yml       # 主任务列表
├── handlers/main.yml    # 处理器
├── templates/           # 模板文件
├── files/               # 静态文件
├── meta/main.yml        # 元数据（依赖、平台）
├── library/             # 自定义模块（可选）
└── tests/               # 测试
```

### 10.2 引用方式

```yaml
# 基本引用
  roles:
    - common
    - nginx
    - { role: app, app_port: 8080 }

# 带条件
  roles:
    - role: nginx
      when: install_nginx | default(true)

# 动态引用
  tasks:
    - include_role:
        name: nginx
      vars:
        nginx_port: 8080

# 静态导入
  tasks:
    - import_role:
        name: nginx
```

### 10.3 执行顺序

```
1. pre_tasks → 2. 触发 handlers → 3. roles（按依赖顺序）
→ 4. tasks → 5. post_tasks → 6. 触发所有 pending handlers
```

### 10.4 依赖

```yaml
# roles/nginx/meta/main.yml
dependencies:
  - role: common
  - role: ssl
    vars:
      ssl_cert_path: /etc/nginx/ssl
```

依赖 role 在当前 role 之前执行，循环依赖检测并报错，同一 role 只执行一次。

---

## 十一、Collections 与 Galaxy

### 11.1 Collections 结构

```
collections/ansible_collections/community/general/
├── meta/runtime.yml
├── plugins/modules/        # 模块
├── plugins/callback/       # 回调插件
├── plugins/connection/     # 连接插件
├── plugins/filter/         # 过滤器插件
├── plugins/lookup/         # 查找插件
├── playbooks/              # Playbook
├── roles/                  # Roles
├── galaxy.yml              # Galaxy 元数据
└── FILES.json
```

### 11.2 Galaxy CLI 命令

```bash
# 角色
ansible-go galaxy install username.rolename
ansible-go galaxy install -r requirements.yml
ansible-go galaxy list
ansible-go galaxy remove username.rolename

# 集合
ansible-go galaxy collection install community.general
ansible-go galaxy collection install -r requirements.yml
ansible-go galaxy collection list
ansible-go galaxy collection remove community.general

# 初始化
ansible-go galaxy init rolename
ansible-go galaxy collection init namespace.name
```

### 11.3 requirements.yml

```yaml
roles:
  - name: geerlingguy.nginx
    version: "3.1.0"

collections:
  - name: community.general
    version: ">=8.0.0"
```

### 11.4 Galaxy API 交互

查询 Galaxy API → 获取版本信息 → 下载 tarball → 解压到目标路径 → 注册模块/插件。

---

## 十二、Vault 加密

### 12.1 加密算法

- 密钥派生：PBKDF2-SHA256（10000 iterations，32 字节 salt）
- 加密：AES-256-CTR（随机 16 字节 IV）
- 完整性：HMAC-SHA256

### 12.2 文件格式

```
$ANSIBLE_VAULT;1.1;AES256
<hex-encoded HMAC>
<hex-encoded IV>
<hex-encoded ciphertext>
```

### 12.3 CLI 命令

```bash
ansible-go vault encrypt file.yml
ansible-go vault decrypt file.yml
ansible-go vault view file.yml
ansible-go vault edit file.yml
ansible-go vault rekey file.yml
ansible-go vault encrypt_string 'secret' --name 'db_password'
```

### 12.4 Vault ID（多密码支持）

```bash
ansible-go vault encrypt --vault-id prod@prompt file.yml
ansible-go playbook site.yml --vault-id prod@prompt --vault-id dev@/path/to/pass
```

### 12.5 密码来源优先级

1. `--vault-password-file` 命令行参数（文件路径或可执行文件）
2. `ANSIBLE_VAULT_PASSWORD_FILE` 环境变量
3. `ansible.cfg` 中的 `vault_password_file` 配置
4. 交互式提示输入

---

## 十三、回调插件与输出

### 13.1 回调插件接口

```
CallbackPlugin
├── OnPlaybookStart(playbook string)
├── OnPlayStart(play Play, hosts []string)
├── OnTaskStart(task Task, isHandler bool)
├── OnTaskOk(result TaskResult)
├── OnTaskFailed(result TaskResult, ignored bool)
├── OnTaskSkipped(result TaskResult)
├── OnTaskUnreachable(host string, result TaskResult)
├── OnPlaybookStats(stats PlayStats)
```

### 13.2 默认回调输出

```
PLAY [Configure webservers] *****************************************************

TASK [Gathering Facts] **********************************************************
ok: [web1]
ok: [web2]

TASK [Install nginx] ************************************************************
changed: [web1]
ok: [web2]

RUNNING HANDLER [restart nginx] **************************************************
changed: [web1]

PLAY RECAP ***********************************************************************
web1     : ok=4  changed=3  unreachable=0  failed=0  skipped=0
web2     : ok=2  changed=0  unreachable=0  failed=0  skipped=0
```

**颜色方案：** ok=绿色, changed=黄色, failed=红色, skipped=青色, PLAY/TASK=青色

### 13.3 其他回调

- **Minimal**：一行一个结果，适合脚本
- **JSON**：完整 JSON 输出，适合 CI/CD
- **YAML**：人类可读 YAML 格式
- **Timer**：显示执行耗时

### 13.4 退出码

| 条件 | 退出码 |
|------|--------|
| 所有主机成功 | 0 |
| 至少一台主机失败 | 2 |
| 无法连接的主机 | 4 |
| 其他错误 | 1 |

---

## 十四、查找插件

### 14.1 接口

```
LookupPlugin
├── Name() string
├── Run(terms []string, variables map[string]any) ([]string, error)
```

### 14.2 内置查找插件

| 插件 | 功能 | 示例 |
|------|------|------|
| `file` | 读取文件内容 | `lookup('file', '/etc/hosts')` |
| `template` | 渲染模板返回字符串 | `lookup('template', 'my.j2')` |
| `pipe` | 执行命令获取输出 | `lookup('pipe', 'git rev-parse HEAD')` |
| `env` | 读取环境变量 | `lookup('env', 'HOME')` |
| `password` | 生成/读取密码 | `lookup('password', '/tmp/pw length=20')` |
| `ini` | 读取 INI 文件 | `lookup('ini', 'user section=defaults')` |
| `url` | HTTP GET | `lookup('url', 'https://api.example.com')` |
| `fileglob` | 文件模式匹配 | `lookup('fileglob', '*.pem')` |
| `dict` | 字典迭代 | `lookup('dict', my_dict)` |
| `sequence` | 生成序列 | `lookup('sequence', 'start=1 end=10')` |

---

## 十五、过滤器与测试插件

### 15.1 Ansible 特有过滤器

需要在 Sprig 基础上额外实现：

| 过滤器 | 功能 |
|--------|------|
| `ipaddr` | IP 地址操作（网络、广播、掩码等） |
| `regex_replace` | 正则替换 |
| `regex_search` | 正则搜索 |
| `regex_findall` | 正则查找所有 |
| `combine` | 深度合并字典 |
| `flatten` | 展平列表 |
| `dict2items` / `items2dict` | 字典/列表互转 |
| `json_query` | JMESPath 查询 |
| `map` / `select` / `reject` | 列表映射/过滤 |
| `sort` / `groupby` / `unique` | 列表操作 |
| `difference` / `intersect` / `union` | 集合操作 |
| `mandatory` | 变量必须存在 |
| `b64encode` / `b64decode` | Base64 编解码 |
| `hash` / `checksum` | 哈希计算 |

### 15.2 测试插件

| 测试 | 功能 | 示例 |
|------|------|------|
| `defined` / `undefined` | 变量是否定义 | `when: foo is defined` |
| `success` / `failed` | 执行结果 | `when: result is success` |
| `changed` / `skipped` | 状态检查 | `when: result is changed` |
| `match` / `search` / `regex` | 模式匹配 | `when: name is match('web*')` |
| `version` | 版本比较 | `when: ver is version('20.04', '>=')` |
| `subset` / `superset` | 集合关系 | `when: hosts is subset(all)` |

---

## 十六、策略插件

### 16.1 Linear 策略（默认）

每个 task 等所有主机完成后再进入下一个。保证执行顺序一致性。

### 16.2 Free 策略

每台主机独立推进，互不等待。适合主机性能差异大的场景。

### 16.3 Serial 控制

```yaml
- hosts: webservers
  serial: 1          # 每次 1 台
  serial: "30%"      # 每次 30%
  serial: [1, 5, "25%"]  # 滚动批次
```

### 16.4 max_fail_percentage

```yaml
- hosts: webservers
  serial: 10
  max_fail_percentage: 30    # 失败超过 30% 则中止
```

### 16.5 并发模型

Worker Pool 大小 = forks（默认5）。使用 goroutine 池调度，空闲 worker 自动领取下一台主机。

---

## 十七、异步任务

### 17.1 使用模式

**等待完成（poll > 0）：**

```yaml
- shell: /opt/scripts/migrate.sh
  async: 3600
  poll: 30
  register: result
```

**立即返回（poll = 0）：**

```yaml
- shell: /opt/scripts/rebuild.sh
  async: 3600
  poll: 0
  register: job

- async_status:
    jid: "{{ job.ansible_job_id }}"
  register: result
  until: result.finished
  retries: 60
  delay: 60
```

### 17.2 执行模型

远程启动后台进程 → 写入元数据文件 → 返回 job_id → 轮询状态文件 → 超时发送 SIGTERM/SIGKILL。

### 17.3 async_status 模块

- `mode: check`（默认）— 查询任务状态
- `mode: cleanup` — 清理临时文件

---

## 十八、配置系统

### 18.1 配置文件层级（从低到高）

1. 内置默认值
2. /etc/ansible/ansible.cfg
3. ~/.ansible.cfg
4. ./ansible.cfg
5. ANSIBLE_CONFIG 环境变量指定的文件
6. 命令行参数

### 18.2 配置文件格式

```ini
[defaults]
inventory           = ./inventory
roles_path          = ./roles:/etc/ansible/roles
remote_user         = deploy
host_key_checking   = False
timeout             = 30
forks               = 5
log_path            = /var/log/ansible-go.log
stdout_callback     = default

[privilege_escalation]
become              = False
become_method       = sudo
become_user         = root

[ssh_connection]
ssh_args            = -o ControlMaster=auto -o ControlPersist=60s
pipelining          = True
```

### 18.3 环境变量映射

所有配置项都可通过 `ANSIBLE_*` 环境变量覆盖，如 `ANSIBLE_FORKS=10`。

### 18.4 配置子命令

```bash
ansible-go config list     # 列出所有配置
ansible-go config dump     # 显示当前值及来源
ansible-go config get key  # 查看单个配置
```

---

## 十九、项目目录结构

```
ansible-go/
├── cmd/ansible-go/main.go           # 入口
├── internal/
│   ├── cli/                         # CLI 层
│   ├── engine/                      # 执行引擎核心
│   ├── strategy/                    # 策略插件
│   ├── inventory/                   # Inventory 系统
│   ├── variables/                   # 变量系统
│   ├── template/                    # 模板引擎
│   ├── connection/                  # 连接层
│   ├── modules/                     # 模块系统
│   ├── plugins/                     # 插件系统
│   │   ├── callback/
│   │   ├── lookup/
│   │   ├── filter/
│   │   └── test/
│   ├── vault/                       # Vault 加密
│   ├── galaxy/                      # Galaxy 客户端
│   ├── roles/                       # Roles 系统
│   ├── collections/                 # Collections 系统
│   ├── config/                      # 配置系统
│   └── logging/                     # 日志系统
├── pkg/types/                       # 公共类型
├── pkg/utils/                       # 工具函数
├── testdata/                        # 测试数据
├── docs/
├── go.mod
├── Makefile
└── README.md
```

**Go Module 依赖：**

```
github.com/spf13/cobra          // CLI 框架
github.com/Masterminds/sprig    // 模板函数库
golang.org/x/crypto             // SSH 实现
gopkg.in/yaml.v3                // YAML 解析
github.com/google/uuid          // UUID 生成
github.com/fatih/color          // 终端颜色
github.com/mattn/go-isatty      // TTY 检测
```

---

## 二十、测试策略

### 20.1 测试分层

- **单元测试**（大量）：每个包独立测试，覆盖率 ≥ 80%
- **集成测试**（中量）：模块组合、引擎流程，使用 mock SSH
- **E2E 测试**（少量）：完整 playbook → SSH → 远程执行

### 20.2 各模块测试重点

| 包 | 重点 | 目标覆盖率 |
|---|------|-----------|
| inventory | INI/YAML 解析、主机模式匹配 | 90% |
| variables | 优先级合并、深度合并、并发安全 | 90% |
| template | 渲染、前缀预处理、Sprig 函数 | 90% |
| connection | SSH 连接、认证、文件传输（mock） | 80% |
| modules | 参数校验、执行、CheckMode、幂等性 | 80% |
| engine | 完整 Playbook 流程、handler、block | 85% |
| vault | 加解密往返、Vault ID、密码来源 | 90% |
| strategy | Linear/Free 调度、并发控制 | 80% |

### 20.3 测试辅助设施

- **Mock SSH Server**：本地 SSH 服务器，可配置命令响应映射
- **Fixture Playbooks**：覆盖各种场景的测试 playbook

### 20.4 测试命令

```bash
make test               # 所有单元测试
make test-coverage      # 覆盖率报告
make test-race          # 竞态检测
make test-e2e           # E2E 测试
make bench              # 基准测试
```

---

## 二十一、构建与开发工具

### 21.1 Makefile 目标

```
build          # 编译二进制
install        # 安装到 $GOPATH/bin
clean          # 清理
test           # 单元测试
test-coverage  # 覆盖率
lint           # golangci-lint
fmt            # gofmt
vet            # go vet
run            # 编译并运行
release        # 交叉编译多平台
```

### 21.2 构建流程

```bash
go build -ldflags "
    -X main.Version=$(git describe --tags)
    -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    -X main.Commit=$(git rev-parse --short HEAD)
" -o bin/ansible-go ./cmd/ansible-go
```

### 21.3 交叉编译

支持：linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

---

## 二十二、错误处理与日志

### 22.1 错误分类

- **FATAL** — 配置错误、Inventory 不存在、Playbook 语法严重错误 → 立即终止
- **HOST** — SSH 连接失败、认证失败、命令执行失败 → 该主机停止
- **TASK** — 模块执行失败、模板渲染失败 → 根据策略处理
- **WARNING** — 废弃语法、未使用变量 → 记录但继续

### 22.2 日志级别

| 级别 | CLI 标志 | 输出内容 |
|------|---------|---------|
| ERROR | 默认 | 错误信息 |
| WARNING | 默认 | 警告信息 |
| INFO | -v | task 结果、play 进度 |
| DEBUG | -vv | 变量值、模板渲染 |
| TRACE | -vvv | SSH 连接细节 |
| DEBUG2 | -vvvv | 原始 SSH 输出 |

### 22.3 退出码

| 条件 | 退出码 |
|------|--------|
| 成功 | 0 |
| 一般错误 | 1 |
| 主机失败 | 2 |
| 主机不可达 | 3 |
| 解析错误 | 4 |
| 权限错误 | 5 |

### 22.4 Retry 文件

失败时生成 `<playbook>.retry`，包含失败主机列表，可通过 `--limit @site.retry` 重试。

---

## 二十三、实现阶段规划

### 阶段总览

| 阶段 | 模块 | 可运行产出 |
|------|------|-----------|
| P0 | 项目骨架 + CLI | `ansible-go --help` |
| P1 | Inventory 系统 | `ansible-go inventory list` |
| P2 | 连接层 (SSH/Local) | SSH 连接测试 |
| P3 | 变量系统 + 模板引擎 | 变量渲染验证 |
| P4 | 核心模块 | `ansible-go all -m ping` |
| P5 | Playbook 引擎 | `ansible-go playbook x.yml` |
| P6 | 更多模块 | 完整 playbook 执行 |
| P7 | Roles 系统 | roles 目录加载执行 |
| P8 | Handlers + 错误处理 | block/rescue/handlers |
| P9 | 异步任务 | async/poll 执行 |
| P10 | Vault 加密 | vault encrypt/decrypt |
| P11 | Collections + Galaxy | galaxy install |
| P12 | 回调插件 + 输出格式化 | 多种输出格式 |
| P13 | 过滤器/测试/查找插件 | 完整模板能力 |
| P14 | E2E 测试 + 文档 | 完整可用工具 |

### 阶段依赖关系

```
P0 → P1 → P2 → P3 → P4 → P5
                        ├──→ P6 → P7
                        ├──→ P8
                        └──→ P9
P5 → P10
P7 → P11
P5 → P12
P3 → P13
P6+P7+P8+P9+P10+P11+P12+P13 → P14
```
