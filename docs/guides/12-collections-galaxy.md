# 12 - Collections 与 Galaxy

> 阶段：P11 | 设计文档引用：第十一章

本章介绍 ansible-go 的 Collections 体系和 Galaxy 包管理平台集成。Collections
是 Ansible 现代化的模块分发机制，Galaxy 是社区共享 Role 和 Collection 的中心
平台。理解这两者是扩展 ansible-go 生态能力的关键。

---

## 1. Galaxy 是什么

### 1.1 概念

Galaxy 是 Ansible 的包管理平台，类似于：
- Python 的 PyPI
- Node.js 的 npm
- Go 的 pkg.go.dev

它提供了一个中心化的仓库，社区可以在其中发布、搜索、安装可复用的 Ansible 内容
（Roles 和 Collections）。

### 1.2 Galaxy 解决什么问题

在没有 Galaxy 之前，复用 Ansible 代码的方式是：

```
手动方式：
1. 在 GitHub 找到一个 Role
2. git clone 到本地
3. 复制到 roles/ 目录
4. 手动管理版本更新
```

Galaxy 提供了标准化的方式：

```bash
# 一行命令安装
ansible-go galaxy install geerlingguy.nginx

# 声明式管理依赖
# requirements.yml
roles:
  - name: geerlingguy.nginx
    version: "3.1.0"
```

### 1.3 Galaxy 内容类型

| 类型 | 说明 | 示例 |
|------|------|------|
| Role | 面向任务的模块化单元 | `geerlingguy.nginx` |
| Collection | 命名空间化的多功能包 | `community.general` |

---

## 2. Roles vs Collections

### 2.1 Roles — 传统方式

Role 是 Ansible 早期的内容分发机制，面向单一任务：

```
roles/
└── geerlingguy.nginx/
    ├── tasks/
    │   └── main.yml
    ├── handlers/
    │   └── main.yml
    ├── templates/
    │   └── nginx.conf.j2
    ├── defaults/
    │   └── main.yml
    ├── vars/
    │   └── main.yml
    └── meta/
        └── main.yml
```

**特点**：
- 结构简单，易于理解
- 面向单一任务（一个 Role 做一件事）
- 没有命名空间，可能命名冲突
- 不能包含模块、插件

### 2.2 Collections — 现代方式

Collection 是 Ansible 2.9+ 引入的新机制，功能更全面：

```
community.general/
├── galaxy.yml              # 元数据
├── meta/runtime.yml        # 运行时要求
├── plugins/
│   ├── modules/            # 自定义模块
│   ├── callback/           # 回调插件
│   ├── connection/         # 连接插件
│   ├── filter/             # 过滤器
│   ├── lookup/             # 查找插件
│   └── test/               # 测试插件
├── playbooks/
├── roles/
├── docs/
└── tests/
```

**特点**：
- 有命名空间（`namespace.collection`），避免冲突
- 可以包含模块、插件、角色、Playbook
- 支持版本约束和依赖声明
- 是 Ansible 官方推荐的分发方式

### 2.3 对比总结

| 特性 | Roles | Collections |
|------|-------|-------------|
| 命名空间 | 无 | `namespace.collection` |
| 包含模块 | 不可以 | 可以 |
| 包含插件 | 不可以 | 可以 |
| 包含 Roles | 不适用 | 可以 |
| 结构复杂度 | 简单 | 较复杂 |
| 适用场景 | 单一任务自动化 | 完整功能分发 |
| 安装方式 | `galaxy install` | `collection install` |
| 推荐程度 | 仍可用，但逐步迁移 | 官方推荐 |

### 2.4 迁移路径

```
旧方式（Role）：
  - hosts: all
    roles:
      - geerlingguy.nginx

新方式（Collection）：
  - hosts: all
    tasks:
      - name: Use collection module
        community.general.some_module:
          param: value
```

---

## 3. Collection 结构

### 3.1 完整目录布局

