# 09 - Handler 机制与错误处理

> 阶段：P8 | 设计文档引用：第五章 5.5-5.6、第二十二章

本章覆盖 go-ansible 中三个紧密相关的主题：Handler 通知机制、Block/Rescue/Always
结构化错误处理、以及全局错误分类与退出码体系。它们共同构成了 Playbook 执行引擎
的"安全网"——确保任务失败时有可控的恢复路径，任务成功时有可靠的后续动作触发。

---

## 1. Handler 机制

### 1.1 什么是 Handler

Handler 是一类特殊的 Task，**不会自动执行**，只在被其他任务通过 `notify` 显式
通知时才运行。典型用途：

- 修改配置文件后重启服务
- 更新证书后重载 Nginx
- 部署新版本后清理缓存

```yaml
tasks:
  - name: Deploy new config
    template:
      src: nginx.conf.j2
      dest: /etc/nginx/nginx.conf
    notify: restart nginx          # <-- 触发条件

handlers:
  - name: restart nginx            # <-- 名称必须匹配
    service:
      name: nginx
      state: restarted
```

### 1.2 触发条件

一个 Handler 被触发需要同时满足三个条件：

| 条件 | 说明 |
|------|------|
| 任务执行成功 | `failed == false` |
| 任务产生了变更 | `changed == true` |
| 任务声明了 notify | `notify` 字段包含该 handler 名称 |

如果任务执行失败（即使有 notify），Handler 不会被触发。如果任务成功但没有实际
变更（幂等操作检测到状态一致），Handler 也不会被触发。

### 1.3 去重：每个 Handler 最多执行一次

这是 Handler 与普通 Task 最关键的区别。无论多少个任务 notify 同一个 Handler，
该 Handler 在当前 Play 中**只执行一次**。

```yaml
tasks:
  - name: Update config A
    template:
      src: config-a.j2
      dest: /etc/app/config-a
    notify: restart app

  - name: Update config B
    template:
      src: config-b.j2
      dest: /etc/app/config-b
    notify: restart app            # 第二次 notify 同一个 handler

  - name: Update config C
    template:
      src: config-c.j2
      dest: /etc/app/config-c
    notify: restart app            # 第三次

# 结果：restart app 只执行一次，而不是三次
```

去重的实现方式：HandlerManager 维护一个 `pending` 集合（`Set[string]`），
`notify` 时将 handler 名称加入集合，Play 结束时遍历集合执行。

### 1.4 执行时机

Handler 在以下两个时机执行：

**时机一：Play 结束后（默认）**

```
Play 开始
  ├── 执行所有 PreTasks
  ├── 执行所有 Tasks         ← notify 在此发生
  ├── 执行所有 PostTasks
  └── 执行 pending Handlers  ← 在这里批量执行
Play 结束
```

**时机二：显式 flush_handlers**

在 Tasks 中间插入 `meta: flush_handlers`，强制立即执行已 pending 的 Handler：

```yaml
tasks:
  - name: Update web config
    template:
      src: web.conf.j2
      dest: /etc/web/web.conf
    notify: restart web

  - meta: flush_handlers          # <-- 立即执行 pending handlers

  - name: Verify web is running
    uri:
      url: http://localhost/health
    register: health

  - name: Update app config
    template:
      src: app.conf.j2
      dest: /etc/app/app.conf
    notify: restart app
```

这在需要"确保上一步变更生效后再继续"的场景中非常有用。

### 1.5 listen 指令

`listen` 允许一个 Handler 响应多个不同的通知名称：

```yaml
handlers:
  - name: restart all services
    listen: "restart services"     # 通用监听主题
    service:
      name: "{{ item }}"
      state: restarted
    loop:
      - nginx
      - php-fpm
      - redis

tasks:
  - name: Update nginx config
    template:
      src: nginx.conf.j2
      dest: /etc/nginx/nginx.conf
    notify: restart services       # 通过 listen 匹配

  - name: Update redis config
    template:
      src: redis.conf.j2
      dest: /etc/redis/redis.conf
    notify: restart services       # 同一个 listen 主题
```

`listen` 的匹配规则：
- `notify` 值先与 handler 的 `name` 匹配
- 如果没有匹配到 `name`，再与 handler 的 `listen` 匹配
- 一个 handler 可以有多个 `listen` 值

### 1.6 Handler 的限制

