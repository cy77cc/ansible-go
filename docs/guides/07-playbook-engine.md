# Playbook 执行引擎

> 阶段：P5 | 设计文档引用：第四章、第五章

Playbook 是 Ansible 的编排语言。如果说模块是单个"动词"，Playbook 就是将多个
动词组织成流程的"剧本"。本章深入讲解 Playbook 的 YAML 结构、执行引擎的工作原理、
以及 Go 实现的关键设计。

---

## 1. Playbook 是什么

### 1.1 从 Ad-hoc 到 Playbook

Ad-hoc 命令适合一次性操作：

```bash
ansible-go webservers -m yum -a "name=nginx state=present"
ansible-go webservers -m service -a "name=nginx state=started"
```

Playbook 将这些操作组织为可重复、可版本控制的声明式配置：

```yaml
# site.yml
- name: Configure webservers
  hosts: webservers
  become: true
  tasks:
    - name: Install nginx
      yum:
        name: nginx
        state: present

    - name: Start nginx
      service:
        name: nginx
        state: started
```

### 1.2 Playbook = Play 列表

Playbook 是一个 YAML 文件，内容是一个 **Play 的列表**。每个 Play 定义了：
- **在哪执行**（`hosts`）—— 目标主机或主机组
- **怎么执行**（`become`, `connection`）—— 执行方式
- **执行什么**（`tasks`, `roles`, `handlers`）—— 任务列表

```yaml
# 一个 Playbook 包含多个 Play
- name: Play 1 - Configure web servers    # 第一个 Play
  hosts: webservers
  tasks: [...]

- name: Play 2 - Configure db servers     # 第二个 Play
  hosts: dbservers
  tasks: [...]
```

### 1.3 Play = 主机 + 任务

每个 Play 是一个独立的执行单元。它先选定目标主机集合，然后按顺序执行任务列表中的
每个 task。

```
Play
├── 选定主机: webservers (可能有多台)
├── 收集 Facts
└── 执行任务列表:
    ├── Task 1: Install nginx     → 对每台主机执行
    ├── Task 2: Configure nginx   → 对每台主机执行
    └── Task 3: Start nginx       → 对每台主机执行
```

---

## 2. Playbook YAML 结构

### 2.1 Play 完整字段

```yaml
- name: <string>              # Play 名称（推荐必填）
  hosts: <pattern>            # 目标主机模式（必填）
  become: <bool>              # 是否提权（默认 false）
  become_method: <string>     # 提权方式（默认 sudo）
  become_user: <string>       # 提权目标用户（默认 root）
  gather_facts: <bool>        # 是否收集 facts（默认 true）
  gather_subset: <list>       # facts 收集子集
  connection: <string>        # 连接方式（ssh/local）
  remote_user: <string>       # 远程用户
  serial: <int/list>          # 批处理大小
  strategy: <string>          # 执行策略（linear/free）

  vars:                       # Play 级变量
    key: value
  vars_files:                 # 变量文件列表
    - vars/common.yml
  vars_prompt:                # 交互式变量输入
    - name: password
      prompt: "Enter password"
      private: true

  roles:                      # 角色列表
    - common
    - { role: nginx, nginx_port: 80 }

  pre_tasks:                  # 角色之前执行的任务
    - name: Pre-task
      debug: msg="pre"

  tasks:                      # 主任务列表
    - name: Main task
      yum: name=nginx state=present

  post_tasks:                 # 角色之后执行的任务
    - name: Post-task
      debug: msg="post"

  handlers:                   # 处理器列表
    - name: restart nginx
      service: name=nginx state=restarted

  tags:                       # Play 级标签
    - web
    - nginx
```

**字段分类：**

| 类别 | 字段 | 必填 | 说明 |
|------|------|------|------|
| 目标 | `hosts` | 是 | 主机模式 |
| 标识 | `name` | 否 | Play 名称，用于输出 |
| 执行 | `become`, `become_method`, `become_user` | 否 | 提权配置 |
| 执行 | `connection`, `remote_user` | 否 | 连接配置 |
| 执行 | `serial`, `strategy` | 否 | 批处理与策略 |
| 变量 | `vars`, `vars_files`, `vars_prompt` | 否 | 变量定义 |
| 任务 | `pre_tasks`, `tasks`, `post_tasks` | 否 | 任务列表 |
| 任务 | `roles` | 否 | 角色列表 |
| 任务 | `handlers` | 否 | 处理器列表 |
| 标记 | `tags` | 否 | 标签过滤 |
| 开关 | `gather_facts`, `gather_subset` | 否 | Facts 收集 |

### 2.2 Task 完整字段

