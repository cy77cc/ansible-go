# 10 - 异步任务

> 阶段：P9 | 设计文档引用：第十七章

本章介绍 go-ansible 的异步任务执行机制。异步任务允许 Playbook 启动长时间运行
的操作而不阻塞整个执行流程，是处理数据库迁移、大规模批量操作、编译构建等场景
的核心能力。

---

## 1. 异步任务场景

### 1.1 为什么需要异步

标准的 Ansible 任务执行是同步阻塞的：SSH 到远程主机 → 执行命令 → 等待完成 →
返回结果。这在以下场景中会出问题：

| 场景 | 耗时 | 同步执行的问题 |
|------|------|---------------|
| 数据库迁移 | 30min - 2h | SSH 连接超时断开 |
| 全量索引重建 | 1h - 6h | Playbook 执行超时 |
| 批量文件处理 | 10min - 1h | 阻塞后续主机执行 |
| 系统升级 | 20min - 40min | 需要重启后继续验证 |
| 编译大型项目 | 15min - 2h | 连接不稳定导致中断 |

### 1.2 异步任务的核心思路

```
传统同步：
  本地 ──SSH──▶ 远程执行命令 ──等待──▶ 返回结果
  │←──── 可能数小时 ──────→│

异步模式：
  本地 ──SSH──▶ 远程启动后台进程 → 写入状态文件 → 返回 job_id
  │←── 几秒钟 ──→│
  
  ... 本地可以做其他事情 ...

  本地 ──SSH──▶ 读取状态文件 → 返回进度/结果
  │←── 几秒钟 ──→│
```

关键设计：将长时间命令交给远程主机的后台进程执行，本地只负责启动和轮询状态。

---

## 2. 两种执行模式

### 2.1 poll > 0：阻塞轮询模式

```yaml
- name: Run long migration
  shell: /opt/scripts/migrate.sh
  async: 3600        # 超时限制：3600 秒（1 小时）
  poll: 30           # 每 30 秒轮询一次状态
  register: result
```

**执行流程**：

```
1. SSH 到远程主机
2. 通过 nohup 启动后台进程，写入状态文件
3. 返回 job_id
4. 每隔 poll 秒 SSH 连接检查状态文件
5. 重复步骤 4 直到：
   a. 任务完成 → 返回结果
   b. 超时（async 秒）→ 发送 SIGTERM → 等待 → SIGKILL
```

**适用场景**：需要等待结果但可以容忍轮询间隔的场景。Playbook 会阻塞在这个任务
上，但不会因为长时间等待而丢失 SSH 连接。

### 2.2 poll = 0：即发即忘模式

```yaml
- name: Fire and forget - start rebuild
  shell: /opt/scripts/rebuild_index.sh
  async: 3600        # 超时限制：3600 秒
  poll: 0            # 立即返回，不等待
  register: job

# 后续可以做其他事情
- name: Do other work while rebuild runs
  shell: /opt/scripts/other_task.sh

# 稍后检查异步任务状态
- name: Check rebuild status
  async_status:
    jid: "{{ job.ansible_job_id }}"
  register: job_result
  until: job_result.finished
  retries: 60
  delay: 60
```

**执行流程**：

```
1. SSH 到远程主机
2. 通过 nohup 启动后台进程，写入状态文件
3. 返回 job_id 和 started 状态
4. Playbook 立即执行下一个任务
   ... 中间可以执行其他任务 ...
5. 通过 async_status 模块检查状态（任意时刻）
6. async_status 返回：started / finished / failed
```

**适用场景**：不需要立即知道结果，可以并行做其他事情。

### 2.3 模式对比

| 特性 | poll > 0 | poll = 0 |
|------|----------|----------|
| 是否阻塞 | 阻塞直到完成/超时 | 立即返回 |
| SSH 连接数 | 持续占用（轮询） | 按需连接 |
| 结果获取 | 自动 | 手动通过 async_status |
| 超时处理 | 自动 SIGTERM/SIGKILL | 需手动处理 |
| 适用场景 | 需要等待结果 | 并行执行其他任务 |
| 典型 poll 值 | 10-60 秒 | 0 |