- Handler 只在当前 Play 的作用域内有效
- Handler 之间不能相互 notify
- Handler 执行失败会阻止后续 Handler 执行（除非使用 `listen` + `force_handlers`）
- Handler 不能使用 `loop`（某些 Ansible 版本已支持，go-ansible 初期不实现）

---

## 2. Block / Rescue / Always

### 2.1 概念类比

Block 结构直接类比编程语言中的异常处理：

| Ansible | 编程语言 | 说明 |
|---------|---------|------|
| `block` | `try` | 主要执行逻辑 |
| `rescue` | `catch` | block 中任何任务失败时执行 |
| `always` | `finally` | 无论成功失败都执行 |

### 2.2 基本语法

```yaml
- block:
    - name: Attempt risky operation
      shell: /opt/scripts/risky_deploy.sh
      register: deploy_result

    - name: Verify deployment
      uri:
        url: http://localhost/health
        status_code: 200

  rescue:
    - name: Log failure
      debug:
        msg: "Deployment failed: {{ deploy_result.stderr }}"

    - name: Rollback
      shell: /opt/scripts/rollback.sh

  always:
    - name: Cleanup temp files
      file:
        path: /tmp/deploy_workdir
        state: absent

    - name: Send notification
      mail:
        to: ops@example.com
        subject: "Deploy {{ 'succeeded' if deploy_result is succeeded else 'failed' }}"
```

### 2.3 执行流程

```
block 开始
  ├── 任务 1 → 成功
  ├── 任务 2 → 成功
  ├── 任务 3 → 失败！
  │     │
  │     ▼
  │   rescue 开始
  │     ├── 回滚任务 1 → 成功
  │     └── 回滚任务 2 → 成功
  │   rescue 结束
  │
  └── always 开始
        └── 清理任务 → 成功
      always 结束
block 结束
```

关键规则：
- block 中**第一个**失败的任务触发 rescue
- rescue 中的任务失败不会再次触发 rescue（不会递归）
- always 中的任务失败会标记为失败，但 always 继续执行剩余任务
- 整个 block 结构的最终状态取决于 rescue/always 的执行结果

### 2.4 嵌套 Block

Block 可以嵌套，形成多层错误处理：

```yaml
- block:
    - block:
        - name: Try primary method
          shell: /opt/primary.sh

      rescue:
        - name: Fallback to secondary
          shell: /opt/secondary.sh

  rescue:
    - name: Both methods failed
      debug:
        msg: "All attempts exhausted"

  always:
    - name: Final cleanup
      shell: /opt/cleanup.sh
```

嵌套规则：
- 内层 block 的 rescue 优先捕获内层失败
- 如果内层 rescue 也失败，外层 rescue 接管
- always 层层执行，从内到外

### 2.5 变量作用域

Block 结构引入了特殊的变量作用域规则：

```
Play 作用域
  └── Block 作用域
        ├── block 任务定义的变量 → 在 rescue 和 always 中可见
        ├── rescue 任务定义的变量 → 在 always 中可见
        └── always 任务定义的变量 → 仅在 always 中可见
```

**重要**：rescue 中可以访问 block 中 `register` 的变量，即使 block 中的任务
失败了。这允许 rescue 根据失败原因做出不同决策：

```yaml
- block:
    - name: Run command
      shell: some_command
      register: cmd_result

  rescue:
    - name: Check error type
      debug:
        msg: "Exit code was {{ cmd_result.rc }}"
      when: cmd_result.rc is defined
```

### 2.6 Block 与 ignore_errors 的交互

```yaml
# 场景 1：block 中使用 ignore_errors
- block:
    - name: Task that might fail
      shell: risky_command
      ignore_errors: true          # 失败不触发 rescue

  rescue:
    - name: This won't run if above fails with ignore_errors
      debug: msg "Rescue triggered"

# 场景 2：block 整体使用 ignore_errors
- block:
    - name: Task 1
      shell: cmd1
    - name: Task 2
      shell: cmd2
  ignore_errors: true              # block 失败不影响后续任务
```

---

## 3. 错误处理策略

### 3.1 ignore_errors

让任务失败后继续执行后续任务：

```yaml
- name: Check if service exists
  shell: systemctl is-active myservice
  register: service_status
  ignore_errors: true              # 失败也继续

- name: Start service if not running
  service:
    name: myservice
    state: started
  when: service_status.rc != 0
```