```yaml
- name: <string>              # 任务描述（推荐必填）
  <module_name>:              # 模块名（必填，与下面的参数二选一）
    <param>: <value>          # 模块参数（dict 形式）
  # 或者
  <module_name>: <arg_string> # 模块参数（key=value 字符串形式）

  # 控制流
  when: <condition>           # 条件表达式
  loop: <list>                # 循环数据源
  loop_control:               # 循环控制
    loop_var: item            # 循环变量名（默认 item）
    index_var: idx            # 索引变量名
    label: "{{ item.name }}"  # 输出标签模板
    pause: 0                  # 每次循环间隔秒数
  register: <var_name>        # 注册结果到变量
  ignore_errors: <bool>       # 忽略错误继续（默认 false）
  failed_when: <condition>    # 自定义失败条件
  changed_when: <condition>   # 自定义变更条件
  check_mode: <bool>          # 覆盖全局 check mode

  # 执行控制
  async: <seconds>            # 异步超时（0=同步）
  poll: <seconds>             # 轮询间隔（0=不等待）
  retries: <n>                # 重试次数
  delay: <seconds>            # 重试间隔
  until: <condition>          # 重试直到条件成立
  throttle: <n>               # 并发限制

  # 标记与委派
  tags: [tag1, tag2]          # 任务标签
  delegate_to: <host>         # 委派到其他主机执行
  run_once: <bool>            # 只在第一台主机执行一次

  # 错误处理（Block 语法）
  block:                      # 任务块
    - name: task in block
      ...
  rescue:                     # 块内失败时执行
    - name: recovery task
      ...
  always:                     # 总是执行
    - name: cleanup task
      ...
```

**字段分类：**

| 类别 | 字段 | 说明 |
|------|------|------|
| 标识 | `name` | 任务描述，用于输出和日志 |
| 模块 | `<module_name>` + 参数 | 调用哪个模块，传什么参数 |
| 条件 | `when` | 条件为 true 时才执行 |
| 循环 | `loop`, `loop_control` | 遍历列表执行 |
| 注册 | `register` | 将结果存入变量供后续使用 |
| 错误 | `ignore_errors`, `failed_when`, `changed_when` | 错误与状态控制 |
| 重试 | `retries`, `delay`, `until` | 重试机制 |
| 异步 | `async`, `poll` | 异步执行 |
| 委派 | `delegate_to`, `run_once` | 执行位置控制 |
| 标记 | `tags` | 标签过滤 |
| 块 | `block`, `rescue`, `always` | 错误处理块 |

### 2.3 Block / Rescue / Always

Block 是 Task 的分组机制，提供类似 try-catch-finally 的错误处理：

```yaml
tasks:
  - name: Error handling example
    block:
      - name: Try to deploy
        copy:
          src: app.tar.gz
          dest: /opt/app/
      - name: Start app
        service:
          name: myapp
          state: started
    rescue:
      - name: Rollback on failure
        copy:
          src: app.tar.gz.bak
          dest: /opt/app/
      - name: Notify failure
        debug:
          msg: "Deployment failed, rolled back"
    always:
      - name: Cleanup temp files
        file:
          path: /tmp/app.tar.gz
          state: absent
```

**执行逻辑：**

```
block 中的 tasks
├── 全部成功 → 跳过 rescue → 执行 always → 继续
└── 任一失败 → 执行 rescue → 执行 always → 继续
```

Block 也可以接受公共属性：

```yaml
- name: Deploy with common settings
  block:
    - name: Step 1
      ...
    - name: Step 2
      ...
  become: true
  tags: [deploy]
  when: deploy_enabled
```

### 2.4 Handler 完整定义

Handler 是特殊的 Task，只在被 `notify` 触发时执行：

```yaml
tasks:
  - name: Update nginx config
    template:
      src: nginx.conf.j2
      dest: /etc/nginx/nginx.conf
    notify: restart nginx

handlers:
  - name: restart nginx
    service:
      name: nginx
      state: restarted
    listen: "restart web services"  # 可选的监听主题
```

---

## 3. 执行流程详解（7 步）

Playbook 引擎的执行流程分为 7 个阶段：

```
Playbook YAML 文件
        │
        ▼
┌───────────────────────┐
│  1. 加载 & 解析        │  读取 YAML，合并 vars_files
└───────────────────────┘
        │
        ▼
┌───────────────────────┐
│  2. 变量渲染           │  用模板引擎渲染所有 {{ }} 表达式
└───────────────────────┘
        │
        ▼
┌───────────────────────┐
│  3. 主机匹配           │  根据 hosts: 模式选择目标主机
└───────────────────────┘
        │
        ▼
┌───────────────────────┐
│  4. Facts 收集         │  SSH 到每台主机收集系统信息
└───────────────────────┘
        │
        ▼
┌───────────────────────┐
│  5. 策略执行           │  按策略（linear/free）并发执行 tasks
└───────────────────────┘
        │
        ▼
┌───────────────────────┐
│  6. 结果收集           │  聚合每台主机的执行结果
└───────────────────────┘
        │
        ▼
┌───────────────────────┐
│  7. 回调输出           │  通知回调插件显示/记录结果
└───────────────────────┘
```

### 3.1 第 1 步：加载与解析

```
输入: site.yml 文件路径

处理:
1. 读取 YAML 文件内容
2. 解析为 []Play 结构体
3. 对每个 Play 的 vars_files 字段：
   a. 渲染文件路径中的变量
   b. 加载文件内容
   c. 解析为 map[string]any
   d. 合并到 Play.Vars
4. 处理 include / import 指令（合并子文件内容）

输出: 完整的 Playbook 结构体（所有变量已加载，但未渲染）
```

### 3.2 第 2 步：变量渲染

