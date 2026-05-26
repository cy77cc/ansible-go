# Roles 系统

> 阶段：P7 | 设计文档引用：第十章

Role 是 Ansible 实现可复用配置的基本单元。它将 tasks、handlers、variables、files、
templates 按照约定的目录结构组织在一起，形成一个自包含的、可共享的配置包。本章讲解
Role 的设计理念、目录结构、变量机制、依赖系统、以及 Go 实现方案。

---

## 1. Roles 设计理念

### 1.1 可复用性

没有 Role 时，配置逻辑散落在 Playbook 中，难以在不同项目间复用：

```yaml
# 不用 Role：nginx 配置逻辑散落在 tasks 中
- name: Configure nginx
  hosts: webservers
  tasks:
    - name: Install nginx
      yum: name=nginx state=present
    - name: Copy config
      template: src=nginx.conf.j2 dest=/etc/nginx/nginx.conf
    - name: Start nginx
      service: name=nginx state=started
```

使用 Role 后，nginx 的全部配置逻辑封装为一个独立单元：

```yaml
# 用 Role：一行引用，逻辑在 roles/nginx/ 中
- name: Configure nginx
  hosts: webservers
  roles:
    - nginx
```

Role 可以跨项目、跨团队复用。一个写好的 `nginx` Role 可以在所有需要 nginx 的
Playbook 中直接引用。

### 1.2 封装

Role 将相关资源组织在一起，形成清晰的边界：

```
roles/nginx/
├── tasks/main.yml         # 做什么
├── handlers/main.yml      # 变更时触发什么
├── defaults/main.yml      # 默认配置
├── vars/main.yml          # 内部变量
├── templates/             # 模板文件
├── files/                 # 静态文件
└── meta/main.yml          # 元数据（依赖等）
```

使用者只需关注 Role 的接口（defaults 中的变量），无需了解内部实现。

### 1.3 约定优于配置

Role 的目录结构是固定的约定。只要按约定放置文件，Ansible 就能自动发现和加载：

| 目录 | 自动加载 | 说明 |
|------|----------|------|
| `tasks/main.yml` | 是 | 默认任务入口 |
| `handlers/main.yml` | 是 | 处理器 |
| `defaults/main.yml` | 是 | 默认变量 |
| `vars/main.yml` | 是 | 角色变量 |
| `templates/` | 按需 | 模板文件（template 模块自动查找） |
| `files/` | 按需 | 静态文件（copy 模块自动查找） |
| `meta/main.yml` | 是 | 元数据和依赖 |

---

## 2. 目录结构详解

### 2.1 完整目录结构

```
roles/nginx/
├── defaults/
│   └── main.yml          # 默认变量（最低优先级）
├── vars/
│   └── main.yml          # 角色变量（高优先级）
├── tasks/
│   ├── main.yml          # 主任务入口
│   ├── install.yml       # 安装任务
│   ├── configure.yml     # 配置任务
│   └── service.yml       # 服务管理任务
├── handlers/
│   └── main.yml          # 处理器（如 restart nginx）
├── templates/
│   ├── nginx.conf.j2     # Nginx 配置模板
│   └── vhost.conf.j2     # 虚拟主机模板
├── files/
│   ├── index.html        # 默认首页
│   └── ssl/              # SSL 证书
│       ├── cert.pem
│       └── key.pem
├── meta/
│   └── main.yml          # 元数据（依赖、平台支持）
├── library/              # 自定义模块（可选）
│   └── custom_module.py
├── module_utils/         # 模块工具库（可选）
│   └── utils.py
├── lookup_plugins/       # 查找插件（可选）
│   └── custom_lookup.py
└── tests/
    ├── inventory         # 测试用 inventory
    └── test.yml          # 测试 playbook
```

### 2.2 defaults/main.yml

默认变量，是 Role 的"用户接口"——使用者可以覆盖这些变量来自定义 Role 行为。

```yaml
# roles/nginx/defaults/main.yml

# Nginx 监听端口
nginx_port: 80

# 工作进程数（auto = CPU 核心数）
nginx_worker_processes: auto

# 最大连接数
nginx_worker_connections: 1024

# 是否启用 SSL
nginx_ssl_enabled: false

# SSL 证书路径
nginx_ssl_cert_path: /etc/nginx/ssl/cert.pem
nginx_ssl_key_path: /etc/nginx/ssl/key.pem

# 日志格式
nginx_log_format: combined

# 虚拟主机列表
nginx_vhosts: []
```

