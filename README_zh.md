# ansible-go

[English](README.md) | [中文](README_zh.md)

用 Go 语言重新实现的 Ansible —— 一个通过 SSH 管理 Linux 服务器的单一静态二进制工具。

## 为什么选择 ansible-go？

| | Ansible (Python) | ansible-go (Go) |
|---|---|---|
| **运行时** | Python 3.8+ + pip 依赖链 | 单一静态二进制（~15MB） |
| **并发模型** | fork 模式，受 GIL 限制 | goroutine 工作池，真正并行 |
| **模板引擎** | Jinja2 | Go `text/template` + Sprig |
| **模块执行** | Python 模块（SSH + 传输 + 执行） | 远程 SSH 命令执行 |
| **平台支持** | 跨平台 | 仅 Linux（控制端 + 被管端） |

## 架构设计

ansible-go 采用 5 层架构：

```
CLI 层             Cobra 命令解析、全局参数
    ↓
命令层             根据子命令编排引擎组件
    ↓
引擎层             主机清单、Playbook、变量、模板、Fact 采集、Handler
    ↓
模块层             ping, shell, command, copy, file, template, yum, apt, service, user ...
    ↓
连接层             SSH (x/crypto/ssh)、Local、become（权限提升）
```

## 项目状态

**早期阶段** —— 设计和文档已完成，正在推进实现。

| 阶段 | 描述 | 状态 |
|------|------|------|
| P0.1 | 项目骨架（go.mod、Makefile） | 已完成 |
| P0.2 | 根命令 + CLI 结构 | 已完成 |
| P0.3 | 占位子命令 | 下一步 |
| P1-P14 | 核心功能实现 | 计划中 |

完整实现计划详见 [implementation plan](docs/superpowers/plans/2026-05-25-ansible-go-implementation.md)。

## 快速开始

### 环境要求

- Go 1.26+

### 构建

```bash
make build
```

生成的二进制文件位于 `bin/ansible-go`。

### 安装

```bash
make install
```

### 使用示例（规划中）

```bash
# Ad-hoc 命令
ansible-go all -m ping -i inventory/hosts
ansible-go webservers -m shell -a "uptime" -i inventory/hosts
ansible-go db -m service -a "name=mysql state=restarted" --become

# Playbook 执行
ansible-go playbook site.yml -i inventory/production
ansible-go playbook deploy.yml --limit webservers --tags deploy
ansible-go playbook site.yml --check --diff

# 主机清单
ansible-go inventory list -i inventory/hosts
ansible-go inventory host web1 -i inventory/hosts

# Vault 加密
ansible-go vault encrypt secrets.yml
ansible-go vault decrypt secrets.yml

# Galaxy
ansible-go galaxy install username.rolename

# 配置
ansible-go config dump
```

## 开发指南

### Make 命令

| 命令 | 说明 |
|------|------|
| `make build` | 编译二进制到 `bin/` |
| `make install` | 安装到 `$GOPATH/bin` |
| `make test` | 运行所有测试 |
| `make test-coverage` | 生成覆盖率报告 |
| `make test-race` | 运行带竞态检测的测试 |
| `make lint` | 运行 golangci-lint 静态分析 |
| `make fmt` | 使用 gofmt 格式化代码 |
| `make vet` | 运行 go vet 检查 |
| `make check` | 完整质量检查流水线（fmt + vet + lint + test） |
| `make clean` | 清理构建产物 |

### 构建信息

版本号、构建时间和 commit hash 通过 ldflags 注入：

```bash
ansible-go version
# ansible-go v0.1.0 (abc1234) built 2026-05-25T12:00:00Z
```

## 依赖

**当前依赖：**
- [cobra](https://github.com/spf13/cobra) — CLI 框架

**计划引入：**
- `golang.org/x/crypto` — SSH 实现
- `gopkg.in/yaml.v3` — YAML 解析
- `github.com/Masterminds/sprig/v3` — 模板函数库（70+ 函数，兼容 Helm）
- `github.com/pkg/sftp` — SFTP 文件传输
- `github.com/fatih/color` — 终端彩色输出

## 文档

详细的设计与实现文档位于 [`docs/`](docs/) 目录：

- [项目概述](docs/guides/00-overview.md)
- [架构设计](docs/guides/01-architecture.md)
- [设计规格书](docs/superpowers/specs/2026-05-25-ansible-go-design.md)
- [实现计划](docs/superpowers/plans/2026-05-25-ansible-go-implementation.md)
- [MVP 里程碑](docs/guides/16-minimal-viable-path.md)

指南系列（02-16）涵盖各子系统：主机清单、连接管理、变量系统、模板引擎、模块系统、Playbook 引擎、Roles、Handlers、异步任务、Vault、Collections、Callbacks、Filters/Tests/Lookups。

## 兼容性

ansible-go 旨在兼容 Ansible 的 YAML Playbook 语法，以下为差异点：

| 特性 | Ansible | ansible-go |
|------|---------|------------|
| 模板引擎 | Jinja2 | Go text/template + Sprig |
| 变量语法 | `{{ var }}` | `{{ .var }}`（自动预处理） |
| 模块 | Python 实现 | Go 实现（远程 SSH 执行） |
| 插件系统 | Python 类 | Go 接口 |

## 许可证

[Apache License 2.0](LICENSE)