```
输入: 带有 {{ }} 表达式的 Playbook 结构体

处理:
1. 构建变量上下文（合并所有已知变量）
2. 对每个 Play:
   a. 渲染 hosts 字段
   b. 渲染 vars 中的值
3. 对每个 Task:
   a. 渲染 task name
   b. 渲染 module args
   c. 渲染 when 条件
   d. 渲染 loop 数据

注意: 此阶段只渲染"静态"字段。
      task name, args, when, loop 在执行时可能需要再次渲染
      （因为 register 和 loop 会引入新变量）
```

### 3.3 第 3 步：主机匹配

```
输入: hosts 模式（如 "webservers", "all", "web:&prod"）

处理:
1. 解析主机模式（支持 &, :!, 通配符, 正则）
2. 从 Inventory 中匹配主机
3. 应用 --limit 过滤
4. 为每台主机创建执行上下文

输出: 目标主机列表
```

**主机模式语法：**

| 模式 | 含义 | 示例 |
|------|------|------|
| `all` | 所有主机 | `hosts: all` |
| 组名 | 指定组 | `hosts: webservers` |
| `:` | 并集 | `hosts: webservers:dbservers` |
| `:&` | 交集 | `hosts: webservers:&production` |
| `:!` | 差集 | `hosts: all:!staging` |
| `*` | 通配符 | `hosts: web*.example.com` |
| 正则 | 正则匹配 | `hosts: ~(web\|db)\d+\.example\.com` |
| 列表 | 多个模式 | `hosts: [webservers, dbservers]` |

### 3.4 第 4 步：Facts 收集

```
输入: 目标主机列表

处理:
1. 检查 gather_facts 是否为 true（默认 true）
2. 对每台主机（并发）:
   a. 建立 SSH 连接
   b. 执行 setup 模块（收集系统信息的 shell 命令）
   c. 解析输出为 key-value
   d. 注入到主机的变量上下文（ansible_* 前缀）

收集内容:
- ansible_os_family: "RedHat"
- ansible_distribution: "CentOS"
- ansible_distribution_version: "7.9"
- ansible_hostname: "web1"
- ansible_default_ipv4.address: "192.168.1.10"
- ansible_memtotal_mb: 8192
- ansible_processor_vcpus: 4
...

输出: 每台主机的变量上下文已注入 facts
```

### 3.5 第 5 步：策略执行

```
输入: Tasks 列表 + 目标主机列表

处理:
1. 根据 strategy 选择执行策略（linear 或 free）
2. 策略引擎调度 task 到各主机执行
3. 每个 task 执行流程:
   a. 模板渲染（task name, args, when, loop）
   b. 条件判断（when）
   c. 循环展开（loop）
   d. 调用模块执行
   e. 结果处理（register, notify, changed_when, failed_when）
4. 收集所有主机结果

输出: 每台主机 × 每个 task 的执行结果
```

### 3.6 第 6 步：结果收集

```
输入: 所有主机 × 所有 task 的执行结果

处理:
1. 聚合结果统计:
   - ok: 成功且无变更的任务数
   - changed: 成功且有变更的任务数
   - failed: 失败的任务数
   - skipped: 跳过的任务数
   - unreachable: 不可达的主机数
2. 处理 play 级错误:
   - 有 failed 且未 ignore_errors → 标记该主机为 failed
   - 所有主机 failed → play 失败

输出: 汇总统计数据
```

### 3.7 第 7 步：回调输出

```
输入: 执行结果和统计数据

处理:
1. 通知所有注册的回调插件
2. 默认回调插件（stdout）输出到终端:
   - Play 标题行
   - 每个 Task 的结果（ok/changed/failed/skipped）
   - 最终统计摘要
3. 其他回调插件可写日志、发通知等

输出: 终端显示 + 日志记录
```

---

## 4. Task 执行流程

每个 Task 的执行遵循以下详细流程：

```
Task 定义
    │
    ▼
┌─────────────────────────┐
│  1. 模板渲染              │  渲染 task name, module args, when, loop
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│  2. when 求值             │  条件为 false → 跳过（Skipped）
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│  3. loop 展开             │  展开为多次执行
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│  4. 模块执行              │  调用 Module.Run()
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│  5. 结果处理              │  register / notify / changed_when / failed_when
└─────────────────────────┘
```

### 4.1 模板渲染

Task 执行前，以下字段会被模板引擎渲染：

```yaml
- name: "Install {{ package_name }}"      # task name
  yum:
    name: "{{ package_name }}"             # module args
    state: "{{ package_state | default('present') }}"
  when: ansible_os_family == "{{ target_os }}"  # when 条件
  loop: "{{ package_list }}"               # loop 数据
```

渲染时机：
- Task 的 name、module args、when 条件在每次执行前渲染
- 如果 task 在 loop 中，每次迭代都会重新渲染（因为变量不同）

### 4.2 when 求值

`when` 字段是一个布尔表达式，决定 task 是否执行：

```yaml
# 简单条件
when: ansible_os_family == "RedHat"

# 布尔变量
when: enable_feature

# 组合条件（and）
when: ansible_os_family == "RedHat" and ansible_distribution_major_version == "7"

# 组合条件（or）
when: ansible_os_family == "RedHat" or ansible_os_family == "Debian"

# 否定条件
when: not skip_this_task

# 列表形式（隐式 and）
when:
  - ansible_os_family == "RedHat"
  - ansible_distribution_major_version | int >= 7
```