**优先级：最低。** 任何地方定义的同名变量都会覆盖 defaults 中的值。

### 2.3 vars/main.yml

角色内部变量，优先级高于 defaults，用于定义 Role 内部使用的固定值。

```yaml
# roles/nginx/vars/main.yml

# Nginx 包名（不同发行版可能不同）
nginx_package_name: nginx

# 配置文件路径
nginx_config_path: /etc/nginx/nginx.conf
nginx_vhost_dir: /etc/nginx/conf.d

# 服务名
nginx_service_name: nginx

# 用户和组
nginx_user: nginx
nginx_group: nginx

# 日志目录
nginx_log_dir: /var/log/nginx
```

**优先级：高。** 高于 play vars，但低于 task vars 和 extra-vars。

### 2.4 tasks/main.yml

任务入口文件，可以是完整的任务列表，也可以引用其他任务文件：

```yaml
# roles/nginx/tasks/main.yml

# 方式一：直接定义任务
- name: Install nginx
  yum:
    name: "{{ nginx_package_name }}"
    state: present

- name: Copy nginx config
  template:
    src: nginx.conf.j2
    dest: "{{ nginx_config_path }}"
    owner: root
    group: root
    mode: "0644"
  notify: restart nginx

- name: Ensure nginx is started
  service:
    name: "{{ nginx_service_name }}"
    state: started
    enabled: true
```

```yaml
# 方式二：拆分为多个文件
- name: Install nginx
  include_tasks: install.yml

- name: Configure nginx
  include_tasks: configure.yml

- name: Manage nginx service
  include_tasks: service.yml
```

### 2.5 handlers/main.yml

处理器列表，只在被 notify 触发时执行：

```yaml
# roles/nginx/handlers/main.yml

- name: restart nginx
  service:
    name: "{{ nginx_service_name }}"
    state: restarted

- name: reload nginx
  service:
    name: "{{ nginx_service_name }}"
    state: reloaded

- name: validate nginx config
  command: nginx -t
  changed_when: false
```

### 2.6 templates/

模板文件目录。template 模块会自动在此目录中查找模板：

```yaml
# template 模块查找顺序:
# 1. roles/<role>/templates/<src>
# 2. <playbook_dir>/templates/<src>
# 3. <absolute_path>/<src>

- name: Deploy config
  template:
    src: nginx.conf.j2      # 自动在 roles/nginx/templates/ 中查找
    dest: /etc/nginx/nginx.conf
```

### 2.7 files/

静态文件目录。copy 模块会自动在此目录中查找文件：

```yaml
- name: Copy static file
  copy:
    src: index.html          # 自动在 roles/nginx/files/ 中查找
    dest: /var/www/html/index.html
```

### 2.8 meta/main.yml

元数据文件，定义 Role 的依赖、平台支持等信息：

```yaml
# roles/nginx/meta/main.yml

galaxy_info:
  author: Your Name
  description: Nginx installation and configuration
  license: MIT
  min_ansible_version: "2.9"
  platforms:
    - name: EL
      versions:
        - 7
        - 8
    - name: Ubuntu
      versions:
        - focal
        - jammy

dependencies:
  - role: common
  - role: ssl
    vars:
      ssl_cert_path: "{{ nginx_ssl_cert_path }}"
    when: nginx_ssl_enabled
```

---

## 3. 变量优先级

### 3.1 Role 变量的两个层级

Role 有两个存放变量的目录，优先级截然不同：

```
优先级（从低到高）:

  ...
  1. role defaults      (roles/x/defaults/main.yml)   ← 最低
  ...
  9. role vars           (roles/x/vars/main.yml)       ← 较高
  ...
  14. role parameters    (roles: [{ role: x, param: v }]) ← 很高
  ...
  16. extra-vars         (-e)                           ← 最高
```

**设计意图：**

| 目录 | 用途 | 谁来修改 |
|------|------|----------|
| `defaults/` | 暴露给使用者的"旋钮" | 使用者覆盖 |
| `vars/` | Role 内部固定值 | Role 作者维护 |