**实现要点**：`ignore_errors: true` 时，任务结果标记 `failed=true` 但不传播
失败，主机继续执行下一个任务。

### 3.2 failed_when

自定义失败条件，覆盖模块默认的失败判定：

```yaml
- name: Run migration script
  shell: /opt/migrate.sh
  register: migration
  failed_when:
    - migration.rc != 0
    - "'ALREADY_MIGRATED' not in migration.stdout"
```

`failed_when` 支持：
- 单个布尔表达式
- 布尔表达式列表（全部为 true 时判定失败）
- 引用 `register` 的变量

### 3.3 changed_when

自定义变更条件，覆盖模块默认的变更判定：

```yaml
- name: Run idempotent check
  shell: /opt/check_state.sh
  register: check
  changed_when: false              # 永远标记为 ok，不触发 handler

- name: Conditional change
  shell: /opt/apply_config.sh
  register: apply
  changed_when: "'CHANGED' in apply.stdout"
```

### 3.4 max_fail_percentage

在批量主机执行中，允许一定比例的主机失败：

```yaml
- hosts: webservers
  max_fail_percentage: 30          # 最多 30% 主机可以失败

  tasks:
    - name: Rolling update
      yum:
        name: myapp
        state: latest
```

**执行逻辑**：
1. 所有主机执行完毕后检查失败比例
2. 如果失败比例 <= `max_fail_percentage`，Play 继续
3. 如果失败比例 > `max_fail_percentage`，Play 中止，剩余未执行的任务跳过
4. `max_fail_percentage: 0` 表示不允许任何失败（等同于默认行为）

---

## 4. 错误分类

go-ansible 将所有错误分为四个级别，决定错误的传播范围和处理方式。

### 4.1 FATAL — 致命错误

配置错误、环境问题，导致整个执行立即终止。

| 场景 | 示例 |
|------|------|
| Inventory 文件不存在 | `inventory file not found: hosts.yml` |
| Playbook 语法严重错误 | `unexpected token '<<' in playbook.yml:15` |
| 配置文件损坏 | `invalid ansible.cfg: missing section header` |
| 循环依赖 | `role A depends on B, B depends on A` |
| Vault 密码错误 | `decryption failed: wrong vault password` |

**处理方式**：立即终止所有主机，返回退出码 4（解析错误）或 5（权限错误）。

### 4.2 HOST — 主机级错误

特定主机的连接或执行问题，该主机停止但其他主机继续。

| 场景 | 示例 |
|------|------|
| SSH 连接失败 | `connect to host web1: Connection refused` |
| 认证失败 | `Permission denied (publickey,password)` |
| 连接超时 | `connect to host web1: i/o timeout` |
| 磁盘空间不足 | `No space left on device` |

**处理方式**：标记该主机为 unreachable/failed，其他主机继续执行。

### 4.3 TASK — 任务级错误

模块执行失败，根据 ignore_errors/failed_when/block 等策略处理。

| 场景 | 示例 |
|------|------|
| 模块执行失败 | `non-zero return code` |
| 模板渲染失败 | `undefined variable: 'foo'` |
| 参数校验失败 | `missing required argument: 'name'` |
| 条件判断失败 | `when` 条件引用未定义变量 |

**处理方式**：根据任务配置决定是否忽略、是否触发 rescue。

### 4.4 WARNING — 警告

不阻断执行，但需要用户关注的问题。

| 场景 | 示例 |
|------|------|
| 废弃语法 | `include` is deprecated, use `include_tasks` |
| 未使用变量 | variable 'old_var' is set but not used |
| 重复定义 | handler 'restart' defined multiple times |
| 性能警告 | `loop` with large list (>1000 items) |

**处理方式**：记录日志，继续执行。

### 4.5 错误类型签名

```go
// ErrorCategory 错误级别分类
type ErrorCategory int

const (
    ErrorCategoryWarning ErrorCategory = iota  // 警告，不阻断
    ErrorCategoryTask                           // 任务级，按策略处理
    ErrorCategoryHost                           // 主机级，该主机停止
    ErrorCategoryFatal                          // 致命，全部停止
)

// ExecutionError 统一错误类型
type ExecutionError struct {
    Category  ErrorCategory
    Message   string
    Host      string        // HOST/TASK 级别时有值
    TaskName  string        // TASK 级别时有值
    FileName  string        // 解析错误时有值
    Line      int           // 解析错误时有值
    Cause     error         // 原始错误
}
```