**求值规则：**

```
输入: when 表达式 + 变量上下文

处理:
1. 将 when 表达式传入模板引擎渲染
2. 渲染结果作为布尔值解析
   - true / True / yes / 1  → true
   - false / False / no / 0  → false
3. 如果 when 是列表，所有项求值后取 and

结果:
- true → 继续执行
- false → 跳过（Skipped），输出 "skipping: [host]"
```

### 4.3 loop 展开

`loop` 将 task 展开为多次执行：

```yaml
- name: Install packages
  yum:
    name: "{{ item }}"
    state: present
  loop:
    - nginx
    - vim
    - curl
```

展开后等价于：

```yaml
- name: Install packages (item=nginx)
  yum: name=nginx state=present

- name: Install packages (item=vim)
  yum: name=vim state=present

- name: Install packages (item=curl)
  yum: name=curl state=present
```

### 4.4 模块执行

```
输入: 渲染后的 module name + args

处理:
1. 从 Registry 查找模块
2. 构建 ExecContext:
   - Host: 当前主机
   - Args: 渲染后的参数
   - Connection: 主机的 SSH 连接
   - CheckMode: 全局 check mode（可被 task 级覆盖）
   - Diff: 全局 diff 模式
   - Variables: 当前变量上下文
3. 调用 Module.Run(ctx)
4. 返回 Result

输出: Result (Changed / Failed / Msg / ...)
```

### 4.5 结果处理

模块执行完成后，进行以下处理：

**register — 注册结果到变量：**
```yaml
- name: Check nginx version
  shell: nginx -v
  register: nginx_result

- name: Show version
  debug:
    msg: "Nginx version: {{ nginx_result.stderr }}"
```

register 后变量的字段：
```go
nginx_result.stdout   // 标准输出
nginx_result.stderr   // 标准错误
nginx_result.rc       // 退出码
nginx_result.changed  // 是否变更
nginx_result.failed   // 是否失败
```

**notify — 触发 Handler：**
```yaml
- name: Update config
  template:
    src: nginx.conf.j2
    dest: /etc/nginx/nginx.conf
  notify: restart nginx
```

触发条件：task 执行成功且 `Changed: true`。

**changed_when — 自定义变更条件：**
```yaml
- name: Check something
  shell: echo "done"
  register: result
  changed_when: result.rc != 0  # 只在非零退出码时报告 changed
  changed_when: false            # 永远不报告 changed
```

**failed_when — 自定义失败条件：**
```yaml
- name: Check service
  shell: systemctl status nginx
  register: result
  failed_when:
    - result.rc != 0
    - "'active (running)' not in result.stdout"
```

---

## 5. 循环机制

### 5.1 loop 基本用法

```yaml
# 简单列表
- name: Install packages
  yum:
    name: "{{ item }}"
    state: present
  loop:
    - nginx
    - vim
    - curl
```

### 5.2 loop 遍历字典列表

```yaml
- name: Create users
  user:
    name: "{{ item.name }}"
    groups: "{{ item.groups }}"
    shell: "{{ item.shell | default('/bin/bash') }}"
  loop:
    - { name: alice, groups: "admin" }
    - { name: bob, groups: "developer", shell: "/bin/zsh" }
```

### 5.3 loop_control

```yaml
- name: Install packages
  yum:
    name: "{{ item }}"
    state: present
  loop:
    - nginx
    - vim
    - curl
  loop_control:
    loop_var: pkg          # 重命名循环变量（默认 item）
    index_var: idx         # 索引变量
    label: "{{ pkg }}"     # 输出时显示的标签
    pause: 1               # 每次循环间隔 1 秒
```

### 5.4 嵌套循环

```yaml
# 使用 product 过滤器实现嵌套
- name: Grant permissions
  mysql_user:
    name: "{{ item.0 }}"
    priv: "{{ item.1 }}.*:ALL"
    state: present
  loop: "{{ ['alice', 'bob'] | product(['db1', 'db2']) | list }}"
  # 等价于嵌套循环:
  # alice × db1, alice × db2, bob × db1, bob × db2
```

### 5.5 register 与 loop

loop 中使用 register 时，变量会被赋值为一个列表，包含每次迭代的结果：

```yaml
- name: Check services
  shell: systemctl is-active "{{ item }}"
  register: results
  loop:
    - nginx
    - mysql
    - redis
  ignore_errors: true

# results 的结构:
# results.results[0] = { stdout: "active", rc: 0, ... }
# results.results[1] = { stdout: "inactive", rc: 3, ... }
# results.results[2] = { stdout: "active", rc: 0, ... }
```

---

## 6. 条件判断

### 6.1 when 基本语法

```yaml
# 字符串比较
when: ansible_os_family == "RedHat"

# 数字比较
when: ansible_memtotal_mb >= 4096

# 布尔值
when: enable_firewall

# 变量存在性
when: my_var is defined
when: my_var is not defined

# 字符串包含
when: "'error' in command_output"
```

### 6.2 组合条件