### 3.2 覆盖示例

```yaml
# roles/nginx/defaults/main.yml
nginx_port: 80          # 默认值

# Playbook 中覆盖
- name: Use nginx on port 8080
  hosts: webservers
  roles:
    - role: nginx
      nginx_port: 8080   # role parameters 覆盖 defaults

# 或通过 vars 覆盖
- name: Use nginx on port 8080
  hosts: webservers
  vars:
    nginx_port: 8080     # play vars 覆盖 defaults
  roles:
    - nginx

# 或通过 extra-vars 覆盖（最高优先级）
# go-ansible playbook site.yml -e "nginx_port=8080"
```

### 3.3 Role Parameters

Role parameters 是在引用 Role 时传入的变量，优先级很高：

```yaml
roles:
  - role: nginx
    nginx_port: 8080
    nginx_worker_processes: 4
    tags: [web]
    when: install_nginx | default(true)
```

等价于在 Role 执行前 `set_fact`：
```yaml
- set_fact:
    nginx_port: 8080
    nginx_worker_processes: 4
```

---

## 4. 依赖机制

### 4.1 meta/main.yml dependencies

Role 的依赖在 `meta/main.yml` 中声明：

```yaml
# roles/nginx/meta/main.yml
dependencies:
  - role: common
  - role: ssl
    vars:
      ssl_cert_path: /etc/nginx/ssl
    when: nginx_ssl_enabled
```

**执行规则：**
- 依赖 Role 在当前 Role **之前**执行
- 同一个 Role 只执行一次（即使被多个 Role 依赖）
- 循环依赖会被检测并报错

### 4.2 拓扑排序

当多个 Role 之间存在依赖关系时，引擎需要进行拓扑排序来确定执行顺序：

```
假设依赖关系:
  nginx → common, ssl
  app → common, nginx

依赖图:
  common ←── nginx ←── app
                ↑
               ssl

拓扑排序结果:
  common → ssl → nginx → app
```

**拓扑排序算法：**

```
输入: Role 列表 + 每个 Role 的依赖列表

步骤:
1. 构建邻接表（依赖图）
2. 计算每个节点的入度
3. 将入度为 0 的节点加入队列
4. 取出队首节点，加入结果列表
5. 将其所有邻居的入度减 1
6. 如果邻居入度变为 0，加入队列
7. 重复 4-6 直到队列为空
8. 如果结果列表长度 < 节点总数 → 存在循环依赖，报错

输出: 有序的 Role 列表
```

### 4.3 循环依赖检测

```yaml
# 循环依赖示例
# roles/a/meta/main.yml
dependencies:
  - role: b

# roles/b/meta/main.yml
dependencies:
  - role: a

# 拓扑排序时检测到：a 和 b 的入度永远不为 0
# 报错: "Circular dependency detected: a -> b -> a"
```

### 4.4 依赖合并

同一个 Role 被多个 Role 依赖时，只执行一次，变量取最后一次传入的值：

```yaml
# Role A 依赖 common（传入 x=1）
# Role B 依赖 common（传入 x=2）

# 执行顺序: common(x=2) → A → B
# common 只执行一次，x 取 2（最后一次的值）
```

---

## 5. 引用方式

### 5.1 roles 指令（Play 级）

在 Play 的 `roles` 字段中引用，这是最常见的方式：

```yaml
# 简写
roles:
  - common
  - nginx

# 带参数
roles:
  - role: nginx
    nginx_port: 8080

# 带条件
roles:
  - role: nginx
    when: install_nginx | default(true)

# 带标签
roles:
  - role: nginx
    tags: [web, nginx]
```

**特点：** 静态引用，在 Playbook 解析阶段就确定 Role 列表。

### 5.2 import_role（静态导入）

在 tasks 中静态导入 Role：

```yaml
tasks:
  - name: Import nginx role
    import_role:
      name: nginx
    vars:
      nginx_port: 8080
```

**特点：**
- **静态**：在解析阶段展开，Role 的 tasks 被"内联"到当前 task 列表
- tags、when 等属性会传播到 Role 内的所有 task
- 可以放在 tasks 的任何位置

### 5.3 include_role（动态包含）