---

## 3. 执行模型详解

### 3.1 远程后台进程启动

go-ansible 在远程主机上启动异步任务时，执行以下步骤：

```bash
# 1. 创建临时目录
mkdir -p ~/.ansible_async

# 2. 启动后台进程（nohup + shell 封装）
nohup sh -c '
  echo '{"started": true, "finished": false, "job_id": "12345"}' > ~/.ansible_async/12345
  # 执行实际命令
  /opt/scripts/migrate.sh > /tmp/ansible_async_12345_stdout 2>&1
  EXIT_CODE=$?
  # 更新状态文件
  echo "{\"finished\": true, \"rc\": $EXIT_CODE, ...}" > ~/.ansible_async/12345
' > /dev/null 2>&1 &

echo $!   # 返回后台进程 PID
```

### 3.2 状态文件格式

异步任务在远程主机上维护一个 JSON 状态文件：

```json
{
  "started": 1685000000.0,
  "finished": 1685003600.0,
  "ansible_job_id": "12345.67890",
  "rc": 0,
  "stdout": "Migration completed successfully",
  "stderr": "",
  "msg": "",
  "start": "2026-05-25 10:00:00",
  "end": "2026-05-25 11:00:00"
}
```

状态文件路径：`~/.ansible_async/<job_id>`

### 3.3 状态轮询

当 `poll > 0` 时，主执行流程按以下循环轮询：

```
for {
    // 等待 poll 间隔
    time.Sleep(pollInterval)
    
    // 检查是否超时
    if time.Since(startTime) > asyncTimeout {
        sendSIGTERM(pid)
        waitGracePeriod()
        sendSIGKILL(pid)
        return TimeoutError
    }
    
    // SSH 读取状态文件
    status := readStatusFile(jobID)
    
    if status.Finished {
        return status.Result
    }
    
    // 未完成，继续轮询
}
```

### 3.4 超时处理

异步任务的超时处理分三个阶段：

```
async 时间到达
  │
  ├── 1. 发送 SIGTERM（优雅终止）
  │     等待 5 秒
  │
  ├── 2. 检查进程是否已退出
  │     ├── 已退出 → 收集结果
  │     └── 未退出 → 继续
  │
  └── 3. 发送 SIGKILL（强制终止）
        收集部分结果
```

### 3.5 完成时自动清理

任务完成后（无论成功或失败），go-ansible 会自动清理远程状态文件：

```
任务完成
  ├── 读取最终结果
  ├── 删除状态文件：rm ~/.ansible_async/<job_id>
  └── 返回结果给 Playbook
```

---

## 4. async_status 模块

### 4.1 模块功能

`async_status` 是一个内置模块，用于查询异步任务的状态。它本身不执行任何操作，
只读取远程主机上的状态文件。

### 4.2 两种模式

**check 模式（默认）**—— 查询任务状态：

```yaml
- name: Check async task status
  async_status:
    jid: "{{ job.ansible_job_id }}"
  register: result
```

返回结果：

```json
{
  "started": true,
  "finished": false,
  "ansible_job_id": "12345.67890"
}
```

任务完成后：

```json
{
  "started": true,
  "finished": true,
  "ansible_job_id": "12345.67890",
  "rc": 0,
  "stdout": "All done",
  "stderr": "",
  "msg": ""
}
```

**cleanup 模式**—— 清理状态文件：

```yaml
- name: Clean up async task state
  async_status:
    jid: "{{ job.ansible_job_id }}"
    mode: cleanup
```

用于手动清理不再需要的状态文件。

### 4.3 典型使用模式