```yaml
# and 组合（同一行）
when: ansible_os_family == "RedHat" and ansible_distribution_major_version | int >= 7

# or 组合
when: ansible_os_family == "RedHat" or ansible_os_family == "Debian"

# 列表形式（隐式 and）
when:
  - ansible_os_family == "RedHat"
  - ansible_distribution_major_version | int >= 7
  - install_nginx | default(true)
```

### 6.3 条件判断求值流程

```
输入: when 表达式 + 变量上下文

步骤:
1. 如果 when 是字符串:
   a. 传入模板引擎渲染
   b. 渲染结果解析为布尔值
2. 如果 when 是列表:
   a. 对每个元素独立求值
   b. 所有元素取 and（全部为 true 才执行）

布尔值解析规则:
  "true", "True", "yes", "1", 1, true  → true
  "false", "False", "no", "0", 0, false, nil  → false
  其他非空字符串 → true（非零值视为 true）
```

---

## 7. Tags 机制

### 7.1 标记任务

```yaml
tasks:
  - name: Install nginx
    yum: name=nginx state=present
    tags: [install, nginx]

  - name: Configure nginx
    template: src=nginx.conf.j2 dest=/etc/nginx/nginx.conf
    tags: [configure, nginx]

  - name: Start nginx
    service: name=nginx state=started
    tags: [service, nginx]
```

### 7.2 标记 Play

```yaml
- name: Configure web servers
  hosts: webservers
  tags: [web]
  tasks: [...]
```

### 7.3 命令行过滤

```bash
# 只执行带 install 标签的任务
ansible-go playbook site.yml --tags install

# 执行多个标签（或关系）
ansible-go playbook site.yml --tags "install,configure"

# 跳过带 install 标签的任务
ansible-go playbook site.yml --skip-tags install
```

### 7.4 特殊标签

| 标签 | 含义 |
|------|------|
| `always` | 总是执行，即使未被 `--tags` 选中 |
| `never` | 总是跳过，除非显式 `--tags never` |

```yaml
tasks:
  - name: This always runs
    debug: msg="Always"
    tags: [always]

  - name: This never runs unless explicitly requested
    debug: msg="Rarely needed"
    tags: [never]

  - name: Normal task
    debug: msg="Normal"
    tags: [deploy]
```

```bash
# 即使 --tags deploy，带 always 标签的任务也会执行
ansible-go playbook site.yml --tags deploy
# 输出: Always, Normal (跳过 never)

# 只有显式 --tags never 才会执行
ansible-go playbook site.yml --tags never
# 输出: Rarely needed (always 仍会执行)
```

### 7.5 标签求值流程

```
输入: Task 标签列表 + 命令行 --tags / --skip-tags

求值:
1. 如果 --skip-tags 包含 task 的某个标签 → 跳过
2. 如果 task 有 never 标签且 --tags 未显式指定 never → 跳过
3. 如果 task 有 always 标签 → 总是执行（忽略 --tags）
4. 如果未指定 --tags → 执行所有 task
5. 如果指定了 --tags → 只执行匹配标签的 task
```

---

## 8. 执行策略：Linear vs Free

### 8.1 Linear 策略（默认）

Linear 策略是"同步屏障"模型：每个 task 等待所有主机完成后才进入下一个 task。

```
           Task 1         Task 2         Task 3
           ──────         ──────         ──────
Host A:    ████ → done    ██ → done      ███ → done
Host B:      █████ → done   █ → done       ████ → done
Host C:    ███ → done     ████ → done      ██ → done
           ────────────── ────────────── ──────────────
                barrier        barrier
```

**特点：**
- 每个 task 结束时是一个同步点（barrier）
- 所有主机完成后才进入下一个 task
- 适合需要跨主机协调的场景
- 输出整齐，易于阅读

**适用场景：**
- 滚动更新（serial: 2 表示每次 2 台）
- 需要前一台完成才能继续的逻辑
- 调试时更易追踪

### 8.2 Free 策略

Free 策略是"独立推进"模型：每台主机以自己的速度执行，互不等待。

```
           Task 1         Task 2         Task 3
           ──────         ──────         ──────
Host A:    ██ → done      █ → done       ██ → done
Host B:      █████ → done    ██ → done      █ → done
Host C:    █ → done       ████ → done       ████ → done
```

**特点：**
- 每台主机独立推进
- 快的主机不会被慢的主机拖累
- 输出交错，不易阅读
- 总体完成时间可能更短

**适用场景：**
- 主机性能差异大
- 任务之间无跨主机依赖
- 追求最短完成时间

### 8.3 Serial 批处理

`serial` 控制每批处理的主机数量，常用于滚动更新：

```yaml
- name: Rolling update
  hosts: webservers
  serial: 2              # 每次 2 台
  tasks: [...]
```

```
批次 1: [Host A, Host B]  → 执行所有 task → 完成
批次 2: [Host C, Host D]  → 执行所有 task → 完成
批次 3: [Host E]          → 执行所有 task → 完成
```

也可以指定百分比或列表：

```yaml
serial: "30%"            # 每次 30% 的主机
serial: [1, 5, "25%"]    # 第一批 1 台，第二批 5 台，之后 25%
```

---

## 9. 变量作用域与上下文链

### 9.1 作用域层级