---

## 5. 退出码

go-ansible 的退出码遵循 Ansible 兼容规范，便于 CI/CD 集成：

| 退出码 | 含义 | 触发条件 |
|--------|------|----------|
| 0 | 成功 | 所有主机所有任务成功 |
| 1 | 一般错误 | 其他未分类错误 |
| 2 | 主机失败 | 至少一台主机有任务失败 |
| 3 | 主机不可达 | 至少一台主机 SSH 连接失败 |
| 4 | 解析错误 | Playbook/Inventory 语法错误 |
| 5 | 权限错误 | Vault 密码错误、权限不足 |

**退出码判定优先级**：从高到低，第一个匹配的条件生效。

```go
// ExitCode 退出码类型
type ExitCode int

const (
    ExitCodeSuccess         ExitCode = 0
    ExitCodeGeneralError    ExitCode = 1
    ExitCodeHostFailed      ExitCode = 2
    ExitCodeHostUnreachable ExitCode = 3
    ExitCodeParseError      ExitCode = 4
    ExitCodePermissionError ExitCode = 5
)

// DetermineExitCode 根据执行结果确定退出码
func DetermineExitCode(stats PlayStats) ExitCode
```

**CI/CD 集成示例**：

```bash
#!/bin/bash
go-ansible playbook deploy.yml
EXIT_CODE=$?

case $EXIT_CODE in
    0) echo "Deploy succeeded" ;;
    2) echo "Some hosts failed, check logs" ;;
    3) echo "Some hosts unreachable, check SSH" ;;
    4) echo "Playbook syntax error" ;;
    *) echo "Unknown error: $EXIT_CODE" ;;
esac

exit $EXIT_CODE
```

---

## 6. Retry 机制

### 6.1 until / retries / delay

任务级别的重试机制，用于处理临时性失败：

```yaml
- name: Wait for service to be ready
  uri:
    url: http://localhost:8080/health
    status_code: 200
  register: health_check
  until: health_check.status == 200   # 重试条件
  retries: 30                          # 最多重试 30 次
  delay: 10                            # 每次间隔 10 秒
```

**执行逻辑**：

```
第 1 次执行 → 失败 → 检查 until 条件 → 不满足
  → 等待 delay 秒
第 2 次执行 → 失败 → 检查 until 条件 → 不满足
  → 等待 delay 秒
...
第 N 次执行 → 成功 → 检查 until 条件 → 满足 → 继续
第 retries 次执行 → 仍失败 → 标记任务失败
```

### 6.2 until 条件的灵活性

```yaml
# 简单条件
- shell: check_status.sh
  register: result
  until: result.rc == 0
  retries: 5
  delay: 3

# 复合条件
- shell: get_nodes.sh
  register: nodes
  until:
    - nodes.rc == 0
    - "'ready' in nodes.stdout"
  retries: 10
  delay: 5

# 引用变量
- uri:
    url: "http://{{ inventory_hostname }}:{{ app_port }}/health"
  register: health
  until: health.status == 200
  retries: "{{ health_check_retries | default(30) }}"
  delay: "{{ health_check_delay | default(10) }}"
```

### 6.3 Retry 文件

当 Playbook 执行有主机失败时，go-ansible 自动生成 retry 文件：

```
# deploy.retry
web1
web3
db2
```

**文件名规则**：`<playbook_name>.retry`，与 playbook 文件同目录。

**重试执行**：

```bash
# 原始执行
go-ansible playbook deploy.yml
# 输出：ERROR! 3 hosts failed

# 仅重试失败的主机
go-ansible playbook deploy.yml --limit @deploy.retry
```

### 6.4 Retry 文件的生命周期

```
Playbook 开始执行
  ├── 删除已有的 .retry 文件（如果存在）
  ├── 执行所有任务
  └── 执行结束
        ├── 有失败主机 → 生成 .retry 文件
        └── 无失败 → 不生成文件
```

### 6.5 Go 实现签名