在 tasks 中动态包含 Role：

```yaml
tasks:
  - name: Include nginx role
    include_role:
      name: nginx
    vars:
      nginx_port: 8080
```

**特点：**
- **动态**：在执行阶段才加载 Role
- tags、when 等属性只应用于 include_role task 本身，不传播到 Role 内部
- 可以在循环中包含（但同一个 Role 仍只执行一次依赖）

### 5.4 import_role vs include_role 对比

| 特性 | import_role | include_role |
|------|-------------|--------------|
| 加载时机 | 解析阶段（静态） | 执行阶段（动态） |
| tags 传播 | 是 | 否 |
| when 传播 | 是 | 否 |
| 循环支持 | 否 | 是 |
| 性能 | 略优（预加载） | 略慢（运行时加载） |
| 适用场景 | 固定的 Role 引用 | 条件化/循环化的 Role 引用 |

---

## 6. 执行顺序

### 6.1 完整执行顺序

一个 Play 的执行顺序严格按以下顺序进行：

```
1. pre_tasks          ← 最先执行
      ↓
2. 触发 pre_tasks 产生的 handlers
      ↓
3. roles              ← 按依赖顺序执行
      ↓
4. tasks              ← 主任务
      ↓
5. post_tasks         ← 最后执行
      ↓
6. 触发所有 pending handlers
```

### 6.2 执行顺序示例

```yaml
- name: Full lifecycle example
  hosts: webservers
  become: true

  pre_tasks:
    - name: Pre-task 1
      debug: msg="I run first"
      notify: pre handler

  roles:
    - common
    - nginx

  tasks:
    - name: Main task 1
      debug: msg="I run after roles"
      notify: main handler

  post_tasks:
    - name: Post-task 1
      debug: msg="I run last"
      notify: post handler

  handlers:
    - name: pre handler
      debug: msg="pre handler triggered"
    - name: main handler
      debug: msg="main handler triggered"
    - name: post handler
      debug: msg="post handler triggered"
```

实际执行顺序：

```
1. Pre-task 1
2. pre handler（如果 Pre-task 1 changed）
3. common role tasks
4. nginx role tasks
5. Main task 1
6. Post-task 1
7. 所有 pending handlers: main handler, post handler
```

### 6.3 为什么 pre_tasks 在 roles 之前

这个设计让使用者可以在 Role 执行前做一些准备工作：

```yaml
pre_tasks:
  - name: Verify target host is reachable
    ping:

  - name: Check prerequisites
    assert:
      that:
        - ansible_os_family == "RedHat"
        - ansible_distribution_major_version | int >= 7
      fail_msg: "This role requires CentOS 7+"

roles:
  - nginx    # 只有通过前置检查才会执行
```

---

## 7. Galaxy 安装的角色

### 7.1 搜索路径

Ansible 在以下路径中搜索 Role：

```
1. playbook_dir/roles/                  # Playbook 同级目录
2. ~/.ansible/roles/                    # 用户级目录
3. /etc/ansible/roles/                  # 系统级目录
4. ansible.cfg 中配置的 roles_path      # 自定义路径
```

`roles_path` 可以配置多个搜索路径：

```ini
# ansible.cfg
[defaults]
roles_path = ./roles:~/.ansible/roles:/shared/roles
```

### 7.2 命名约定

Galaxy Role 使用 `namespace.rolename` 格式：

```bash
# 安装 Galaxy Role
go-ansible galaxy install geerlingguy.nginx

# 安装到指定目录
go-ansible galaxy install geerlingguy.nginx -p ./roles

# 指定版本
go-ansible galaxy install geerlingguy.nginx,3.1.0
```

安装后的目录结构：

```
roles/
└── geerlingguy.nginx/        # namespace.rolename 格式
    ├── defaults/
    ├── tasks/
    ├── handlers/
    ├── templates/
    ├── meta/
    └── ...
```

### 7.3 版本固定

通过 `requirements.yml` 固定 Role 版本：

```yaml
# requirements.yml
roles:
  - name: geerlingguy.nginx
    version: "3.1.0"
  - name: geerlingguy.mysql
    version: "4.0.0"
    src: https://github.com/geerlingguy/ansible-role-mysql
```