```yaml
# 模式 1：轮询等待完成
- name: Start long task
  shell: /opt/build.sh
  async: 7200
  poll: 0
  register: build_job

- name: Wait for build to complete
  async_status:
    jid: "{{ build_job.ansible_job_id }}"
  register: build_result
  until: build_result.finished
  retries: 120
  delay: 60

- name: Check build result
  fail:
    msg: "Build failed with rc={{ build_result.rc }}"
  when: build_result.rc != 0

# 模式 2：并行执行多个异步任务
- name: Start rebuild on all nodes
  shell: /opt/rebuild.sh
  async: 3600
  poll: 0
  register: rebuild_jobs
  loop: "{{ groups['workers'] }}"

- name: Wait for all rebuilds
  async_status:
    jid: "{{ item.ansible_job_id }}"
  register: rebuild_results
  until: rebuild_results.finished
  retries: 60
  delay: 60
  loop: "{{ rebuild_jobs.results }}"

# 模式 3：即发即忘（不关心结果）
- name: Clear remote cache (fire and forget)
  shell: rm -rf /var/cache/app/*
  async: 300
  poll: 0
```

---

## 5. 超时与清理

### 5.1 async 参数即超时

`async` 参数的值同时作为超时限制（秒）。这是一个容易误解的点：

```yaml
- shell: long_running_task.sh
  async: 600    # 超时限制：600 秒（10 分钟）
  poll: 10      # 每 10 秒检查一次
```

如果任务在 600 秒内未完成，go-ansible 会：
1. 发送 SIGTERM 给远程进程
2. 等待一小段时间（默认 5 秒）
3. 如果仍未退出，发送 SIGKILL

### 5.2 超时建议值

| 场景 | 建议 async 值 | 建议 poll 值 |
|------|--------------|-------------|
| 短暂后台任务 | 300 (5min) | 5 |
| 数据库迁移 | 3600 (1h) | 30 |
| 全量索引 | 7200 (2h) | 60 |
| 系统升级 | 3600 (1h) | 0 (fire-and-forget) |
| 编译构建 | 14400 (4h) | 0 |

**原则**：async 值应该比预期完成时间多出 50% 以上的缓冲。

### 5.3 状态文件清理策略

| 时机 | 行为 |
|------|------|
| poll > 0 任务完成 | 自动清理 |
| poll > 0 超时 | 自动清理 |
| poll = 0 任务完成 | 需通过 async_status cleanup 手动清理 |
| async_status check 完成 | 可选自动清理 |

### 5.4 清理失败的处理

如果状态文件清理失败（SSH 断开等），文件会残留在远程主机上。这不会影响功能，
但会占用少量磁盘空间。可以定期清理：

```yaml
- name: Cleanup stale async files
  shell: find ~/.ansible_async -mtime +7 -delete
  become: true
```

---

## 6. Go 实现要点

### 6.1 AsyncManager

```go
// AsyncManager 管理异步任务的生命周期
type AsyncManager struct {
    jobs    map[string]*AsyncJob  // job_id → 任务信息
    mu      sync.RWMutex
}

// AsyncJob 异步任务信息
type AsyncJob struct {
    ID        string
    Host      string
    Command   string
    StartedAt time.Time
    Timeout   time.Duration
    Poll      time.Duration
    Status    AsyncStatus
}

// AsyncStatus 异步任务状态
type AsyncStatus int

const (
    AsyncStatusPending   AsyncStatus = iota // 等待启动
    AsyncStatusRunning                      // 运行中
    AsyncStatusFinished                     // 已完成
    AsyncStatusFailed                       // 失败
    AsyncStatusTimeout                      // 超时
)

// AsyncResult 异步任务结果
type AsyncResult struct {
    JobID    string `json:"ansible_job_id"`
    Started  bool   `json:"started"`
    Finished bool   `json:"finished"`
    RC       int    `json:"rc,omitempty"`
    Stdout   string `json:"stdout,omitempty"`
    Stderr   string `json:"stderr,omitempty"`
    Msg      string `json:"msg,omitempty"`
    Start    string `json:"start,omitempty"`
    End      string `json:"end,omitempty"`
}

// Launch 启动异步任务
func (m *AsyncManager) Launch(
    ctx context.Context,
    conn Connection,
    cmd string,
    timeout time.Duration,
) (*AsyncResult, error)

// Poll 轮询异步任务状态
func (m *AsyncManager) Poll(
    ctx context.Context,
    conn Connection,
    jobID string,
) (*AsyncResult, error)

// Cleanup 清理远程状态文件
func (m *AsyncManager) Cleanup(
    ctx context.Context,
    conn Connection,
    jobID string,
) error
```