```
ansible_collections/
└── my_namespace/
    └── my_collection/
        ├── galaxy.yml                # 必需：Galaxy 元数据
        ├── README.md                 # 可选：说明文档
        ├── LICENSE                   # 可选：许可证
        ├── changelogs/               # 可选：变更日志
        │   └── changelog.yaml
        ├── docs/                     # 可选：文档
        │   └── docsite/
        ├── meta/
        │   └── runtime.yml           # 必需：运行时配置
        ├── plugins/
        │   ├── modules/              # 自定义模块
        │   │   ├── my_module.py
        │   │   └── another_module.py
        │   ├── module_utils/         # 模块工具库
        │   ├── callback/             # 回调插件
        │   ├── connection/           # 连接插件
        │   ├── filter/               # 过滤器插件
        │   ├── lookup/               # 查找插件
        │   ├── test/                 # 测试插件
        │   ├── inventory/            # Inventory 插件
        │   └── vars/                 # Vars 插件
        ├── playbooks/                # Playbook
        │   ├── tasks/
        │   └── files/
        ├── roles/                    # Roles
        │   ├── my_role/
        │   └── another_role/
        ├── tests/                    # 测试
        │   ├── integration/
        │   └── unit/
        └── FILES.json                # 文件校验清单
```

### 3.2 galaxy.yml — 核心元数据

```yaml
# galaxy.yml
namespace: my_namespace
name: my_collection
version: 1.2.0
readme: README.md
authors:
  - Your Name (https://your.site)
description: A collection for managing my infrastructure
license:
  - GPL-2.0-or-later
license_file: LICENSE
tags:
  - cloud
  - infrastructure
  - linux
dependencies:
  "community.general": ">=5.0.0"
  "ansible.posix": "*"
repository: https://github.com/my_namespace/my_collection
documentation: https://my-collection.readthedocs.io
homepage: https://github.com/my_namespace/my_collection
issues: https://github.com/my_namespace/my_collection/issues
build_ignore:
  - tests/
  - docs/
  - .github/
```

**字段说明**：

| 字段 | 必需 | 说明 |
|------|------|------|
| `namespace` | 是 | 命名空间，通常是 GitHub 用户名或组织名 |
| `name` | 是 | Collection 名称 |
| `version` | 是 | 语义化版本号 |
| `authors` | 是 | 作者列表 |
| `description` | 是 | 简短描述 |
| `license` | 是 | 许可证列表 |
| `dependencies` | 否 | 依赖的其他 Collection |
| `tags` | 否 | Galaxy 搜索标签 |
| `build_ignore` | 否 | 构建时忽略的文件/目录 |

### 3.3 meta/runtime.yml — 运行时配置

```yaml
# meta/runtime.yml
requires_ansible: ">=2.14"
action_groups:
  my_namespace.my_collection.group_name:
    - module1
    - module2
plugin_routing:
  modules:
    old_module_name:
      redirect: my_namespace.my_collection.new_module_name
    deprecated_module:
      deprecation:
        removal_date: "2026-12-01"
        warning_text: Use my_namespace.my_collection.replacement instead.
  filters:
    old_filter_name:
      redirect: my_namespace.my_collection.new_filter_name
```

**字段说明**：

| 字段 | 说明 |
|------|------|
| `requires_ansible` | 要求的 Ansible/Go-Ansible 版本 |
| `action_groups` | 模块分组（用于 `module_defaults`） |
| `plugin_routing` | 插件重命名和废弃映射 |

### 3.4 FILES.json — 文件校验

```json
{
  "files": [
    {
      "name": "galaxy.yml",
      "chksum_type": "sha256",
      "chksum_sha256": "a3f8b2c1d4e5..."
    },
    {
      "name": "plugins/modules/my_module.py",
      "chksum_type": "sha256",
      "chksum_sha256": "b4c5d6e7f8a9..."
    }
  ],
  "format": 1
}
```

用于验证 Collection 安装完整性。

---

## 4. Galaxy CLI 命令

### 4.1 Role 命令

```bash
# 安装单个 Role
ansible-go galaxy install geerlingguy.nginx

# 安装指定版本
ansible-go galaxy install geerlingguy.nginx,3.1.0

# 从 requirements.yml 安装
ansible-go galaxy install -r requirements.yml

# 列出已安装的 Role
ansible-go galaxy list

# 列出特定 Role 信息
ansible-go galaxy list geerlingguy.nginx

# 删除已安装的 Role
ansible-go galaxy remove geerlingguy.nginx

# 初始化新 Role
ansible-go galaxy init my_new_role
```