```bash
# 批量安装
go-ansible galaxy install -r requirements.yml
```

### 7.4 Role 引用名称解析

当引用一个 Role 时，引擎按以下顺序查找：

```
引用: nginx

查找顺序:
1. roles/nginx/                          # Playbook 相对路径
2. ~/.ansible/roles/nginx/               # 用户目录
3. /etc/ansible/roles/nginx/             # 系统目录
4. roles_path 中的每个路径/nginx/        # 配置路径

如果引用: geerlingguy.nginx
1. roles/geerlingguy.nginx/
2. ~/.ansible/roles/geerlingguy.nginx/
3. ...
```

---

## 8. Go 实现要点

### 8.1 Role 数据结构

```go
// pkg/role/role.go

// Role 表示一个 Ansible Role。
type Role struct {
    // Name Role 名称。
    Name string

    // Path Role 在磁盘上的完整路径。
    Path string

    // Defaults 默认变量（roles/x/defaults/main.yml）。
    Defaults map[string]any

    // Vars 角色变量（roles/x/vars/main.yml）。
    Vars map[string]any

    // Tasks 主任务列表（roles/x/tasks/main.yml）。
    Tasks []Task

    // Handlers 处理器列表（roles/x/handlers/main.yml）。
    Handlers []Task

    // Dependencies 依赖的 Role 列表（roles/x/meta/main.yml）。
    Dependencies []RoleDependency

    // Templates 模板文件目录路径。
    TemplatesDir string

    // Files 静态文件目录路径。
    FilesDir string

    // Meta 元数据（Galaxy 信息、平台支持等）。
    Meta *RoleMeta
}

// RoleDependency Role 依赖。
type RoleDependency struct {
    // Name 依赖的 Role 名称。
    Name string

    // Vars 传递给依赖 Role 的变量。
    Vars map[string]any

    // When 条件（可选）。
    When string

    // Tags 标签（可选）。
    Tags []string
}

// RoleMeta Role 元数据。
type RoleMeta struct {
    Author            string
    Description       string
    License           string
    MinAnsibleVersion string
    Platforms         []Platform
}

// Platform 支持的平台。
type Platform struct {
    Name     string
    Versions []string
}
```

### 8.2 Role 加载器

```go
// pkg/role/loader.go

// RoleLoader 从磁盘加载 Role。
type RoleLoader struct {
    searchPaths []string // Role 搜索路径列表
}

// NewRoleLoader 创建 Role 加载器。
func NewRoleLoader(searchPaths []string) *RoleLoader

// Load 加载指定名称的 Role。
// 按 searchPaths 顺序查找，返回第一个找到的 Role。
func (l *RoleLoader) Load(name string) (*Role, error)

// LoadFromPath 从指定路径加载 Role。
func (l *RoleLoader) LoadFromPath(path string) (*Role, error)

// loadDefaults 加载 defaults/main.yml。
func (l *RoleLoader) loadDefaults(rolePath string) (map[string]any, error)

// loadVars 加载 vars/main.yml。
func (l *RoleLoader) loadVars(rolePath string) (map[string]any, error)

// loadTasks 加载 tasks/main.yml。
func (l *RoleLoader) loadTasks(rolePath string) ([]Task, error)

// loadHandlers 加载 handlers/main.yml。
func (l *RoleLoader) loadHandlers(rolePath string) ([]Task, error)

// loadMeta 加载 meta/main.yml。
func (l *RoleLoader) loadMeta(rolePath string) (*RoleMeta, []RoleDependency, error)

// findRolePath 在搜索路径中查找 Role 目录。
func (l *RoleLoader) findRolePath(name string) (string, error)
```

### 8.3 依赖解析器