变量上下文是一个嵌套的层级结构，从全局到局部：

```
GlobalContext                     # 全局
│   extra-vars (-e)
│   config defaults
│
└── PlayContext                   # Play 级
    │   vars: {key: value}
    │   vars_files: ...
    │   inventory vars
    │
    └── RoleContext               # Role 级
    │   │   role defaults
    │   │   role vars
    │   │   role parameters
    │   │
    │   └── TaskContext           # Task 级
    │       │   task vars
    │       │   include_vars
    │       │   set_fact / register
    │       │
    │       └── HostContext       # 主机级
    │           host_vars
    │           facts
    │
    └── HandlerContext            # Handler 级
        handler vars
```

### 9.2 变量优先级（从低到高）

```
 1. role defaults                 roles/x/defaults/main.yml
 2. inventory file vars           [group:vars]
 3. inventory group_vars/         group_vars/all.yml → group_vars/<group>.yml
 4. inventory host_vars/          host_vars/<hostname>.yml
 5. inventory host vars           host ansible_var=x
 6. play vars                     playbook vars:
 7. play vars_files               playbook vars_files:
 8. play vars_prompt              交互式输入
 9. role vars                     roles/x/vars/main.yml
10. block vars
11. task vars
12. include_vars
13. set_facts / registered vars
14. role parameters
15. include parameters
16. extra-vars (-e)               最高优先级
```

### 9.3 合并规则

```go
// 变量合并规则（不可变：每次合并产生新对象）

// 同名 dict → 递归深度合并
{a: {x: 1, y: 2}} + {a: {y: 3, z: 4}} = {a: {x: 1, y: 3, z: 4}}

// 同名 list → 后者覆盖
{a: [1, 2]} + {a: [3, 4]} = {a: [3, 4]}

// 同名 scalar → 后者覆盖
{a: "hello"} + {a: "world"} = {a: "world"}
```

### 9.4 内置变量（Magic Variables）

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

---

## 10. Go 实现要点

### 10.1 核心数据结构

```go
// pkg/playbook/playbook.go

// Playbook 表示一个完整的 Playbook 文件。
type Playbook struct {
    Plays []Play
    Path  string // Playbook 文件路径
}

// Play 表示一个 Play。
type Play struct {
    Name            string
    Hosts           string
    Become          bool
    BecomeMethod    string
    BecomeUser      string
    GatherFacts     bool
    GatherSubset    []string
    Connection      string
    RemoteUser      string
    Serial          any // int, string ("30%"), or []any
    Strategy        string
    Vars            map[string]any
    VarsFiles       []string
    VarsPrompt      []VarPrompt
    Roles           []RoleRef
    PreTasks        []Task
    Tasks           []Task
    PostTasks       []Task
    Handlers        []Task
    Tags            []string
}

// Task 表示一个任务。
type Task struct {
    Name          string
    Module        string
    Args          map[string]any
    When          any // string or []string
    Loop          any
    LoopControl   *LoopControl
    Register      string
    IgnoreErrors  bool
    FailedWhen    any // string or []string
    ChangedWhen   any // string or []string
    CheckMode     *bool
    Async         int
    Poll          int
    Retries       int
    Delay         int
    Until         string
    Throttle      int
    Tags          []string
    DelegateTo    string
    RunOnce       bool
    Notify        []string
    Block         *Block
}

// Block 表示一个任务块（block/rescue/always）。
type Block struct {
    Tasks  []Task
    Rescue []Task
    Always []Task
}

// LoopControl 循环控制参数。
type LoopControl struct {
    LoopVar string
    IndexVar string
    Label   string
    Pause   int
}

// RoleRef 角色引用。
type RoleRef struct {
    Name string
    Vars map[string]any
    Tags []string
    When string
}

// VarPrompt 变量提示。
type VarPrompt struct {
    Name    string
    Prompt  string
    Private bool
    Default string
}
```

### 10.2 Playbook 解析器

```go
// pkg/playbook/parser.go

// PlaybookParser 解析 Playbook YAML 文件。
type PlaybookParser struct {
    basePath string // Playbook 所在目录
}

// NewPlaybookParser 创建解析器。
func NewPlaybookParser(basePath string) *PlaybookParser

// Parse 解析 Playbook 文件，返回 Playbook 结构体。
func (p *PlaybookParser) Parse(path string) (*Playbook, error)

// ParseBytes 解析 YAML 字节内容。
func (p *PlaybookParser) ParseBytes(data []byte, path string) (*Playbook, error)

// parsePlay 解析单个 Play。
func (p *PlaybookParser) parsePlay(raw map[string]any) (Play, error)

// parseTask 解析单个 Task。
func (p *PlaybookParser) parseTask(raw map[string]any) (Task, error)

// parseBlock 解析 block/rescue/always。
func (p *PlaybookParser) parseBlock(raw map[string]any) (*Block, error)
```

### 10.3 Playbook 执行引擎