### 4.2 Collection 命令

```bash
# 安装单个 Collection
ansible-go galaxy collection install community.general

# 安装指定版本
ansible-go galaxy collection install community.general:8.0.0

# 从 requirements.yml 安装
ansible-go galaxy collection install -r requirements.yml

# 安装到指定路径
ansible-go galaxy collection install community.general -p ./collections

# 列出已安装的 Collection
ansible-go galaxy collection list

# 列出特定 Collection 信息
ansible-go galaxy collection list community.general

# 删除已安装的 Collection
ansible-go galaxy collection remove community.general

# 初始化新 Collection
ansible-go galaxy collection init my_namespace.my_collection
```

### 4.3 搜索命令

```bash
# 搜索 Role
ansible-go galaxy search nginx

# 搜索 Collection
ansible-go galaxy collection search nginx

# 按作者搜索
ansible-go galaxy search --author geerlingguy

# 按标签搜索
ansible-go galaxy search --galaxy-tags cloud
```

### 4.4 信息命令

```bash
# 查看 Role 详情
ansible-go galaxy info geerlingguy.nginx

# 查看 Collection 详情
ansible-go galaxy collection info community.general
```

---

## 5. Galaxy API 交互

### 5.1 API 端点

Galaxy 提供 REST API 供 CLI 交互：

| 操作 | 端点 | 方法 |
|------|------|------|
| 搜索 | `/api/v1/search/roles/?name=nginx` | GET |
| Role 详情 | `/api/v1/roles/<id>/` | GET |
| Role 版本 | `/api/v1/roles/<id>/versions/` | GET |
| Collection 搜索 | `/api/v2/collections/` | GET |
| Collection 详情 | `/api/v2/collections/<namespace>/<name>/` | GET |
| Collection 版本 | `/api/v2/collections/<namespace>/<name>/versions/` | GET |
| 下载 tarball | Galaxy API 返回的 download_url | GET |

### 5.2 搜索流程

```
用户执行：ansible-go galaxy search nginx
    │
    ▼
1. 构造请求：GET /api/v1/search/roles/?name=nginx
    │
    ▼
2. 发送 HTTP 请求到 Galaxy API
    │
    ▼
3. 解析响应 JSON
    │
    ▼
4. 格式化输出：
   Found 1234 roles matching 'nginx':
   
   Name                Description
   ----                -----------
   geerlingguy.nginx   Nginx installation and configuration
   ...
```

### 5.3 安装流程

```
用户执行：ansible-go galaxy install geerlingguy.nginx,3.1.0
    │
    ▼
1. 查询 Role 信息：GET /api/v1/roles/?owner__username=geerlingguy&name=nginx
    │
    ▼
2. 获取版本信息：匹配 version=3.1.0
    │
    ▼
3. 下载 tarball：GET https://github.com/.../archive/v3.1.0.tar.gz
    │
    ▼
4. 解压到 roles/geerlingguy.nginx/
    │
    ▼
5. 写入元数据（.galaxy_install_info）
```

### 5.4 Tarball 格式

Galaxy 下载的 tarball 通常是 GitHub archive 格式：

```
geerlingguy-nginx-3.1.0.tar.gz
└── geerlingguy-nginx-3.1.0/
    ├── tasks/
    ├── handlers/
    ├── templates/
    ├── defaults/
    ├── vars/
    ├── meta/
    └── README.md
```

安装时需要：
1. 解压 tarball
2. 识别根目录（通常是 `<user>-<repo>-<version>/`）
3. 移动内容到 `roles/<namespace>.<rolename>/`
4. 删除解压的临时目录

### 5.5 Galaxy API 客户端签名