```go
// pkg/role/resolver.go

// DependencyResolver 解析 Role 依赖关系，进行拓扑排序。
type DependencyResolver struct {
    loader *RoleLoader
}

// NewDependencyResolver 创建依赖解析器。
func NewDependencyResolver(loader *RoleLoader) *DependencyResolver

// Resolve 解析 Role 列表的依赖关系，返回拓扑排序后的 Role 列表。
// 返回的列表中，依赖 Role 排在被依赖 Role 之前。
// 如果检测到循环依赖，返回错误。
func (r *DependencyResolver) Resolve(roles []RoleRef) ([]*Role, error)

// buildGraph 构建依赖图。
func (r *DependencyResolver) buildGraph(roles []*Role) (map[string][]string, error)

// topologicalSort 拓扑排序。
// 返回排序后的 Role 名称列表。
// 如果存在循环依赖，返回错误并包含循环路径信息。
func (r *DependencyResolver) topologicalSort(
    graph map[string][]string,
    inDegree map[string]int,
) ([]string, error)

// detectCycle 检测循环依赖。
// 返回循环路径（如 "a -> b -> c -> a"）。
func (r *DependencyResolver) detectCycle(graph map[string][]string) (string, error)
```

### 8.4 Role 执行器

```go
// pkg/role/executor.go

// RoleExecutor 执行 Role。
type RoleExecutor struct {
    taskExec TaskExecutor  // 任务执行器
    varMgr   VariableManager // 变量管理器
    loader   *RoleLoader
    resolver *DependencyResolver
}

// NewRoleExecutor 创建 Role 执行器。
func NewRoleExecutor(
    taskExec TaskExecutor,
    varMgr VariableManager,
    loader *RoleLoader,
) *RoleExecutor

// Execute 执行 Role 列表（包括依赖解析）。
func (e *RoleExecutor) Execute(roles []RoleRef, ctx ExecContext) error

// executeSingle 执行单个 Role。
func (e *RoleExecutor) executeSingle(role *Role, ctx ExecContext) error

// buildVarContext 构建 Role 的变量上下文。
// 合并 defaults + vars + parameters + play vars。
func (e *RoleExecutor) buildVarContext(
    role *Role,
    params map[string]any,
    parentVars map[string]any,
) map[string]any
```

### 8.5 RoleRef 结构

```go
// pkg/role/ref.go

// RoleRef Playbook 中的 Role 引用。
type RoleRef struct {
    // Name Role 名称。
    Name string

    // Vars 传递给 Role 的参数。
    Vars map[string]any

    // Tags 标签过滤。
    Tags []string

    // When 条件。
    When string
}

// ParseRoleRef 从 YAML 值解析 RoleRef。
// 支持两种格式:
//   - 简写: "nginx" → RoleRef{Name: "nginx"}
//   - 完整: {role: nginx, nginx_port: 8080} → RoleRef{Name: "nginx", Vars: {...}}
func ParseRoleRef(raw any) (RoleRef, error)
```

### 8.6 完整类型关系

```
RoleRef (引用)
├── Name string
├── Vars map[string]any
├── Tags []string
└── When string

Role (完整定义)
├── Name string
├── Path string
├── Defaults map[string]any
├── Vars map[string]any
├── Tasks []Task
├── Handlers []Task
├── Dependencies []RoleDependency
├── TemplatesDir string
├── FilesDir string
└── Meta *RoleMeta

RoleDependency
├── Name string
├── Vars map[string]any
├── When string
└── Tags []string

RoleLoader (加载)
├── searchPaths []string
├── Load(name) → *Role
└── LoadFromPath(path) → *Role

DependencyResolver (解析)
├── Resolve(roles []RoleRef) → []*Role
├── topologicalSort(...) → []string
└── detectCycle(...) → string

RoleExecutor (执行)
├── Execute(roles []RoleRef, ctx) → error
├── executeSingle(role, ctx) → error
└── buildVarContext(role, params, parentVars) → map
```

---

## 9. 任务拆解

### T7.1 Role 目录加载与执行

**目标：** 实现 Role 的完整生命周期：加载、依赖解析、执行。

**交付物：**
- `pkg/role/role.go` — Role / RoleDependency / RoleMeta 数据结构
- `pkg/role/ref.go` — RoleRef 解析
- `pkg/role/loader.go` — RoleLoader 磁盘加载
- `pkg/role/resolver.go` — DependencyResolver 依赖解析
- `pkg/role/executor.go` — RoleExecutor 执行器
- `pkg/role/loader_test.go` — 加载器测试
- `pkg/role/resolver_test.go` — 依赖解析测试
- `pkg/role/executor_test.go` — 执行器测试

**加载范围：**

