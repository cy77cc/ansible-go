# ansible-go 教学文档 04：变量系统与模板引擎

> **阶段：** P3 | **设计文档引用：** 第八章 模板引擎、第九章 变量系统
>
> 本文档覆盖 ansible-go 中变量系统的完整设计——22 层优先级、合并规则、Magic Variables、Facts 收集，以及模板引擎的 Go text/template + Sprig 方案。

---

## 目录

1. [变量系统概述](#1-变量系统概述)
2. [变量来源详解（22 层）](#2-变量来源详解22-层)
3. [变量合并规则](#3-变量合并规则)
4. [Magic Variables](#4-magic-variables)
5. [Facts 收集机制](#5-facts-收集机制)
6. [Go text/template 基础](#6-go-texttemplate-基础)
7. [Sprig 函数库](#7-sprig-函数库)
8. [变量前缀预处理](#8-变量前缀预处理)
9. [渲染时机](#9-渲染时机)
10. [Go 实现要点](#10-go-实现要点)
11. [任务拆解](#11-任务拆解)

---

## 1. 变量系统概述

### 1.1 变量是 Ansible 灵活性的骨架

如果把 Ansible 比作一台机器，变量就是它的调节旋钮。通过变量，同一份 Playbook 可以在开发、测试、生产环境中表现不同行为——不需要写三份 Playbook。

```yaml
# 同一份 Playbook，不同环境的不同行为
- hosts: all
  tasks:
    - name: Install nginx
      yum:
        name: "nginx-{{ nginx_version }}"
        state: present

    - name: Configure nginx
      template:
        src: nginx.conf.j2
        dest: /etc/nginx/nginx.conf
      notify: restart nginx
```

`nginx_version` 和 `nginx.conf.j2` 中的端口号、日志路径等，全部由变量控制。在开发环境中 `nginx_version=1.24`，在生产环境中 `nginx_version=1.22`（稳定版）。

### 1.2 变量系统的核心挑战

Ansible 的变量系统是其最复杂的部分之一：

- **来源众多**：22 个不同来源，每层有不同优先级
- **合并复杂**：dict 递归合并、list 覆盖、scalar 覆盖
- **作用域嵌套**：Global → Play → Role → Task → Host
- **并发安全**：多主机并行执行时变量不可变

### 1.3 设计原则

ansible-go 的变量系统遵循三个原则：

1. **不可变（Immutable）**：变量上下文通过深拷贝传递，避免并发竞争
2. **分层覆盖（Layered Override）**：高优先级覆盖低优先级，dict 递归合并
3. **接口驱动（Interface Driven）**：通过 `VariableManager` 接口隔离实现细节

---

## 2. 变量来源详解（22 层）

### 2.1 完整优先级列表（从低到高）

```
 1. role defaults                 (roles/x/defaults/main.yml)
 2. inventory file vars           ([group:vars] section)
 3. inventory group_vars/all      (group_vars/all.yml)
 4. inventory group_vars/<group>  (group_vars/<group>.yml)
 5. inventory host_vars/<host>    (host_vars/<hostname>.yml)
 6. inventory host vars           (host ansible_var=x 行内变量)
 7. play vars                     (playbook vars: 块)
 8. play vars_files               (playbook vars_files: 列表)
 9. play vars_prompt              (playbook vars_prompt: 交互输入)
10. role vars                     (roles/x/vars/main.yml)
11. block vars                    (block: level vars:)
12. task vars                     (task level vars:)
13. include_vars                  (include_vars 模块加载)
14. set_facts / registered vars   (set_fact 模块 / register 结果)
15. role parameters               (roles: [{role: x, param: val}])
16. include parameters            (include_role/import_role 的 vars)
17. extra-vars (-e)               (命令行 --extra-vars，最高优先级)
```

### 2.2 每层详解与示例

**Layer 1: role defaults（最低优先级）**

```yaml
# roles/nginx/defaults/main.yml
nginx_port: 80
nginx_user: www-data
nginx_worker_processes: auto
nginx_log_path: /var/log/nginx
```

角色的默认变量，设计意图是提供"出厂设置"。使用者可以通过更高优先级的变量覆盖任何默认值。

```yaml
# 使用者可以轻松覆盖
- hosts: webservers
  roles:
    - role: nginx
      nginx_port: 8080   # 覆盖默认的 80
```

**Layer 2: inventory file vars**

```ini
# hosts 文件
[webservers:vars]
http_port=8080
nginx_version=1.24

[dbservers:vars]
mysql_port=3306
```

Inventory 主文件中通过 `[group:vars]` section 定义的组变量。

**Layer 3: inventory group_vars/all**

```yaml
# group_vars/all.yml
ansible_user: deploy
ansible_ssh_private_key_file: ~/.ssh/deploy_key
ansible_python_interpreter: /usr/bin/python3
ntp_server: ntp.aliyun.com
```

所有主机共享的全局变量。适用于设置统一的 SSH 用户、NTP 服务器等。

**Layer 4: inventory group_vars/<group>**

```yaml
# group_vars/webservers.yml
http_port: 80
nginx_version: "1.24"
max_clients: 1000

# group_vars/dbservers.yml
mysql_port: 3306
innodb_buffer_pool_size: 2G
```

特定组的变量。每个组可以有独立的变量文件。

**Layer 5: inventory host_vars/<host>**

```yaml
# host_vars/web1.yml
http_port: 9090        # web1 使用非标准端口
mysql_role: master
custom_config:
  max_connections: 500

# host_vars/db1.yml
mysql_role: master
server_id: 1
```

单台主机的专属变量。用于处理"特例"——某台主机需要特殊配置。

**Layer 6: inventory host vars**

```ini
# hosts 文件中的行内变量
[webservers]
web1 ansible_host=192.168.1.10 ansible_port=2222 http_port=3000
web2 ansible_host=192.168.1.11
```

主机名后面直接跟的变量。注意：这些变量同时影响连接行为（`ansible_*`）和业务逻辑。

**Layer 7: play vars**

```yaml
- hosts: webservers
  vars:
    http_port: 443
    ssl_enabled: true
  tasks:
    - debug:
        msg: "Port is {{ http_port }}"
```

Playbook 中 `vars:` 块定义的变量。作用域限定在当前 Play 内。

**Layer 8: play vars_files**

```yaml
- hosts: webservers
  vars_files:
    - vars/common.yml
    - "vars/{{ ansible_os_family }}.yml"
    - "vars/{{ deploy_env }}.yml"
  tasks:
    - debug:
        msg: "{{ app_name }}"
```

从外部文件加载变量。路径支持模板渲染——可以根据环境动态选择文件。

```yaml
# vars/common.yml
app_name: my-application
app_version: "2.0"

# vars/RedHat.yml
package_manager: yum
service_name: httpd

# vars/Debian.yml
package_manager: apt
service_name: apache2
```

**Layer 9: play vars_prompt**

```yaml
- hosts: webservers
  vars_prompt:
    - name: db_password
      prompt: "Enter database password"
      private: true           # 输入时不显示

    - name: deploy_env
      prompt: "Deployment environment"
      default: staging        # 默认值
      private: false
```

交互式提示用户输入变量。适合需要手动确认的敏感操作。

**Layer 10: role vars**

```yaml
# roles/nginx/vars/main.yml
nginx_config_path: /etc/nginx/nginx.conf
nginx_modules:
  - http_ssl_module
  - http_gzip_module
```

角色内部使用的变量。优先级远高于 `defaults/`——设计意图是"内部常量"，不建议使用者覆盖。

**Layer 11: block vars**

```yaml
tasks:
  - block:
      - name: Task 1
        debug:
          msg: "Port is {{ http_port }}"
      - name: Task 2
        shell: "echo {{ http_port }}"
    vars:
      http_port: 9999     # 只在 block 内生效
```

Block 级别的变量，作用域限定在 block 及其 rescue/always 中。

**Layer 12: task vars**

```yaml
- name: Use custom port for this task only
  shell: "curl http://localhost:{{ http_port }}"
  vars:
    http_port: 7777     # 只在当前 task 生效
```

单个 task 级别的变量，作用域最小。

**Layer 13: include_vars**

```yaml
- name: Load environment-specific vars
  include_vars:
    file: "vars/{{ deploy_env }}.yml"
    name: env_config    # 可选：加载到指定变量名下

- name: Load all yml files from directory
  include_vars:
    dir: vars/extra
    extensions:
      - yml
      - yaml
```

在 task 执行过程中动态加载变量文件。与 `vars_files` 不同，它可以有条件地加载。

**Layer 14: set_facts / registered vars**

```yaml
# set_fact：设置计算后的变量
- name: Compute app URL
  set_fact:
    app_url: "http://{{ ansible_host }}:{{ http_port }}/api"
    is_production: "{{ deploy_env == 'production' }}"

# register：捕获 task 执行结果
- name: Get current nginx version
  shell: nginx -v 2>&1
  register: nginx_result

- name: Show version
  debug:
    msg: "Nginx: {{ nginx_result.stdout }}"
```

`set_fact` 和 `register` 的变量在同一优先级层，会覆盖之前的值。

**Layer 15: role parameters**

```yaml
roles:
  - role: nginx
    nginx_port: 8080
    nginx_worker_processes: 4
```

通过参数传入角色，优先级高于角色的 `vars/` 和 `defaults/`。

**Layer 16: include parameters**

```yaml
tasks:
  - include_role:
      name: nginx
    vars:
      nginx_port: 9090
```

通过 `include_role` 或 `import_role` 传入的参数。

**Layer 17: extra-vars（最高优先级）**

```bash
# 命令行传入
ansible-go playbook site.yml -e "http_port=1234"

# JSON 格式
ansible-go playbook site.yml -e '{"http_port": 1234, "debug": true}'

# 从文件加载
ansible-go playbook site.yml -e @extra_vars.yml

# 多个 -e 叠加
ansible-go playbook site.yml -e "a=1" -e "b=2"
```

`extra-vars` 是"上帝模式"——没有任何变量可以覆盖它。这是运维人员在紧急情况下覆盖一切配置的手段。

### 2.3 常见陷阱

**陷阱 1：role defaults 和 role vars 的优先级差距**

```
role defaults  → Layer 1（最低）
role vars      → Layer 10（较高）
```

两者差距很大。`defaults/` 中定义的变量几乎可以被任何来源覆盖；`vars/` 中定义的变量只被 `include_vars`、`set_fact`、`extra-vars` 等少数来源覆盖。

**陷阱 2：set_fact 的"粘滞性"**

```yaml
# 在 role A 中
- set_fact:
    config_mode: strict

# 进入 role B 后，config_mode 仍然是 "strict"
# 因为 set_fact 的优先级（Layer 14）高于 role defaults（Layer 1）
```

**陷阱 3：同层变量的覆盖顺序**

同一优先级层内，后定义的覆盖先定义的：

```yaml
# vars_files 列表中
vars_files:
  - vars/common.yml    # 先加载
  - vars/override.yml  # 后加载，覆盖同名变量
```

---

## 3. 变量合并规则

### 3.1 三种类型的合并策略

当高优先级和低优先级都定义了同名变量时，合并策略取决于变量的类型：

| 类型 | 合并策略 | 说明 |
|------|----------|------|
| dict (map) | 递归合并 | 保留双方的键，冲突时高优先级覆盖 |
| list | 覆盖 | 高优先级整体替换低优先级 |
| scalar | 覆盖 | 高优先级整体替换低优先级 |

### 3.2 Dict 递归合并

这是最复杂也最常用的合并策略：

```yaml
# 低优先级 (Layer 1: role defaults)
config:
  database:
    host: localhost
    port: 3306
    pool_size: 10
  cache:
    enabled: true
    ttl: 300

# 高优先级 (Layer 7: play vars)
config:
  database:
    port: 5432        # 覆盖
    password: secret  # 新增
  logging:
    level: debug      # 新增顶层键

# 合并结果
config:
  database:
    host: localhost       # 保留（低优先级独有）
    port: 5432            # 覆盖（高优先级）
    pool_size: 10         # 保留（低优先级独有）
    password: secret      # 新增（高优先级独有）
  cache:
    enabled: true         # 保留（低优先级独有）
    ttl: 300              # 保留（低优先级独有）
  logging:
    level: debug          # 新增（高优先级独有）
```

**递归意味着嵌套的 dict 也会合并**——不是替换整个 `database` 对象，而是逐键合并。

### 3.3 List 覆盖

```yaml
# 低优先级
nginx_modules:
  - http_ssl_module
  - http_gzip_module

# 高优先级
nginx_modules:
  - http_ssl_module
  - http_gzip_module
  - http_v2_module

# 结果：高优先级的列表整体替换低优先级
nginx_modules:
  - http_ssl_module
  - http_gzip_module
  - http_v2_module
```

**注意**：list 不是追加（append），而是替换（replace）。这是常见的混淆点。

### 3.4 Scalar 覆盖

```yaml
# 低优先级
http_port: 80

# 高优先级
http_port: 8080

# 结果：8080
```

标量（string、int、float、bool）的合并就是简单的覆盖。

### 3.5 不可变合并

每次合并都产生**新的对象**，不修改原有的低优先级变量：

```go
// 伪代码
low := map[string]any{"a": 1, "b": 2}
high := map[string]any{"b": 99, "c": 3}

result := deepMerge(low, high)
// result = {a:1, b:99, c:3}

// low 不变：{a:1, b:2}
// high 不变：{b:99, c:3}
```

这在并发场景中非常重要——多个 goroutine 可以安全地读取同一个低优先级变量，而不担心被其他 goroutine 修改。

### 3.6 DeepMerge 实现要点

```go
// deepMerge 合并两个值：
// - dict + dict → 递归合并
// - 其他类型 → override 覆盖 base
func deepMerge(base, override any) any {
    baseMap, baseIsMap := base.(map[string]any)
    overrideMap, overrideIsMap := override.(map[string]any)

    if baseIsMap && overrideIsMap {
        result := make(map[string]any)
        // 先拷贝 base 的所有键
        for k, v := range baseMap {
            result[k] = v
        }
        // 再合并 override 的键
        for k, v := range overrideMap {
            if existing, ok := result[k]; ok {
                // 递归合并
                result[k] = deepMerge(existing, v)
            } else {
                // 新键
                result[k] = v
            }
        }
        return result
    }

    // 非 dict 类型：override 直接覆盖
    return override
}
```

---

## 4. Magic Variables

### 4.1 什么是 Magic Variables

Magic Variables 是 Ansible 自动注入的内置变量，不需要用户定义。它们提供关于当前执行环境的元信息。

### 4.2 完整列表

| 变量 | 类型 | 说明 |
|------|------|------|
| `inventory_hostname` | string | 当前主机在 inventory 中的名称 |
| `inventory_hostname_short` | string | 主机名的第一段（`.` 之前） |
| `inventory_file` | string | inventory 文件的路径 |
| `inventory_dir` | string | inventory 文件所在目录 |
| `group_names` | list | 当前主机所属的所有组 |
| `groups` | dict | 所有组及组内主机的映射 |
| `hostvars` | dict | 所有主机的变量（可跨主机访问） |
| `ansible_check_mode` | bool | 是否为干跑模式（--check） |
| `ansible_diff` | bool | 是否为 diff 模式（--diff） |
| `ansible_forks` | int | 并发数（--forks） |
| `ansible_play_hosts` | list | 当前 play 中所有未失败的主机 |
| `ansible_play_hosts_all` | list | 当前 play 中所有主机 |
| `playbook_dir` | string | playbook 文件所在目录 |
| `role_name` | string | 当前角色名（仅在 role 内有效） |
| `role_path` | string | 当前角色路径（仅在 role 内有效） |

### 4.3 常用 Magic Variables 示例

**inventory_hostname**：

```yaml
- name: Show hostname
  debug:
    msg: "Running on {{ inventory_hostname }}"
# 输出: Running on web1
```

**group_names**：

```yaml
- name: Show groups
  debug:
    msg: "{{ inventory_hostname }} belongs to {{ group_names }}"
# 输出: web1 belongs to [webservers, production, all]
```

**groups**：

```yaml
- name: List all webservers
  debug:
    msg: "Web servers: {{ groups['webservers'] }}"
# 输出: Web servers: ['web1', 'web2', 'web3']

- name: List all groups
  debug:
    msg: "All groups: {{ groups.keys() | list }}"
```

**hostvars（跨主机访问）**：

```yaml
# 访问 web1 主机的变量（从 db1 上执行时）
- name: Get web1's IP
  debug:
    msg: "Web1 IP: {{ hostvars['web1']['ansible_host'] }}"
```

**ansible_check_mode**：

```yaml
- name: Conditional on check mode
  debug:
    msg: "This is a dry run"
  when: ansible_check_mode

- name: Only execute when not in check mode
  yum:
    name: nginx
    state: present
  when: not ansible_check_mode
```

**playbook_dir**：

```yaml
- name: Load file relative to playbook
  include_vars:
    file: "{{ playbook_dir }}/vars/common.yml"
```

### 4.4 hostvars 的延迟求值

`hostvars` 在 Ansible 中是延迟求值的——只有当你实际访问某台主机的变量时，才会去收集。在 ansible-go 中，由于 Facts 收集是通过 SSH 逐台执行的，`hostvars` 的值在所有主机的 Facts 收集完成后才完整。

---

## 5. Facts 收集机制

### 5.1 什么是 Facts

Facts 是通过 SSH 到远程主机收集的系统信息。它们以 `ansible_*` 变量的形式注入变量上下文。

```yaml
# 在 playbook 中使用 facts
- hosts: all
  gather_facts: true     # 默认就是 true
  tasks:
    - name: Show system info
      debug:
        msg: |
          OS: {{ ansible_distribution }} {{ ansible_distribution_version }}
          Arch: {{ ansible_machine }}
          CPUs: {{ ansible_processor_cores }}
          Memory: {{ ansible_memtotal_mb }} MB
```

### 5.2 收集类别

Facts 按类别组织，可以通过 `gather_subset` 控制收集哪些类别：

| 类别 | 收集内容 | 示例变量 |
|------|----------|----------|
| `hardware` | CPU、内存、架构 | `ansible_processor_cores`, `ansible_memtotal_mb`, `ansible_machine` |
| `network` | IP 地址、接口、网关 | `ansible_all_ipv4_addresses`, `ansible_default_ipv4` |
| `virtual` | 虚拟化类型 | `ansible_virtualization_type` |
| `distribution` | OS 发行版、版本号 | `ansible_distribution`, `ansible_distribution_version`, `ansible_os_family` |
| `user` | 当前用户信息 | `ansible_user_id`, `ansible_user_uid`, `ansible_user_gid` |
| `date_time` | 系统时间 | `ansible_date_time.iso8601` |

```yaml
# 只收集特定类别
- hosts: all
  gather_facts: true
  gather_subset:
    - hardware
    - network
    - "!all"       # 排除所有其他类别
```

### 5.3 收集方式

ansible-go 的 Facts 收集通过 SSH 执行预定义的 shell 命令：

```
收集项                   Shell 命令
─────────────────────────────────────────────────────────────
ansible_hostname         hostname
ansible_fqdn             hostname -f
ansible_machine          uname -m
ansible_system           uname -s
ansible_kernel           uname -r
ansible_os_family        cat /etc/os-release | grep ^ID_LIKE | cut -d= -f2
ansible_distribution     cat /etc/os-release | grep ^ID | head -1 | cut -d= -f2
ansible_distribution_version  cat /etc/os-release | grep ^VERSION_ID | cut -d= -f2
ansible_processor_cores  nproc
ansible_memtotal_mb      free -m | awk '/^Mem:/ {print $2}'
ansible_user_id          whoami
ansible_user_uid         id -u
ansible_user_gid         id -g
ansible_all_ipv4_addresses  ip -4 addr show | grep 'inet ' | awk '{print $2}'
```

### 5.4 Facts 注入变量上下文

```
Play 开始
    │
    ▼
gather_facts: true?
    │
    ├── 是 → SSH 到每台主机执行 setup 模块
    │         │
    │         ▼
    │       收集 stdout → 解析为 key-value
    │         │
    │         ▼
    │       注入 HostContext.Variables（Layer: set_fact 级别）
    │
    └── 否 → 跳过
```

Facts 注入后的变量等效于 `set_fact` 的优先级（Layer 14），可以被 `extra-vars` 覆盖。

### 5.5 禁用 Facts 收集

```yaml
# 如果不需要系统信息，可以禁用以加速执行
- hosts: all
  gather_facts: false
  tasks:
    - name: Quick task
      ping:
```

禁用后，所有 `ansible_*` facts 变量将不可用。

---

## 6. Go text/template 基础

### 6.1 为什么选择 Go text/template

ansible-go 使用 Go 标准库的 `text/template` + Sprig 函数库，替代 Ansible 的 Jinja2。这是经过权衡的技术决策：

| 特性 | Jinja2 (Python) | Go text/template |
|------|-----------------|------------------|
| 语言 | Python | Go（标准库） |
| 依赖 | 需要 Python 运行时 | 无额外依赖 |
| 生态 | 大量过滤器 | Sprig 补充 |
| 语法 | `{{ x \| filter }}` | `{{ .x \| filter }}` |
| 条件 | `{% if x %}` | `{{ if .x }}` |
| 循环 | `{% for i in items %}` | `{{ range .items }}` |
| 兼容性 | Ansible 原生 | 不兼容 Jinja2 |

**关键差异**：ansible-go 不兼容 Jinja2 语法，用户需要使用 Go template 语法。

### 6.2 基本语法对照

| Jinja2 | Go text/template | 说明 |
|--------|------------------|------|
| `{{ foo }}` | `{{ .foo }}` | 变量引用需要 `.` 前缀 |
| `{{ foo \| upper }}` | `{{ .foo \| upper }}` | 过滤器/管道 |
| `{{ foo \| default('bar') }}` | `{{ .foo \| default "bar" }}` | 函数参数用空格分隔 |
| `{{ items \| length }}` | `{{ .items \| len }}` | 函数名不同 |
| `{% if x %}...{% endif %}` | `{{ if .x }}...{{ end }}` | 条件 |
| `{% if x %}A{% else %}B{% endif %}` | `{{ if .x }}A{{ else }}B{{ end }}` | 条件+else |
| `{% for i in items %}{{ i }}{% endfor %}` | `{{ range .items }}{{ . }}{{ end }}` | 循环 |
| `{% for k, v in dict.items() %}` | `{{ range $k, $v := .dict }}` | 字典遍历 |
| `{{ dict.key }}` | `{{ .dict.key }}` | 嵌套访问 |
| `{{ items[0] }}` | `{{ index .items 0 }}` | 索引访问 |

### 6.3 Go template 核心概念

**点（`.`）的含义**：

```go
// . 代表当前作用域的数据
// 在顶层，. 就是传入的 vars map
// 在 range 内部，. 变成当前迭代元素

// 模板：
// {{ .name }}  → 从 vars 中取 name
// {{ range .items }}{{ . }}{{ end }}  → . 变成每个 item
```

**管道（`|`）**：

```go
// 管道将前一个表达式的结果作为下一个函数的最后一个参数
// {{ .name | upper }}  等价于  {{ upper .name }}
// {{ .name | replace "old" "new" }}  等价于  {{ replace "old" "new" .name }}
```

**条件**：

```go
// {{ if .condition }}...{{ end }}
// {{ if .condition }}...{{ else }}...{{ end }}
// {{ if .a }}A{{ else if .b }}B{{ else }}C{{ end }}
```

**循环**：

```go
// {{ range .items }}{{ . }} {{ end }}
// {{ range $index, $element := .items }}[{{ $index }}]={{ $element }} {{ end }}
```

**变量赋值**：

```go
// {{ $var := .value }}
// {{ $var := .items | len }}
```

### 6.4 模板执行

```go
// Render 渲染模板字符串
func Render(tmplStr string, vars map[string]any) (string, error) {
    t, err := template.New("name").Funcs(FuncMap()).Parse(tmplStr)
    if err != nil {
        return "", err
    }

    var buf bytes.Buffer
    err = t.Execute(&buf, vars)
    return buf.String(), err
}
```

---

## 7. Sprig 函数库

### 7.1 什么是 Sprig

Sprig 是 Helm 使用的 Go template 函数库，提供了 70+ 个常用函数。ansible-go 使用 Sprig 来补充 Go 标准库 `text/template` 缺少的常用函数。

```
go get github.com/Masterminds/sprig/v3
```

### 7.2 常用函数分类

**字符串操作**：

| 函数 | Jinja2 等价 | 说明 | 示例 |
|------|-------------|------|------|
| `upper` | `upper` | 转大写 | `{{ .name \| upper }}` |
| `lower` | `lower` | 转小写 | `{{ .name \| lower }}` |
| `title` | `title` | 首字母大写 | `{{ .name \| title }}` |
| `trim` | `trim` | 去首尾空格 | `{{ .name \| trim }}` |
| `replace` | `replace` | 字符串替换 | `{{ .path \| replace "/" "\\" }}` |
| `contains` | `in` | 是否包含 | `{{ contains "foo" .str }}` |
| `hasPrefix` | `startswith` | 前缀匹配 | `{{ hasPrefix "http" .url }}` |
| `hasSuffix` | `endswith` | 后缀匹配 | `{{ hasSuffix ".yml" .file }}` |
| `repeat` | - | 重复字符串 | `{{ "*" \| repeat 3 }}` |
| `substr` | `slice` | 子字符串 | `{{ substr 0 5 .str }}` |
| `trunc` | `truncate` | 截断 | `{{ trunc 10 .str }}` |
| `quote` | - | 双引号包裹 | `{{ .name \| quote }}` |
| `squote` | - | 单引号包裹 | `{{ .name \| squote }}` |

**默认值**：

| 函数 | Jinja2 等价 | 说明 | 示例 |
|------|-------------|------|------|
| `default` | `default` | 设置默认值 | `{{ .name \| default "unknown" }}` |
| `empty` | - | 判断是否为空 | `{{ empty .list }}` |

```yaml
# 常见用法：变量不存在时使用默认值
msg: "Hello, {{ .name | default "World" }}!"
```

**类型转换**：

| 函数 | 说明 | 示例 |
|------|------|------|
| `toString` | 转字符串 | `{{ .port \| toString }}` |
| `toInt` | 转整数 | `{{ .str \| toInt }}` |
| `toJson` | 转 JSON 字符串 | `{{ .data \| toJson }}` |
| `toPrettyJson` | 转美化 JSON | `{{ .data \| toPrettyJson }}` |
| `toRawJson` | 转原始 JSON | `{{ .data \| toRawJson }}` |

```yaml
# 将变量序列化为 JSON
config_json: "{{ .config | toJson }}"
```

**列表操作**：

| 函数 | 说明 | 示例 |
|------|------|------|
| `list` | 创建列表 | `{{ list 1 2 3 }}` |
| `append` | 追加元素 | `{{ .items \| append 4 }}` |
| `prepend` | 前置元素 | `{{ .items \| prepend 0 }}` |
| `uniq` | 去重 | `{{ .items \| uniq }}` |
| `sortAlpha` | 字母排序 | `{{ .items \| sortAlpha }}` |
| `reverse` | 反转 | `{{ .items \| reverse }}` |
| `first` | 取第一个 | `{{ .items \| first }}` |
| `last` | 取最后一个 | `{{ .items \| last }}` |
| `len` | 长度 | `{{ .items \| len }}` |

**字典操作**：

| 函数 | 说明 | 示例 |
|------|------|------|
| `dict` | 创建字典 | `{{ dict "key" "value" }}` |
| `keys` | 获取所有键 | `{{ .data \| keys }}` |
| `values` | 获取所有值 | `{{ .data \| values }}` |
| `hasKey` | 是否有某键 | `{{ hasKey .data "name" }}` |
| `pick` | 选取指定键 | `{{ .data \| pick "a" "b" }}` |
| `omit` | 排除指定键 | `{{ .data \| omit "password" }}` |
| `merge` | 合并字典 | `{{ merge .defaults .overrides }}` |
| `mustMerge` | 合并字典（失败 panic） | `{{ mustMerge .a .b }}` |

**数学运算**：

| 函数 | 说明 | 示例 |
|------|------|------|
| `add` | 加法 | `{{ add .a .b }}` |
| `sub` | 减法 | `{{ sub .a .b }}` |
| `mul` | 乘法 | `{{ mul .a .b }}` |
| `div` | 除法 | `{{ div .a .b }}` |
| `mod` | 取模 | `{{ mod .a .b }}` |
| `max` | 最大值 | `{{ max 1 2 3 }}` |
| `min` | 最小值 | `{{ min 1 2 3 }}` |

**编码/加密**：

| 函数 | 说明 | 示例 |
|------|------|------|
| `b64enc` | Base64 编码 | `{{ .data \| b64enc }}` |
| `b64dec` | Base64 解码 | `{{ .data \| b64dec }}` |
| `sha256sum` | SHA256 哈希 | `{{ .data \| sha256sum }}` |
| `sha1sum` | SHA1 哈希 | `{{ .data \| sha1sum }}` |
| `md5sum` | MD5 哈希 | `{{ .data \| md5sum }}` |

### 7.3 与 Jinja2 过滤器的对照表

| Jinja2 过滤器 | Sprig 函数 | 差异 |
|---------------|------------|------|
| `{{ x \| upper }}` | `{{ .x \| upper }}` | 需要 `.` 前缀 |
| `{{ x \| default(y) }}` | `{{ .x \| default y }}` | 参数用空格而非括号 |
| `{{ x \| length }}` | `{{ .x \| len }}` | 函数名不同 |
| `{{ x \| to_json }}` | `{{ .x \| toJson }}` | 驼峰命名 |
| `{{ x \| regex_replace(a, b) }}` | 自定义实现 | Sprig 无此函数 |
| `{{ x \| ipaddr }}` | 自定义实现 | 需要额外实现 |

### 7.4 需要额外实现的 Ansible 过滤器

Sprig 不包含 Ansible 特有的过滤器，需要在 Phase P13 中额外实现：

```go
// 需要自定义实现的过滤器
var ansibleFilters = template.FuncMap{
    "regex_replace":  regexReplace,   // 正则替换
    "regex_search":   regexSearch,    // 正则搜索
    "regex_findall":  regexFindall,   // 正则查找所有
    "ipaddr":         ipaddr,         // IP 地址操作
    "combine":        combine,        // 深度合并字典
    "flatten":        flatten,        // 展平列表
    "dict2items":     dict2items,     // 字典转列表
    "items2dict":     items2dict,     // 列表转字典
    "json_query":     jsonQuery,      // JMESPath 查询
    "mandatory":      mandatory,      // 变量必须存在
}
```

---

## 8. 变量前缀预处理

### 8.1 问题背景

Ansible 的 Jinja2 使用裸变量引用：

```yaml
# Jinja2（Ansible）
msg: "Hello, {{ name }}!"
```

Go text/template 需要 `.` 前缀：

```yaml
# Go template
msg: "Hello, {{ .name }}!"
```

为了兼容 Ansible 的使用习惯，ansible-go 引擎在渲染前自动将 `{{ name }}` 转换为 `{{ .name }}`。

### 8.2 预处理规则

```go
// preprocess 将裸变量引用转换为带 . 前缀的形式
// 转换规则：
//   {{ foo }}        → {{ .foo }}
//   {{ foo | upper }} → {{ .foo | upper }}
//   {{ .foo }}       → {{ .foo }}（不变）
//   {{ range items }} → {{ range .items }}
//   {{ if x }}       → {{ if .x }}
//   不转换函数调用：{{ upper foo }} → 不变（可能是函数名）
```

### 8.3 边界情况

**情况 1：已有前缀**

```yaml
# 已经有 . 前缀，不转换
{{ .foo }}      → {{ .foo }}      # 不变
{{ .foo.bar }}  → {{ .foo.bar }}  # 不变
```

**情况 2：管道表达式**

```yaml
# 变量名后面有管道符
{{ foo | upper }}  → {{ .foo | upper }}
```

**情况 3：函数调用**

```yaml
# 无法确定是函数还是变量，保守处理
{{ upper foo }}  → 不转换（upper 可能是函数名）
```

**情况 4：range / if 等控制结构**

```yaml
# range 和 if 后面的变量也需要前缀
{{ range items }}   → {{ range .items }}
{{ if condition }}  → {{ if .condition }}
```

**情况 5：赋值语句**

```yaml
# 赋值语句中的变量不转换
{{ $var := .value }}  → {{ $var := .value }}  # 不变
```

### 8.4 实现方案

```go
// 使用正则表达式匹配 {{ ... }} 中的裸变量引用
var templateVarRe = regexp.MustCompile(
    `\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(\||\}\})`,
)

func preprocess(tmpl string) string {
    return templateVarRe.ReplaceAllStringFunc(tmpl, func(match string) string {
        // 如果已经有点前缀，跳过
        if strings.Contains(match, ".") {
            return match
        }
        // 提取变量名，添加 . 前缀
        // ...（详见实现计划 Task 3.2）
    })
}
```

---

## 9. 渲染时机

### 9.1 三个渲染阶段

模板渲染在三个不同时间点发生：

```
阶段 1: Playbook 加载时
    │
    ▼
阶段 2: Task 执行前
    │
    ▼
阶段 3: 模块内部
```

### 9.2 阶段 1：Playbook 加载时

在 Playbook YAML 被加载和解析之后，某些字段立即渲染：

```yaml
# vars_files 路径需要立即渲染
- hosts: webservers
  vars_files:
    - "vars/{{ ansible_os_family }}.yml"    # ← 加载时渲染
    - "vars/{{ deploy_env }}.yml"           # ← 加载时渲染

# hosts 字段需要立即渲染（如果使用变量）
  hosts: "{{ target_group }}"               # ← 加载时渲染
```

**此时可用的变量**：extra-vars、inventory 变量、group_vars、host_vars。

### 9.3 阶段 2：Task 执行前

每个 Task 在执行之前渲染其字段：

```yaml
# task name
- name: "Install {{ package_name }} on {{ inventory_hostname }}"
  # ↑ 执行前渲染

# module args
  yum:
    name: "{{ package_name }}-{{ package_version }}"
    # ↑ 执行前渲染
    state: present

# when 条件
  when: ansible_os_family == "RedHat"
  # ↑ 执行前渲染

# loop 数据
  loop: "{{ packages }}"
  # ↑ 执行前渲染
```

**此时可用的变量**：所有已合并的变量 + Facts + set_fact + registered vars。

### 9.4 阶段 3：模块内部

某些模块在执行过程中会渲染模板：

```yaml
# template 模块
- name: Deploy nginx config
  template:
    src: nginx.conf.j2      # 模板文件路径
    dest: /etc/nginx/nginx.conf
  # 模块内部：
  # 1. 读取 nginx.conf.j2 文件内容
  # 2. 用当前变量渲染模板
  # 3. 将渲染结果写入 dest 路径
```

**此时可用的变量**：所有变量（同阶段 2，加上 task 执行过程中可能产生的新变量）。

### 9.5 渲染时机的影响

理解渲染时机对调试变量问题至关重要：

```yaml
# 问题：为什么 {{ ansible_hostname }} 在 vars_files 中不可用？
- hosts: webservers
  vars_files:
    - "vars/{{ ansible_hostname }}.yml"
    # ↑ 此时 Facts 还未收集，ansible_hostname 不可用！
    # 会报错：template: undefined variable

# 解决：使用 inventory_hostname（加载时就可用）
  vars_files:
    - "vars/{{ inventory_hostname }}.yml"
```

---

## 10. Go 实现要点

### 10.1 VarContext 类型签名

```go
// Context 持有变量的分层作用域链
// Context 是不可变的——Set 创建新条目，Child 创建子作用域
type Context struct {
    vars   map[string]any
    parent *Context
    mu     sync.RWMutex
}

// NewContext 创建根上下文
func NewContext() *Context

// Child 创建子上下文（继承父上下文）
func (c *Context) Child() *Context

// Set 设置变量（写入当前层）
func (c *Context) Set(key string, value any)

// Get 获取变量（沿作用域链查找）
func (c *Context) Get(key string) any

// GetAll 合并所有层的变量到一个 map（子层覆盖父层）
func (c *Context) GetAll() map[string]any

// ToMap 同 GetAll
func (c *Context) ToMap() map[string]any
```

### 10.2 VariableManager 接口

```go
// VariableManager 管理变量的加载、合并和查询
type VariableManager interface {
    // LoadRoleDefaults 加载角色默认变量
    LoadRoleDefaults(rolePath string) error

    // LoadInventoryVars 加载 inventory 变量
    LoadInventoryVars(inv *inventory.Inventory) error

    // LoadPlayVars 加载 play 级变量
    LoadPlayVars(vars map[string]any, varsFiles []string) error

    // LoadTaskVars 加载 task 级变量
    LoadTaskVars(vars map[string]any) error

    // SetFact 设置 fact 变量
    SetFact(key string, value any)

    // RegisterVar 注册 task 执行结果
    RegisterVar(name string, result any)

    // GetContext 获取当前合并后的变量上下文
    GetContext() *Context

    // MergeWithExtraVars 合并 extra-vars（最高优先级）
    MergeWithExtraVars(extraVars map[string]any)

    // SetMagicVariables 设置 Magic Variables
    SetMagicVariables(host *inventory.Host, inv *inventory.Inventory)

    // InjectFacts 注入 Facts
    InjectFacts(facts map[string]any)
}
```

### 10.3 DeepCopyMap 签名

```go
// DeepCopyMap 深拷贝一个 map[string]any
// 递归拷贝所有嵌套的 map 和 slice
func DeepCopyMap(src map[string]any) map[string]any

// DeepCopyValue 深拷贝任意值（递归处理 map/slice/基本类型）
func DeepCopyValue(src any) any
```

### 10.4 模板引擎签名

```go
// FuncMap 返回 Sprig + 自定义函数的合并函数映射
func FuncMap() template.FuncMap

// Render 渲染模板字符串
func Render(tmplStr string, vars map[string]any) (string, error)

// Evaluate 评估布尔表达式（用于 when 条件）
func Evaluate(expr string, vars map[string]any) (bool, error)

// preprocess 添加点前缀到裸变量引用
func preprocess(tmpl string) string
```

### 10.5 关键设计决策

**不可变性**：

```go
// 每次合并都产生新对象，不修改原有的
func (c *Context) Child() *Context {
    return &Context{
        vars:   make(map[string]any),  // 新的 vars map
        parent: c,                      // 指向父上下文
    }
}

// GetAll 也是返回新 map
func (c *Context) GetAll() map[string]any {
    result := make(map[string]any)  // 新 map
    // ... 合并逻辑
    return result
}
```

**并发安全**：

```go
// 使用 RWMutex 保护读写
func (c *Context) Get(key string) any {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // ... 读取逻辑
}

func (c *Context) Set(key string, value any) {
    c.mu.Lock()
    defer c.mu.Unlock()
    // ... 写入逻辑
}
```

### 10.6 作用域链的执行流程

```
执行一个 Task 时的变量上下文构建过程：

1. 创建 GlobalContext（extra-vars, config）
     │
     │  LoadInventoryVars()
     ▼
2. 创建 PlayContext（play vars, vars_files）
     │
     │  LoadRoleDefaults() + LoadRoleVars()
     ▼
3. 创建 RoleContext（如果在 role 内）
     │
     │  InjectFacts()
     ▼
4. 创建 TaskContext（task vars, include_vars, set_fact）
     │
     │  SetMagicVariables()
     ▼
5. 创建 HostContext（per-host 变量）
     │
     ▼
6. 渲染模板：template.Render(tmpl, hostContext.ToMap())
```

每次创建子上下文都是深拷贝——父上下文不受影响。

---

## 11. 任务拆解

### 11.1 T3.1 变量上下文与优先级合并

**目标**：实现变量的分层作用域链、优先级合并和不可变传递。

**子任务**：

1. **Context 实现**（`variables/context.go`）
   - `NewContext()` 构造函数
   - `Child()` 子上下文创建
   - `Set()` / `Get()` 变量读写
   - `GetAll()` / `ToMap()` 全量合并
   - `sync.RWMutex` 并发保护

2. **DeepMerge 实现**（`variables/merge.go`）
   - dict + dict 递归合并
   - list + list 覆盖
   - scalar + scalar 覆盖
   - 不可变：每次合并返回新对象

3. **DeepCopyMap 实现**（`variables/copy.go`）
   - `map[string]any` 深拷贝
   - 递归处理嵌套 map 和 slice
   - 基本类型直接赋值

4. **VariableManager 实现**（`variables/manager.go`）
   - 按 22 层优先级加载变量
   - `LoadInventoryVars()` — 从 Inventory 对象加载
   - `LoadPlayVars()` — 从 Play 配置加载
   - `SetFact()` / `RegisterVar()` — 运行时设置
   - `MergeWithExtraVars()` — 最终合并
   - `SetMagicVariables()` — 注入 Magic Variables
   - `InjectFacts()` — 注入 Facts

5. **测试**（`variables/context_test.go`、`variables/merge_test.go`）
   - 基本 Set/Get
   - 子上下文覆盖父上下文
   - dict 递归合并
   - list 覆盖
   - 不可变性验证
   - 并发安全测试

**验收标准**：

```bash
go test ./internal/variables/ -v -race
# 所有测试通过，无竞态条件
```

### 11.2 T3.2 模板引擎

**目标**：实现模板渲染引擎，支持变量前缀预处理和 Sprig 函数。

**子任务**：

1. **模板引擎**（`template/engine.go`）
   - `FuncMap()` — 注册 Sprig 函数
   - `Render()` — 渲染模板字符串
   - `Evaluate()` — 评估布尔表达式
   - `preprocess()` — 变量前缀预处理

2. **变量前缀预处理**（`template/preprocess.go`）
   - 正则匹配裸变量引用
   - 处理已有前缀的情况
   - 处理管道表达式
   - 处理 range/if 控制结构
   - 不转换函数调用和赋值语句

3. **测试**（`template/engine_test.go`、`template/preprocess_test.go`）
   - 基本变量渲染
   - Sprig 函数（upper、default、len、toJson）
   - 条件渲染（if/else）
   - 循环渲染（range）
   - 管道表达式
   - 前缀预处理的各种边界情况
   - 布尔表达式评估

**验收标准**：

```bash
go test ./internal/template/ -v
# 所有测试通过
```

---

## 附录 A：变量调试技巧

### A.1 查看所有变量

```yaml
# 在 task 中打印所有变量
- name: Debug all variables
  debug:
    var: hostvars[inventory_hostname]
    verbosity: 1   # 只在 -v 模式下显示
```

### A.2 查看特定变量来源

```bash
# 使用 ansible-go 的 verbose 模式
ansible-go playbook site.yml -vvv
# -v:   task 结果
# -vv:  变量值、模板渲染
# -vvv: 完整调试信息
```

### A.3 常见变量问题排查

| 症状 | 可能原因 | 排查方法 |
|------|----------|----------|
| 变量未定义 | 来源层不正确 | 检查变量优先级 |
| 变量值意外 | 高优先级覆盖 | 用 `-vv` 查看来源 |
| dict 合并不完整 | list 被覆盖而非合并 | 确认类型是否为 list |
| Facts 变量不可用 | `gather_facts: false` | 检查 play 设置 |
| `vars_files` 路径报错 | 路径中用了 Facts 变量 | 改用 inventory 变量 |

---

## 附录 B：Go text/template 速查表

### B.1 基本输出

```go
{{ .Variable }}
{{ .Obj.Field }}
{{ .Map.key }}
{{ index .Slice 0 }}
{{ .Obj.Nested.Field }}
```

### B.2 条件

```go
{{ if .condition }}
  ...
{{ else if .other }}
  ...
{{ else }}
  ...
{{ end }}
```

### B.3 循环

```go
{{ range .items }}
  {{ . }}
{{ end }}

{{ range $i, $item := .items }}
  [{{ $i }}] {{ $item }}
{{ end }}

{{ range .dict }}
  Key={{ . }}   // 只能访问 value
{{ end }}

{{ range $k, $v := .dict }}
  {{ $k }}={{ $v }}
{{ end }}
```

### B.4 变量

```go
{{ $var := .value }}
{{ $var := .items | len }}
{{ $var }}
```

### B.5 管道

```go
{{ .name | upper }}
{{ .name | default "unknown" | upper }}
{{ .items | len }}
{{ .data | toJson }}
```

### B.6 函数调用

```go
{{ funcName arg1 arg2 }}
{{ .value | funcName arg1 }}
```

---

## 附录 C：参考资源

- [Go text/template 文档](https://pkg.go.dev/text/template)
- [Sprig 函数库文档](https://masterminds.github.io/sprig/)
- [Ansible 官方文档 - 变量](https://docs.ansible.com/ansible/latest/playbook_guide/playbooks_variables.html)
- [Ansible 官方文档 - Facts](https://docs.ansible.com/ansible/latest/playbook_guide/playbooks_vars_facts.html)
- 设计文档：`docs/superpowers/specs/2026-05-25-ansible-go-design.md` 第八章、第九章
- 实现计划：`docs/superpowers/plans/2026-05-25-ansible-go-implementation.md` Phase P3