```go
// GalaxyClient Galaxy API 客户端接口
type GalaxyClient interface {
    // SearchRoles 搜索 Role
    SearchRoles(query string, opts SearchOptions) ([]RoleSearchResult, error)

    // GetRole 获取 Role 详情
    GetRole(owner, name string) (*RoleDetail, error)

    // GetRoleVersions 获取 Role 版本列表
    GetRoleVersions(roleID int) ([]Version, error)

    // DownloadRole 下载 Role tarball
    DownloadRole(url string) ([]byte, error)

    // SearchCollections 搜索 Collection
    SearchCollections(query string, opts SearchOptions) ([]CollectionSearchResult, error)

    // GetCollection 获取 Collection 详情
    GetCollection(namespace, name string) (*CollectionDetail, error)

    // GetCollectionVersions 获取 Collection 版本列表
    GetCollectionVersions(namespace, name string) ([]Version, error)

    // DownloadCollection 下载 Collection tarball
    DownloadCollection(url string) ([]byte, error)
}

// SearchOptions 搜索选项
type SearchOptions struct {
    Author    string
    Tags      []string
    Page      int
    PageSize  int
}

// RoleSearchResult Role 搜索结果
type RoleSearchResult struct {
    ID          int
    Name        string
    Description string
    Author      string
    GitHubUser  string
    GitHubRepo  string
    Stars       int
    Downloads   int
}

// RoleDetail Role 详情
type RoleDetail struct {
    ID          int
    Name        string
    Description string
    Author      string
    Versions    []Version
    DownloadURL string
}

// CollectionDetail Collection 详情
type CollectionDetail struct {
    Namespace   string
    Name        string
    Description string
    Versions    []Version
    DownloadURL string
}

// Version 版本信息
type Version struct {
    Version     string
    DownloadURL string
    Created     time.Time
}
```

---

## 6. Go 实现要点

### 6.1 Galaxy 客户端实现

```go
// DefaultGalaxyClient 默认 Galaxy API 客户端
type DefaultGalaxyClient struct {
    baseURL    string       // Galaxy API 地址
    httpClient *http.Client // HTTP 客户端
    token      string       // 认证 token（可选）
}

// NewGalaxyClient 创建 Galaxy 客户端
func NewGalaxyClient(baseURL string, opts ...ClientOption) *DefaultGalaxyClient

// ClientOption 客户端选项
type ClientOption func(*DefaultGalaxyClient)

// WithToken 设置认证 token
func WithToken(token string) ClientOption

// WithHTTPClient 设置自定义 HTTP 客户端
func WithHTTPClient(client *http.Client) ClientOption

// WithTimeout 设置请求超时
func WithTimeout(timeout time.Duration) ClientOption
```

### 6.2 安装管理器

```go
// GalaxyInstaller Galaxy 安装管理器
type GalaxyInstaller struct {
    client      GalaxyClient
    rolesPath   string           // roles 安装路径
    collsPath   string           // collections 安装路径
    force       bool             // 是否强制覆盖
}

// NewGalaxyInstaller 创建安装管理器
func NewGalaxyInstaller(client GalaxyClient, cfg *Config) *GalaxyInstaller

// InstallRole 安装 Role
func (i *GalaxyInstaller) InstallRole(
    ctx context.Context,
    name string,
    version string,
) error

// InstallCollection 安装 Collection
func (i *GalaxyInstaller) InstallCollection(
    ctx context.Context,
    name string,
    version string,
) error

// InstallFromRequirements 从 requirements.yml 安装
func (i *GalaxyInstaller) InstallFromRequirements(
    ctx context.Context,
    reqFile string,
) error

// ListInstalled 列出已安装的 Role/Collection
func (i *GalaxyInstaller) ListInstalled() (*InstalledContent, error)

// RemoveRole 删除已安装的 Role
func (i *GalaxyInstaller) RemoveRole(name string) error

// RemoveCollection 删除已安装的 Collection
func (i *GalaxyInstaller) RemoveCollection(namespace, name string) error
```

### 6.3 Tarball 解压

