# 测试策略与 E2E 测试

> 阶段：P14 | 设计文档引用：第二十章

本文件描述 ansible-go 项目的完整测试策略，包括测试分层、单元测试模式、模块测试、集成测试、E2E 测试、测试辅助设施以及质量保障。

---

## 目录

1. [测试分层策略](#1-测试分层策略)
2. [单元测试模式](#2-单元测试模式)
3. [模块测试策略](#3-模块测试策略)
4. [集成测试](#4-集成测试)
5. [E2E 测试](#5-e2e-测试)
6. [测试辅助设施](#6-测试辅助设施)
7. [覆盖率与质量](#7-覆盖率与质量)
8. [Go 实现要点](#8-go-实现要点)
9. [任务拆解](#9-任务拆解)

---

## 1. 测试分层策略

### 1.1 三层测试模型

ansible-go 采用经典的三层测试金字塔：

```
          ┌─────────┐
          │  E2E    │   少量（~10 个关键场景）
          │  测试   │   完整 playbook 执行
          ├─────────┤
          │ 集成测试 │   适量（~50 个场景）
          │         │   模块组合、引擎流程
          ├─────────┤
          │ 单元测试 │   大量（~500+ 用例）
          │         │   每个包独立测试
          └─────────┘
```

### 1.2 各层职责

| 层级 | 职责 | 速度 | 数量 | 依赖 |
|------|------|------|------|------|
| 单元测试 | 验证单个函数/类型的正确性 | 快（毫秒级） | 大量 | 无外部依赖 |
| 集成测试 | 验证多个组件协作 | 中等（秒级） | 适量 | mock SSH |
| E2E 测试 | 验证完整用户场景 | 慢（分钟级） | 少量 | Docker/真实 SSH |

### 1.3 覆盖率目标

| 包 | 目标覆盖率 | 重点 |
|---|-----------|------|
| inventory | 90% | INI/YAML 解析、主机模式匹配 |
| variables | 90% | 优先级合并、深度合并、并发安全 |
| template | 90% | 渲染、前缀预处理、Sprig 函数 |
| connection | 80% | SSH 连接、认证、文件传输（mock） |
| modules | 80% | 参数校验、执行、CheckMode、幂等性 |
| engine | 85% | 完整 Playbook 流程、handler、block |
| vault | 90% | 加解密往返、Vault ID、密码来源 |
| strategy | 80% | Linear/Free 调度、并发控制 |

**整体目标**：所有包覆盖率 >= 80%，关键包 >= 90%。

### 1.4 测试命令

```bash
make test               # 所有单元测试
make test-coverage      # 覆盖率报告
make test-race          # 竞态检测
make test-e2e           # E2E 测试
make bench              # 基准测试
```

---

## 2. 单元测试模式

### 2.1 表驱动测试（Table-Driven Tests）

ansible-go 统一使用 Go 标准的表驱动测试模式：

```go
// 示例：inventory 包的主机模式匹配测试
func TestMatchHostPattern(t *testing.T) {
    tests := []struct {
        name     string   // 测试用例名称
        pattern  string   // 主机模式
        host     string   // 目标主机
        expected bool     // 期望结果
    }{
        {
            name:     "exact match",
            pattern:  "web1",
            host:     "web1",
            expected: true,
        },
        {
            name:     "wildcard match",
            pattern:  "web*",
            host:     "web1",
            expected: true,
        },
        {
            name:     "no match",
            pattern:  "web*",
            host:     "db1",
            expected: false,
        },
        {
            name:     "group pattern",
            pattern:  "webservers",
            host:     "web1",
            expected: true, // 假设 web1 在 webservers 组
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := MatchHostPattern(tt.pattern, tt.host)
            if result != tt.expected {
                t.Errorf("MatchHostPattern(%q, %q) = %v, want %v",
                    tt.pattern, tt.host, result, tt.expected)
            }
        })
    }
}
```

**表驱动测试的优势**：
- 新增用例只需添加 struct 实例
- 测试输出清晰标识失败用例
- 便于测试边界条件和错误路径
- Go 社区标准实践

### 2.2 Mock Connection 接口

connection 层通过接口解耦，测试时使用 mock：

```go
// Connection 接口定义
type Connection interface {
    // Connect 建立连接。
    Connect(host string, port int, user string) error

    // ExecCommand 在远程主机执行命令。
    ExecCommand(cmd string) (stdout, stderr string, rc int, err error)

    // CopyFile 将本地文件复制到远程主机。
    CopyFile(localPath, remotePath string, mode fs.FileMode) error

    // FetchFile 从远程主机获取文件。
    FetchFile(remotePath, localPath string) error

    // Close 关闭连接。
    Close() error
}

// MockConnection 用于测试的 mock 实现。
type MockConnection struct {
    // ExecCommandFunc 可自定义的命令执行函数。
    ExecCommandFunc func(cmd string) (stdout, stderr string, rc int, err error)

    // CopyFileFunc 可自定义的文件复制函数。
    CopyFileFunc func(localPath, remotePath string, mode fs.FileMode) error

    // RecordedCommands 记录所有执行过的命令。
    RecordedCommands []string

    // ConnectedHost 记录连接的主机。
    ConnectedHost string
}
```

**Mock 使用示例**：

```go
func TestShellModule(t *testing.T) {
    mock := &MockConnection{
        ExecCommandFunc: func(cmd string) (string, string, int, error) {
            if strings.Contains(cmd, "nginx") {
                return "nginx is running", "", 0, nil
            }
            return "", "command not found", 1, nil
        },
    }

    module := NewShellModule(mock)
    result, err := module.Execute(map[string]any{
        "cmd": "systemctl status nginx",
    })

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result.Changed {
        t.Error("expected Changed=false")
    }
    // 验证执行的命令
    if len(mock.RecordedCommands) != 1 {
        t.Fatalf("expected 1 command, got %d", len(mock.RecordedCommands))
    }
}
```

### 2.3 testdata/ fixtures

每个包使用 `testdata/` 目录存放测试数据：

```
internal/
├── inventory/
│   ├── testdata/
│   │   ├── valid.ini            # 有效 INI inventory
│   │   ├── valid.yaml           # 有效 YAML inventory
│   │   ├── invalid.ini          # 格式错误的 INI
│   │   ├── empty.ini            # 空文件
│   │   ├── groups.ini           # 包含嵌套组的 inventory
│   │   └── hostvars.ini         # 包含 host_vars 的 inventory
│   └── parser_test.go
├── template/
│   ├── testdata/
│   │   ├── simple.tmpl          # 简单模板
│   │   ├── with_filters.tmpl    # 包含过滤器的模板
│   │   ├── with_lookups.tmpl    # 包含查找的模板
│   │   └── invalid.tmpl         # 语法错误的模板
│   └── engine_test.go
└── vault/
    ├── testdata/
    │   ├── vault_password.txt   # 测试密码
│   │   ├── encrypted.yaml     # 加密的 YAML
│   │   └── plaintext.yaml     # 明文 YAML
│   └── vault_test.go
```

**testdata 使用规则**：
- 使用 `os.ReadFile("testdata/xxx")` 或 `testdata` 嵌入
- 文件名清晰描述内容和用途
- 包含有效和无效两类测试数据
- 敏感数据（密码）仅用于测试，不提交到版本控制

### 2.4 子测试与并行测试

```go
func TestVariableMerge(t *testing.T) {
    t.Run("priority", func(t *testing.T) {
        t.Parallel() // 可并行的子测试
        // 测试变量优先级合并
    })

    t.Run("deep_merge", func(t *testing.T) {
        t.Parallel()
        // 测试深度合并
    })

    t.Run("concurrent_access", func(t *testing.T) {
        // 并发安全测试不并行运行
        // 测试并发读写
    })
}
```

---

## 3. 模块测试策略

### 3.1 Mock Connection 测试模块

每个模块通过 Mock Connection 测试，验证：
1. 生成的 shell 命令是否正确
2. 参数校验是否完整
3. 结果解析是否准确
4. CheckMode 行为是否正确

```go
func TestAptModule(t *testing.T) {
    tests := []struct {
        name      string
        params    map[string]any
        wantCmd   string       // 期望执行的命令
        wantRC    int          // 期望的返回码
        wantChanged bool       // 期望的 Changed 状态
        wantErr   bool         // 期望是否报错
    }{
        {
            name:   "install package",
            params: map[string]any{"name": "nginx", "state": "present"},
            wantCmd: "apt-get install -y nginx",
            wantRC:  0,
            wantChanged: true,
        },
        {
            name:   "package already installed",
            params: map[string]any{"name": "nginx", "state": "present"},
            wantCmd: "apt-get install -y nginx",
            wantRC:  0,
            wantChanged: false, // dpkg 返回 0 表示已是最新
        },
        {
            name:   "remove package",
            params: map[string]any{"name": "nginx", "state": "absent"},
            wantCmd: "apt-get remove -y nginx",
            wantRC:  0,
            wantChanged: true,
        },
        {
            name:   "missing name parameter",
            params: map[string]any{"state": "present"},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mock := &MockConnection{
                ExecCommandFunc: func(cmd string) (string, string, int, error) {
                    // 验证命令
                    if !strings.Contains(cmd, tt.wantCmd) {
                        t.Errorf("expected command containing %q, got %q", tt.wantCmd, cmd)
                    }
                    return "", "", tt.wantRC, nil
                },
            }

            module := NewAptModule(mock)
            result, err := module.Execute(tt.params)

            if tt.wantErr {
                if err == nil {
                    t.Error("expected error, got nil")
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if result.Changed != tt.wantChanged {
                t.Errorf("Changed = %v, want %v", result.Changed, tt.wantChanged)
            }
        })
    }
}
```

### 3.2 验证生成的 shell 命令

模块测试的核心是验证生成的 shell 命令是否正确：

```go
func TestCommandGeneration(t *testing.T) {
    // 记录实际执行的命令
    var executedCommands []string

    mock := &MockConnection{
        ExecCommandFunc: func(cmd string) (string, string, int, error) {
            executedCommands = append(executedCommands, cmd)
            return "", "", 0, nil
        },
    }

    // 执行模块
    module := NewFileModule(mock)
    module.Execute(map[string]any{
        "path":  "/etc/app/config.yaml",
        "owner": "app",
        "group": "app",
        "mode":  "0644",
        "state": "file",
    })

    // 验证生成的命令
    expected := []string{
        "chown app:app /etc/app/config.yaml",
        "chmod 0644 /etc/app/config.yaml",
    }
    if !reflect.DeepEqual(executedCommands, expected) {
        t.Errorf("commands = %v, want %v", executedCommands, expected)
    }
}
```

### 3.3 幂等性测试

所有模块必须测试幂等性——运行两次，第二次应报告 ok（无变更）：

```go
func TestIdempotency(t *testing.T) {
    // 模拟文件已存在且属性正确
    callCount := 0
    mock := &MockConnection{
        ExecCommandFunc: func(cmd string) (string, string, int, error) {
            callCount++
            if strings.Contains(cmd, "stat") {
                // 文件已存在，属性正确
                return `{"st_mode": "0644", "st_uid": "1000", "st_gid": "1000"}`, "", 0, nil
            }
            return "", "", 0, nil
        },
    }

    module := NewFileModule(mock)

    // 第一次执行
    result1, err := module.Execute(map[string]any{
        "path":  "/etc/app/config.yaml",
        "state": "file",
        "mode":  "0644",
    })
    if err != nil {
        t.Fatalf("first run: %v", err)
    }

    // 第二次执行
    result2, err := module.Execute(map[string]any{
        "path":  "/etc/app/config.yaml",
        "state": "file",
        "mode":  "0644",
    })
    if err != nil {
        t.Fatalf("second run: %v", err)
    }

    // 第二次应无变更
    if result2.Changed {
        t.Error("second run should not report Changed")
    }
}
```

### 3.4 CheckMode 测试

```go
func TestCheckMode(t *testing.T) {
    mock := &MockConnection{}

    module := NewAptModule(mock)
    module.SetCheckMode(true)

    result, err := module.Execute(map[string]any{
        "name":  "nginx",
        "state": "present",
    })

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    // CheckMode 下不应执行实际命令
    if len(mock.RecordedCommands) > 0 {
        t.Error("check mode should not execute commands")
    }
    // 但应报告是否会有变更
    // Changed 的值取决于预检查结果
}
```

---

## 4. 集成测试

### 4.1 模块组合测试

集成测试验证多个模块在同一个 Play 中的协作：

```go
func TestModuleCombination(t *testing.T) {
    // 场景：先创建目录，再复制配置文件，最后启动服务
    mock := &MockConnection{
        ExecCommandFunc: func(cmd string) (string, string, int, error) {
            // 记录并模拟所有命令
            return "", "", 0, nil
        },
    }

    play := Play{
        Name:  "Deploy app",
        Hosts: "all",
        Tasks: []Task{
            {Module: "file", Params: map[string]any{"path": "/etc/app", "state": "directory"}},
            {Module: "copy", Params: map[string]any{"src": "app.conf", "dest": "/etc/app/app.conf"}},
            {Module: "service", Params: map[string]any{"name": "app", "state": "started"}},
        },
    }

    engine := NewEngine(mock)
    stats, err := engine.ExecutePlay(play, []string{"web1"})

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if stats.HostStats["web1"].Failed > 0 {
        t.Error("expected no failures")
    }
    // 验证命令执行顺序
    // 1. mkdir /etc/app
    // 2. copy app.conf
    // 3. systemctl start app
}
```

### 4.2 引擎级 Mock SSH 测试

使用 Mock SSH Server 测试引擎的完整流程：

```go
func TestEngineWithMockSSH(t *testing.T) {
    // 启动 mock SSH 服务器
    sshServer := NewMockSSHServer(MockSSHConfig{
        Port: 2222,
        CommandResponses: map[string]CommandResponse{
            "cat /etc/os-release": {
                Stdout: `NAME="Ubuntu" VERSION="20.04"`,
                RC:     0,
            },
            "apt-get install -y nginx": {
                Stdout: "nginx installed",
                RC:     0,
            },
            "systemctl status nginx": {
                Stdout: "active (running)",
                RC:     0,
            },
        },
    })
    defer sshServer.Close()

    // 创建 inventory 指向 mock SSH
    inventory := NewInventory([]Host{
        {Name: "web1", Address: "127.0.0.1", Port: 2222},
    })

    // 执行 playbook
    engine := NewEngine(inventory)
    stats, err := engine.ExecutePlaybook("testdata/simple_playbook.yaml")

    if err != nil {
        t.Fatalf("playbook execution failed: %v", err)
    }
    if stats.HostStats["web1"].Failed > 0 {
        t.Error("expected no failures")
    }
}
```

### 4.3 变量合并集成测试

测试变量从多个来源的合并行为：

```go
func TestVariableMergeIntegration(t *testing.T) {
    // 测试优先级：CLI vars > play vars > host_vars > group_vars > defaults
    sources := VariableSources{
        Defaults:   map[string]any{"port": 80, "debug": false},
        GroupVars:  map[string]any{"port": 8080, "env": "production"},
        HostVars:   map[string]any{"port": 9090},
        PlayVars:   map[string]any{"app_name": "myapp"},
        CLIVars:    map[string]any{"debug": true},
    }

    merged, err := MergeVariables(sources)
    if err != nil {
        t.Fatalf("merge failed: %v", err)
    }

    // 验证优先级
    expected := map[string]any{
        "port":     9090,     // host_vars 覆盖 group_vars 覆盖 defaults
        "debug":    true,     // CLI vars 最高优先级
        "env":      "production", // group_vars 中的值保留
        "app_name": "myapp",  // play vars 中的值保留
    }

    for key, want := range expected {
        got, ok := merged[key]
        if !ok {
            t.Errorf("key %q not found in merged result", key)
            continue
        }
        if !reflect.DeepEqual(got, want) {
            t.Errorf("merged[%q] = %v, want %v", key, got, want)
        }
    }
}
```

### 4.4 Handler 触发集成测试

```go
func TestHandlerTrigger(t *testing.T) {
    mock := &MockConnection{
        ExecCommandFunc: func(cmd string) (string, string, int, error) {
            return "", "", 0, nil
        },
    }

    play := Play{
        Name:     "Configure web",
        Hosts:    "all",
        Handlers: []Handler{
            {Name: "restart nginx", Module: "service", Params: map[string]any{"name": "nginx", "state": "restarted"}},
        },
        Tasks: []Task{
            {
                Module: "copy",
                Params: map[string]any{"src": "nginx.conf", "dest": "/etc/nginx/nginx.conf"},
                Notify: []string{"restart nginx"}, // 变更时通知 handler
            },
        },
    }

    engine := NewEngine(mock)
    stats, _ := engine.ExecutePlay(play, []string{"web1"})

    // 验证 handler 被触发
    // 检查 mock 中是否执行了 restart 命令
    restartFound := false
    for _, cmd := range mock.RecordedCommands {
        if strings.Contains(cmd, "systemctl restart nginx") {
            restartFound = true
            break
        }
    }
    if !restartFound {
        t.Error("expected handler 'restart nginx' to be triggered")
    }
}
```

---

## 5. E2E 测试

### 5.1 Mock SSH Server

E2E 测试的核心基础设施是 Mock SSH Server——一个本地运行的 SSH 服务器，可配置命令响应映射。

```go
// MockSSHServer 是用于 E2E 测试的本地 SSH 服务器。
type MockSSHServer struct {
    // listener 监听 TCP 连接。
    listener net.Listener

    // config 服务器配置。
    config MockSSHConfig

    // hostKey 服务器私钥。
    hostKey ssh.Signer
}

// MockSSHConfig 配置 mock SSH 服务器。
type MockSSHConfig struct {
    // Port 监听端口，0 表示自动分配。
    Port int

    // CommandResponses 命令到响应的映射。
    CommandResponses map[string]CommandResponse

    // DefaultResponse 未匹配命令时的默认响应。
    DefaultResponse CommandResponse

    // AuthorizedKeys 允许的公钥列表（为空则允许任何连接）。
    AuthorizedKeys []ssh.PublicKey
}

// CommandResponse 定义命令的模拟响应。
type CommandResponse struct {
    Stdout   string // 标准输出
    Stderr   string // 标准错误
    RC       int    // 返回码
    Delay    time.Duration // 模拟延迟
    // ExecFunc 可选的动态响应函数。
    ExecFunc func(cmd string) CommandResponse
}
```

**Mock SSH Server 使用示例**：

```go
func setupMockSSH(t *testing.T) *MockSSHServer {
    t.Helper()

    server := NewMockSSHServer(MockSSHConfig{
        Port: 0, // 自动分配端口
        CommandResponses: map[string]CommandResponse{
            // Gathering Facts
            "cat /etc/os-release": {
                Stdout: `NAME="Ubuntu"
VERSION_ID="20.04"
ID=ubuntu`,
            },
            "uname -a": {
                Stdout: "Linux web1 5.4.0 #1 SMP x86_64 GNU/Linux",
            },
            "python3 --version": {
                Stdout: "Python 3.8.10",
            },
            // 常见模块命令
            "apt-get update":                   {RC: 0},
            "apt-get install -y nginx":          {RC: 0, Stdout: "nginx is already the newest version"},
            "systemctl is-active nginx":         {RC: 0, Stdout: "active"},
            "systemctl start nginx":             {RC: 0},
            "systemctl enable nginx":            {RC: 0},
        },
        DefaultResponse: CommandResponse{
            Stdout: "",
            Stderr: "command not found",
            RC:     127,
        },
    })

    t.Cleanup(func() { server.Close() })
    return server
}
```

### 5.2 完整 Playbook 执行测试

```go
func TestE2E_SimplePlaybook(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping E2E test in short mode")
    }

    sshServer := setupMockSSH(t)
    port := sshServer.Port()

    // 准备测试 playbook
    playbookContent := `
- name: Configure web server
  hosts: all
  gather_facts: true
  tasks:
    - name: Install nginx
      apt:
        name: nginx
        state: present

    - name: Start nginx
      service:
        name: nginx
        state: started
        enabled: yes
`

    // 写入临时 playbook 文件
    playbookPath := filepath.Join(t.TempDir(), "playbook.yaml")
    os.WriteFile(playbookPath, []byte(playbookContent), 0644)

    // 准备 inventory
    inventoryContent := fmt.Sprintf("[webservers]\nweb1 ansible_host=127.0.0.1 ansible_port=%d\n", port)
    inventoryPath := filepath.Join(t.TempDir(), "inventory.ini")
    os.WriteFile(inventoryPath, []byte(inventoryContent), 0644)

    // 执行 ansible-go
    cmd := exec.Command("ansible-go", "playbook",
        "-i", inventoryPath,
        playbookPath,
    )
    output, err := cmd.CombinedOutput()

    if err != nil {
        t.Fatalf("ansible-go failed: %v\nOutput:\n%s", err, output)
    }

    // 验证输出包含期望内容
    outputStr := string(output)
    if !strings.Contains(outputStr, "PLAY RECAP") {
        t.Error("output missing PLAY RECAP")
    }
    if !strings.Contains(outputStr, "failed=0") {
        t.Error("output shows failures")
    }
}
```

### 5.3 Docker 测试环境

对于需要真实 SSH 连接的 E2E 测试，使用 Docker 容器：

```go
func TestE2E_DockerSSH(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping E2E test in short mode")
    }
    if os.Getenv("CI") == "" && os.Getenv("E2E_DOCKER") == "" {
        t.Skip("skipping Docker E2E test (set E2E_DOCKER=1 to enable)")
    }

    // 启动 Docker 容器
    containerID := startSSHContainer(t)
    defer stopSSHContainer(t, containerID)

    // 获取容器 IP
    containerIP := getContainerIP(t, containerID)

    // 准备 inventory
    inventory := fmt.Sprintf("[webservers]\ntarget ansible_host=%s ansible_user=root ansible_ssh_pass=testpass\n", containerIP)

    // 执行 playbook
    // ...
}

func startSSHContainer(t *testing.T) string {
    t.Helper()
    cmd := exec.Command("docker", "run", "-d",
        "--name", "ansible-go-test-"+t.Name(),
        "-p", "0:22",
        "ansible-go-test-sshd:latest",
    )
    output, err := cmd.Output()
    if err != nil {
        t.Fatalf("failed to start container: %v", err)
    }
    return strings.TrimSpace(string(output))
}
```

### 5.4 E2E 测试场景清单

| 编号 | 场景 | 说明 |
|------|------|------|
| E2E-01 | 基础 playbook | 安装包、启动服务 |
| E2E-02 | 带 handler 的 playbook | 通知和触发 handler |
| E2E-03 | 带条件的 playbook | when 条件判断 |
| E2E-04 | 带循环的 playbook | loop/with_items |
| E2E-05 | template 模块 | 渲染模板并部署 |
| E2E-06 | 多 play playbook | 多个 play 串行执行 |
| E2E-07 | 错误处理 | ignore_errors、block/rescue |
| E2E-08 | Vault 加密 | 加密变量的 playbook |
| E2E-09 | 回调输出格式 | JSON/YAML/Minimal 输出 |
| E2E-10 | 退出码验证 | 各种失败场景的退出码 |

---

## 6. 测试辅助设施

### 6.1 MockSSHServer 设计

完整的 MockSSHServer 设计：

```go
// MockSSHServer 提供用于测试的本地 SSH 服务器。
type MockSSHServer struct {
    listener net.Listener
    config   MockSSHConfig
    hostKey  ssh.Signer

    // mu 保护 sessions 和 recordedCommands。
    mu sync.Mutex

    // sessions 活跃的 SSH 会话。
    sessions map[string]*MockSession

    // recordedCommands 记录所有执行的命令（按顺序）。
    recordedCommands []RecordedCommand
}

// RecordedCommand 记录单条命令的执行信息。
type RecordedCommand struct {
    Command   string        // 执行的命令
    Timestamp time.Time     // 执行时间
    Duration  time.Duration // 执行耗时
}

// MockSession 表示一个 mock SSH 会话。
type MockSession struct {
    User    string
    RemoteAddr net.Addr
}

// NewMockSSHServer 创建并启动 mock SSH 服务器。
func NewMockSSHServer(config MockSSHConfig) (*MockSSHServer, error)

// Port 返回服务器监听的端口。
func (s *MockSSHServer) Port() int

// Close 关闭服务器。
func (s *MockSSHServer) Close() error

// RecordedCommands 返回所有记录的命令。
func (s *MockSSHServer) RecordedCommands() []RecordedCommand

// Reset 清除命令记录。
func (s *MockSSHServer) Reset()
```

### 6.2 Fixture Playbook 集合

在 `testdata/playbooks/` 目录中维护一套覆盖各种场景的测试 playbook：

```
testdata/
└── playbooks/
    ├── 01_simple.yaml              # 基础任务
    ├── 02_with_handler.yaml        # Handler 触发
    ├── 03_with_when.yaml           # 条件执行
    ├── 04_with_loop.yaml           # 循环
    ├── 05_with_template.yaml       # Template 模块
    ├── 06_multi_play.yaml          # 多 Play
    ├── 07_with_roles.yaml          # Role 引用
    ├── 08_block_rescue.yaml        # Block/Rescue/Always
    ├── 09_with_vault.yaml          # Vault 加密变量
    ├── 10_ignore_errors.yaml       # 忽略错误
    ├── 11_async.yaml               # 异步任务
    ├── 12_serial.yaml              # 滚动更新
    ├── 13_delegate.yaml            # 任务委派
    ├── 14_include_tasks.yaml       # 任务包含
    ├── 15_complex_vars.yaml        # 复杂变量场景
    └── inventories/
        ├── single_host.ini         # 单主机
        ├── multi_host.ini          # 多主机
        ├── with_groups.ini         # 带组
        └── with_hostvars.ini       # 带主机变量
```

**每个 fixture playbook 的结构**：

```yaml
# 01_simple.yaml
# 测试目的：验证最基础的任务执行
# 预期结果：所有任务成功，无变更
# 依赖：inventory/single_host.ini

- name: Simple tasks
  hosts: all
  gather_facts: false
  tasks:
    - name: Create temp file
      file:
        path: /tmp/ansible-go-test
        state: touch

    - name: Verify file exists
      stat:
        path: /tmp/ansible-go-test
      register: file_stat

    - name: Assert file exists
      assert:
        that:
          - file_stat.stat.exists
```

### 6.3 测试辅助函数

在 `internal/testutil/` 包中提供常用测试辅助函数：

```go
package testutil

// MustReadFile 读取文件内容，失败时 panic（仅用于测试）。
func MustReadFile(path string) []byte

// TempFile 创建临时文件并返回路径，注册 cleanup。
func TempFile(t *testing.T, content string) string

// TempDir 创建临时目录并返回路径，注册 cleanup。
func TempDir(t *testing.T) string

// AssertNoError 断言 err 为 nil。
func AssertNoError(t *testing.T, err error)

// AssertEqual 断言两个值相等。
func AssertEqual(t *testing.T, got, want any)

// AssertContains 断言字符串包含子串。
func AssertContains(t *testing.T, s, substr string)

// WaitForCondition 等待条件满足或超时。
func WaitForCondition(t *testing.T, timeout time.Duration, condition func() bool)
```

---

## 7. 覆盖率与质量

### 7.1 覆盖率报告

```bash
# 生成覆盖率报告
make test-coverage

# 等价于：
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

**覆盖率目标检查**：

```bash
# 在 CI 中检查覆盖率是否达标
go test -coverprofile=coverage.out ./...
total=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
if (( $(echo "$total < 80" | bc -l) )); then
    echo "Coverage $total% is below 80% threshold"
    exit 1
fi
```

### 7.2 竞态检测

```bash
# 运行竞态检测
make test-race

# 等价于：
go test -race ./...
```

**竞态检测重点**：
- variables 包的并发读写
- engine 包的并行任务执行
- strategy 包的 worker pool 调度
- callback 包的多回调并发

### 7.3 基准测试

```bash
# 运行基准测试
make bench

# 等价于：
go test -bench=. -benchmem ./...
```

**基准测试重点**：

```go
// 模板渲染性能
func BenchmarkTemplateRender(b *testing.B) {
    engine := NewTemplateEngine()
    vars := map[string]any{"name": "test", "items": []string{"a", "b", "c"}}
    tmpl := "Hello {{ .name }}, items: {{ range .items }}{{ . }} {{ end }}"

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.Render(tmpl, vars)
    }
}

// Inventory 解析性能
func BenchmarkInventoryParseINI(b *testing.B) {
    content := []byte(`[webservers]
web1 ansible_host=10.0.0.1
web2 ansible_host=10.0.0.2
...`) // 大量主机

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ParseINI(content)
    }
}

// 变量合并性能
func BenchmarkVariableMerge(b *testing.B) {
    sources := prepareLargeVariableSources()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        MergeVariables(sources)
    }
}
```

### 7.4 测试质量检查

| 检查项 | 工具 | 命令 |
|--------|------|------|
| 覆盖率 >= 80% | go test -cover | `make test-coverage` |
| 无竞态 | go test -race | `make test-race` |
| 静态分析 | golangci-lint | `make lint` |
| 格式化 | gofmt | `make fmt` |
| 代码审查 | go vet | `make vet` |

---

## 8. Go 实现要点

### 8.1 测试文件组织

```
internal/
├── inventory/
│   ├── parser.go
│   ├── parser_test.go        # parser 的单元测试
│   ├── pattern.go
│   ├── pattern_test.go       # pattern 的单元测试
│   └── testdata/             # 测试数据
├── modules/
│   ├── apt.go
│   ├── apt_test.go           # apt 模块的单元测试
│   ├── shell.go
│   ├── shell_test.go
│   └── module_test.go        # 模块公共行为的测试
└── engine/
    ├── engine.go
    ├── engine_test.go         # 引擎单元测试
    └── engine_integration_test.go  // 集成测试（go:build integration）
```

**文件命名规范**：
- 单元测试：`xxx_test.go`（与被测文件同目录）
- 集成测试：`xxx_integration_test.go`（使用 build tag）
- E2E 测试：`test/e2e/` 独立目录

### 8.2 Build Tags

使用 build tag 区分测试层级：

```go
//go:build integration

package engine

func TestEngineIntegration(t *testing.T) {
    // 集成测试需要 mock SSH
}
```

```go
//go:build e2e

package e2e

func TestE2EFullPlaybook(t *testing.T) {
    // E2E 测试需要 Docker 或真实 SSH
}
```

运行方式：

```bash
go test ./...                              # 仅单元测试
go test -tags=integration ./...            # 单元 + 集成测试
go test -tags=e2e ./...                    # 单元 + 集成 + E2E 测试
go test -short ./...                       # 跳过耗时测试
```

### 8.3 testdata 目录结构

```
testdata/
├── playbooks/           # 测试 playbook
├── inventories/         # 测试 inventory
├── templates/           # 测试模板
├── vault/               # Vault 测试数据
├── ssh/                 # SSH 密钥和配置
│   ├── test_key         # 测试私钥
│   └── test_key.pub     # 测试公钥
└── responses/           # 命令响应 fixtures
    ├── facts.json       # Gathering Facts 响应
    └── apt_install.json # apt install 响应
```

### 8.4 CI 集成

```yaml
# .github/workflows/test.yml 示例
name: Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      # 单元测试 + 覆盖率
      - name: Unit Tests
        run: make test-coverage

      # 覆盖率检查
      - name: Check Coverage
        run: |
          total=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
          echo "Total coverage: $total%"
          if (( $(echo "$total < 80" | bc -l) )); then
            echo "Coverage below 80% threshold"
            exit 1
          fi

      # 竞态检测
      - name: Race Detection
        run: make test-race

      # 静态分析
      - name: Lint
        run: make lint

      # E2E 测试（仅 main 分支）
      - name: E2E Tests
        if: github.ref == 'refs/heads/main'
        run: make test-e2e
```

### 8.5 测试文件模板

每个包的测试文件遵循统一结构：

```go
package mypackage

import (
    "testing"
)

// ==================== 单元测试 ====================

func TestFunctionName(t *testing.T) {
    tests := []struct {
        name     string
        input    InputType
        expected OutputType
        wantErr  bool
    }{
        // 正常情况
        {
            name:     "basic case",
            input:    InputType{...},
            expected: OutputType{...},
        },
        // 边界条件
        {
            name:    "empty input",
            input:   InputType{},
            wantErr: true,
        },
        // 错误路径
        {
            name:    "invalid input",
            input:   InputType{...},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := FunctionName(tt.input)
            if tt.wantErr {
                if err == nil {
                    t.Error("expected error, got nil")
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if !reflect.DeepEqual(got, tt.expected) {
                t.Errorf("FunctionName() = %v, want %v", got, tt.expected)
            }
        })
    }
}

// ==================== 边界条件测试 ====================

func TestFunctionName_EdgeCases(t *testing.T) {
    t.Run("nil input", func(t *testing.T) {
        // ...
    })
    t.Run("very large input", func(t *testing.T) {
        // ...
    })
}

// ==================== 基准测试 ====================

func BenchmarkFunctionName(b *testing.B) {
    input := prepareLargeInput()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        FunctionName(input)
    }
}
```

---

## 9. 任务拆解

### T14.1 端到端测试

**目标**：建立完整的测试基础设施和 E2E 测试套件。

**子任务**：

| 编号 | 任务 | 说明 | 预估 |
|------|------|------|------|
| T14.1.1 | MockSSHServer 实现 | 本地 SSH 服务器，命令响应映射 | 2d |
| T14.1.2 | 测试辅助函数库 | testutil 包，通用断言和工具 | 1d |
| T14.1.3 | Fixture Playbook 集合 | 15 个覆盖各场景的测试 playbook | 2d |
| T14.1.4 | Fixture Inventory 集合 | 各种 inventory 测试数据 | 0.5d |
| T14.1.5 | E2E 测试框架 | 测试组织、setup/teardown、Docker 支持 | 1d |
| T14.1.6 | E2E-01 基础 playbook | 最简单的 playbook 执行 | 0.5d |
| T14.1.7 | E2E-02 Handler 触发 | 通知和触发 handler | 0.5d |
| T14.1.8 | E2E-03 条件执行 | when 条件判断 | 0.5d |
| T14.1.9 | E2E-04 循环 | loop/with_items | 0.5d |
| T14.1.10 | E2E-05 Template 模块 | 渲染模板并部署 | 0.5d |
| T14.1.11 | E2E-06 错误处理 | ignore_errors、block/rescue | 0.5d |
| T14.1.12 | E2E-07 Vault | 加密变量的 playbook | 0.5d |
| T14.1.13 | E2E-08 输出格式 | JSON/YAML/Minimal 回调输出 | 0.5d |
| T14.1.14 | E2E-09 退出码 | 各种失败场景的退出码验证 | 0.5d |
| T14.1.15 | CI 集成 | GitHub Actions 测试流水线 | 1d |
| T14.1.16 | 覆盖率基线 | 建立各包覆盖率基线和报告 | 0.5d |

**总预估**：12.5 天

**验收标准**：

- [ ] MockSSHServer 能正确处理 SSH 连接和命令执行
- [ ] 所有 15 个 Fixture Playbook 有对应的 E2E 测试
- [ ] E2E 测试在 CI 中稳定运行（无 flaky 测试）
- [ ] 整体覆盖率 >= 80%
- [ ] 竞态检测通过
- [ ] 基准测试建立性能基线

---

*上一篇：[14-filters-tests-lookups.md](14-filters-tests-lookups.md) | 下一篇：待续*