```go
// RetryPolicy 重试策略
type RetryPolicy struct {
    Until    []string      // 重试条件表达式列表
    Retries  int           // 最大重试次数
    Delay    time.Duration // 重试间隔
}

// RetryExecutor 重试执行器
type RetryExecutor struct {
    policy   RetryPolicy
    attempts int
}

// Execute 执行带重试的任务
func (r *RetryExecutor) Execute(
    ctx context.Context,
    task func() (TaskResult, error),
) (TaskResult, error)

// ShouldRetry 判断是否应该继续重试
func (r *RetryExecutor) ShouldRetry(result TaskResult) bool

// RetryFileWriter 写入 retry 文件
type RetryFileWriter struct{}

// Write 写入失败主机列表到 retry 文件
func (w *RetryFileWriter) Write(playbookPath string, failedHosts []string) error
```

---

## 7. Go 实现要点

### 7.1 HandlerManager

```go
// HandlerManager 管理 Handler 的注册、通知和执行
type HandlerManager struct {
    handlers    map[string]Task     // name → handler 定义
    listenMap   map[string][]string // listen主题 → handler名称列表
    pending     map[string]struct{} // 待执行的 handler 名称集合
    executed    map[string]struct{} // 已执行的 handler 名称集合（去重）
    mu          sync.Mutex          // 并发安全
}

// NewHandlerManager 创建 HandlerManager
func NewHandlerManager(handlerDefs []Task) *HandlerManager

// Notify 通知 handler（任务成功且 changed 时调用）
func (m *HandlerManager) Notify(name string)

// Flush 执行所有 pending 的 handler，返回执行结果
func (m *HandlerManager) Flush(
    ctx context.Context,
    executor TaskExecutor,
    playCtx PlayContext,
) ([]TaskResult, error)

// HasPending 检查是否有待执行的 handler
func (m *HandlerManager) HasPending() bool

// Reset 重置状态（新 Play 开始时调用）
func (m *HandlerManager) Reset()
```

### 7.2 BlockExecutor

```go
// BlockExecutor 执行 Block/Rescue/Always 结构
type BlockExecutor struct{}

// Execute 执行一个完整的 block 结构
func (e *BlockExecutor) Execute(
    ctx context.Context,
    block Block,
    executor TaskExecutor,
    playCtx PlayContext,
) BlockResult

// BlockResult block 执行结果
type BlockResult struct {
    BlockResults   []TaskResult  // block 中的任务结果
    RescueResults  []TaskResult  // rescue 中的任务结果
    AlwaysResults  []TaskResult  // always 中的任务结果
    Failed         bool          // 整体是否失败
    Changed        bool          // 整体是否产生变更
    RescueTriggered bool         // rescue 是否被触发
}
```

### 7.3 ErrorHandler

```go
// ErrorHandler 统一错误处理器
type ErrorHandler struct {
    logger      Logger
    callbackMgr CallbackManager
}

// HandleTaskError 处理任务级错误
func (h *ErrorHandler) HandleTaskError(
    result TaskResult,
    task Task,
    playCtx PlayContext,
) ErrorAction

// ErrorAction 错误处理动作
type ErrorAction int

const (
    ErrorActionContinue  ErrorAction = iota // 忽略错误继续
    ErrorActionRescue                       // 进入 rescue 块
    ErrorActionStopHost                     // 停止该主机
    ErrorActionStopAll                      // 停止所有主机
)
```

### 7.4 回调插件集成

Handler 和错误处理过程中需要通知回调插件：