| 文件 | 是否必须 | 说明 |
|------|----------|------|
| `tasks/main.yml` | 是 | 主任务入口 |
| `defaults/main.yml` | 否 | 默认变量 |
| `vars/main.yml` | 否 | 角色变量 |
| `handlers/main.yml` | 否 | 处理器 |
| `meta/main.yml` | 否 | 元数据和依赖 |
| `templates/` | 否 | 模板文件目录 |
| `files/` | 否 | 静态文件目录 |

**依赖解析要求：**
- [ ] 支持单层依赖：A → B, C
- [ ] 支持多层依赖：A → B → C
- [ ] 支持钻石依赖：A → B, C; B → D; C → D（D 只执行一次）
- [ ] 循环依赖检测并报错：A → B → A → 报错
- [ ] 同一 Role 被多次引用只执行一次
- [ ] 依赖变量正确传递

**执行顺序要求：**
- [ ] 按拓扑排序后的顺序执行
- [ ] defaults 变量优先级最低
- [ ] vars 变量优先级高于 defaults
- [ ] Role parameters 优先级高于 vars
- [ ] templates/ 目录自动加入模板搜索路径
- [ ] files/ 目录自动加入文件搜索路径
- [ ] handlers 正确注册到 HandlerManager

**验收标准：**
- [ ] 能加载一个完整的 Role（所有目录和文件）
- [ ] 缺少 tasks/main.yml 时报错
- [ ] 其他文件缺失时静默忽略（不报错）
- [ ] 依赖解析正确（拓扑排序）
- [ ] 循环依赖检测正确（报错信息清晰）
- [ ] Role 变量正确合并到变量上下文
- [ ] Role tasks 按正确顺序执行
- [ ] Role handlers 正确注册和触发
- [ ] templates/ 和 files/ 路径正确设置
- [ ] Galaxy 安装的 Role 能正确加载（namespace.rolename 格式）
- [ ] 搜索路径按优先级查找
- [ ] 单元测试覆盖加载、解析、执行各环节

---

## 附录：Role 开发速查表

### 最小 Role 结构

```
roles/myrole/
└── tasks/
    └── main.yml
```

```yaml
# roles/myrole/tasks/main.yml
- name: My first task
  debug:
    msg: "Hello from myrole"
```

### 标准 Role 结构

```
roles/myrole/
├── defaults/main.yml
├── vars/main.yml
├── tasks/main.yml
├── handlers/main.yml
├── templates/config.j2
├── files/data.txt
└── meta/main.yml
```

### defaults/main.yml 模板

```yaml
# Role 配置项（使用者可覆盖）
myrole_option_a: default_value_a
myrole_option_b: default_value_b
myrole_enabled: true
```

### vars/main.yml 模板

```yaml
# Role 内部固定值（不要覆盖）
myrole_package_name: mypackage
myrole_config_path: /etc/myrole/config
myrole_service_name: myrole
```

### tasks/main.yml 模板

```yaml
- name: Install package
  yum:
    name: "{{ myrole_package_name }}"
    state: present
  when: myrole_enabled

- name: Deploy config
  template:
    src: config.j2
    dest: "{{ myrole_config_path }}"
    owner: root
    group: root
    mode: "0644"
  notify: restart myrole

- name: Ensure service is running
  service:
    name: "{{ myrole_service_name }}"
    state: started
    enabled: true
```

### handlers/main.yml 模板

```yaml
- name: restart myrole
  service:
    name: "{{ myrole_service_name }}"
    state: restarted
```

### meta/main.yml 模板

```yaml
galaxy_info:
  author: Your Name
  description: Brief description of the role
  license: MIT
  min_ansible_version: "2.9"
  platforms:
    - name: EL
      versions:
        - 7
        - 8
    - name: Ubuntu
      versions:
        - focal
        - jammy

dependencies:
  - role: common
```

### 引用 Role 的三种方式

```yaml
# 方式一：roles 指令（Play 级，静态）
roles:
  - myrole
  - { role: myrole, myrole_option_a: "custom" }

# 方式二：import_role（task 级，静态）
tasks:
  - import_role:
      name: myrole
    vars:
      myrole_option_a: "custom"

# 方式三：include_role（task 级，动态）
tasks:
  - include_role:
      name: myrole
    vars:
      myrole_option_a: "custom"
```