```go
// TarballExtractor tarball 解压器
type TarballExtractor struct{}

// ExtractRole 解压 Role tarball 到目标路径
func (e *TarballExtractor) ExtractRole(
    data []byte,
    destPath string,
    roleName string,
) error

// ExtractCollection 解压 Collection tarball 到目标路径
func (e *TarballExtractor) ExtractCollection(
    data []byte,
    destPath string,
    namespace string,
    name string,
) error

// detectRootDir 检测 tarball 中的根目录
// GitHub archive 通常有一个顶层目录：user-repo-version/
func detectRootDir(tarReader *tar.Reader) (string, error)
```

### 6.4 版本约束

```go
// VersionConstraint 版本约束
type VersionConstraint struct {
    Operator string // "==", ">=", "<=", ">", "<", "~=", "*"
    Version  string
}

// ParseVersionConstraint 解析版本约束字符串
// 例如：">=8.0.0", "~=5.0", "*", "3.1.0"
func ParseVersionConstraint(s string) (*VersionConstraint, error)

// Match 检查版本是否满足约束
func (c *VersionConstraint) Match(version string) bool

// ResolveVersion 从可用版本中选择满足约束的最新版本
func ResolveVersion(
    available []string,
    constraints []VersionConstraint,
) (string, error)
```

---

## 7. requirements.yml

### 7.1 格式规范

`requirements.yml` 声明项目依赖的 Role 和 Collection：

```yaml
---
roles:
  # 基本格式
  - name: geerlingguy.nginx

  # 指定版本
  - name: geerlingguy.nginx
    version: "3.1.0"

  # 从 GitHub 直接安装
  - name: my_role
    src: https://github.com/user/ansible-role-my_role
    version: "main"

  # 指定 scm 类型
  - name: my_role
    src: https://gitlab.com/user/my_role.git
    scm: git
    version: "v2.0.0"

  # 本地 Role
  - name: local_role
    src: ./local_roles/my_role

collections:
  # 基本格式
  - name: community.general

  # 指定版本约束
  - name: community.general
    version: ">=8.0.0"

  # 从 Git 仓库安装
  - name: my_namespace.my_collection
    source: https://github.com/my_namespace/my_collection
    type: git
    version: "main"

  # 从本地路径安装
  - name: my_namespace.my_collection
    source: ./local_collections/my_collection
    type: dir
```

### 7.2 版本约束语法

| 约束 | 含义 | 示例 |
|------|------|------|
| `*` | 任意版本 | `version: "*"` |
| `1.2.3` | 精确版本 | `version: "3.1.0"` |
| `>=1.2.0` | 大于等于 | `version: ">=8.0.0"` |
| `<=1.2.0` | 小于等于 | `version: "<=10.0.0"` |
| `>1.2.0` | 大于 | `version: ">5.0.0"` |
| `<1.2.0` | 小于 | `version: "<12.0.0"` |
| `~=1.2.0` | 兼容版本 | `version: "~=8.0.0"` (>=8.0.0, <9.0.0) |
| `>=1.0,<2.0` | 范围约束 | `version: ">=8.0,<10.0"` |

### 7.3 依赖解析

```yaml
# Collection A 依赖 Collection B
# galaxy.yml (Collection A)
dependencies:
  "community.general": ">=8.0.0"
  "ansible.posix": ">=1.5.0"
```

**依赖解析流程**：

```
requirements.yml
  ├── community.general >=8.0.0
  │     ├── 依赖 ansible.posix >=1.5.0
  │     └── 依赖 community.crypto >=2.0.0
  └── my_namespace.my_collection
        └── 依赖 community.general >=7.0.0

解析结果：
  ansible.posix >=1.5.0
  community.crypto >=2.0.0
  community.general >=8.0.0  (取最高约束)
  my_namespace.my_collection
```

### 7.4 requirements.yml 解析签名