```go
// CallbackManager 回调管理器接口
type CallbackManager interface {
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

Handler 执行时，`OnTaskStart` 的 `isHandler` 参数为 `true`，输出格式与普通
任务不同（显示 `RUNNING HANDLER` 而不是 `TASK`）。

---

## 8. 任务拆解

### T8.1 Handler 机制

| 子任务 | 描述 | 依赖 | 验收标准 |
|--------|------|------|----------|
| T8.1.1 | Handler 定义解析 | P5 Playbook 解析 | 能从 YAML 中解析 handlers 段 |
| T8.1.2 | HandlerManager 实现 | T8.1.1 | 注册、通知、去重逻辑通过单元测试 |
| T8.1.3 | notify 触发逻辑 | T8.1.2, P5 Task 执行 | 任务成功且 changed 时正确触发 |
| T8.1.4 | Play 结束执行 Handler | T8.1.2, P5 策略引擎 | Play 结束后正确执行 pending handler |
| T8.1.5 | flush_handlers 支持 | T8.1.2 | meta: flush_handlers 立即执行 |
| T8.1.6 | listen 指令支持 | T8.1.2 | listen 匹配逻辑正确 |
| T8.1.7 | Handler 回调输出 | T8.1.4, P12 回调插件 | 显示 RUNNING HANDLER 前缀 |

**单元测试覆盖**：
- notify 去重：多次 notify 同一 handler，只执行一次
- listen 匹配：通过 listen 名称触发 handler
- flush_handlers：在 tasks 中间强制执行
- 未触发 handler：changed=false 时不触发

### T8.2 Block / Rescue / Always

| 子任务 | 描述 | 依赖 | 验收标准 |
|--------|------|------|----------|
| T8.2.1 | Block YAML 解析 | P5 Playbook 解析 | block/rescue/always 结构正确解析 |
| T8.2.2 | BlockExecutor 实现 | T8.2.1 | block/rescue/always 执行逻辑通过单元测试 |
| T8.2.3 | 嵌套 Block 支持 | T8.2.2 | 多层嵌套时 rescue 传播正确 |
| T8.2.4 | 变量作用域 | T8.2.2, P3 变量系统 | block 变量在 rescue/always 中可见 |
| T8.2.5 | ignore_errors 集成 | T8.2.2 | ignore_errors 不触发 rescue |
| T8.2.6 | failed_when/changed_when | T8.2.2 | 自定义条件覆盖默认判定 |

**单元测试覆盖**：
- 正常执行：block 全部成功，不执行 rescue
- 触发 rescue：block 中任务失败，rescue 正确执行
- always 总是执行：无论成功失败
- 嵌套 block：内层 rescue 优先
- 变量传递：block 中 register 的变量在 rescue 中可用

---

## 附录：Playbook 语法速查

### Handler 完整示例

```yaml
---
- hosts: webservers
  handlers:
    - name: restart nginx
      service:
        name: nginx
        state: restarted

    - name: reload nginx
      service:
        name: nginx
        state: reloaded
      listen: "reload web services"

    - name: clear cache
      shell: rm -rf /var/cache/nginx/*

  tasks:
    - name: Deploy nginx config
      template:
        src: nginx.conf.j2
        dest: /etc/nginx/nginx.conf
      notify: restart nginx

    - name: Deploy SSL cert
      copy:
        src: "{{ vault_ssl_cert }}"
        dest: /etc/nginx/ssl/cert.pem
      notify: reload web services

    - name: Update app
      git:
        repo: https://github.com/example/app.git
        dest: /var/www/app
      notify:
        - restart nginx
        - clear cache
```

### Block / Rescue / Always 完整示例

```yaml
---
- hosts: dbservers
  tasks:
    - name: Database migration
      block:
        - name: Backup database
          shell: pg_dump mydb > /tmp/mydb_backup.sql
          register: backup

        - name: Run migration
          command: python manage.py migrate
          register: migration
          environment:
            DJANGO_SETTINGS_MODULE: myapp.settings.prod

        - name: Verify migration
          shell: python manage.py showmigrations --plan | grep -c '\[X\]'
          register: verify

      rescue:
        - name: Log migration failure
          debug:
            msg: |
              Migration failed!
              Backup: {{ backup.stderr | default('ok') }}
              Migration: {{ migration.stderr | default('unknown') }}

        - name: Restore from backup
          shell: psql mydb < /tmp/mydb_backup.sql
          when: backup.rc == 0

        - name: Alert on call team
          mail:
            to: oncall@example.com
            subject: "DB migration failed on {{ inventory_hostname }}"
            body: "{{ migration.stderr }}"

      always:
        - name: Archive backup
          copy:
            src: /tmp/mydb_backup.sql
            dest: "/backups/{{ ansible_date_time.date }}_mydb.sql"
          ignore_errors: true

        - name: Cleanup temp files
          file:
            path: /tmp/mydb_backup.sql
            state: absent
```

### 重试机制示例

```yaml
---
- hosts: webservers
  tasks:
    - name: Wait for database to be ready
      uri:
        url: "http://{{ db_host }}:5432/health"
        status_code: 200
      register: db_health
      until: db_health.status == 200
      retries: 30
      delay: 10

    - name: Wait for all nodes to join cluster
      shell: consul members | grep -c alive
      register: members
      until: members.stdout | int >= groups['consul'] | length
      retries: 12
      delay: 5

    - name: Wait for service deployment
      uri:
        url: "http://{{ inventory_hostname }}:{{ app_port }}/version"
      register: version
      until:
        - version.status == 200
        - version.json.version == target_version
      retries: 60
      delay: 5
```
