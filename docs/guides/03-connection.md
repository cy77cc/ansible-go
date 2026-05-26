# ansible-go 教学文档 03：连接层

> **阶段：** P2 | **设计文档引用：** 第七章 连接层
>
> 本文档覆盖 ansible-go 中连接层的完整设计——SSH 连接原理、认证机制、命令执行模型、文件传输、提权机制，以及 Go 实现要点。

---

## 目录

1. [连接层在 Ansible 中的位置](#1-连接层在-ansible-中的位置)
2. [SSH 连接原理](#2-ssh-连接原理)
3. [SSH 认证机制](#3-ssh-认证机制)
4. [命令执行模型](#4-命令执行模型)
5. [SFTP 文件传输](#5-sftp-文件传输)
6. [提权机制（Become）](#6-提权机制become)
7. [本地连接](#7-本地连接)
8. [连接池与复用](#8-连接池与复用)
9. [Go 实现要点](#9-go-实现要点)
10. [任务拆解](#10-任务拆解)

---

## 1. 连接层在 Ansible 中的位置

### 1.1 架构层次

连接层是 Ansible 五层架构中最底层的组件：

```
┌─────────────────────────────────────────┐
│              CLI Layer                  │  用户入口
├─────────────────────────────────────────┤
│           Command Layer                 │  命令编排
├─────────────────────────────────────────┤
│            Engine Layer                 │  执行引擎
├─────────────────────────────────────────┤
│           Module Layer                  │  模块逻辑
├─────────────────────────────────────────┤
│         Connection Layer                │  ← 本文档
│  ┌──────────┐ ┌──────────┐             │
│  │   SSH    │ │  Local   │             │
│  └──────────┘ └──────────┘             │
└─────────────────────────────────────────┘
```

### 1.2 连接层的职责

连接层只做三件事：

1. **执行命令** — 在目标主机上运行 shell 命令，返回 stdout/stderr/exit code
2. **传输文件** — 将本地文件推送到远程（PutFile），或从远程拉取文件（FetchFile）
3. **管理连接** — 建立、维护、关闭网络连接

### 1.3 对上层透明

连接层的设计目标是**对模块层完全透明**。模块不关心命令是通过 SSH 发到远程执行，还是在本地通过 `os/exec` 执行——它们只需要调用 `Exec(cmd)` 方法。

```
模块层视角：
  conn.Exec("nginx -t")
  conn.PutFile("/local/nginx.conf", "/etc/nginx/nginx.conf")

不管 conn 是 SSHConnection 还是 LocalConnection，接口完全一致。
```

### 1.4 连接类型

| 类型 | 使用场景 | 底层实现 |
|------|----------|----------|
| `ssh` | 默认，管理远程主机 | `golang.org/x/crypto/ssh` |
| `local` | 管理本机、网络设备 | `os/exec` |
| `paramiko` | Python SSH 库（Ansible 特有） | ansible-go 不实现 |
| `winrm` | Windows 远程管理 | ansible-go 不实现（仅 Linux） |
| `docker` | Docker 容器管理 | 后续扩展 |

ansible-go 只实现 `ssh` 和 `local` 两种连接类型。

---

## 2. SSH 连接原理

### 2.1 SSH 协议基础

SSH（Secure Shell）是一个加密网络协议，用于在不安全的网络上安全地执行命令。

**SSH 连接建立过程**：

```
客户端                                    服务端
  │                                         │
  │─────── TCP 三次握手 ───────────────────→│
  │                                         │
  │←────── 版本协商 ───────────────────────│
  │                                         │
  │─────── 密钥交换 (DH) ────────────────→│
  │←────── 服务器公钥 ─────────────────────│
  │                                         │
  │─────── 认证请求 ──────────────────────→│
  │←────── 认证结果 ───────────────────────│
  │                                         │
  │─────── 打开会话 channel ──────────────→│
  │←────── channel 确认 ───────────────────│
  │                                         │
  │─────── 执行命令 / 传输文件 ───────────→│
  │←────── 返回结果 ───────────────────────│
```

### 2.2 ControlMaster / ControlPersist

原生 Ansible 通过 OpenSSH 的 ControlMaster 功能实现连接复用：

```
# ansible.cfg
[ssh_connection]
ssh_args = -o ControlMaster=auto -o ControlPersist=60s
```

**工作原理**：

```
第一次连接：
  ansible ──SSH──→ 控制连接 ──→ 目标主机
                (持久化到 socket 文件)

后续连接：
  ansible ──SSH──→ 复用控制连接 ──→ 目标主机
                (不需要重新握手)
```

**优势**：
- 避免重复的 TCP 握手和密钥交换
- 显著减少多 task 场景的连接开销
- ControlPersist=60s 表示空闲连接保持 60 秒

**ansible-go 的方案**：

ansible-go 通过 `golang.org/x/crypto/ssh` 库在进程内维护 SSH 连接，天然实现连接复用——每个 `SSHConnection` 对象持有一个 `*ssh.Client`，多 task 串行复用同一个 client，无需额外的 socket 文件。

```
ansible-go 的连接复用：
  SSHConnection.client (ssh.Client)
      ├── session 1 (task 1)
      ├── session 2 (task 2)
      └── session 3 (task 3)

  每个 task 创建新的 session，但复用底层的 TCP 连接
```

### 2.3 SSH 协议版本

- SSH-2 是当前标准（SSH-1 已废弃，有安全漏洞）
- `golang.org/x/crypto/ssh` 只支持 SSH-2
- 不需要关心版本协商——库会自动处理

---

## 3. SSH 认证机制

### 3.1 三种认证方式

ansible-go 按以下优先级尝试认证：

```
优先级 1: SSH Key（最高优先级）
    ↓ 失败
优先级 2: SSH Agent
    ↓ 失败
优先级 3: Password
    ↓ 失败
连接失败
```

### 3.2 SSH Key 认证

SSH Key 是最推荐的认证方式——无需密码，适合自动化场景。

**支持的密钥类型**：

| 算法 | 密钥文件 | 安全性 | 推荐度 |
|------|----------|--------|--------|
| Ed25519 | `id_ed25519` | 最高 | 推荐 |
| ECDSA | `id_ecdsa` | 高 | 可用 |
| RSA | `id_rsa` | 中 | 兼容性好 |
| DSA | `id_dsa` | 低 | 已废弃 |

**密钥解析流程**：

```
1. 读取密钥文件内容
2. ssh.ParsePrivateKey(data)
   ├── 成功 → 使用该密钥认证
   └── 失败（需要 passphrase）
       ├── 有密码 → ssh.ParsePrivateKeyWithPassphrase(data, passphrase)
       │   ├── 成功 → 使用该密钥认证
       │   └── 失败 → 跳过
       └── 无密码 → 跳过
```

**Passphrase 保护的密钥**：

```ini
# inventory 中指定
web1 ansible_ssh_private_key_file=~/.ssh/id_ed25519 ansible_ssh_pass=my_passphrase
```

注意：`ansible_ssh_pass` 在密钥认证场景中用作 passphrase，而非 SSH 登录密码。

### 3.3 SSH Agent 认证

SSH Agent 是一个后台进程，管理已解锁的密钥。

**工作原理**：

```
1. 用户启动 ssh-agent
2. 用户通过 ssh-add 添加密钥（输入一次 passphrase）
3. Agent 缓存解锁后的密钥
4. SSH 客户端通过 UNIX socket ($SSH_AUTH_SOCK) 连接 Agent
5. Agent 代替客户端完成签名
```

**ansible-go 的实现**：

```go
// 通过环境变量 SSH_AUTH_SOCK 检测 Agent
// 如果存在，通过 golang.org/x/crypto/ssh/agent 包连接
authSock := os.Getenv("SSH_AUTH_SOCK")
if authSock != "" {
    conn, err := net.Dial("unix", authSock)
    agentClient := agent.NewClient(conn)
    methods = append(methods, ssh.PublicKeysCallback(agentClient.Signers))
}
```

### 3.4 Password 认证

密码认证是最简单但最不安全的方式：

```ini
web1 ansible_ssh_pass=my_secret_password
```

**安全警告**：
- 密码明文存储在 Inventory 文件中
- 建议使用 Vault 加密敏感变量
- 生产环境应优先使用 SSH Key

### 3.5 SSH 变量清单

以下是控制 SSH 连接行为的完整变量列表：

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `ansible_host` | SSH 目标地址 | 主机名 |
| `ansible_port` | SSH 端口 | 22 |
| `ansible_user` | SSH 用户名 | 当前系统用户 |
| `ansible_ssh_pass` | SSH 密码或密钥 passphrase | 空 |
| `ansible_ssh_private_key_file` | 私钥文件路径 | `~/.ssh/id_rsa` |
| `ansible_ssh_common_args` | 额外 SSH 参数（如 ProxyJump） | 空 |
| `ansible_ssh_pipelining` | 是否启用管道模式 | false |
| `ansible_timeout` | 连接超时（秒） | 10 |
| `ansible_connection` | 连接类型 | ssh |

### 3.6 默认值推导逻辑

```go
// 伪代码：SSH 配置构建
func SSHConfigFromVars(vars map[string]any) *SSHConfig {
    cfg := &SSHConfig{
        Port:    22,                    // 默认端口
        Timeout: 10,                    // 默认超时
    }

    // 从变量中覆盖
    if v, ok := vars["ansible_host"]; ok {
        cfg.Host = fmt.Sprintf("%v", v)
    }
    if v, ok := vars["ansible_port"]; ok {
        cfg.Port = toInt(v)
    }
    // ... 其他变量

    // 推导默认值
    if cfg.User == "" {
        cfg.User = os.Getenv("USER")   // 当前系统用户
        if cfg.User == "" {
            cfg.User = "root"           // 兜底 root
        }
    }
    if cfg.KeyFile == "" {
        // 检查默认密钥路径
        home, _ := os.UserHomeDir()
        for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
            path := filepath.Join(home, ".ssh", name)
            if _, err := os.Stat(path); err == nil {
                cfg.KeyFile = path
                break
            }
        }
    }

    return cfg
}
```

---

## 4. 命令执行模型

### 4.1 命令执行的完整流程

```
Go 代码                         SSH 连接                     远程主机
   │                               │                           │
   │  conn.Exec("nginx -t")        │                           │
   │──────────────────────────────→│                           │
   │                               │  /bin/sh -c 'nginx -t'    │
   │                               │─────────────────────────→│
   │                               │                           │
   │                               │  stdout: "syntax is ok"   │
   │                               │  stderr: ""               │
   │                               │  exit code: 0             │
   │                               │←─────────────────────────│
   │  (stdout, stderr, rc, err)    │                           │
   │←──────────────────────────────│                           │
```

### 4.2 Shell 包装

Ansible 所有命令都通过 `/bin/sh -c` 包装执行：

```
原始命令:  nginx -t
实际执行:  /bin/sh -c 'nginx -t'
```

这样做的原因：
- 支持 shell 特性（管道、重定向、通配符）
- 保证在不同 Linux 发行版上行为一致
- 与 Ansible 的行为保持兼容

### 4.3 Quoting 处理

命令中的特殊字符需要正确处理引号：

```go
// 错误：单引号冲突
cmd := "echo 'hello world'"
// /bin/sh -c 'echo 'hello world''  ← 语法错误

// 正确：转义或使用双引号
cmd := `echo "hello world"`
// /bin/sh -c 'echo "hello world"'
```

**ansible-go 的 quoting 策略**：

```go
// shellQuote 用单引号包裹字符串，内部的单引号用 '\'' 转义
func shellQuote(s string) string {
    return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
```

**常见需要 quoting 的场景**：

| 原始命令 | 包装后 | 说明 |
|----------|--------|------|
| `echo hello` | `/bin/sh -c 'echo hello'` | 简单 |
| `echo "hello world"` | `/bin/sh -c 'echo "hello world"'` | 双引号 |
| `echo 'it\'s'` | `/bin/sh -c 'echo '\''it'\''s'\'''` | 单引号转义 |
| `cat /tmp/my file` | `/bin/sh -c 'cat /tmp/my file'` | 空格在引号内安全 |

### 4.4 stdout / stderr / exit code 捕获

每个命令执行后返回三个信息：

```go
type ExecResult struct {
    Stdout string  // 标准输出
    Stderr string  // 标准错误
    Rc     int     // 返回码 (return code / exit code)
}
```

**返回码约定**：

| 返回码 | 含义 | Ansible 行为 |
|--------|------|-------------|
| 0 | 成功 | ok / changed |
| 非 0 | 失败 | failed（除非 ignore_errors） |

**Go 实现细节**：

```go
// golang.org/x/crypto/ssh 的 session.Run() 返回 *ssh.ExitError
err = session.Run(cmd)
if exitErr, ok := err.(*ssh.ExitError); ok {
    rc = exitErr.ExitStatus()
    err = nil  // 不是连接错误，只是命令返回非 0
}
```

### 4.5 Pipelining 模式

传统 Ansible 执行模式（非 pipelining）：

```
1. SSH 连接 → 生成模块脚本
2. SFTP → 上传模块脚本到远程临时目录
3. SSH 连接 → 执行模块脚本
4. SSH 连接 → 删除临时文件
```

这需要 4 次 SSH 操作，效率低下。

**Pipelining 模式**：

```
1. SSH 连接 → 通过 stdin 将模块脚本传给 Python
   python -c 'import sys; exec(sys.stdin.read())' < module.py
```

只需要 1 次 SSH 操作。

**ansible-go 的方案**：

ansible-go 不拷贝模块脚本到远程。它通过 SSH 直接执行命令（如 `yum install nginx -y`），天然等效于 pipelining 模式——一次 SSH 调用完成所有操作。

```go
// ansible-go 的模块执行模型
// 模块在本地生成 shell 命令，通过 SSH 直接执行
cmd := "yum install nginx -y"
stdout, stderr, rc, err := conn.Exec(cmd)
// 解析结果
```

### 4.6 执行超时

```go
// 通过 context 控制命令执行超时
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

session, err := client.NewSession()
// session 有 Start() 和 Wait() 方法
// 但没有内置超时——需要通过 context 控制

// ansible-go 的方案：
// 在 conn.Exec() 中通过 channel select 实现超时
type ExecResult struct {
    Stdout string
    Stderr string
    Rc     int
    Err    error
}

// 使用 channel 实现超时
done := make(chan ExecResult, 1)
go func() {
    // 执行命令
    done <- ExecResult{...}
}()

select {
case result := <-done:
    return result
case <-time.After(time.Duration(timeout) * time.Second):
    session.Signal(ssh.SIGKILL)
    return ExecResult{Err: ErrTimeout}
}
```

---

## 5. SFTP 文件传输

### 5.1 SFTP 子系统

SFTP（SSH File Transfer Protocol）是 SSH 协议的子系统，提供文件传输能力。

**与 SCP 的区别**：

| 特性 | SFTP | SCP |
|------|------|-----|
| 协议 | SSH 子系统 | SSH 上的独立命令 |
| 目录操作 | 支持 mkdir/rmdir/readdir | 不支持 |
| 断点续传 | 支持 | 不支持 |
| 进度回调 | 支持 | 有限 |
| 性能 | 稍慢（协议开销） | 略快 |
| ansible-go | 使用 | 不使用 |

### 5.2 文件传输流程

```
PutFile (本地 → 远程):

1. 建立 SFTP 会话
   sftpClient, err := sftp.NewClient(sshClient)

2. 确保远程目录存在
   sftpClient.MkdirAll(remoteDir)

3. 创建远程文件
   remoteFile, err := sftpClient.Create(remotePath)

4. 写入数据
   localData, _ := os.ReadFile(localPath)
   remoteFile.Write(localData)

5. 设置权限
   remoteFile.Chmod(0644)

6. 关闭文件
   remoteFile.Close()
```

```
FetchFile (远程 → 本地):

1. 打开远程文件
   remoteFile, err := sftpClient.Open(remotePath)

2. 读取数据
   var buf bytes.Buffer
   buf.ReadFrom(remoteFile)

3. 写入本地文件
   os.WriteFile(localPath, buf.Bytes(), 0644)
```

### 5.3 目录创建

在 `PutFile` 之前，需要确保远程目录存在：

```go
// MkdirAll 递归创建远程目录
// 类似于 os.MkdirAll 的行为
remoteDir := filepath.Dir(remotePath)
sftpClient.MkdirAll(remoteDir)

// 例如：remotePath = /etc/nginx/conf.d/site.conf
// 需要确保 /etc/nginx/conf.d/ 目录存在
```

### 5.4 权限设置

```go
// 创建文件时设置权限
remoteFile, _ := sftpClient.Create(remotePath)
remoteFile.Chmod(0644)   // 普通文件
// 或
remoteFile.Chmod(0755)   // 可执行文件

// 对于 copy 模块，可以通过 mode 参数控制
// web1 mode=0644   → 普通权限
// web1 mode=0755   → 可执行
```

---

## 6. 提权机制（Become）

### 6.1 为什么需要提权

许多系统管理操作需要 root 权限：

- 安装软件包（`yum install`）
- 管理系统服务（`systemctl restart nginx`）
- 修改系统配置（`/etc/ssh/sshd_config`）
- 管理用户（`useradd`）

但直接用 root 连接 SSH 是不安全的。Ansible 的方案是：用普通用户连接，然后**提权**到 root。

### 6.2 提权方式

| 方式 | 命令 | 适用场景 |
|------|------|----------|
| `sudo` | `sudo -u root cmd` | Linux 标准提权 |
| `su` | `su - root -c cmd` | 无 sudo 的系统 |
| `pbrun` | `pbrun cmd` | PowerBroker |
| `pfexec` | `pfexec cmd` | Solaris |
| `doas` | `doas cmd` | OpenBSD |

ansible-go 只实现 `sudo` 和 `su`。

### 6.3 sudo 命令包装

**无密码 sudo**：

```bash
# 原始命令
cat /etc/shadow

# sudo 包装后
sudo -H -S -n -u root /bin/sh -c 'cat /etc/shadow'
```

参数说明：
- `-H`：设置 HOME 环境变量为目标用户的 HOME
- `-S`：从 stdin 读取密码
- `-n`：非交互模式，不提示输入密码（无密码 sudo）
- `-u root`：目标用户

**有密码 sudo**：

```bash
# 原始命令
cat /etc/shadow

# sudo 包装后（通过管道传密码）
echo 'my_password' | sudo -H -S -u root /bin/sh -c 'cat /etc/shadow'
```

**Go 实现**：

```go
// WrapCommand 用提权方式包装命令
func WrapCommand(cmd, method, user, password string) string {
    if method == "" {
        return cmd
    }

    escaped := shellQuote(cmd)

    switch method {
    case "sudo":
        if password != "" {
            // 有密码：通过 echo 管道传入
            return fmt.Sprintf("echo %s | sudo -H -S -u %s /bin/sh -c %s",
                shellQuote(password), user, escaped)
        }
        // 无密码：使用 -n 标志
        return fmt.Sprintf("sudo -H -S -n -u %s /bin/sh -c %s", user, escaped)

    case "su":
        return fmt.Sprintf("su - %s -c %s", user, escaped)

    default:
        return cmd
    }
}
```

### 6.4 su 命令包装

```bash
# 原始命令
whoami

# su 包装后
su - root -c 'whoami'
```

`su -` 会切换到目标用户的完整环境（加载 `.bash_profile` 等）。

### 6.5 Become 的变量控制

```ini
# inventory 变量
ansible_become=true
ansible_become_method=sudo
ansible_become_user=root
ansible_become_pass=sudo_password
```

```yaml
# playbook 中
- hosts: webservers
  become: true
  become_method: sudo
  become_user: root
  tasks:
    - name: Install nginx
      yum:
        name: nginx
        state: present
```

### 6.6 提权的安全注意事项

**密码泄露风险**：

```bash
# 危险：密码可能出现在 /proc/<pid>/cmdline 中
echo 'secret' | sudo -H -S -u root /bin/sh -c 'cmd'

# 更安全的方式：使用 sudo -S 从 stdin 读取
# 但 ansible-go 的 echo 管道方式仍可能泄露
```

**最佳实践**：
- 配置无密码 sudo（`NOPASSWD`）
- 使用 SSH Key + NOPASSWD sudo
- 避免在命令行传递密码

```bash
# /etc/sudoers.d/deploy
deploy ALL=(ALL) NOPASSWD: ALL
```

---

## 7. 本地连接

### 7.1 使用场景

`connection: local` 在以下场景使用：

- **管理本机**：在当前机器上执行操作
- **网络设备**：通过 API 管理交换机/路由器
- **云平台**：通过 SDK 管理云资源
- **编排任务**：不需要 SSH 连接的纯逻辑操作

```yaml
- hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - name: Create cloud instance
      uri:
        url: "https://api.cloud.com/instances"
        method: POST
        body: '{"name": "web1"}'
```

### 7.2 os/exec 实现

本地连接通过 Go 标准库 `os/exec` 实现：

```go
// LocalConnection 通过 os/exec 在本地执行命令
type LocalConnection struct{}

func (c *LocalConnection) Exec(cmd string) (stdout, stderr string, rc int, err error) {
    execCmd := exec.Command("/bin/sh", "-c", cmd)

    var outBuf, errBuf bytes.Buffer
    execCmd.Stdout = &outBuf
    execCmd.Stderr = &errBuf

    err = execCmd.Run()
    stdout = outBuf.String()
    stderr = errBuf.String()

    if exitErr, ok := err.(*exec.ExitError); ok {
        rc = exitErr.ExitCode()
        err = nil  // 不是执行错误，只是命令返回非 0
    } else if err != nil {
        rc = 1
    }

    return
}
```

### 7.3 本地连接与 SSH 连接的差异

| 特性 | SSH | Local |
|------|-----|-------|
| 命令执行 | 远程 | 本地 |
| 文件传输 | SFTP | os.ReadFile/WriteFile |
| 连接建立 | TCP + 认证 | 无开销 |
| 连接关闭 | 需要 Close() | 无操作 |
| Shell | `/bin/sh`（远程） | `/bin/sh`（本地） |
| 环境变量 | 远程用户环境 | 当前进程环境 |

### 7.4 本地连接的文件传输

```go
func (c *LocalConnection) PutFile(localPath, remotePath string) error {
    data, err := os.ReadFile(localPath)
    if err != nil {
        return err
    }
    return os.WriteFile(remotePath, data, 0644)
}

func (c *LocalConnection) FetchFile(remotePath, localPath string) error {
    data, err := os.ReadFile(remotePath)
    if err != nil {
        return err
    }
    return os.WriteFile(localPath, data, 0644)
}
```

对于本地连接，`PutFile` 和 `FetchFile` 本质就是文件复制。

---

## 8. 连接池与复用

### 8.1 Ansible 的并发模型

Ansible 的并发由 `forks` 参数控制：

```
forks=5（默认）
    → 最多 5 台主机并行执行
    → 每台主机一个"worker"
    → 同一主机的多个 task 串行执行
```

```
                    ┌── worker 1 ──→ host1 (task1, task2, task3, ...)
                    │
Main Thread ────────┼── worker 2 ──→ host2 (task1, task2, task3, ...)
                    │
                    ├── worker 3 ──→ host3 (task1, task2, task3, ...)
                    │
                    ├── worker 4 ──→ host4 (task1, task2, task3, ...)
                    │
                    └── worker 5 ──→ host5 (task1, task2, task3, ...)
```

### 8.2 ansible-go 的连接池设计

```
ConnectionPool
├── connections map[string]*connection  // key = "host:port"
├── mu          sync.Mutex
│
├── Get(host, port) Connection          // 获取或创建连接
├── Put(host, port, conn)               // 归还连接
└── CloseAll()                          // 关闭所有连接
```

**连接复用规则**：

```
规则 1: 每个 host:port 最多一个连接
规则 2: 同一主机的 task 串行复用连接（通过 goroutine 池串行调度）
规则 3: 不同主机的 task 通过 goroutine 并行执行
规则 4: forks 控制并行 goroutine 数量
```

### 8.3 与 OpenSSH ControlMaster 的对比

| 特性 | OpenSSH ControlMaster | ansible-go 连接池 |
|------|----------------------|-------------------|
| 实现层 | 操作系统 socket 文件 | Go 进程内 map |
| 跨进程 | 支持 | 不支持 |
| 配置 | ssh_args | 代码内部控制 |
| 持久化 | ControlPersist=60s | 进程退出即销毁 |
| 性能 | 有 IPC 开销 | 无额外开销 |

ansible-go 的方案更简洁——不需要外部 socket 文件，不需要配置 ControlMaster 参数，进程内的 map 就是连接池。

### 8.4 Goroutine 调度模型

```go
// 伪代码：Worker Pool
type WorkerPool struct {
    sem chan struct{}  // 信号量，大小 = forks
}

func (p *WorkerPool) Submit(task func()) {
    p.sem <- struct{}{}  // 获取信号量（阻塞直到有空位）
    go func() {
        defer func() { <-p.sem }()  // 释放信号量
        task()
    }()
}

// 使用
pool := &WorkerPool{sem: make(chan struct{}, forks)}
for _, host := range hosts {
    host := host  // 捕获循环变量
    pool.Submit(func() {
        // 串行执行该主机的所有 task
        for _, task := range tasks {
            conn := pool.GetConnection(host)
            executeTask(conn, task)
        }
    })
}
```

---

## 9. Go 实现要点

### 9.1 Connection 接口定义

```go
// Connection 定义了命令执行和文件传输的接口
type Connection interface {
    // Connect 建立连接
    Connect() error

    // Exec 执行命令，返回 stdout、stderr、exit code 和错误
    Exec(cmd string) (stdout, stderr string, rc int, err error)

    // PutFile 将本地文件传输到远程路径
    PutFile(localPath, remotePath string) error

    // FetchFile 将远程文件拉取到本地路径
    FetchFile(remotePath, localPath string) error

    // Close 关闭连接
    Close() error

    // Shell 返回默认 shell 路径
    Shell() string
}
```

### 9.2 SSHConnection 类型签名

```go
// SSHConfig 持有 SSH 连接配置
type SSHConfig struct {
    Host       string
    Port       int
    User       string
    KeyFile    string
    Password   string
    Timeout    int
    BecomePass string
}

// SSHConfigFromVars 从 inventory 变量中提取 SSH 配置
func SSHConfigFromVars(vars map[string]any) *SSHConfig

// SSHConnection 通过 SSH 实现 Connection 接口
type SSHConnection struct {
    Config     *SSHConfig
    client     *ssh.Client
    sftpClient *sftp.Client
}

// NewSSHConnection 创建 SSH 连接实例
func NewSSHConnection(cfg *SSHConfig) *SSHConnection

// Connect 建立 SSH 连接和 SFTP 会话
func (c *SSHConnection) Connect() error

// Exec 通过 SSH session 执行命令
func (c *SSHConnection) Exec(cmd string) (stdout, stderr string, rc int, err error)

// PutFile 通过 SFTP 传输本地文件到远程
func (c *SSHConnection) PutFile(localPath, remotePath string) error

// FetchFile 通过 SFTP 从远程拉取文件到本地
func (c *SSHConnection) FetchFile(remotePath, localPath string) error

// Close 关闭 SFTP 和 SSH 连接
func (c *SSHConnection) Close() error

// Shell 返回远程 shell 路径
func (c *SSHConnection) Shell() string

// buildAuthMethods 构建认证方法列表（按优先级）
func (c *SSHConnection) buildAuthMethods() ([]ssh.AuthMethod, error)
```

### 9.3 LocalConnection 类型签名

```go
// LocalConnection 在本地机器上执行命令
type LocalConnection struct{}

// NewLocalConnection 创建本地连接实例
func NewLocalConnection() *LocalConnection

// Connect 本地连接无需建立，直接返回 nil
func (c *LocalConnection) Connect() error

// Exec 通过 os/exec 在本地执行命令
func (c *LocalConnection) Exec(cmd string) (stdout, stderr string, rc int, err error)

// PutFile 通过文件复制模拟远程传输
func (c *LocalConnection) PutFile(localPath, remotePath string) error

// FetchFile 通过文件复制模拟远程拉取
func (c *LocalConnection) FetchFile(remotePath, localPath string) error

// Close 本地连接无需关闭
func (c *LocalConnection) Close() error

// Shell 返回本地 shell 路径
func (c *LocalConnection) Shell() string
```

### 9.4 Become 函数签名

```go
// WrapCommand 用提权方式包装命令
func WrapCommand(cmd, method, user, password string) string

// shellQuote 用单引号包裹字符串并转义内部单引号
func shellQuote(s string) string
```

### 9.5 ConnectionPool 签名

```go
// ConnectionPool 管理每台主机的连接
type ConnectionPool struct {
    connections map[string]Connection
    mu          sync.Mutex
    factory     func(vars map[string]any) (Connection, error)
}

// NewConnectionPool 创建连接池
func NewConnectionPool(factory func(vars map[string]any) (Connection, error)) *ConnectionPool

// Get 获取或创建指定主机的连接
func (p *ConnectionPool) Get(host string, vars map[string]any) (Connection, error)

// CloseAll 关闭所有连接
func (p *ConnectionPool) CloseAll() error
```

### 9.6 连接工厂函数

```go
// NewConnection 根据 inventory 变量创建合适的连接实例
func NewConnection(vars map[string]any) (Connection, error) {
    connType := "ssh"
    if v, ok := vars["ansible_connection"]; ok {
        connType = fmt.Sprintf("%v", v)
    }

    switch connType {
    case "local":
        return NewLocalConnection(), nil
    default:
        cfg := SSHConfigFromVars(vars)
        return NewSSHConnection(cfg), nil
    }
}
```

### 9.7 关键依赖

```
golang.org/x/crypto/ssh          SSH 客户端实现
github.com/pkg/sftp              SFTP 文件传输
```

---

## 10. 任务拆解

### 10.1 T2.1 SSH 实现

**目标**：实现完整的 SSH 连接，支持认证、命令执行和文件传输。

**子任务**：

1. **Connection 接口**（`connection.go`）
   - 定义 `Connection` 接口
   - 定义 `NewConnection()` 工厂函数

2. **SSHConfig**（`ssh.go`）
   - 定义 `SSHConfig` 结构体
   - 实现 `SSHConfigFromVars()` 配置提取函数
   - 变量映射：`ansible_host` → Host, `ansible_port` → Port, ...
   - 默认值推导：端口 22、超时 10 秒、当前用户、默认密钥路径

3. **SSHConnection**（`ssh.go`）
   - 实现 `Connect()`：建立 TCP 连接、SSH 握手、SFTP 会话
   - 实现 `buildAuthMethods()`：按优先级构建认证方法列表
     - SSH Key（支持 passphrase）
     - SSH Agent（通过 SSH_AUTH_SOCK）
     - Password
   - 实现 `Exec()`：创建 session、执行命令、捕获输出
   - 实现 `PutFile()` / `FetchFile()`：通过 SFTP 传输文件
   - 实现 `Close()`：关闭 SFTP 和 SSH 连接

4. **测试**（`ssh_test.go`）
   - `TestSSHConnection_ConfigFromVars`：配置提取
   - `TestSSHConnection_DefaultConfig`：默认值
   - 完整集成测试需要 Mock SSH Server（放在 E2E 阶段）

**验收标准**：

```bash
go test ./internal/connection/ -v -run TestSSHConnection
```

### 10.2 T2.2 本地连接与连接池

**目标**：实现本地连接和连接池管理。

**子任务**：

1. **LocalConnection**（`local.go`）
   - 实现 `Connection` 接口的所有方法
   - `Connect()` / `Close()` → 无操作
   - `Exec()` → `os/exec.Command("/bin/sh", "-c", cmd)`
   - `PutFile()` / `FetchFile()` → `os.ReadFile` / `os.WriteFile`

2. **Become 包装**（`become.go`）
   - 实现 `WrapCommand(cmd, method, user, password)` 函数
   - 支持 `sudo`（有密码和无密码）
   - 支持 `su`
   - 实现 `shellQuote()` 引号转义

3. **ConnectionPool**（`pool.go`）
   - 实现连接池的 Get / Put / CloseAll
   - 使用 `sync.Mutex` 保护并发访问
   - key 格式：`host:port`

4. **测试**
   - `local_test.go`：命令执行、失败返回码、Shell 路径
   - `become_test.go`：sudo 包装（有/无密码）、su 包装、无提权
   - `pool_test.go`：连接创建、复用、关闭

**验收标准**：

```bash
go test ./internal/connection/ -v
# 所有测试通过
```

---

## 附录 A：SSH 配置文件参考

### A.1 典型的 SSH 连接调试流程

```bash
# 1. 测试基础连接
ssh -v deploy@192.168.1.10

# 2. 指定端口
ssh -v -p 2222 deploy@192.168.1.10

# 3. 指定密钥
ssh -v -i ~/.ssh/deploy_key deploy@192.168.1.10

# 4. 测试 sudo
ssh deploy@192.168.1.10 "sudo whoami"

# 5. 测试 SFTP
sftp deploy@192.168.1.10
```

### A.2 SSH 常见错误

| 错误 | 原因 | 解决方案 |
|------|------|----------|
| `Connection refused` | SSH 服务未启动或端口不对 | 检查 sshd 状态和端口 |
| `Permission denied` | 认证失败 | 检查密钥/密码/用户名 |
| `Host key verification failed` | 主机密钥不匹配 | 更新 known_hosts |
| `Connection timed out` | 网络不通或防火墙 | 检查网络和安全组 |
| `No route to host` | 路由不可达 | 检查网络配置 |

### A.3 生产环境建议

```
1. 使用 Ed25519 密钥（最安全、最快）
2. 禁用密码认证（/etc/ssh/sshd_config: PasswordAuthentication no）
3. 配置无密码 sudo（/etc/sudoers.d/deploy: NOPASSWD）
4. 使用非标准端口（减少扫描攻击）
5. 限制 SSH 来源 IP（防火墙/安全组）
```

---

## 附录 B：参考资源

- [Ansible 官方文档 - 连接插件](https://docs.ansible.com/ansible/latest/plugins/connection.html)
- [Ansible 官方文档 - Become](https://docs.ansible.com/ansible/latest/become.html)
- [golang.org/x/crypto/ssh 文档](https://pkg.go.dev/golang.org/x/crypto/ssh)
- [github.com/pkg/sftp 文档](https://pkg.go.dev/github.com/pkg/sftp)
- 设计文档：`docs/superpowers/specs/2026-05-25-ansible-go-design.md` 第七章
- 实现计划：`docs/superpowers/plans/2026-05-25-ansible-go-implementation.md` Phase P2
