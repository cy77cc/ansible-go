# 六、模块详细原理

> 对应阶段：P4 + P6 | 设计文档引用：第六章

模块是 Ansible 的"动词"——每个模块管理一种特定的系统状态（文件、包、服务等）。go-ansible 采用**本地编排 + SSH 命令执行**模式：模块根据参数在本地生成 shell 命令，通过 SSH 在远程主机执行，收集 stdout/stderr/exit code，解析为统一的 `Result` 结构。

---

## 目录

- [一、模块执行模型总览](#一模块执行模型总览)
- [二、文件管理类](#二文件管理类)
  - [copy](#copy) | [template](#template) | [file](#file) | [stat](#stat) | [find](#find)
  - [lineinfile](#lineinfile) | [blockinfile](#blockinfile) | [synchronize](#synchronize) | [fetch](#fetch) | [unarchive](#unarchive)
- [三、包管理类](#三包管理类)
  - [yum](#yum) | [apt](#apt) | [dnf](#dnf) | [pip](#pip)
- [四、服务管理类](#四服务管理类)
  - [service](#service) | [systemd](#systemd)
- [五、命令执行类](#五命令执行类)
  - [shell](#shell) | [command](#command) | [script](#script) | [raw](#raw) | [expect](#expect)
- [六、用户管理类](#六用户管理类)
  - [user](#user) | [group](#group) | [authorized_key](#authorized_key)
- [七、网络类](#七网络类)
  - [uri](#uri) | [get_url](#get_url) | [wait_for](#wait_for) | [wait_for_connection](#wait_for_connection)
- [八、系统类](#八系统类)
  - [setup](#setup) | [debug](#debug) | [assert](#assert) | [set_fact](#set_fact) | [hostname](#hostname)
  - [cron](#cron) | [sysctl](#sysctl) | [meta](#meta)
- [九、异步与其他](#九异步与其他)
  - [async_status](#async_status) | [pause](#pause)
- [十、总结表格](#十总结表格)

---

## 一、模块执行模型总览

### Ansible 原生模式 vs go-ansible 模式

**Ansible（Python）的执行流程：**
1. 控制节点将模块代码（Python 脚本）序列化
2. 通过 SSH 传到远程主机的临时目录（`~/.ansible/tmp/`）
3. 在远程执行 Python 脚本
4. 脚本输出 JSON 到 stdout
5. 控制节点解析 JSON，清理临时文件

**go-ansible 的执行流程：**
1. 模块根据参数在**本地**生成 shell 命令字符串
2. 通过 SSH 的 `Exec()` 方法在远程执行命令
3. 收集 stdout/stderr/exit code
4. 解析为统一的 `Result` 结构

这意味着 go-ansible 的每个模块本质上是一个"shell 命令生成器"——它知道如何检查当前状态、如何达到目标状态、如何判断是否发生了变更。

### 接口定义

```go
type Module interface {
    Name() string
    Args() []ModuleArg
    Run(ctx ExecContext) (Result, error)
    SupportsCheckMode() bool
}

type ExecContext struct {
    Host       *Host
    Args       map[string]any
    Connection Connection
    CheckMode  bool
    Diff       bool
    Variables  map[string]any
}

type Result struct {
    Changed bool
    Failed  bool
    Msg     string
    Stdout  string
    Stderr  string
    Rc      int
    Diff    *DiffResult
    Extra   map[string]any
}
```

### 幂等性三原则

每个模块的 `Run()` 方法遵循相同模式：

1. **检查**：通过 SSH 执行命令，获取当前状态
2. **比较**：当前状态 vs 目标状态（来自参数）
3. **执行**：仅在有差异时执行变更命令，返回 `Changed: true`

Check Mode（`--check`）下，只执行步骤 1 和 2，跳过步骤 3，返回"是否会发生变更"。

---

## 二、文件管理类

### `copy`

**解决什么问题：** 将本地文件拷贝到远程主机的指定路径，可同时设置所有者、权限。

**Ansible 内部实现：**
- Ansible 使用 SFTP 子系统传输文件，不生成 shell 命令
- 传输前先计算本地文件的 SHA-256 校验和
- 通过 SSH 执行 `stat` 命令获取远程文件的校验和
- 仅在校验和不同时传输文件（幂等性）
- 传输后执行 `chmod`/`chown` 设置权限

**go-ansible 生成的 Shell 命令：**
```bash
# 1. 检查远程文件的校验和
sha256sum /etc/app.conf 2>/dev/null | cut -d' ' -f1

# 2. 如果校验和不同，通过 SFTP 传输文件（非 shell 命令）

# 3. 设置权限
chmod 0644 /etc/app.conf
chown root:root /etc/app.conf

# 4. 如果 backup=true，备份原文件
cp /etc/app.conf /etc/app.conf.2026-05-26@12:00:00~
```

**Go 实现要点：**
```go
type CopyModule struct{}
func (m *CopyModule) Name() string
func (m *CopyModule) Args() []ModuleArg
func (m *CopyModule) Run(ctx ExecContext) (Result, error)
func (m *CopyModule) SupportsCheckMode() bool
```
- 使用 `Connection.PutFile()` 传输文件
- 先用 `Connection.Exec("sha256sum ...")` 检查校验和
- backup 机制：传输前用 `cp` 备份原文件

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `src` | path | 是 | - | 本地源文件路径 |
| `dest` | path | 是 | - | 远程目标路径 |
| `owner` | str | 否 | - | 文件所有者 |
| `group` | str | 否 | - | 文件所属组 |
| `mode` | str | 否 | - | 文件权限（如 `0644`） |
| `backup` | bool | 否 | false | 覆盖前是否备份 |
| `force` | bool | 否 | true | 目标存在但内容不同时是否覆盖 |

**实战示例：**
```yaml
- name: Deploy application config
  copy:
    src: files/app.conf
    dest: /etc/app.conf
    owner: root
    group: root
    mode: '0644'
    backup: true
```

---

### `template`

**解决什么问题：** 将 Jinja2/Go-template 模板渲染后传输到远程主机。与 `copy` 的区别是：内容经过模板渲染，可以嵌入变量。

**Ansible 内部实现：**
- 在控制节点用模板引擎渲染模板文件（`{{ }}` 替换为变量值）
- 渲染后计算内容的 SHA-256 校验和
- 与远程文件校验和比较
- 仅在内容不同时传输

**go-ansible 生成的 Shell 命令：**
```bash
# 1. 检查远程文件校验和
sha256sum /etc/nginx/nginx.conf 2>/dev/null | cut -d' ' -f1

# 2. 模板渲染在本地完成（Go text/template + Sprig）

# 3. 通过 SFTP 传输渲染后的文件

# 4. 设置权限
chmod 0644 /etc/nginx/nginx.conf
chown root:root /etc/nginx/nginx.conf
```

**Go 实现要点：**
```go
type TemplateModule struct{}
func (m *TemplateModule) Name() string
func (m *TemplateModule) Args() []ModuleArg
func (m *TemplateModule) Run(ctx ExecContext) (Result, error)
func (m *TemplateModule) SupportsCheckMode() bool
```
- 先调用 `TemplateEngine.RenderFile(src, variables)` 渲染模板
- 再走 `copy` 模块的 SFTP + 校验和逻辑
- 模板路径相对于 playbook 目录或 role 的 `templates/` 目录

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `src` | path | 是 | - | 模板文件路径（相对于 playbook 或 role/templates） |
| `dest` | path | 是 | - | 远程目标路径 |
| `owner` | str | 否 | - | 文件所有者 |
| `group` | str | 否 | - | 文件所属组 |
| `mode` | str | 否 | - | 文件权限 |
| `backup` | bool | 否 | false | 覆盖前是否备份 |

**实战示例：**
```yaml
# templates/nginx.conf.j2
# server {
#     listen {{ http_port }};
#     server_name {{ server_name }};
# }

- name: Deploy nginx config from template
  template:
    src: nginx.conf.j2
    dest: /etc/nginx/nginx.conf
    owner: root
    mode: '0644'
  notify: restart nginx
```

---

### `file`

**解决什么问题：** 管理文件、目录、符号链接的状态——创建、删除、设置权限、创建链接等。

**Ansible 内部实现：**
- 根据 `state` 参数决定操作类型
- 先用 `stat` 命令检查目标当前状态
- 比较当前状态与目标状态，仅在有差异时执行

**go-ansible 生成的 Shell 命令：**
```bash
# state=directory: 创建目录
stat /opt/app 2>/dev/null || mkdir -p /opt/app
chmod 0755 /opt/app
chown app:app /opt/app

# state=absent: 删除
rm -rf /opt/app

# state=link: 创建符号链接
ln -sf /opt/app/current /var/www/app

# state=file + mode: 设置权限（文件必须已存在）
chmod 0644 /etc/app.conf
chown root:root /etc/app.conf

# state=touch: 创建空文件或更新时间戳
touch /var/log/app.log
```

**Go 实现要点：**
```go
type FileModule struct{}
func (m *FileModule) Name() string
func (m *FileModule) Args() []ModuleArg
func (m *FileModule) Run(ctx ExecContext) (Result, error)
func (m *FileModule) SupportsCheckMode() bool
```
- `state` 是枚举：`file`, `directory`, `absent`, `link`, `touch`, `hard`
- `state=directory` 时递归设置权限（`recurse=true`）
- `state=link` 时 `src` 是链接目标，`dest` 是链接路径

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `path` | path | 是 | - | 目标路径 |
| `state` | str | 否 | file | file/directory/absent/link/touch/hard |
| `mode` | str | 否 | - | 权限 |
| `owner` | str | 否 | - | 所有者 |
| `group` | str | 否 | - | 所属组 |
| `recurse` | bool | 否 | false | 递归设置权限 |
| `src` | path | 否 | - | 链接目标（state=link 时必填） |

**实战示例：**
```yaml
- name: Create application directory
  file:
    path: /opt/myapp
    state: directory
    owner: app
    group: app
    mode: '0755'

- name: Create symlink to current release
  file:
    src: /opt/myapp/releases/v2.0
    dest: /opt/myapp/current
    state: link

- name: Remove old logs
  file:
    path: /var/log/myapp-old.log
    state: absent
```

---

### `stat`

**解决什么问题：** 获取远程文件的元数据（大小、权限、校验和、类型等），用于条件判断。

**Ansible 内部实现：**
- 纯只读操作，不修改任何状态
- 在远程执行 `stat` 命令并解析 JSON 输出
- 结果存储在 `register` 变量中供后续使用

**go-ansible 生成的 Shell 命令：**
```bash
stat -c '{"size":%s,"mode":"%a","uid":%u,"gid":%g,"mtime":%Y}' /etc/app.conf
# 或获取校验和
sha256sum /etc/app.conf | cut -d' ' -f1
```

**Go 实现要点：**
```go
type StatModule struct{}
func (m *StatModule) Name() string
func (m *StatModule) Args() []ModuleArg
func (m *StatModule) Run(ctx ExecContext) (Result, error)
func (m *StatModule) SupportsCheckMode() bool  // true（只读）
```
- 解析 `stat` 命令的 JSON 输出为结构化数据
- 结果放入 `Result.Extra`，供 `register` 使用

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `path` | path | 是 | - | 文件路径 |
| `checksum_algorithm` | str | 否 | sha1 | 校验和算法 |
| `get_checksum` | bool | 否 | true | 是否获取校验和 |

**实战示例：**
```yaml
- name: Check if config exists
  stat:
    path: /etc/app.conf
  register: config_stat

- name: Deploy config if missing
  copy:
    src: files/app.conf
    dest: /etc/app.conf
  when: not config_stat.stat.exists
```

---

### `find`

**解决什么问题：** 在远程主机上按条件查找文件，返回匹配的文件列表。

**Ansible 内部实现：**
- 纯只读操作
- 组合 `find` 命令的参数（`-name`, `-type`, `-mtime`, `-size` 等）

**go-ansible 生成的 Shell 命令：**
```bash
find /var/log -name "*.log" -type f -mtime +30 -size +1M
```

**Go 实现要点：**
```go
type FindModule struct{}
func (m *FindModule) Name() string
func (m *FindModule) Args() []ModuleArg
func (m *FindModule) Run(ctx ExecContext) (Result, error)
func (m *FindModule) SupportsCheckMode() bool  // true（只读）
```

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `paths` | list | 是 | - | 搜索路径列表 |
| `patterns` | list | 否 | - | 文件名模式（glob） |
| `file_type` | str | 否 | file | file/directory/any |
| `age` | str | 否 | - | 文件年龄（如 `30d`） |
| `size` | str | 否 | - | 文件大小（如 `1M`） |
| `recurse` | bool | 否 | false | 是否递归 |

**实战示例：**
```yaml
- name: Find old log files
  find:
    paths: /var/log
    patterns: "*.log"
    age: 30d
  register: old_logs

- name: Remove old logs
  file:
    path: "{{ item.path }}"
    state: absent
  loop: "{{ old_logs.files }}"
```

---

### `lineinfile`

**解决什么问题：** 确保文件中某一行存在或不存在。常用于配置文件的单行修改。

**Ansible 内部实现：**
- 用 `grep -E` 检查正则表达式是否匹配文件中的某行
- 匹配到：用 `sed` 替换该行（如果内容不同）
- 未匹配到：用 `sed` 或 `echo >>` 在指定位置插入
- `state=absent`：用 `sed` 删除匹配的行

**go-ansible 生成的 Shell 命令：**
```bash
# 检查是否存在匹配行
grep -E '^SELINUX=' /etc/selinux/config

# 替换匹配行
sed -i 's/^SELINUX=.*/SELINUX=enforcing/' /etc/selinux/config

# 插入新行（在某行之后）
sed -i '/^#SELINUX=/a SELINUX=enforcing' /etc/selinux/config

# 删除匹配行
sed -i '/^SELINUX=/d' /etc/selinux/config
```

**Go 实现要点：**
```go
type LineinfileModule struct{}
func (m *LineinfileModule) Name() string
func (m *LineinfileModule) Args() []ModuleArg
func (m *LineinfileModule) Run(ctx ExecContext) (Result, error)
func (m *LineinfileModule) SupportsCheckMode() bool
```
- 用 `grep -E` 检查，`sed -i` 修改
- `regexp` 用于匹配现有行，`line` 是替换/插入的内容
- `insertafter`/`insertbefore` 控制插入位置

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `path` | path | 是 | - | 文件路径 |
| `line` | str | 条件 | - | 要确保的行内容 |
| `regexp` | str | 否 | - | 匹配现有行的正则 |
| `state` | str | 否 | present | present/absent |
| `insertafter` | str | 否 | EOF | 插入位置（正则或 EOF） |
| `insertbefore` | str | 否 | - | 插入位置（正则或 BOF） |
| `backrefs` | bool | 否 | false | 如果 regexp 不匹配则不操作 |

**实战示例：**
```yaml
- name: Set SELinux to enforcing
  lineinfile:
    path: /etc/selinux/config
    regexp: '^SELINUX='
    line: 'SELINUX=enforcing'

- name: Add custom host entry
  lineinfile:
    path: /etc/hosts
    regexp: 'myserver'
    line: '192.168.1.100 myserver.local myserver'
    insertafter: '^127.0.0.1'
```

---

### `blockinfile`

**解决什么问题：** 在文件中插入/更新一个文本块，用标记注释包裹，便于后续识别和更新。

**Ansible 内部实现：**
- 用 `grep` 检查标记注释是否存在
- 存在：用 `sed` 替换标记之间的内容
- 不存在：在指定位置插入整个标记块

**go-ansible 生成的 Shell 命令：**
```bash
# 检查标记是否存在
grep -F '# BEGIN MANAGED BLOCK' /etc/hosts

# 替换标记块（sed 多行替换）
sed -i '/# BEGIN MANAGED BLOCK/,/# END MANAGED BLOCK/c\# BEGIN MANAGED BLOCK\n192.168.1.100 myserver\n# END MANAGED BLOCK' /etc/hosts
```

**Go 实现要点：**
```go
type BlockinfileModule struct{}
func (m *BlockinfileModule) Name() string
func (m *BlockinfileModule) Args() []ModuleArg
func (m *BlockinfileModule) Run(ctx ExecContext) (Result, error)
func (m *BlockinfileModule) SupportsCheckMode() bool
```

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `path` | path | 是 | - | 文件路径 |
| `block` | str | 是 | - | 文本块内容 |
| `marker` | str | 否 | `# {mark} ANSIBLE MANAGED BLOCK` | 标记模板 |
| `state` | str | 否 | present | present/absent |
| `insertafter` | str | 否 | EOF | 插入位置 |

**实战示例：**
```yaml
- name: Add custom hosts entry
  blockinfile:
    path: /etc/hosts
    block: |
      192.168.1.10 web1.local web1
      192.168.1.11 web2.local web2
    marker: "# {mark} APP SERVERS"
```

---

### `synchronize`

**解决什么问题：** 基于 rsync 的文件同步，支持增量传输、删除目标多余文件。

**Ansible 内部实现：**
- 在远程执行 `rsync` 命令（通过 SSH 通道）
- rsync 自带增量传输（只传差异部分）

**go-ansible 生成的 Shell 命令：**
```bash
rsync --delay-updates -F --compress --archive --rsh='ssh -S none' \
  --out-format='<<CHANGED>>%i %n%L' /local/path/ user@host:/remote/path/
```

**Go 实现要点：**
```go
type SynchronizeModule struct{}
func (m *SynchronizeModule) Name() string
func (m *SynchronizeModule) Args() []ModuleArg
func (m *SynchronizeModule) Run(ctx ExecContext) (Result, error)
func (m *SynchronizeModule) SupportsCheckMode() bool
```
- 需要远程主机安装 rsync
- 通过 SSH 执行 rsync，利用 SSH 连接参数

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `src` | path | 是 | - | 本地源路径 |
| `dest` | path | 是 | - | 远程目标路径 |
| `mode` | str | 否 | push | push/pull |
| `delete` | bool | 否 | false | 删除目标多余文件 |
| `recursive` | bool | 否 | true | 递归同步 |

**实战示例：**
```yaml
- name: Sync application files
  synchronize:
    src: /opt/app/dist/
    dest: /var/www/app/
    delete: true
```

---

### `fetch`

**解决什么问题：** 从远程主机拉取文件到本地。与 `copy` 方向相反。

**Ansible 内部实现：**
- 使用 SFTP 从远程下载文件
- 按主机名创建本地目录结构（`dest/hostname/path`）

**go-ansible 生成的 Shell 命令：**
```bash
# 检查远程文件是否存在
stat /etc/app.conf
# 文件传输通过 SFTP（非 shell 命令）
```

**Go 实现要点：**
```go
type FetchModule struct{}
func (m *FetchModule) Name() string
func (m *FetchModule) Args() []ModuleArg
func (m *FetchModule) Run(ctx ExecContext) (Result, error)
func (m *FetchModule) SupportsCheckMode() bool
```
- 使用 `Connection.FetchFile()` 反向传输
- `flat=true` 时不创建主机名子目录

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `src` | path | 是 | - | 远程源文件路径 |
| `dest` | path | 是 | - | 本地目标路径 |
| `flat` | bool | 否 | false | 不创建主机名子目录 |

**实战示例：**
```yaml
- name: Fetch remote logs
  fetch:
    src: /var/log/app.log
    dest: ./logs/
    flat: true
```

---

### `unarchive`

**解决什么问题：** 在远程主机解压归档文件（tar、zip 等）。

**Ansible 内部实现：**
- `remote_src=false`（默认）：先通过 SFTP 传输本地归档文件到远程，再解压
- `remote_src=true`：直接在远程解压已存在的归档文件
- 幂等性：检查目标目录是否存在

**go-ansible 生成的 Shell 命令：**
```bash
# tar 归档
tar -xf /tmp/app.tar.gz -C /opt/app

# zip 归档
unzip -o /tmp/app.zip -d /opt/app

# 检查目标是否已存在（幂等性）
stat /opt/app/extracted_marker
```

**Go 实现要点：**
```go
type UnarchiveModule struct{}
func (m *UnarchiveModule) Name() string
func (m *UnarchiveModule) Args() []ModuleArg
func (m *UnarchiveModule) Run(ctx ExecContext) (Result, error)
func (m *UnarchiveModule) SupportsCheckMode() bool
```

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `src` | path | 是 | - | 归档文件路径 |
| `dest` | path | 是 | - | 解压目标目录 |
| `remote_src` | bool | 否 | false | 归档文件是否已在远程 |
| `creates` | path | 否 | - | 如果此路径存在则跳过 |

**实战示例：**
```yaml
- name: Extract application archive
  unarchive:
    src: files/app-v2.0.tar.gz
    dest: /opt/app/
    creates: /opt/app/v2.0
```

---

## 三、包管理类

### `yum`

**解决什么问题：** 管理 RPM 系统的软件包（CentOS/RHEL/Fedora）。

**Ansible 内部实现：**
- 用 `rpm -q` 检查包是否已安装
- 根据 `state` 参数决定操作：`present`→安装，`absent`→卸载，`latest`→升级
- 支持批量安装（`name` 接受列表）

**go-ansible 生成的 Shell 命令：**
```bash
# 检查包是否安装
rpm -q nginx

# 安装（state=present）
yum -y install nginx

# 卸载（state=absent）
yum -y remove nginx

# 升级（state=latest）
yum -y update nginx

# 批量安装
yum -y install nginx vim curl

# 启用额外仓库
yum -y --enablerepo=epel install nginx
```

**Go 实现要点：**
```go
type YumModule struct{}
func (m *YumModule) Name() string
func (m *YumModule) Args() []ModuleArg
func (m *YumModule) Run(ctx ExecContext) (Result, error)
func (m *YumModule) SupportsCheckMode() bool
```
- 先 `rpm -q` 检查，再决定是否执行 `yum install/remove`
- `name` 可以是单个包名或逗号分隔的列表
- `state=present` + 已安装 → 返回 `Changed: false`

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str/list | 是 | - | 包名或包名列表 |
| `state` | str | 否 | present | present/absent/latest |
| `enablerepo` | str | 否 | - | 启用的仓库 |
| `disablerepo` | str | 否 | - | 禁用的仓库 |

**实战示例：**
```yaml
- name: Install web server packages
  yum:
    name:
      - nginx
      - php-fpm
      - redis
    state: present

- name: Remove unnecessary packages
  yum:
    name: httpd
    state: absent
```

---

### `apt`

**解决什么问题：** 管理 Debian/Ubuntu 系统的软件包。

**Ansible 内部实现：**
- 用 `dpkg -s` 检查包是否已安装
- 支持 `update_cache` 在安装前更新包索引
- 支持 `deb` 文件直接安装

**go-ansible 生成的 Shell 命令：**
```bash
# 检查包是否安装
dpkg -s nginx 2>/dev/null | grep -q 'Status: install ok installed'

# 更新包索引
apt-get update

# 安装
DEBIAN_FRONTEND=noninteractive apt-get -y install nginx

# 卸载
apt-get -y remove nginx

# 清理缓存
apt-get clean
```

**Go 实现要点：**
```go
type AptModule struct{}
func (m *AptModule) Name() string
func (m *AptModule) Args() []ModuleArg
func (m *AptModule) Run(ctx ExecContext) (Result, error)
func (m *AptModule) SupportsCheckMode() bool
```
- `update_cache` 相当于先执行 `apt-get update`
- `cache_valid_time` 控制缓存有效期（秒），避免重复 update
- 设置 `DEBIAN_FRONTEND=noninteractive` 避免交互提示

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str/list | 是 | - | 包名或列表 |
| `state` | str | 否 | present | present/absent/latest |
| `update_cache` | bool | 否 | false | 安装前是否 apt-get update |
| `cache_valid_time` | int | 否 | 0 | 缓存有效时间（秒） |

**实战示例：**
```yaml
- name: Install packages with cache update
  apt:
    name:
      - nginx
      - python3-pip
    state: present
    update_cache: true
    cache_valid_time: 3600
```

---

### `dnf`

**解决什么问题：** 管理 RPM 系统的软件包（Fedora/RHEL 8+），是 `yum` 的现代替代。

**go-ansible 生成的 Shell 命令：**
```bash
rpm -q nginx
dnf -y install nginx
```

**Go 实现要点：**
```go
type DnfModule struct{}
// 与 YumModule 接口相同，底层命令从 yum 换为 dnf
```

---

### `pip`

**解决什么问题：** 管理 Python 包。

**go-ansible 生成的 Shell 命令：**
```bash
# 检查包是否安装
pip freeze 2>/dev/null | grep -i nginx

# 安装
pip install nginx

# 指定 virtualenv
/opt/venvs/myapp/bin/pip install nginx

# 卸载
pip uninstall -y nginx
```

**Go 实现要点：**
```go
type PipModule struct{}
func (m *PipModule) Name() string
func (m *PipModule) Args() []ModuleArg
func (m *PipModule) Run(ctx ExecContext) (Result, error)
func (m *PipModule) SupportsCheckMode() bool
```

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str/list | 是 | - | 包名 |
| `state` | str | 否 | present | present/absent/latest |
| `virtualenv` | path | 否 | - | virtualenv 路径 |
| `executable` | path | 否 | - | pip 可执行文件路径 |

**实战示例：**
```yaml
- name: Install Python packages in virtualenv
  pip:
    name:
      - django
      - gunicorn
    virtualenv: /opt/myapp/venv
```

---

## 四、服务管理类

### `service`

**解决什么问题：** 管理系统服务的启动/停止/重启和开机自启。通用接口，兼容 init.d、upstart、systemd。

**Ansible 内部实现：**
- 用 `service X status` 或 `systemctl is-active X` 检查当前状态
- 根据 `state` 参数执行相应操作
- `enabled` 控制开机自启

**go-ansible 生成的 Shell 命令：**
```bash
# 检查服务状态
service nginx status
# 或
systemctl is-active nginx

# 启动（state=started）
service nginx start

# 停止（state=stopped）
service nginx stop

# 重启（state=restarted）
service nginx restart

# 设置开机自启（enabled=true）
chkconfig nginx on
# 或
systemctl enable nginx

# 禁止开机自启（enabled=false）
systemctl disable nginx
```

**Go 实现要点：**
```go
type ServiceModule struct{}
func (m *ServiceModule) Name() string
func (m *ServiceModule) Args() []ModuleArg
func (m *ServiceModule) Run(ctx ExecContext) (Result, error)
func (m *ServiceModule) SupportsCheckMode() bool
```
- 需要检测系统使用 init.d 还是 systemd
- `pattern` 参数用于没有标准 status 命令的服务（通过 `ps` 检查）

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str | 是 | - | 服务名 |
| `state` | str | 否 | - | started/stopped/restarted/reloaded |
| `enabled` | bool | 否 | - | 是否开机自启 |
| `pattern` | str | 否 | - | 进程匹配模式（用于 status 检查） |

**实战示例：**
```yaml
- name: Ensure nginx is running and enabled
  service:
    name: nginx
    state: started
    enabled: true

- name: Restart nginx after config change
  service:
    name: nginx
    state: restarted
```

---

### `systemd`

**解决什么问题：** 专用于 systemd 的服务管理，支持 `daemon_reload` 等 systemd 特有功能。

**go-ansible 生成的 Shell 命令：**
```bash
# 检查状态
systemctl is-active nginx
systemctl is-enabled nginx

# 启动/停止/重启
systemctl start nginx
systemctl stop nginx
systemctl restart nginx

# 开机自启
systemctl enable nginx
systemctl disable nginx

# 重载 systemd 配置
systemctl daemon-reload

# 用户级服务
systemctl --user start myservice
```

**Go 实现要点：**
```go
type SystemdModule struct{}
func (m *SystemdModule) Name() string
func (m *SystemdModule) Args() []ModuleArg
func (m *SystemdModule) Run(ctx ExecContext) (Result, error)
func (m *SystemdModule) SupportsCheckMode() bool
```

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str | 是 | - | 服务名 |
| `state` | str | 否 | - | started/stopped/restarted/reloaded |
| `enabled` | bool | 否 | - | 是否开机自启 |
| `daemon_reload` | bool | 否 | false | 执行前是否 daemon-reload |
| `user` | bool | 否 | false | 是否为用户级服务 |

**实战示例：**
```yaml
- name: Deploy and start custom service
  systemd:
    name: myapp
    state: started
    enabled: true
    daemon_reload: true
```

---

## 五、命令执行类

### `shell`

**解决什么问题：** 在远程主机执行 shell 命令（支持管道、重定向、环境变量等 shell 特性）。

**Ansible 内部实现：**
- 将命令包装为 `/bin/sh -c "用户命令"`
- 支持 `creates`/`removes` 实现伪幂等
- **默认不幂等**——每次执行都会运行命令

**go-ansible 生成的 Shell 命令：**
```bash
# 基本执行
/bin/sh -c 'uptime'

# 带管道
/bin/sh -c 'ps aux | grep nginx | wc -l'

# 带 creates 检查（幂等性）
test -f /tmp/already_ran || /bin/sh -c '/opt/setup.sh'

# 带 removes 检查
test -f /tmp/lockfile && /bin/sh -c '/opt/cleanup.sh'

# 切换目录
/bin/sh -c 'cd /opt/app && make build'
```

**Go 实现要点：**
```go
type ShellModule struct{}
func (m *ShellModule) Name() string
func (m *ShellModule) Args() []ModuleArg
func (m *ShellModule) Run(ctx ExecContext) (Result, error)
func (m *ShellModule) SupportsCheckMode() bool  // false
```
- 命令通过 `/bin/sh -c` 执行
- `creates`/`removes` 是实现幂等性的关键：先 `test -e` 检查，存在/不存在则跳过
- `chdir` 先 `cd` 再执行

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `cmd` / `_raw_params` | str | 是 | - | 要执行的命令 |
| `chdir` | path | 否 | - | 执行前先 cd 到此目录 |
| `creates` | path | 否 | - | 如果此文件存在则跳过 |
| `removes` | path | 否 | - | 如果此文件不存在则跳过 |
| `executable` | path | 否 | /bin/sh | 指定 shell |

**实战示例：**
```yaml
- name: Check disk usage
  shell: df -h | grep '/$'
  register: disk_usage
  changed_when: false

- name: Run migration script (only once)
  shell: /opt/app/migrate.sh
  args:
    creates: /opt/app/.migrated
```

---

### `command`

**解决什么问题：** 在远程主机执行命令（**不经过 shell**），更安全，不受 shell 注入影响。

**与 shell 的区别：**
- `command` 不支持管道 `|`、重定向 `>`、环境变量 `$VAR`
- 参数直接作为 `exec` 的参数数组，不经过 `/bin/sh`
- 更安全——用户输入不会被 shell 解释

**go-ansible 生成的 Shell 命令：**
```bash
# 直接执行，不经 shell
/usr/bin/uptime

# 带参数
/usr/bin/hostnamectl set-hostname web1

# 带 creates 检查
test -f /tmp/flag || /usr/bin/whoami
```

**Go 实现要点：**
```go
type CommandModule struct{}
func (m *CommandModule) Name() string
func (m *CommandModule) Args() []ModuleArg
func (m *CommandModule) Run(ctx ExecContext) (Result, error)
func (m *CommandModule) SupportsCheckMode() bool  // false
```
- 将命令字符串按空格分割为参数数组
- 用 `Connection.Exec()` 直接执行（不包装 `/bin/sh -c`）

**实战示例：**
```yaml
- name: Get hostname
  command: hostname
  register: hostname_result
  changed_when: false

- name: Run setup script
  command: /opt/setup.sh --mode=production
  args:
    creates: /opt/.setup_done
```

---

### `script`

**解决什么问题：** 将本地脚本传输到远程主机并执行。

**Ansible 内部实现：**
1. 通过 SFTP 将本地脚本上传到远程临时目录
2. `chmod +x` 设置执行权限
3. 执行脚本
4. 清理临时文件

**go-ansible 生成的 Shell 命令：**
```bash
# 上传后执行
chmod +x /tmp/script.sh
/tmp/script.sh arg1 arg2
rm -f /tmp/script.sh
```

**Go 实现要点：**
```go
type ScriptModule struct{}
func (m *ScriptModule) Name() string
func (m *ScriptModule) Args() []ModuleArg
func (m *ScriptModule) Run(ctx ExecContext) (Result, error)
func (m *ScriptModule) SupportsCheckMode() bool  // false
```
- `Connection.PutFile()` 上传脚本
- `Connection.Exec("chmod +x ... && ...")` 执行
- 执行后清理

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `cmd` / `_raw_params` | path | 是 | - | 本地脚本路径 |
| `chdir` | path | 否 | - | 远程执行目录 |
| `creates` | path | 否 | - | 幂等性检查 |

**实战示例：**
```yaml
- name: Run bootstrap script
  script: scripts/bootstrap.sh --env=prod
  args:
    creates: /opt/.bootstrapped
```

---

### `raw`

**解决什么问题：** 执行原始 SSH 命令，不经过模块系统。用于 bootstrap 场景（远程主机没有 Python）。

**go-ansible 生成的 Shell 命令：**
```bash
# 直接执行，完全不处理
yum install -y python3
```

**Go 实现要点：**
```go
type RawModule struct{}
func (m *RawModule) Name() string
func (m *RawModule) Args() []ModuleArg
func (m *RawModule) Run(ctx ExecContext) (Result, error)
func (m *RawModule) SupportsCheckMode() bool  // false
```
- 最简单的模块：直接把参数传给 `Connection.Exec()`
- 不解析结果，直接返回 stdout/stderr

---

### `expect`

**解决什么问题：** 执行交互式命令，自动响应提示（如密码输入）。

**go-ansible 生成的 Shell 命令：**
```bash
# 通过 expect 或 pexpect 实现
expect -c '
  spawn passwd user1
  expect "New password:"
  send "secret123\r"
  expect "Retype new password:"
  send "secret123\r"
  expect eof
'
```

**Go 实现要点：**
```go
type ExpectModule struct{}
func (m *ExpectModule) Name() string
func (m *ExpectModule) Args() []ModuleArg
func (m *ExpectModule) Run(ctx ExecContext) (Result, error)
func (m *ExpectModule) SupportsCheckMode() bool  // false
```
- 需要在远程主机安装 `expect` 命令
- Go 端构造 expect 脚本并执行

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `command` | str | 是 | - | 要执行的命令 |
| `responses` | dict | 是 | - | 提示→响应映射 |
| `timeout` | int | 否 | 30 | 超时秒数 |

**实战示例：**
```yaml
- name: Change user password
  expect:
    command: passwd johndoe
    responses:
      "New password:": "s3cur3P@ss"
      "Retype new password:": "s3cur3P@ss"
    timeout: 10
```

---

## 六、用户管理类

### `user`

**解决什么问题：** 管理系统用户账户——创建、修改、删除。

**Ansible 内部实现：**
- 用 `id` 或 `getent passwd` 检查用户是否存在
- 存在时比较属性（uid、groups、shell 等），仅修改有差异的
- 不存在时用 `useradd` 创建

**go-ansible 生成的 Shell 命令：**
```bash
# 检查用户是否存在
id johndoe 2>/dev/null

# 创建用户（state=present）
useradd -m -s /bin/bash -u 1001 -g users -G docker,sudo johndoe

# 修改用户
usermod -aG docker johndoe
usermod -s /bin/zsh johndoe

# 删除用户（state=absent）
userdel -r johndoe

# 设置密码（加密后的密码哈希）
usermod -p '$6$rounds=656000$...' johndoe

# 创建系统用户（system=true）
useradd -r -s /bin/false -d /var/lib/myapp myapp
```

**Go 实现要点：**
```go
type UserModule struct{}
func (m *UserModule) Name() string
func (m *UserModule) Args() []ModuleArg
func (m *UserModule) Run(ctx ExecContext) (Result, error)
func (m *UserModule) SupportsCheckMode() bool
```
- `id` 检查存在性，`getent passwd` 获取详细信息
- 比较每个属性（uid、gid、shell、home、groups），仅 `usermod` 有差异的
- `password` 参数需要传入加密后的哈希（用 `python -c "import crypt; ..."` 或 `openssl passwd -6`）

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str | 是 | - | 用户名 |
| `state` | str | 否 | present | present/absent |
| `uid` | int | 否 | - | 用户 ID |
| `groups` | list | 否 | - | 附加组 |
| `shell` | str | 否 | - | 登录 shell |
| `home` | path | 否 | - | 家目录 |
| `create_home` | bool | 否 | true | 是否创建家目录 |
| `password` | str | 否 | - | 加密后的密码哈希 |
| `system` | bool | 否 | false | 是否为系统用户 |

**实战示例：**
```yaml
- name: Create application user
  user:
    name: myapp
    system: true
    shell: /bin/false
    home: /var/lib/myapp
    create_home: true

- name: Add user to docker group
  user:
    name: johndoe
    groups: docker
    append: true
```

---

### `group`

**解决什么问题：** 管理系统用户组。

**go-ansible 生成的 Shell 命令：**
```bash
# 检查组是否存在
getent group mygroup

# 创建组
groupadd -g 1001 mygroup

# 修改 GID
groupmod -g 1002 mygroup

# 删除组
groupdel mygroup
```

**Go 实现要点：**
```go
type GroupModule struct{}
func (m *GroupModule) Name() string
func (m *GroupModule) Args() []ModuleArg
func (m *GroupModule) Run(ctx ExecContext) (Result, error)
func (m *GroupModule) SupportsCheckMode() bool
```

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str | 是 | - | 组名 |
| `state` | str | 否 | present | present/absent |
| `gid` | int | 否 | - | 组 ID |
| `system` | bool | 否 | false | 是否为系统组 |

---

### `authorized_key`

**解决什么问题：** 管理用户的 SSH 公钥（`~/.ssh/authorized_keys` 文件）。

**go-ansible 生成的 Shell 命令：**
```bash
# 检查公钥是否存在
grep -F 'ssh-rsa AAAAB3...' ~/.ssh/authorized_keys

# 添加公钥
echo 'ssh-rsa AAAAB3...' >> ~/.ssh/authorized_keys

# 删除公钥
sed -i '/ssh-rsa AAAAB3.../d' ~/.ssh/authorized_keys

# 确保 .ssh 目录存在
mkdir -p ~/.ssh && chmod 700 ~/.ssh
```

**Go 实现要点：**
```go
type AuthorizedKeyModule struct{}
func (m *AuthorizedKeyModule) Name() string
func (m *AuthorizedKeyModule) Args() []ModuleArg
func (m *AuthorizedKeyModule) Run(ctx ExecContext) (Result, error)
func (m *AuthorizedKeyModule) SupportsCheckMode() bool
```
- 用 `grep -F` 检查 key 是否已存在
- 确保 `~/.ssh` 目录和 `authorized_keys` 文件存在且权限正确

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `user` | str | 是 | - | 目标用户 |
| `key` | str | 是 | - | SSH 公钥内容 |
| `state` | str | 否 | present | present/absent |
| `key_options` | str | 否 | - | 密钥选项（如 `no-port-forwarding`） |

**实战示例：**
```yaml
- name: Add deploy key
  authorized_key:
    user: deploy
    key: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... deploy@workstation"
    state: present
```

---

## 七、网络类

### `uri`

**解决什么问题：** 发送 HTTP 请求（GET/POST/PUT/DELETE 等），常用于 API 调用和健康检查。

**go-ansible 生成的 Shell 命令：**
```bash
# GET 请求
curl -s -o /tmp/response.json -w '%{http_code}' https://api.example.com/health

# POST 请求
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"name":"test"}' https://api.example.com/items

# 带认证
curl -s -u admin:password https://api.example.com/admin
```

**Go 实现要点：**
```go
type UriModule struct{}
func (m *UriModule) Name() string
func (m *UriModule) Args() []ModuleArg
func (m *UriModule) Run(ctx ExecContext) (Result, error)
func (m *UriModule) SupportsCheckMode() bool  // false
```
- 可以用 Go 的 `net/http` 代替 curl（更灵活）
- 或通过 SSH 执行 curl 命令
- `status_code` 验证响应码，不匹配则 Failed

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `url` | str | 是 | - | 请求 URL |
| `method` | str | 否 | GET | HTTP 方法 |
| `body` | str | 否 | - | 请求体 |
| `headers` | dict | 否 | - | 请求头 |
| `status_code` | int | 否 | 200 | 期望的状态码 |
| `return_content` | bool | 否 | false | 是否返回响应内容 |

**实战示例：**
```yaml
- name: Wait for API to be ready
  uri:
    url: http://localhost:8080/health
    status_code: 200
  register: health
  until: health.status == 200
  retries: 30
  delay: 5
```

---

### `get_url`

**解决什么问题：** 从 URL 下载文件到远程主机。

**go-ansible 生成的 Shell 命令：**
```bash
# 下载文件
curl -sSL -o /tmp/file.zip https://example.com/file.zip

# 带校验和验证
sha256sum -c <<< 'abc123...  /tmp/file.zip'

# wget 替代
wget -q -O /tmp/file.zip https://example.com/file.zip
```

**Go 实现要点：**
```go
type GetUrlModule struct{}
func (m *GetUrlModule) Name() string
func (m *GetUrlModule) Args() []ModuleArg
func (m *GetUrlModule) Run(ctx ExecContext) (Result, error)
func (m *GetUrlModule) SupportsCheckMode() bool
```
- 下载后校验 checksum（sha256/sha1/md5）
- 幂等性：先检查目标文件的校验和

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `url` | str | 是 | - | 下载 URL |
| `dest` | path | 是 | - | 本地保存路径 |
| `checksum` | str | 否 | - | 校验和（`sha256:abc...`） |
| `mode` | str | 否 | - | 文件权限 |

---

### `wait_for`

**解决什么问题：** 等待某个条件满足（端口打开、文件存在、正则匹配等）。常用于服务启动后的健康检查。

**go-ansible 生成的 Shell 命令：**
```bash
# 等待端口打开
while ! nc -z localhost 8080; do sleep 2; done

# 等待文件存在
while [ ! -f /var/run/app.pid ]; do sleep 2; done

# 等待文件中出现匹配行
while ! grep -q 'ready' /var/log/app.log; do sleep 2; done
```

**Go 实现要点：**
```go
type WaitForModule struct{}
func (m *WaitForModule) Name() string
func (m *WaitForModule) Args() []ModuleArg
func (m *WaitForModule) Run(ctx ExecContext) (Result, error)
func (m *WaitForModule) SupportsCheckMode() bool  // false
```
- 轮询循环：每 `delay` 秒检查一次，最多 `timeout` 秒
- Go 实现可用 `context.WithTimeout` + `time.Ticker`

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `host` | str | 否 | 127.0.0.1 | 目标主机 |
| `port` | int | 否 | - | 目标端口 |
| `state` | str | 否 | started | started/stopped/drained |
| `timeout` | int | 否 | 300 | 超时秒数 |
| `delay` | int | 否 | 0 | 初始等待秒数 |
| `path` | path | 否 | - | 等待文件存在 |
| `search_regex` | str | 否 | - | 等待文件内容匹配 |

**实战示例：**
```yaml
- name: Wait for database to be ready
  wait_for:
    host: db-server
    port: 5432
    timeout: 60
    delay: 5

- name: Wait for app to write PID file
  wait_for:
    path: /var/run/app.pid
    timeout: 30
```

---

### `wait_for_connection`

**解决什么问题：** 等待远程主机可连接（SSH 可用）。常用于等待新机器启动。

**go-ansible 生成的 Shell 命令：**
```bash
# 尝试 SSH 连接，失败则重试
while ! ssh -o ConnectTimeout=5 host 'echo ok'; do sleep 5; done
```

**Go 实现要点：**
```go
type WaitForConnectionModule struct{}
func (m *WaitForConnectionModule) Name() string
func (m *WaitForConnectionModule) Args() []ModuleArg
func (m *WaitForConnectionModule) Run(ctx ExecContext) (Result, error)
func (m *WaitForConnectionModule) SupportsCheckMode() bool
```

---

## 八、系统类

### `setup`

**解决什么问题：** 收集远程主机的系统信息（Facts），注入到变量上下文中供后续使用。

**Ansible 内部实现：**
- 执行一系列 shell 命令收集系统信息
- 解析输出为结构化数据
- 注入到变量上下文，以 `ansible_*` 前缀访问

**go-ansible 生成的 Shell 命令：**
```bash
# 操作系统信息
cat /etc/os-release
uname -srm

# 硬件信息
nproc                    # CPU 核数
free -b                  # 内存信息
cat /proc/cpuinfo        # CPU 详情

# 网络信息
ip -4 addr show          # IPv4 地址
ip route show default    # 默认网关
cat /etc/resolv.conf     # DNS 配置

# 用户信息
id -un                   # 当前用户
id -u                    # UID
id -g                    # GID

# 时间信息
date +%Y-%m-%d %H:%M:%S
timedatectl show --property=Timezone

# 虚拟化信息
systemd-detect-virt 2>/dev/null || echo "physical"
```

**Go 实现要点：**
```go
type SetupModule struct{}
func (m *SetupModule) Name() string
func (m *SetupModule) Args() []ModuleArg
func (m *SetupModule) Run(ctx ExecContext) (Result, error)
func (m *SetupModule) SupportsCheckMode() bool  // true（只读）
```
- 定义一个 `fact → shell command` 映射表
- 批量执行命令，解析输出
- `gather_subset` 控制收集哪些类别
- 结果放入 `Result.Extra`，引擎注入变量上下文

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `gather_subset` | list | 否 | all | 收集类别：hardware/network/virtual/distribution/user/date_time |
| `filter` | str | 否 | - | 过滤返回的 facts（glob 模式） |

**收集的 Facts 示例：**
```json
{
  "ansible_os_family": "RedHat",
  "ansible_distribution": "CentOS",
  "ansible_distribution_version": "9",
  "ansible_kernel": "5.14.0-162.el9.x86_64",
  "ansible_architecture": "x86_64",
  "ansible_memtotal_mb": 16384,
  "ansible_processor_vcpus": 4,
  "ansible_default_ipv4": {"address": "192.168.1.10", "gateway": "192.168.1.1"},
  "ansible_hostname": "web1",
  "ansible_fqdn": "web1.example.com",
  "ansible_user": "root",
  "ansible_date_time": {"iso8601": "2026-05-26T12:00:00Z"}
}
```

---

### `debug`

**解决什么问题：** 输出调试信息或变量值，不执行任何远程操作。

**Go 实现要点：**
```go
type DebugModule struct{}
func (m *DebugModule) Name() string
func (m *DebugModule) Args() []ModuleArg
func (m *DebugModule) Run(ctx ExecContext) (Result, error)
func (m *DebugModule) SupportsCheckMode() bool  // true
```
- 纯本地操作，不调用 `Connection.Exec()`
- `msg` 输出固定消息，`var` 输出变量值
- `verbosity` 控制在几级 `-v` 下才显示

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `msg` | str | 否 | Hello world! | 输出消息 |
| `var` | str | 否 | - | 要显示的变量名 |
| `verbosity` | int | 否 | 0 | 显示所需的最低 verbosity 级别 |

**实战示例：**
```yaml
- name: Show variable value
  debug:
    var: ansible_facts.ansible_hostname

- name: Print message
  debug:
    msg: "Deploying to {{ inventory_hostname }}:{{ http_port }}"
```

---

### `assert`

**解决什么问题：** 断言条件为真，否则失败。用于前置条件检查。

**Go 实现要点：**
```go
type AssertModule struct{}
func (m *AssertModule) Name() string
func (m *AssertModule) Args() []ModuleArg
func (m *AssertModule) Run(ctx ExecContext) (Result, error)
func (m *AssertModule) SupportsCheckMode() bool  // true
```
- 纯本地操作
- `that` 是条件列表，全部为真才通过
- 使用模板引擎求值条件表达式

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `that` | list | 是 | - | 条件表达式列表 |
| `fail_msg` | str | 否 | Assertion failed | 失败消息 |
| `success_msg` | str | 否 | All assertions passed | 成功消息 |

**实战示例：**
```yaml
- name: Verify OS is supported
  assert:
    that:
      - ansible_os_family == "RedHat"
      - ansible_distribution_major_version | int >= 8
    fail_msg: "Only RHEL 8+ is supported"
```

---

### `set_fact`

**解决什么问题：** 在运行时设置变量，供后续 task 使用。

**Go 实现要点：**
```go
type SetFactModule struct{}
func (m *SetFactModule) Name() string
func (m *SetFactModule) Args() []ModuleArg
func (m *SetFactModule) Run(ctx ExecContext) (Result, error)
func (m *SetFactModule) SupportsCheckMode() bool  // true
```
- 纯本地操作
- 返回的 `Result.Extra` 中包含要设置的变量
- 引擎将这些变量注入到当前主机的变量上下文

**实战示例：**
```yaml
- name: Set deployment version
  set_fact:
    deploy_version: "2.0.1"
    deploy_env: "production"

- name: Use the fact
  debug:
    msg: "Deploying {{ deploy_version }} to {{ deploy_env }}"
```

---

### `hostname`

**解决什么问题：** 设置系统主机名。

**go-ansible 生成的 Shell 命令：**
```bash
# 检查当前主机名
hostname

# 设置主机名
hostnamectl set-hostname web1.example.com
```

**Go 实现要点：**
```go
type HostnameModule struct{}
func (m *HostnameModule) Name() string
func (m *HostnameModule) Args() []ModuleArg
func (m *HostnameModule) Run(ctx ExecContext) (Result, error)
func (m *HostnameModule) SupportsCheckMode() bool
```

---

### `cron`

**解决什么问题：** 管理 cron 定时任务。

**go-ansible 生成的 Shell 命令：**
```bash
# 检查是否存在同名任务
crontab -l 2>/dev/null | grep -F '# Ansible: backup'

# 添加任务
(crontab -l 2>/dev/null; echo '0 2 * * * /opt/backup.sh # Ansible: backup') | crontab -

# 删除任务
crontab -l 2>/dev/null | grep -v '# Ansible: backup' | crontab -
```

**Go 实现要点：**
```go
type CronModule struct{}
func (m *CronModule) Name() string
func (m *CronModule) Args() []ModuleArg
func (m *CronModule) Run(ctx ExecContext) (Result, error)
func (m *CronModule) SupportsCheckMode() bool
```
- 用 `name` 作为任务标识（注释形式 `# Ansible: name`）
- `crontab -l` 读取，修改后 `crontab -` 写入

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str | 是 | - | 任务名称（标识符） |
| `job` | str | 条件 | - | 要执行的命令 |
| `state` | str | 否 | present | present/absent |
| `minute` | str | 否 | * | 分钟 |
| `hour` | str | 否 | * | 小时 |
| `day` | str | 否 | * | 日 |
| `month` | str | 否 | * | 月 |
| `weekday` | str | 否 | * | 星期 |
| `user` | str | 否 | root | crontab 所属用户 |

**实战示例：**
```yaml
- name: Schedule daily backup
  cron:
    name: "daily backup"
    minute: "0"
    hour: "2"
    job: "/opt/scripts/backup.sh >> /var/log/backup.log 2>&1"

- name: Remove old cron job
  cron:
    name: "old cleanup"
    state: absent
```

---

### `sysctl`

**解决什么问题：** 管理内核参数（`/proc/sys/` 下的值）。

**go-ansible 生成的 Shell 命令：**
```bash
# 检查当前值
sysctl -n net.ipv4.ip_forward

# 临时设置
sysctl -w net.ipv4.ip_forward=1

# 持久化到 /etc/sysctl.conf
grep -q '^net.ipv4.ip_forward' /etc/sysctl.conf || \
  echo 'net.ipv4.ip_forward = 1' >> /etc/sysctl.conf

# 重新加载
sysctl -p /etc/sysctl.conf
```

**Go 实现要点：**
```go
type SysctlModule struct{}
func (m *SysctlModule) Name() string
func (m *SysctlModule) Args() []ModuleArg
func (m *SysctlModule) Run(ctx ExecContext) (Result, error)
func (m *SysctlModule) SupportsCheckMode() bool
```

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | str | 是 | - | 参数名 |
| `value` | str | 是 | - | 参数值 |
| `state` | str | 否 | present | present/absent |
| `reload` | bool | 否 | true | 是否 sysctl -p |
| `sysctl_file` | path | 否 | /etc/sysctl.conf | 持久化文件 |

---

### `meta`

**解决什么问题：** 控制 Playbook 执行流程——刷新 handlers、结束 play、清除 facts 等。

**Go 实现要点：**
```go
type MetaModule struct{}
func (m *MetaModule) Name() string
func (m *MetaModule) Args() []ModuleArg
func (m *MetaModule) Run(ctx ExecContext) (Result, error)
func (m *MetaModule) SupportsCheckMode() bool  // true
```
- 不执行 shell 命令
- 由引擎拦截处理，不是真正的"模块执行"
- `flush_handlers`：立即执行所有 pending handlers
- `end_play`：结束当前 play
- `clear_facts`：清除所有 facts
- `clear_host_errors`：清除主机错误状态

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `_raw_params` | str | 是 | - | meta 动作：flush_handlers/end_play/clear_facts/clear_host_errors |

**实战示例：**
```yaml
- name: Flush handlers before continuing
  meta: flush_handlers

- name: End play for this host
  meta: end_play
  when: ansible_os_family != "RedHat"
```

---

## 九、异步与其他

### `async_status`

**解决什么问题：** 查询异步任务的执行状态。与 `async`/`poll` 配合使用。

**go-ansible 生成的 Shell 命令：**
```bash
# check 模式：读取状态文件
cat /root/.ansible_async/<jid>

# cleanup 模式：删除状态文件
rm -f /root/.ansible_async/<jid>
```

**Go 实现要点：**
```go
type AsyncStatusModule struct{}
func (m *AsyncStatusModule) Name() string
func (m *AsyncStatusModule) Args() []ModuleArg
func (m *AsyncStatusModule) Run(ctx ExecContext) (Result, error)
func (m *AsyncStatusModule) SupportsCheckMode() bool  // true
```
- 读取远程临时目录的状态文件
- 状态文件包含 JSON：`{"started": 1, "finished": 0, "stdout": "..."}`
- `mode=check`（默认）查询状态，`mode=cleanup` 删除文件

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `jid` | str | 是 | - | 异步任务的 job ID |
| `mode` | str | 否 | check | check/cleanup |

**实战示例：**
```yaml
- name: Start long migration
  shell: /opt/migrate.sh
  async: 3600
  poll: 0
  register: migration_job

- name: Check migration status
  async_status:
    jid: "{{ migration_job.ansible_job_id }}"
  register: job_result
  until: job_result.finished
  retries: 60
  delay: 60
```

---

### `pause`

**解决什么问题：** 暂停执行一段时间或等待用户输入。

**Go 实现要点：**
```go
type PauseModule struct{}
func (m *PauseModule) Name() string
func (m *PauseModule) Args() []ModuleArg
func (m *PauseModule) Run(ctx ExecContext) (Result, error)
func (m *PauseModule) SupportsCheckMode() bool  // true
```
- 纯本地操作
- `seconds` 用 `time.Sleep`
- `prompt` 在终端等待用户按回车

**关键参数：**

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `seconds` | int | 否 | - | 暂停秒数 |
| `minutes` | int | 否 | - | 暂停分钟数 |
| `prompt` | str | 否 | - | 显示提示等待用户输入 |

**实战示例：**
```yaml
- name: Wait for manual verification
  pause:
    prompt: "Please verify the deployment and press Enter to continue"

- name: Wait 30 seconds
  pause:
    seconds: 30
```

---

## 十、总结表格

| 模块 | 类别 | Check Mode | 幂等 | 核心 Shell 命令 |
|------|------|:----------:|:----:|-----------------|
| copy | 文件 | ✅ | ✅ | SFTP + sha256sum |
| template | 文件 | ✅ | ✅ | SFTP + sha256sum（本地渲染） |
| file | 文件 | ✅ | ✅ | stat/mkdir/ln/chmod/rm |
| stat | 文件 | ✅ | ✅ | stat（只读） |
| find | 文件 | ✅ | ✅ | find（只读） |
| lineinfile | 文件 | ✅ | ✅ | grep + sed |
| blockinfile | 文件 | ✅ | ✅ | grep + sed（多行） |
| synchronize | 文件 | ✅ | ✅ | rsync |
| fetch | 文件 | ✅ | ✅ | SFTP（反向） |
| unarchive | 文件 | ✅ | ✅ | tar/unzip |
| yum | 包 | ✅ | ✅ | rpm -q + yum install |
| apt | 包 | ✅ | ✅ | dpkg -s + apt-get install |
| dnf | 包 | ✅ | ✅ | rpm -q + dnf install |
| pip | 包 | ✅ | ✅ | pip freeze + pip install |
| service | 服务 | ✅ | ✅ | service/systemctl |
| systemd | 服务 | ✅ | ✅ | systemctl |
| shell | 命令 | ❌ | ❌ | /bin/sh -c（creates/removes 可选） |
| command | 命令 | ❌ | ❌ | 直接 exec（creates/removes 可选） |
| script | 命令 | ❌ | ❌ | SFTP + chmod +x + 执行 |
| raw | 命令 | ❌ | ❌ | 原始 SSH 命令 |
| expect | 命令 | ❌ | ❌ | expect 脚本 |
| user | 用户 | ✅ | ✅ | id + useradd/usermod/userdel |
| group | 用户 | ✅ | ✅ | getent + groupadd/groupmod/groupdel |
| authorized_key | 用户 | ✅ | ✅ | grep + echo/sed（authorized_keys） |
| uri | 网络 | ❌ | ❌ | curl |
| get_url | 网络 | ✅ | ✅ | curl/wget + sha256sum |
| wait_for | 网络 | ❌ | ✅ | nc/test/grep 循环 |
| wait_for_connection | 网络 | ❌ | ✅ | SSH 连接循环 |
| setup | 系统 | ✅ | ✅ | uname/free/ip/id 等（只读） |
| debug | 系统 | ✅ | ✅ | 无（纯本地） |
| assert | 系统 | ✅ | ✅ | 无（纯本地） |
| set_fact | 系统 | ✅ | ✅ | 无（纯本地） |
| hostname | 系统 | ✅ | ✅ | hostname + hostnamectl |
| cron | 系统 | ✅ | ✅ | crontab -l + crontab - |
| sysctl | 系统 | ✅ | ✅ | sysctl -n + sysctl -w |
| meta | 系统 | ✅ | ✅ | 无（引擎拦截） |
| async_status | 异步 | ✅ | ✅ | cat 状态文件（只读） |
| pause | 其他 | ✅ | ✅ | 无（time.Sleep） |

> **图例：** ✅ = 支持/是，❌ = 不支持/否

---

## 参考资料

- 设计文档第六章：模块系统
- 设计文档第七章：连接层
- [Ansible 官方模块索引](https://docs.ansible.com/ansible/latest/modules/modules_by_category.html)
- [Go text/template 文档](https://pkg.go.dev/text/template)
- [Sprig 函数库](https://masterminds.github.io/sprig/)