```go
// pkg/playbook/engine.go

// PlaybookEngine Playbook 执行引擎。
type PlaybookEngine struct {
    inventory   *inventory.Inventory
    varManager  *variable.Manager
    tmplEngine  *template.Engine
    factColl    *facts.Collector
    callback    callback.Callback
    strategy    string
    forks       int
    checkMode   bool
    diffMode    bool
    tags        []string
    skipTags    []string
    limit       string
}

// NewPlaybookEngine 创建执行引擎。
func NewPlaybookEngine(opts ...EngineOption) *PlaybookEngine

// Run 执行整个 Playbook。
func (e *PlaybookEngine) Run(playbook *Playbook) error

// runPlay 执行单个 Play。
func (e *PlaybookEngine) runPlay(play Play) error

// runTasks 执行任务列表。
func (e *PlaybookEngine) runTasks(tasks []Task, hosts []*inventory.Host) error

// runTask 执行单个 Task（单台主机）。
func (e *PlaybookEngine) runTask(task Task, host *inventory.Host) (module.Result, error)

// evaluateWhen 评估 when 条件。
func (e *PlaybookEngine) evaluateWhen(when any, vars map[string]any) (bool, error)

// expandLoop 展开 loop。
func (e *PlaybookEngine) expandLoop(loop any, vars map[string]any) ([]any, error)

// handleResult 处理执行结果（register, notify, changed_when, failed_when）。
func (e *PlaybookEngine) handleResult(task Task, result module.Result, vars map[string]any) error
```

### 10.4 WorkerPool 并发执行

```go
// pkg/playbook/worker.go

// WorkerPool 管理多主机并发执行。
type WorkerPool struct {
    maxWorkers int
    jobs       chan TaskJob
    results    chan TaskResult
}

// TaskJob 一个任务执行单元。
type TaskJob struct {
    Task Task
    Host *inventory.Host
}

// TaskResult 一个任务执行结果。
type TaskResult struct {
    Host   *inventory.Host
    Task   Task
    Result module.Result
    Error  error
}

// NewWorkerPool 创建工作池。
func NewWorkerPool(maxWorkers int) *WorkerPool

// Submit 提交任务。
func (p *WorkerPool) Submit(job TaskJob)

// Collect 收集结果。
func (p *WorkerPool) Collect() <-chan TaskResult

// Close 关闭工作池。
func (p *WorkerPool) Close()
```

### 10.5 Handler 管理器

```go
// pkg/playbook/handler.go

// HandlerManager 管理 handler 的注册和触发。
type HandlerManager struct {
    handlers  map[string][]Task  // name → handler tasks
    pending   []string           // 待触发的 handler 名
    triggered map[string]bool    // 已触发的 handler（防止重复执行）
}

// NewHandlerManager 创建 handler 管理器。
func NewHandlerManager() *HandlerManager

// Register 注册 handler。
func (m *HandlerManager) Register(name string, task Task)

// Notify 将 handler 加入待触发队列。
func (m *HandlerManager) Notify(name string)

// FlushPending 执行所有待触发的 handler。
func (m *HandlerManager) FlushPending(executor TaskExecutor) error

// Reset 重置待触发队列（新 Play 开始时）。
func (m *HandlerManager) Reset()
```

### 10.6 EngineOption 函数选项模式

```go
// pkg/playbook/options.go

// EngineOption 引擎配置选项。
type EngineOption func(*PlaybookEngine)

// WithForks 设置并发数。
func WithForks(n int) EngineOption

// WithCheckMode 启用干跑模式。
func WithCheckMode(enabled bool) EngineOption

// WithDiffMode 启用 diff 模式。
func WithDiffMode(enabled bool) EngineOption

// WithTags 设置执行标签。
func WithTags(tags []string) EngineOption

// WithSkipTags 设置跳过标签。
func WithSkipTags(tags []string) EngineOption

// WithLimit 设置主机限制。
func WithLimit(limit string) EngineOption

// WithCallback 设置回调插件。
func WithCallback(cb callback.Callback) EngineOption

// WithStrategy 设置执行策略。
func WithStrategy(strategy string) EngineOption
```

---

## 11. 任务拆解

### T5.1 Playbook YAML 解析

**目标：** 实现 Playbook YAML 文件的完整解析。

**交付物：**
- `pkg/playbook/playbook.go` — Playbook / Play / Task / Block 数据结构
- `pkg/playbook/parser.go` — PlaybookParser 解析器
- `pkg/playbook/parser_test.go` — 解析器单元测试

**解析范围：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 否 | Play/Task 名称 |
| `hosts` | string | 是 | 主机模式（仅 Play） |
| `become` | bool | 否 | 提权（仅 Play） |
| `gather_facts` | bool | 否 | 收集 facts（仅 Play） |
| `vars` | map | 否 | 变量 |
| `vars_files` | []string | 否 | 变量文件 |
| `roles` | []RoleRef | 否 | 角色引用 |
| `tasks` | []Task | 否 | 主任务列表 |
| `pre_tasks` | []Task | 否 | 前置任务 |
| `post_tasks` | []Task | 否 | 后置任务 |
| `handlers` | []Task | 否 | 处理器 |
| `<module>` | map/string | 是 | 模块名和参数 |
| `when` | string/[]string | 否 | 条件 |
| `loop` | any | 否 | 循环 |
| `register` | string | 否 | 注册变量 |
| `tags` | []string | 否 | 标签 |
| `notify` | string/[]string | 否 | 通知 handler |
| `block`/`rescue`/`always` | []Task | 否 | 块结构 |

