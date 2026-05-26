# go-ansible 教学文档 02：Inventory 系统

> **阶段：** P1 | **设计文档引用：** 第三章 Inventory 系统
>
> 本文档覆盖 go-ansible 中 Inventory 子系统的完整设计——从数据模型到格式解析、变量优先级、主机模式匹配，以及 Go 实现要点。

---

## 目录

1. [Inventory 在 Ansible 中的角色](#1-inventory-在-ansible-中的角色)
2. [数据模型：Host, Group, Inventory](#2-数据模型host-group-inventory)
3. [INI 格式详解](#3-ini-格式详解)
4. [YAML 格式详解](#4-yaml-格式详解)
5. [目录格式：host_vars/ 和 group_vars/](#5-目录格式host_vars-和-group_vars)
6. [变量优先级（22 层）](#6-变量优先级22-层)
7. [主机模式匹配](#7-主机模式匹配)
8. [Dynamic Inventory](#8-dynamic-inventory)
9. [Go 实现要点](#9-go-实现要点)
10. [任务拆解](#10-任务拆解)

---

## 1. Inventory 在 Ansible 中的角色

### 1.1 什么是 Inventory

Inventory 是 Ansible 的"通讯录"——它告诉 Ansible：

- **有哪些主机**（IP 地址、主机名）
- **如何连接它们**（SSH 端口、用户名、密钥路径）
- **它们的属性是什么**（分组关系、业务变量）

没有 Inventory，Ansible 就是一台没有电话簿的电话机——具备拨号能力，却不知道该拨给谁。

### 1.2 三大核心职能

```
Inventory
├── 主机管理   —— 注册、发现、维护所有被管主机
├── 分组       —— 按业务角色/环境/地理位置对主机分类
└── 变量赋值   —— 为主机和组绑定配置变量
```

**主机管理**：Inventory 记录了每一台被管主机的连接信息。你可以手动维护一份静态文件，也可以通过脚本从云平台（AWS EC2、阿里云 ECS）动态拉取。

**分组**：分组是 Ansible 的灵魂能力。一条命令 `go-ansible webservers -m shell -a "nginx -t"` 可以同时作用于所有 web 服务器，而不必逐台敲命令。组可以嵌套——`production` 组可以包含 `webservers` 和 `dbservers` 两个子组。

**变量赋值**：Inventory 不只是主机列表，更是变量的"锚点"。你可以为 `webservers` 组设置 `http_port: 80`，为 `db1` 主机单独设置 `mysql_port: 3306`。这些变量会在后续的 Playbook 渲染和模块执行中被使用。

### 1.3 Inventory 的生命周期

```
                    ┌──────────────┐
                    │  CLI -i 参数  │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │  路径检测     │  文件？目录？
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
     ┌────────▼───┐ ┌──────▼──────┐ ┌──▼──────────┐
     │ 单文件解析  │ │ 目录式加载   │ │ 动态脚本    │
     │ (INI/YAML) │ │ (组合加载)   │ │ (JSON输出)  │
     └────────┬───┘ └──────┬──────┘ └──┬──────────┘
              │            │            │
              └────────────┼────────────┘
                           │
                    ┌──────▼───────┐
                    │  Inventory   │
                    │  内存对象     │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │ 变量合并      │
                    │ group_vars/  │
                    │ host_vars/   │
                    └──────────────┘
```

---

## 2. 数据模型：Host, Group, Inventory

### 2.1 三个核心实体

Inventory 系统由三个相互关联的实体构成：

```
Inventory (顶层容器)
├── Host (主机)
│   ├── Name      string         // 主机名或标识符，如 "web1"
│   ├── Port      int            // SSH 端口，默认 22
│   ├── Variables map[string]any // 主机级变量
│   └── Groups    []*Group       // 该主机所属的所有组（多对多）
│
├── Group (主机组)
│   ├── Name      string         // 组名，如 "webservers"
│   ├── Hosts     map[string]*Host  // 组内主机
│   ├── Children  map[string]*Group  // 子组
│   ├── Variables map[string]any    // 组级变量
│   └── Parent    *Group            // 父组引用（单向）
│
└── InventorySource (来源元数据)
    ├── File      string         // 文件路径
    ├── Format    string         // ini / yaml / dynamic
    └── Parsed    bool           // 是否已解析
```

### 2.2 关系图

```
           ┌──────────────────────────────────────┐
           │              Inventory                │
           │                                      │
           │   all ──────┐                        │
           │   │         │                        │
           │   │    ┌────▼──────┐                 │
           │   │    │ production │                 │
           │   │    │  (Group)   │                 │
           │   │    └────┬───┬──┘                 │
           │   │    ┌────┘   └────┐               │
           │   │ ┌──▼──────┐ ┌────▼──────┐       │
           │   │ │webservers│ │ dbservers │       │
           │   │ │ (Group)  │ │ (Group)   │       │
           │   │ └┬──┬──┬──┘ └──┬──┬─────┘       │
           │   │  │  │  │       │  │              │
           │   │ ┌▼─┐┌▼─┐┌▼─┐ ┌▼─┐┌▼─┐          │
           │   │ │w1││w2││w3│ │d1││d2│ (Hosts)   │
           │   │ └──┘└──┘└──┘ └──┘└──┘           │
           └──────────────────────────────────────┘

关系特征：
- Host ↔ Group：多对多（一台主机可属多个组）
- Group → Group：父子关系（一个组可有多个子组）
- "all" 是隐含的根组，所有主机自动属于 all
```

### 2.3 多对多关系的实际意义

一台主机可以同时属于多个组，这在实际运维中非常常见：

```ini
# 一台机器既是 web 服务器，又属于 staging 环境
[webservers]
web-stg-01

[staging]
web-stg-01

# web-stg-01 同时属于 webservers 和 staging 两个组
```

这意味着当你执行 `go-ansible staging -m ping` 和 `go-ansible webservers -m ping` 时，`web-stg-01` 都会被命中。

### 2.4 all 隐含根组

Ansible 中有一个特殊的隐含组 `all`：

- **所有主机**自动成为 `all` 组的成员
- `all:vars` 中定义的变量对所有主机生效
- `all` 是所有组的隐式父组

```ini
[all:vars]
ansible_user=deploy
ansible_ssh_private_key_file=~/.ssh/deploy_key

# 上面的变量会自动应用到所有主机
```

### 2.5 数据模型的不变量（Invariants）

在 Go 实现中，需要保证以下不变量：

1. **每个 Inventory 必须有 `all` 组**——构造时自动创建
2. **添加主机到 Inventory 时，自动加入 `all` 组**
3. **`Host.Groups` 必须与 `Group.Hosts` 双向一致**
4. **`Group.Parent` 设置后，`Parent.Children` 必须包含该子组**
5. **组名和主机名在 Inventory 内唯一**

---

## 3. INI 格式详解

INI 是 Ansible 最经典的 Inventory 格式，简洁直观。

### 3.1 基本结构

INI Inventory 文件由三种 section 构成：

```ini
# ====== 主机 section：[groupname] ======
[webservers]
web1 ansible_host=192.168.1.10 ansible_port=22
web2 ansible_host=192.168.1.11
web3    # 如果没有 ansible_host，使用主机名作为连接地址

[dbservers]
db1 ansible_host=192.168.1.20
db2 ansible_host=192.168.1.21

# ====== 组变量 section：[groupname:vars] ======
[webservers:vars]
http_port=80
nginx_version=1.24

[dbservers:vars]
mysql_port=3306

# ====== 子组 section：[groupname:children] ======
[production:children]
webservers
dbservers

# ====== 全局变量 ======
[all:vars]
ansible_user=deploy
ansible_ssh_private_key_file=~/.ssh/deploy_key
```

### 3.2 主机行语法

```
hostname [key=value [key=value ...]]
```

主机行由一个主机名开头，后面可以跟零个或多个 `key=value` 对：

```ini
# 最简形式——只有主机名
web1

# 带连接变量
web1 ansible_host=192.168.1.10 ansible_port=2222

# 带业务变量
web1 ansible_host=192.168.1.10 http_port=80 app_env=production

# 多个变量用空格分隔
db1 ansible_host=10.0.1.20 ansible_port=22 ansible_user=admin mysql_role=master
```

**变量名约定**：

| 前缀 | 用途 | 示例 |
|------|------|------|
| `ansible_*` | Ansible 内置连接/控制变量 | `ansible_host`, `ansible_port` |
| 无前缀 | 用户自定义业务变量 | `http_port`, `app_env` |

### 3.3 注释

```ini
# 这是注释（行首 #）
; 这也是注释（行首 ;）
[group]  ; 行内注释不支持
host1    # host1 会被解析（行内 # 不是注释）
```

**关键规则**：INI 格式中，只有行首的 `#` 和 `;` 才是注释。行中间的 `#` 会被当作值的一部分。

### 3.4 三种 Section 的解析优先级

解析顺序影响变量合并结果：

```
1. 先扫描所有 [groupname] section → 建立主机和组的骨架
2. 再处理 [groupname:children]    → 建立父子关系
3. 最后处理 [groupname:vars]      → 设置组变量
```

这意味着即使 `[groupname:vars]` 写在文件最前面，它的变量也会在最后才生效。

### 3.5 值的类型推断

INI parser 需要对值做类型推断：

```ini
port=22           # → int: 22
timeout=10.5      # → float64: 10.5
debug=true        # → bool: true
name=web-server   # → string: "web-server"
quoted="hello world"  # → string: "hello world"（去掉引号）
```

推断顺序：`int` → `float64` → `bool` → `string`（去引号）

### 3.6 常见陷阱

**陷阱 1：端口号被解析为字符串**

```ini
# 错误：某些旧版本 Ansible 把端口当字符串
web1 ansible_port=22    # 如果不注意类型推断，可能变成 string "22"
```

**陷阱 2：主机行的空格**

```ini
# 以下两种写法等效
web1 ansible_host=192.168.1.10 ansible_port=22
web1    ansible_host=192.168.1.10    ansible_port=22
```

**陷阱 3：特殊字符**

```ini
# 值中包含空格时需要引号
web1 greeting="hello world"
# 但引号本身会被保留为值的一部分，除非 parser 主动去掉
```

---

## 4. YAML 格式详解

YAML 格式是 Ansible 2.x 以后推荐的 Inventory 格式，结构更清晰，支持嵌套。

### 4.1 基本结构

```yaml
all:
  vars:
    ansible_user: deploy
    ansible_ssh_private_key_file: ~/.ssh/deploy_key
  children:
    webservers:
      hosts:
        web1:
          ansible_host: 192.168.1.10
          http_port: 80
        web2:
          ansible_host: 192.168.1.11
        web3: {}   # 没有额外变量
      vars:
        nginx_version: "1.24"
    dbservers:
      hosts:
        db1:
          ansible_host: 192.168.1.20
        db2:
          ansible_host: 192.168.1.21
      vars:
        mysql_port: 3306
    production:
      children:
        webservers: {}   # 空值表示引用已定义的组
        dbservers: {}
```

### 4.2 递归节点结构

YAML Inventory 的核心是一个递归结构。每个节点可以包含三个可选键：

```yaml
<group_name>:
  hosts:        # 该组包含的主机
    <host_name>:
      <var>: <value>
  children:     # 该组包含的子组（递归）
    <child_group_name>:
      hosts: ...
      children: ...
      vars: ...
  vars:         # 该组的变量
    <var>: <value>
```

对应的 Go 数据结构：

```go
// yamlNode 是 YAML inventory 的递归节点
type yamlNode struct {
    Hosts    map[string]map[string]any `yaml:"hosts,omitempty"`
    Children map[string]*yamlNode     `yaml:"children,omitempty"`
    Vars     map[string]any           `yaml:"vars,omitempty"`
}
```

### 4.3 YAML 格式 vs INI 格式对比

| 特性 | INI | YAML |
|------|-----|------|
| 可读性 | 简单场景好 | 复杂嵌套好 |
| 嵌套组 | 需要 `[group:children]` | 自然递归 |
| 变量定义 | 分散在多个 section | 集中在节点内 |
| 类型支持 | 需要推断 | 原生支持（int/float/bool/string/list/map） |
| 注释 | `#` 或 `;` | `#` |
| 文件大小 | 通常更小 | 通常更大（缩进） |
| 主机行变量 | `host key=val` 语法 | 嵌套 map |

### 4.4 YAML 特有的注意事项

**空节点用 `{}` 或 null**：

```yaml
production:
  children:
    webservers: {}     # 推荐：明确表示空
    dbservers:         # 也可以：null
```

**YAML 类型自动解析**：

```yaml
web1:
  port: 22           # → int
  timeout: 10.5      # → float64
  debug: true        # → bool
  name: "web1"       # → string
  tags:              # → []any
    - web
    - production
```

这比 INI 的字符串推断更可靠。

---

## 5. 目录格式：host_vars/ 和 group_vars/

### 5.1 目录布局

当 `-i` 参数指向一个目录时，Ansible 会按以下规则加载：

```
inventory/                  # 传给 -i 的目录
├── hosts                   # 主文件（自动检测）
├── hosts.ini               # 或这个
├── hosts.yml               # 或这个
│
├── group_vars/             # 组变量目录
│   ├── all.yml             # all 组的变量
│   ├── webservers.yml      # webservers 组的变量
│   ├── dbservers.yml       # dbservers 组的变量
│   └── production/         # 也可以用目录
│       ├── vars.yml        # 目录内所有 .yml 文件会被合并
│       └── secrets.yml
│
└── host_vars/              # 主机变量目录
    ├── web1.yml            # web1 主机的变量
    ├── web2.yml
    ├── db1.yml
    └── db1/                # 同样支持目录
        ├── config.yml
        └── secrets.yml
```

### 5.2 主文件自动检测

加载器在目录中按以下顺序查找主文件：

```
hosts → hosts.ini → hosts.yml → hosts.yaml
```

如果都不存在，仍然会创建一个空的 Inventory，然后从 `group_vars/` 和 `host_vars/` 中加载变量。

### 5.3 文件命名规则

**group_vars/**：
- 文件名 = 组名 + `.yml`/`.yaml`/`.json`
- `all.yml` → 应用到 `all` 组
- `webservers.yml` → 应用到 `webservers` 组

**host_vars/**：
- 文件名 = 主机名 + `.yml`/`.yaml`/`.json`
- `web1.yml` → 应用到 `web1` 主机

**目录形式**：
- 如果 `group_vars/webservers/` 是一个目录，目录内所有 `.yml`/`.yaml`/`.json` 文件都会被加载并合并

### 5.4 加载顺序与合并方式

```
1. 加载主文件 (hosts/hosts.ini/hosts.yml)
2. 加载 group_vars/ 中的所有文件
   - 按文件名字母序加载
   - 同名变量：后加载的覆盖先加载的
3. 加载 host_vars/ 中的所有文件
   - 同样按字母序
   - 同名变量覆盖
```

**合并规则**：
- dict + dict → 递归合并（深度合并）
- list + list → 后者覆盖前者
- scalar + scalar → 后者覆盖前者

```yaml
# group_vars/webservers.yml
http_port: 80
config:
  max_clients: 100
  timeout: 30

# 如果另一个文件也定义了 config:
config:
  timeout: 60
  retry: 3

# 合并结果：
# config:
#   max_clients: 100    ← 保留
#   timeout: 60         ← 覆盖
#   retry: 3            ← 新增
```

---

## 6. 变量优先级（22 层）

### 6.1 完整优先级列表（从低到高）

Ansible 的变量优先级是其最复杂也最强大的特性之一。以下是完整的 22 层优先级：

```
 1. role defaults                (roles/x/defaults/main.yml)
 2. inventory file vars          ([group:vars] section)
 3. inventory group_vars/        (group_vars/all.yml)
 4. inventory group_vars/        (group_vars/<group>.yml)
 5. inventory host_vars/         (host_vars/<hostname>.yml)
 6. inventory host vars          (host ansible_var=x 行内变量)
 7. play vars                    (playbook vars: 块)
 8. play vars_files              (playbook vars_files:)
 9. play vars_prompt             (playbook vars_prompt: 交互输入)
10. role vars                    (roles/x/vars/main.yml)
11. block vars                  (block: vars:)
12. task vars                   (task 级 vars:)
13. include_vars                (include_vars 模块加载)
14. set_facts / registered vars (set_fact 模块 / register 结果)
15. role parameters             (roles: [{role: x, param: val}])
16. include parameters          (include_role/import_role 的 vars)
17. extra-vars (-e)             (命令行 --extra-vars，最高优先级)
```

### 6.2 每层详解

**Layer 1: role defaults（最低优先级）**

```yaml
# roles/nginx/defaults/main.yml
nginx_port: 80
nginx_user: www-data
```

这是"兜底默认值"——只有当更高优先级都没有定义同名变量时才生效。设计意图是让 Role 的使用者可以轻松覆盖默认配置。

**Layer 2: inventory file vars**

```ini
[webservers:vars]
http_port=8080
```

主文件中通过 `[group:vars]` 定义的组变量。

**Layer 3-4: inventory group_vars/**

```yaml
# group_vars/all.yml          ← Layer 3
ansible_user: deploy

# group_vars/webservers.yml   ← Layer 4
http_port: 80
```

目录式 Inventory 的组变量文件。`all.yml` 的优先级低于具体组名的文件。

**Layer 5: inventory host_vars/**

```yaml
# host_vars/web1.yml
http_port: 9090     # web1 的端口与众不同
```

**Layer 6: inventory host vars**

```ini
[webservers]
web1 ansible_host=192.168.1.10 http_port=3000
```

主机行后面直接跟的变量。

**Layer 7: play vars**

```yaml
- hosts: webservers
  vars:
    http_port: 443
```

**Layer 8: play vars_files**

```yaml
- hosts: webservers
  vars_files:
    - vars/common.yml
    - "vars/{{ ansible_os_family }}.yml"
```

**Layer 9: play vars_prompt**

```yaml
- hosts: webservers
  vars_prompt:
    - name: db_password
      prompt: "Enter database password"
      private: true
```

**Layer 10: role vars**

```yaml
# roles/nginx/vars/main.yml
nginx_config_path: /etc/nginx/nginx.conf
```

注意与 `defaults/main.yml` 的区别：`vars/` 的优先级远高于 `defaults/`。

**Layer 11: block vars**

```yaml
- block:
    - debug:
        msg: "{{ http_port }}"
  vars:
    http_port: 9999
```

**Layer 12: task vars**

```yaml
- name: Use custom port
  shell: "echo {{ http_port }}"
  vars:
    http_port: 7777
```

**Layer 13: include_vars**

```yaml
- name: Load extra vars
  include_vars:
    file: vars/extra.yml
```

**Layer 14: set_fact / registered vars**

```yaml
- name: Set computed fact
  set_fact:
    app_url: "http://{{ ansible_host }}:{{ http_port }}"

- name: Run command
  shell: uptime
  register: uptime_result
# uptime_result.stdout 可以在后续 task 中使用
```

**Layer 15-16: role/include parameters**

```yaml
roles:
  - role: nginx
    nginx_port: 8080   # 作为参数传入，优先级高于 role vars

tasks:
  - include_role:
      name: nginx
    vars:
      nginx_port: 9090  # 同理
```

**Layer 17: extra-vars（最高优先级）**

```bash
go-ansible playbook site.yml -e "http_port=1234"
go-ansible playbook site.yml -e '{"http_port": 1234}'
go-ansible playbook site.yml -e @extra_vars.yml
```

没有任何变量可以覆盖 `extra-vars`。这是"上帝模式"——运维人员的最后手段。

### 6.3 常见陷阱

**陷阱 1：role defaults vs role vars 混淆**

```yaml
# defaults/main.yml — 低优先级，可被覆盖
nginx_port: 80

# vars/main.yml — 高优先级，很难被覆盖
nginx_port: 80  # ← 不要在这里定义同样的变量！
```

**陷阱 2：set_fact 的"粘滞性"**

`set_fact` 设置的变量一旦设定，会在后续所有 task 中持续存在，即使进入了新的 role。这可能导致意外的变量覆盖。

**陷阱 3：group_vars 与 inventory file vars 的优先级**

```ini
# hosts 文件中
[webservers:vars]
http_port=80
```

```yaml
# group_vars/webservers.yml
http_port: 8080
```

`group_vars/webservers.yml`（Layer 4）优先级**高于** `[webservers:vars]`（Layer 2）。最终 `http_port=8080`。

**陷阱 4：变量合并不是替换**

当高优先级和低优先级都定义了一个 dict 类型变量时，会**递归合并**而非整体替换：

```yaml
# 低优先级
config:
  a: 1
  b: 2

# 高优先级
config:
  b: 99
  c: 3

# 结果：config.a=1, config.b=99, config.c=3
# 而不是：config={b:99, c:3}
```

---

## 7. 主机模式匹配

### 7.1 模式语法总览

主机模式（host pattern）用于在命令行和 Playbook 的 `hosts:` 字段中选择目标主机：

| 模式 | 含义 | 示例 | 匹配结果 |
|------|------|------|----------|
| `all` / `*` | 所有主机 | `all` | 所有主机 |
| 组名 | 指定组的所有主机 | `webservers` | web1, web2, web3 |
| `:` | 并集（OR） | `webservers:dbservers` | 两组所有主机 |
| `:&` | 交集（AND） | `webservers:&staging` | 同时在两个组的主机 |
| `:!` | 差集（NOT） | `all:!dbservers` | 除 dbservers 外的所有主机 |
| 通配符 `*`/`?` | 模式匹配 | `web*.example.com` | 匹配的主机名 |
| 索引 `[N]` | 取第 N 台 | `webservers[0]` | webservers 的第一台 |
| 切片 `[N:M]` | 取范围 | `webservers[0:2]` | 前两台 |
| 正则 `~` | 正则匹配 | `~web[0-9]+\.prod` | 匹配正则的主机 |

### 7.2 并集（Union）—— `:`

冒号 `:` 是最基本的组合操作符，取两个集合的并集：

```
webservers:dbservers
→ webservers 的所有主机 ∪ dbservers 的所有主机
```

可以链式使用：

```
webservers:dbservers:monitoring
→ 三个组的所有主机（去重）
```

**注意**：`://` 不会被解析为并集分隔符（因为 URL 中有 `://`）。

### 7.3 交集（Intersection）—— `:&`

`:&` 取两个集合的交集：

```
webservers:&staging
→ 同时属于 webservers 组和 staging 组的主机
```

实际场景：你想只对 staging 环境的 web 服务器执行操作。

### 7.4 差集（Difference）—— `:!`

`:!` 从第一个集合中排除第二个集合的成员：

```
all:!dbservers
→ 所有主机中排除 dbservers 组的主机
```

多级排除：

```
all:!dbservers:!monitoring
→ 排除 dbservers 和 monitoring
```

### 7.5 通配符（Wildcard）—— `*` 和 `?`

通配符用于主机名匹配：

```
web*           → web1, web2, web3, webserver, ...
web?.example   → web1.example, web2.example（? 匹配单字符）
*.prod.example → 所有 .prod.example 结尾的主机
```

通配符匹配的是**主机名**，不是组名。

### 7.6 正则表达式（Regex）—— `~`

以 `~` 开头表示使用正则表达式：

```
~web[0-9]+\.example\.com
→ web1.example.com, web2.example.com, web100.example.com, ...
```

Go 实现中使用 `regexp.Compile` 和 `MatchString`。

### 7.7 索引和切片—— `[N]` 和 `[N:M]`

从组中选取特定位置的主机：

```
webservers[0]     → 第一台（0-based）
webservers[-1]    → 最后一台
webservers[0:3]   → 前三台（左闭右开）
webservers[1:]    → 从第二台开始的所有
webservers[:2]    → 前两台
```

**注意**：索引操作在所有其他操作之后执行。

### 7.8 模式组合与解析优先级

复杂的模式可以组合使用：

```
webservers:dbservers:&production:!staging[0]
```

解析顺序（从左到右）：

```
1. webservers:dbservers     → 并集
2. :&production             → 交集
3. :!staging                → 差集
4. [0]                      → 索引
```

**实现建议**：按 `:!` → `:&` → `:` → `~` → `[` → 通配符 → 组名/主机名 的顺序解析。

---

## 8. Dynamic Inventory

### 8.1 概念

Dynamic Inventory 允许通过可执行脚本动态生成 Inventory 数据。脚本输出 JSON 格式的 Inventory 信息。

### 8.2 脚本约定

Dynamic Inventory 脚本必须：

1. 支持 `--list` 参数——输出所有组和主机
2. 支持 `--host <hostname>` 参数——输出单个主机的变量
3. 输出合法的 JSON
4. 有可执行权限（`chmod +x`）

### 8.3 `--list` 输出格式

```json
{
    "_meta": {
        "hostvars": {
            "web1": {
                "ansible_host": "192.168.1.10",
                "http_port": 80
            },
            "db1": {
                "ansible_host": "192.168.1.20"
            }
        }
    },
    "all": {
        "children": ["webservers", "dbservers"]
    },
    "webservers": {
        "hosts": ["web1", "web2"],
        "vars": {
            "nginx_version": "1.24"
        }
    },
    "dbservers": {
        "hosts": ["db1", "db2"]
    },
    "production": {
        "children": ["webservers", "dbservers"]
    }
}
```

关键字段：
- `_meta.hostvars`：所有主机的变量（可选，避免逐主机查询）
- `<groupname>.hosts`：组内主机列表
- `<groupname>.children`：子组列表
- `<groupname>.vars`：组变量

### 8.4 `--host` 输出格式

```json
{
    "ansible_host": "192.168.1.10",
    "ansible_port": 22,
    "http_port": 80
}
```

如果 `--list` 输出中已包含 `_meta.hostvars`，则不需要调用 `--host`。

### 8.5 与 YAML Inventory 的关系

Dynamic Inventory 的 JSON 结构与 YAML Inventory 几乎一一对应——本质上都是描述同一个递归的组-主机树。

---

## 9. Go 实现要点

### 9.1 核心类型定义

```go
// Host 表示一台被管主机
type Host struct {
    Name      string
    Port      int
    Variables map[string]any
    Groups    []*Group
}

// Group 表示一个主机组
type Group struct {
    Name      string
    Hosts     map[string]*Host
    Children  map[string]*Group
    Variables map[string]any
    Parent    *Group
}

// Inventory 是顶层容器
type Inventory struct {
    Groups map[string]*Group
    Hosts  map[string]*Host
}

// InventorySource 记录来源信息
type InventorySource struct {
    File   string
    Format string // "ini", "yaml", "dynamic"
}
```

### 9.2 Parser 接口

```go
// Parser 定义 Inventory 解析器接口
type Parser interface {
    // Parse 将原始字节数据解析为 Inventory
    Parse(data []byte) (*Inventory, error)

    // Detect 检测数据是否属于该解析器的格式
    Detect(data []byte) bool
}
```

需要两个实现：
- `INIParser` — 解析 INI 格式
- `YAMLParser` 解析 YAML 格式

### 9.3 主机模式匹配函数签名

```go
// MatchPattern 将主机模式解析为匹配的主机列表
func MatchPattern(inv *Inventory, pattern string) []*Host

// resolveHostSet 解析单个模式片段（不含 ::&:!）
func resolveHostSet(inv *Inventory, pattern string) []*Host

// getGroupHosts 获取组内所有主机（含子组递归）
func getGroupHosts(inv *Inventory, groupName string) []*Host

// matchWildcard 使用通配符匹配主机名
func matchWildcard(inv *Inventory, pattern string) []*Host

// matchRegex 使用正则匹配主机名
func matchRegex(inv *Inventory, pattern string) []*Host

// applyIndex 对主机列表应用索引/切片
func applyIndex(hosts []*Host, indexStr string) []*Host

// difference 计算两个主机列表的差集
func difference(a, b []*Host) []*Host

// intersect 计算两个主机列表的交集
func intersect(a, b []*Host) []*Host
```

### 9.4 Loader 函数签名

```go
// Load 从文件或目录路径加载 Inventory
func Load(path string) (*Inventory, error)

// loadFile 加载单个文件
func loadFile(path string) (*Inventory, error)

// loadDirectory 加载目录式 Inventory
func loadDirectory(dir string) (*Inventory, error)

// loadVarsDir 加载 group_vars/ 或 host_vars/ 目录
func loadVarsDir(dir string, apply func(name string, vars map[string]any))
```

### 9.5 构造函数

```go
// New 创建一个包含 all 组的空 Inventory
func New() *Inventory

// NewHost 创建主机，自动解析端口等默认值
func NewHost(name string, vars map[string]any) *Host

// NewGroup 创建空组
func NewGroup(name string) *Group

// NewINIParser 创建 INI 解析器
func NewINIParser() *INIParser

// NewYAMLParser 创建 YAML 解析器
func NewYAMLParser() *YAMLParser
```

### 9.6 关键方法

```go
// Host 方法
func (h *Host) GetVar(key string) any

// Group 方法
func (g *Group) AddHost(h *Host)
func (g *Group) AddChild(child *Group)

// Inventory 方法
func (inv *Inventory) GetGroup(name string) *Group
func (inv *Inventory) GetHost(name string) *Host
func (inv *Inventory) AddGroup(g *Group)
func (inv *Inventory) AddHost(h *Host)
func (inv *Inventory) AllHosts() []*Host
```

### 9.7 不变量保证

以下逻辑需要在 `AddHost`、`AddGroup`、`AddChild` 中维护：

```go
// AddHost 时：
// 1. inv.Hosts[name] = host
// 2. inv.Groups["all"].Hosts[name] = host  (自动加入 all)
// 3. host.Groups 包含 all 组

// AddChild 时：
// 1. parent.Children[child.Name] = child
// 2. child.Parent = parent
```

---

## 10. 任务拆解

### 10.1 T1.1 INI 解析

**目标**：实现 INI 格式 Inventory 文件的完整解析。

**输入**：INI 格式的 `[]byte`

**输出**：`*Inventory` 对象

**子任务**：

1. **数据模型**（`inventory.go`）
   - 定义 `Host`、`Group`、`Inventory` 结构体
   - 实现 `New()`、`NewHost()`、`NewGroup()` 构造函数
   - 实现 `AddHost()`、`AddGroup()`、`AddChild()` 方法
   - 维护双向引用（Host ↔ Group）和 all 组自动归属

2. **INI Parser**（`ini_parser.go`）
   - 实现 `Parser` 接口
   - 解析三种 section：`[group]`、`[group:vars]`、`[group:children]`
   - 解析主机行：主机名 + key=value 变量
   - 值类型推断：int → float64 → bool → string
   - 注释处理：行首 `#` 和 `;`
   - `Detect()` 方法：判断数据是否为 INI 格式

3. **测试**（`ini_parser_test.go`）
   - 基本组解析
   - 主机变量解析
   - 组变量解析
   - 子组关系解析
   - 空输入处理
   - 测试 fixture：`testdata/hosts.ini`

**验收标准**：

```bash
go test ./internal/inventory/ -v -run TestINI
# 所有测试通过
```

### 10.2 T1.2 YAML 解析

**目标**：实现 YAML 格式 Inventory 文件的完整解析。

**输入**：YAML 格式的 `[]byte`

**输出**：`*Inventory` 对象

**子任务**：

1. **YAML Parser**（`yaml_parser.go`）
   - 定义 `yamlNode` 递归结构
   - 解析 `all` 根节点
   - 递归解析 `hosts`、`children`、`vars`
   - 处理 `_meta` 节点（dynamic inventory 兼容）
   - `Detect()` 方法

2. **测试**（`yaml_parser_test.go`）
   - 基本组和主机解析
   - 变量解析
   - 子组递归解析
   - 与 INI 解析器的结果一致性测试
   - 测试 fixture：`testdata/hosts.yml`

**验收标准**：

```bash
go test ./internal/inventory/ -v -run TestYAML
# 所有测试通过
```

### 10.3 T1.3 主机模式匹配

**目标**：实现完整的 Ansible 主机模式匹配引擎。

**输入**：`*Inventory` + 模式字符串

**输出**：匹配的 `[]*Host` 列表

**子任务**：

1. **模式匹配引擎**（`host_pattern.go`）
   - 实现 `MatchPattern()` 入口函数
   - 并集解析（`:`）
   - 交集解析（`:&`）
   - 差集解析（`:!`）
   - 通配符匹配（`*`、`?`）
   - 正则匹配（`~`）
   - 索引/切片（`[N]`、`[N:M]`、`[-1]`）
   - `all` / `*` 特殊处理
   - 组名回退（不是主机名则尝试组名）
   - 结果排序保证确定性

2. **集合操作辅助函数**
   - `difference(a, b)` — 差集
   - `intersect(a, b)` — 交集
   - `applyIndex(hosts, indexStr)` — 索引/切片

3. **测试**（`host_pattern_test.go`）
   - `all` / `*` 匹配
   - 组名匹配
   - 并集 `webservers:dbservers`
   - 交集 `webservers:&staging`
   - 差集 `all:!dbservers`
   - 通配符 `web*`
   - 正则 `~web[0-9]`
   - 索引 `webservers[0]`
   - 切片 `webservers[0:2]`
   - 负索引 `webservers[-1]`
   - 不存在的组/主机返回空列表

**验收标准**：

```bash
go test ./internal/inventory/ -v -run TestMatchPattern
# 所有测试通过
```

### 10.4 T1.4 目录加载与 CLI 集成

**目标**：实现目录式 Inventory 加载和 `inventory` CLI 子命令。

**子任务**：

1. **目录加载器**（`loader.go`）
   - `Load(path)` 自动检测文件/目录
   - 自动检测格式（INI vs YAML）
   - `group_vars/` 和 `host_vars/` 加载
   - 变量深度合并

2. **CLI 集成**（修改 `internal/cli/inventory.go`）
   - `inventory list` — 列出所有组和主机
   - `inventory host <name>` — 显示单个主机变量
   - `inventory graph` — 显示组关系树

**验收标准**：

```bash
go run ./cmd/go-ansible inventory list -i testdata/hosts.ini
go run ./cmd/go-ansible inventory list -i testdata/hosts.yml
go run ./cmd/go-ansible inventory host web1 -i testdata/hosts.ini
go run ./cmd/go-ansible inventory graph -i testdata/hosts.ini
```

---

## 附录 A：Inventory 文件示例

### A.1 最小可用 INI 文件

```ini
[webservers]
web1 ansible_host=192.168.1.10
```

### A.2 生产级 INI 文件

```ini
# ============================================
# Production Inventory
# ============================================

[all:vars]
ansible_user=deploy
ansible_ssh_private_key_file=~/.ssh/deploy_key
ansible_python_interpreter=/usr/bin/python3

[webservers]
web1 ansible_host=10.0.1.10 http_port=80
web2 ansible_host=10.0.1.11 http_port=80
web3 ansible_host=10.0.1.12 http_port=80

[dbservers]
db1 ansible_host=10.0.2.10 mysql_role=master
db2 ansible_host=10.0.2.11 mysql_role=slave

[monitoring]
mon1 ansible_host=10.0.3.10

[staging:children]
webservers

[production:children]
webservers
dbservers
monitoring

[webservers:vars]
nginx_version=1.24
nginx_worker_processes=auto

[dbservers:vars]
mysql_version=8.0
innodb_buffer_pool_size=1G
```

### A.3 等效 YAML 文件

```yaml
all:
  vars:
    ansible_user: deploy
    ansible_ssh_private_key_file: ~/.ssh/deploy_key
    ansible_python_interpreter: /usr/bin/python3
  children:
    webservers:
      hosts:
        web1:
          ansible_host: 10.0.1.10
          http_port: 80
        web2:
          ansible_host: 10.0.1.11
          http_port: 80
        web3:
          ansible_host: 10.0.1.12
          http_port: 80
      vars:
        nginx_version: "1.24"
        nginx_worker_processes: auto
    dbservers:
      hosts:
        db1:
          ansible_host: 10.0.2.10
          mysql_role: master
        db2:
          ansible_host: 10.0.2.11
          mysql_role: slave
      vars:
        mysql_version: "8.0"
        innodb_buffer_pool_size: 1G
    monitoring:
      hosts:
        mon1:
          ansible_host: 10.0.3.10
    staging:
      children:
        webservers: {}
    production:
      children:
        webservers: {}
        dbservers: {}
        monitoring: {}
```

---

## 附录 B：参考资源

- [Ansible 官方文档 - Inventory](https://docs.ansible.com/ansible/latest/inventory_guide/intro_inventory.html)
- [Ansible 官方文档 - 变量优先级](https://docs.ansible.com/ansible/latest/playbook_guide/playbooks_variables.html#understanding-variable-precedence)
- 设计文档：`docs/superpowers/specs/2026-05-25-go-ansible-design.md` 第三章
- 实现计划：`docs/superpowers/plans/2026-05-25-go-ansible-implementation.md` Phase P1