```go
// RequirementsFile requirements.yml 文件结构
type RequirementsFile struct {
    Roles       []RoleRequirement       `yaml:"roles"`
    Collections []CollectionRequirement `yaml:"collections"`
}

// RoleRequirement Role 依赖
type RoleRequirement struct {
    Name   string `yaml:"name"`
    Src    string `yaml:"src"`
    Scm    string `yaml:"scm"`
    Version string `yaml:"version"`
}

// CollectionRequirement Collection 依赖
type CollectionRequirement struct {
    Name    string `yaml:"name"`
    Source  string `yaml:"source"`
    Type    string `yaml:"type"` // galaxy, git, url, dir
    Version string `yaml:"version"`
}

// ParseRequirements 解析 requirements.yml
func ParseRequirements(path string) (*RequirementsFile, error)

// ResolveDependencies 解析依赖树
func ResolveDependencies(
    reqs *RequirementsFile,
    client GalaxyClient,
) (*DependencyTree, error)

// DependencyTree 依赖树
type DependencyTree struct {
    Roles       []ResolvedRole
    Collections []ResolvedCollection
}

// ResolvedRole 解析后的 Role
type ResolvedRole struct {
    Name        string
    Version     string
    DownloadURL string
}

// ResolvedCollection 解析后的 Collection
type ResolvedCollection struct {
    Namespace   string
    Name        string
    Version     string
    DownloadURL string
}
```

---

## 8. 任务拆解

### T11.1 Galaxy 客户端

| 子任务 | 描述 | 依赖 | 验收标准 |
|--------|------|------|----------|
| T11.1.1 | Galaxy API HTTP 客户端 | 无 | 能发送请求并解析 JSON 响应 |
| T11.1.2 | Role 搜索/详情 API | T11.1.1 | 搜索和获取 Role 信息正确 |
| T11.1.3 | Collection 搜索/详情 API | T11.1.1 | 搜索和获取 Collection 信息正确 |
| T11.1.4 | Tarball 下载 | T11.1.1 | 能下载 Role/Collection tarball |
| T11.1.5 | Tarball 解压安装 | T11.1.4 | 正确解压到 roles/ 或 collections/ |
| T11.1.6 | 版本约束解析 | 无 | 解析 >=, <=, ~=, * 等约束 |
| T11.1.7 | 版本选择逻辑 | T11.1.6 | 从可用版本中选择满足约束的最新版 |
| T11.1.8 | requirements.yml 解析 | 无 | 正确解析 roles 和 collections 段 |
| T11.1.9 | 依赖解析 | T11.1.7, T11.1.8 | 递归解析依赖，合并版本约束 |
| T11.1.10 | galaxy install 命令 | T11.1.5, T11.1.9 | 安装 Role/Collection 到正确路径 |
| T11.1.11 | galaxy list 命令 | T11.1.5 | 列出已安装的 Role/Collection |
| T11.1.12 | galaxy remove 命令 | T11.1.5 | 删除已安装的 Role/Collection |
| T11.1.13 | galaxy init 命令 | 无 | 生成 Role/Collection 骨架目录 |
| T11.1.14 | Collection 加载集成 | T11.1.5, P5 引擎 | 执行时能加载 Collection 中的模块/插件 |

**单元测试覆盖**：
- API 客户端：mock HTTP 响应，验证请求参数和响应解析
- Tarball 解压：使用测试 tarball 验证解压逻辑
- 版本约束：各种约束语法的匹配和选择
- requirements.yml：各种格式的解析
- 依赖解析：循环依赖检测、版本冲突处理

**集成测试场景**：
- 完整安装流程：search → download → extract → verify
- requirements.yml 批量安装
- 版本更新：已安装旧版本时升级
- 强制覆盖：`--force` 选项

---

## 附录：常用 Collection 速查

### 官方维护的 Collection

| Collection | 用途 |
|------------|------|
| `ansible.posix` | POSIX 系统管理（at, cron, mount, selinux） |
| `ansible.utils` | 网络工具（ipaddr, macaddr） |
| `ansible.builtin` | 内置模块（随 Ansible 自带） |
| `community.general` | 社区通用模块（最大的 Collection） |
| `community.docker` | Docker 管理 |
| `community.kubernetes` | Kubernetes 管理 |
| `community.aws` | AWS 管理 |
| `community.crypto` | 证书和加密操作 |

### Collection 引用语法

```yaml
# 完整引用（推荐）
- name: Use module from collection
  community.general.nmcli:
    conn_name: eth0
    state: present

# 短引用（需要 collection 加载）
- name: Short form
  nmcli:
    conn_name: eth0
    state: present
```