### 6.2 异步执行的 goroutine 模型

```go
// AsyncExecutor 异步任务执行器
type AsyncExecutor struct {
    conn      Connection
    manager   *AsyncManager
    callback  CallbackManager
}

// ExecuteAsync 执行异步任务
func (e *AsyncExecutor) ExecuteAsync(
    ctx context.Context,
    task Task,
    playCtx PlayContext,
) (*AsyncResult, error)

// ExecuteWithPoll 执行带轮询的异步任务（poll > 0）
func (e *AsyncExecutor) ExecuteWithPoll(
    ctx context.Context,
    task Task,
    playCtx PlayContext,
) (TaskResult, error)

// ExecuteFireAndForget 执行即发即忘任务（poll = 0）
func (e *AsyncExecutor) ExecuteFireAndForget(
    ctx context.Context,
    task Task,
    playCtx PlayContext,
) (TaskResult, error)
```

### 6.3 context 取消支持

异步任务的轮询和等待支持 Go context 取消，用于处理 Ctrl+C 中断：

```go
// PollWithContext 带取消支持的轮询
func (m *AsyncManager) PollWithContext(
    ctx context.Context,
    conn Connection,
    jobID string,
    interval time.Duration,
) (*AsyncResult, error) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            // 用户取消，尝试清理远程进程
            m.terminateJob(ctx, conn, jobID)
            return nil, ctx.Err()
            
        case <-ticker.C:
            result, err := m.Poll(ctx, conn, jobID)
            if err != nil {
                return nil, err
            }
            if result.Finished {
                return result, nil
            }
        }
    }
}

// terminateJob 终止远程异步任务
func (m *AsyncManager) terminateJob(
    ctx context.Context,
    conn Connection,
    jobID string,
) error {
    // 1. 读取状态文件获取 PID
    // 2. 发送 SIGTERM
    // 3. 等待短暂时间
    // 4. 发送 SIGKILL
    return nil
}
```

### 6.4 async_status 模块

```go
// AsyncStatusModule async_status 模块
type AsyncStatusModule struct{}

// Name 返回模块名称
func (m *AsyncStatusModule) Name() string { return "async_status" }

// Run 执行模块
func (m *AsyncStatusModule) Run(ctx ExecContext) (Result, error) {
    jid := ctx.Args["jid"].(string)
    mode := "check"
    if v, ok := ctx.Args["mode"]; ok {
        mode = v.(string)
    }
    
    switch mode {
    case "check":
        return m.checkStatus(ctx, jid)
    case "cleanup":
        return m.cleanup(ctx, jid)
    default:
        return Result{Failed: true, Msg: "invalid mode"}, nil
    }
}

// checkStatus 检查异步任务状态
func (m *AsyncStatusModule) checkStatus(ctx ExecContext, jid string) (Result, error)

// cleanup 清理异步任务状态文件
func (m *AsyncStatusModule) cleanup(ctx ExecContext, jid string) (Result, error)
```

---

## 7. 任务拆解

### T9.1 Async/Poll 执行模型