**验收标准：**
- [ ] 能正确解析包含多个 Play 的 Playbook
- [ ] 支持所有 Task 字段
- [ ] 支持 block/rescue/always 嵌套
- [ ] 支持 key=value 和 dict 两种模块参数格式
- [ ] 正确识别 `_raw_params`（命令类模块）
- [ ] YAML 语法错误给出清晰的错误信息
- [ ] 单元测试覆盖各种 YAML 结构

### T5.2 Playbook 执行引擎（线性策略）

**目标：** 实现 Playbook 的核心执行逻辑，先实现 Linear 策略。

**交付物：**
- `pkg/playbook/engine.go` — PlaybookEngine 执行引擎
- `pkg/playbook/worker.go` — WorkerPool 并发工作池
- `pkg/playbook/handler.go` — HandlerManager
- `pkg/playbook/options.go` — EngineOption 函数选项
- `pkg/playbook/engine_test.go` — 引擎单元测试

**执行流程实现：**

```
Playbook
    ↓
for each Play:
    1. 解析 hosts 模式 → 匹配主机
    2. 构建变量上下文（合并 inventory + play vars）
    3. gather_facts? → 并发收集 facts
    4. 执行 pre_tasks（linear 策略）
    5. 触发 pre_tasks 产生的 handlers
    6. 执行 roles（按依赖顺序）
    7. 执行 tasks（linear 策略）
    8. 执行 post_tasks（linear 策略）
    9. 触发所有 pending handlers
```

**Task 执行子流程：**

```
for each Task:
    1. 模板渲染 task name / args / when / loop
    2. 检查 tags 过滤
    3. 求值 when 条件 → false 则 skip
    4. 展开 loop → 多次执行
    5. 查找模块 → 调用 Module.Run()
    6. 处理结果:
       - register → 存入变量
       - failed_when → 重新判断失败
       - changed_when → 重新判断变更
       - notify → 加入 handler 队列
    7. 回调通知（onTaskStart / onTaskOK / onTaskFailed）
```

**验收标准：**
- [ ] 能执行包含单个 Play 的 Playbook
- [ ] 能执行包含多个 Play 的 Playbook
- [ ] Linear 策略：每个 task 等所有主机完成后再进入下一个
- [ ] 并发执行多台主机（forks 控制并发数）
- [ ] when 条件正确判断
- [ ] loop 正确展开
- [ ] register 正确注册变量
- [ ] Handler 正确触发（changed 时 notify）
- [ ] Handler 在 Play 结束时统一执行
- [ ] block/rescue/always 正确处理
- [ ] ignore_errors 正确忽略错误
- [ ] tags 过滤正确工作
- [ ] Check Mode 正确传递
- [ ] 回调插件正确通知
- [ ] 错误处理：主机不可达时标记并继续
- [ ] 单元测试覆盖主要执行路径

---

## 附录：Playbook 执行示例

### 完整 Playbook 示例

```yaml
---
# site.yml — 完整的 Web 服务器配置
- name: Common configuration
  hosts: all
  become: true
  gather_facts: true
  roles:
    - common

- name: Configure web servers
  hosts: webservers
  become: true
  serial: 2
  vars:
    http_port: 80
    max_clients: 200
  vars_files:
    - vars/secrets.yml

  pre_tasks:
    - name: Verify connectivity
      ping:
      tags: [health]

  roles:
    - { role: nginx, nginx_port: "{{ http_port }}" }
    - { role: app, tags: [app] }

  tasks:
    - name: Deploy application
      copy:
        src: "files/app-{{ app_version }}.tar.gz"
        dest: /opt/app/
        owner: app
        group: app
      notify: restart app
      tags: [deploy]

    - name: Configure monitoring
      template:
        src: monitoring.conf.j2
        dest: /etc/monitoring/monitoring.conf
      when: enable_monitoring | default(false)
      tags: [monitoring]

  post_tasks:
    - name: Verify service is running
      uri:
        url: "http://localhost:{{ http_port }}/health"
        status_code: 200
      retries: 5
      delay: 3
      tags: [health]

  handlers:
    - name: restart app
      service:
        name: myapp
        state: restarted

    - name: reload nginx
      service:
        name: nginx
        state: reloaded
```

### 执行输出示例

```
PLAY [Common configuration] *********************************

TASK [Gathering Facts] **************************************
ok: [web1]
ok: [web2]
ok: [db1]

TASK [common : Install base packages] ***********************
ok: [web1]
changed: [db1]
ok: [web2]

PLAY [Configure web servers] ********************************

TASK [Gathering Facts] **************************************
ok: [web1]
ok: [web2]

TASK [Verify connectivity] **********************************
ok: [web1]
ok: [web2]

TASK [Deploy application] ***********************************
changed: [web1]
changed: [web2]

RUNNING HANDLER [restart app] *******************************
changed: [web1]
changed: [web2]

PLAY RECAP **************************************************
web1     : ok=6  changed=2  failed=0  skipped=0  rescued=0
web2     : ok=6  changed=2  failed=0  skipped=0  rescued=0
db1      : ok=2  changed=1  failed=0  skipped=0  rescued=0
```