| 子任务 | 描述 | 依赖 | 验收标准 |
|--------|------|------|----------|
| T9.1.1 | 异步任务远程启动 | P2 连接层 | 通过 SSH 在远程启动后台进程，返回 job_id |
| T9.1.2 | 状态文件读取 | T9.1.1 | 能读取远程 JSON 状态文件并解析 |
| T9.1.3 | poll > 0 轮询逻辑 | T9.1.1, T9.1.2 | 按间隔轮询，超时发送 SIGTERM/SIGKILL |
| T9.1.4 | poll = 0 立即返回 | T9.1.1 | 启动后立即返回 job_id，不阻塞 |
| T9.1.5 | async_status 模块 | T9.1.2 | check/cleanup 两种模式正确实现 |
| T9.1.6 | 超时与信号处理 | T9.1.3 | SIGTERM 后优雅退出，SIGKILL 强制终止 |
| T9.1.7 | 状态文件自动清理 | T9.1.1 | 任务完成后自动清理远程状态文件 |
| T9.1.8 | context 取消支持 | T9.1.3 | Ctrl+C 时正确取消轮询并清理远程进程 |

**单元测试覆盖**：
- 异步启动：验证后台进程启动命令生成正确
- 状态轮询：模拟状态文件从 started → finished 的变化
- 超时处理：模拟超时场景，验证 SIGTERM/SIGKILL 发送
- 即发即忘：验证 poll=0 时立即返回
- async_status：check 和 cleanup 模式
- context 取消：验证取消时的清理逻辑

**集成测试场景**：
- 完整异步流程：启动 → 轮询 → 完成 → 清理
- 超时流程：启动 → 超时 → SIGTERM → SIGKILL
- 并行异步：同时启动多个异步任务
- fire-and-forget + 手动检查：启动 → 其他任务 → async_status 检查

---

## 附录：Playbook 语法速查

### 阻塞轮询示例

```yaml
---
- hosts: dbservers
  tasks:
    - name: Run database migration
      shell: |
        cd /opt/myapp
        python manage.py migrate --no-input
      async: 3600
      poll: 30
      register: migration_result

    - name: Display migration output
      debug:
        var: migration_result.stdout_lines
```

### 即发即忘 + 手动检查示例

```yaml
---
- hosts: webservers
  tasks:
    - name: Start index rebuild (fire and forget)
      shell: /opt/search/rebuild_index.sh
      async: 7200
      poll: 0
      register: rebuild_job

    - name: Deploy application while index rebuilds
      include_tasks: deploy.yml

    - name: Run smoke tests
      include_tasks: smoke_test.yml

    - name: Wait for index rebuild to complete
      async_status:
        jid: "{{ rebuild_job.ansible_job_id }}"
      register: rebuild_result
      until: rebuild_result.finished
      retries: 120
      delay: 60

    - name: Verify index rebuild succeeded
      fail:
        msg: "Index rebuild failed: {{ rebuild_result.stderr }}"
      when: rebuild_result.rc is defined and rebuild_result.rc != 0

    - name: Activate new index
      shell: /opt/search/activate_index.sh
      when: rebuild_result.finished
```

### 并行异步任务示例

```yaml
---
- hosts: workers
  serial: "100%"
  tasks:
    - name: Start parallel processing on all workers
      shell: /opt/processing/run_batch.sh
      async: 3600
      poll: 0
      register: batch_job

    - name: Wait for all workers to finish
      async_status:
        jid: "{{ batch_job.ansible_job_id }}"
      register: batch_result
      until: batch_result.finished
      retries: 60
      delay: 60

    - name: Report results
      debug:
        msg: "{{ inventory_hostname }}: rc={{ batch_result.rc }}"
```

### 超时处理示例

```yaml
---
- hosts: build_servers
  tasks:
    - name: Build project with generous timeout
      shell: |
        cd /opt/project
        make clean && make all
      async: 14400       # 4 小时超时
      poll: 60           # 每分钟检查一次
      register: build

    - name: Build result
      debug:
        msg: >
          Build {{ 'succeeded' if build.rc == 0 else 'failed' }}
          after {{ build.end }}
```
