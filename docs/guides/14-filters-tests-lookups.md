# 过滤器、测试与查找插件

> 阶段：P13 | 设计文档引用：第十四章、第十五章

本文件描述 ansible-go 中三大模板扩展机制：过滤器（Filters）、测试插件（Tests）和查找插件（Lookups）。它们是模板引擎的核心扩展点，使 ansible-go 能够处理复杂的运维数据转换逻辑。

---

## 目录

1. [过滤器概述](#1-过滤器概述)
2. [Ansible 特有过滤器](#2-ansible-特有过滤器)
3. [测试插件](#3-测试插件)
4. [查找插件](#4-查找插件)
5. [Go 实现要点](#5-go-实现要点)
6. [自定义插件扩展](#6-自定义插件扩展)
7. [任务拆解](#7-任务拆解)

---

## 1. 过滤器概述

### 1.1 模板中的数据转换

过滤器是模板表达式中对数据进行转换的函数。在 Ansible（Jinja2）中，过滤器使用管道语法 `|`：

```yaml
# Jinja2 语法（Ansible）
msg: "Hello {{ name | upper }}"
items: "{{ list | unique | sort }}"
```

在 ansible-go 中，使用 Go `text/template` 管道语法：

```yaml
# Go template 语法（ansible-go）
msg: "Hello {{ .name | upper }}"
items: "{{ .list | unique | sort }}"
```

### 1.2 Jinja2 vs Go Template 管道语法

虽然两者都使用 `|` 管道符号，但有重要差异：

| 特性 | Jinja2 | Go text/template |
|------|--------|------------------|
| 管道语法 | `{{ val \| filter }}` | `{{ val \| filter }}` |
| 函数调用 | `{{ func(arg1, arg2) }}` | `{{ func arg1 arg2 }}` |
| 链式过滤 | `{{ val \| f1 \| f2 }}` | `{{ val \| f1 \| f2 }}` |
| 带参过滤 | `{{ val \| filter(arg) }}` | `{{ filter arg val }}`（注意参数顺序） |
| 默认值 | `{{ val \| default('x') }}` | `{{ val \| d "x" }}`（Sprig 风格） |
| 条件表达式 | `{{ 'yes' if x else 'no' }}` | 不支持内联条件 |

**关键差异**：Go template 管道中，前一个结果作为**最后一个参数**传入下一个函数。这与 Jinja2 的第一个参数位置不同。

### 1.3 Sprig 函数库

ansible-go 使用 [Sprig](https://github.com/Masterminds/sprig) 作为基础函数库（与 Helm 相同）。Sprig 提供了 70+ 个常用函数：

- 字符串操作：`trim`, `upper`, `lower`, `replace`, `contains`, `hasPrefix`
- 数学运算：`add`, `sub`, `mul`, `div`, `max`, `min`
- 类型转换：`toString`, `toInt`, `toFloat64`
- 列表操作：`list`, `append`, `prepend`, `first`, `last`, `uniq`
- 字典操作：`dict`, `get`, `set`, `keys`, `values`, `merge`
- 编码：`b64enc`, `b64dec`, `json`, `toJson`, `fromJson`
- 日期时间：`now`, `date`, `dateInZone`
- 路径操作：`base`, `dir`, `ext`, `clean`

ansible-go 在 Sprig 基础上添加 Ansible 特有过滤器，以下章节详述。

---

## 2. Ansible 特有过滤器

以下过滤器需要在 Sprig 之外额外实现。每个过滤器包含说明、示例和 Go 实现思路。

### 2.1 ipaddr — IP 地址操作

**功能**：对 IP 地址、网络、CIDR 进行各种操作和判断。

**典型用法**：

```yaml
# 判断是否为有效 IP
valid: "{{ '192.168.1.1' | ipaddr }}"           # 返回 "192.168.1.1"

# 提取网络地址
network: "{{ '192.168.1.100/24' | ipaddr('network') }}"  # "192.168.1.0"

# 提取广播地址
broadcast: "{{ '192.168.1.100/24' | ipaddr('broadcast') }}"  # "192.168.1.255"

# 提取子网掩码
netmask: "{{ '192.168.1.100/24' | ipaddr('netmask') }}"  # "255.255.255.0"

# 提取前缀长度
prefix: "{{ '192.168.1.100/24' | ipaddr('prefix') }}"  # 24

# 过滤列表中的有效 IP
hosts: "{{ my_list | ipaddr }}"  # 只保留有效的 IP/网络

# 判断是否在网段内
in_range: "{{ '192.168.1.5' | ipaddr('192.168.1.0/24') }}"  # "192.168.1.5"

# IPv6 支持
ipv6: "{{ '2001:db8::1' | ipaddr }}"  # "2001:db8::1"
```

**Go 实现思路**：

```go
// 使用标准库 net 包实现，无需外部依赖。
// 核心逻辑：
// 1. 解析输入为 *net.IP 或 *net.IPNet
// 2. 根据 query 参数执行相应操作
// 3. 支持 IPv4 和 IPv6
//
// ipaddr 过滤器的 Go 函数签名。
func ipaddrFilter(value any, query ...string) (any, error)
```

**实现要点**：
- 使用 `net.ParseCIDR()` 解析 CIDR 格式
- 使用 `net.ParseIP()` 解析纯 IP 格式
- 列表输入时遍历过滤，返回有效 IP 列表
- 空查询返回原始值（如果是有效 IP）或 false

### 2.2 regex_replace — 正则替换

**功能**：使用正则表达式替换字符串中的匹配部分。

```yaml
# 基本替换
clean: "{{ 'hello-world' | regex_replace('-', '_') }}"  # "hello_world"

# 使用捕获组
extract: "{{ 'version-1.2.3' | regex_replace('^version-(.*)$', '\\1') }}"  # "1.2.3"

# 替换所有匹配（默认已是全局）
fixed: "{{ 'a-b-c' | regex_replace('-', ':') }}"  # "a:b:c"
```

**Go 实现思路**：

```go
// 使用 regexp 包。
func regexReplaceFilter(value, pattern, replacement string) (string, error)
```

- 使用 `regexp.MustCompile()` 编译正则
- 使用 `re.ReplaceAllString()` 执行替换
- 替换字符串中 `\1`, `\2` 映射为 `${1}`, `${2}`（Go 语法）

### 2.3 regex_search — 正则搜索

**功能**：返回正则匹配的第一个子串。

```yaml
# 搜索匹配
port: "{{ 'server:8080' | regex_search(':(\\d+)') }}"  # ":8080"

# 使用捕获组
port_num: "{{ 'server:8080' | regex_search(':(\\d+)', '\\1') }}"  # "8080"

# 无匹配返回空
none: "{{ 'hello' | regex_search('xyz') }}"  # ""
```

**Go 实现思路**：

```go
func regexSearchFilter(value, pattern string, args ...string) (string, error)
```

- 使用 `re.FindString()` 或 `re.FindStringSubmatch()`
- 有额外参数时提取对应捕获组

### 2.4 regex_findall — 正则查找所有

**功能**：返回所有匹配的列表。

```yaml
# 查找所有数字
nums: "{{ 'a1b2c3' | regex_findall('\\d') }}"  # ["1", "2", "3"]

# 查找所有 IP
ips: "{{ text | regex_findall('\\d+\\.\\d+\\.\\d+\\.\\d+') }}"
```

**Go 实现思路**：

```go
func regexFindallFilter(value, pattern string) ([]string, error)
```

- 使用 `re.FindAllString()` 返回所有匹配

### 2.5 combine — 深度合并字典

**功能**：递归合并多个字典，支持深度合并。

```yaml
# 浅合并
merged: "{{ dict1 | combine(dict2) }}"  # dict2 覆盖 dict1 的同名键

# 深度合并
deep: "{{ dict1 | combine(dict2, recursive=true) }}"  # 递归合并嵌套字典

# 列表合并
lists: "{{ dict1 | combine(dict2, list_merge='append') }}"  # 列表追加而非覆盖
```

**Go 实现思路**：

```go
// combine 过滤器签名。
// listMerge 参数: "replace"(默认), "append", "prepend", "keep", "prepend_rp"
func combineFilter(dicts []map[string]any, recursive bool, listMerge string) (map[string]any, error)
```

- 浅合并：遍历所有 dict，后者覆盖前者
- 深度合并：递归处理 `map[string]any` 类型的值
- 列表合并策略：replace（覆盖）、append（追加）、prepend（前置）

### 2.6 flatten — 展平列表

**功能**：将嵌套列表展平为一维列表。

```yaml
# 基本展平
flat: "{{ [[1, 2], [3, [4, 5]]] | flatten }}"  # [1, 2, 3, 4, 5]

# 限制展平深度
level1: "{{ [[1, 2], [3, [4, 5]]] | flatten(levels=1) }}"  # [1, 2, 3, [4, 5]]
```

**Go 实现思路**：

```go
// 递归展平，levels 控制深度，-1 或未指定表示完全展平。
func flattenFilter(list []any, levels ...int) ([]any, error)
```

### 2.7 dict2items / items2dict — 字典列表互转

**功能**：字典转为键值对列表，或反向转换。

```yaml
# dict → items
items: "{{ {'a': 1, 'b': 2} | dict2items }}"
# 结果: [{key: "a", value: 1}, {key: "b", value: 2}]

# 自定义键名
items: "{{ {'a': 1} | dict2items(key_name='name', value_name='val') }}"
# 结果: [{name: "a", val: 1}]

# items → dict
dict: "{{ items | items2dict }}"

# 自定义键名
dict: "{{ items | items2dict(key_name='name', value_name='val') }}"
```

**Go 实现思路**：

```go
func dict2itemsFilter(dict map[string]any, keyName, valueName string) ([]map[string]any, error)
func items2dictFilter(items []map[string]any, keyName, valueName string) (map[string]any, error)
```

- 默认键名：`key`, `value`
- `dict2items`：遍历 map，生成 `{key: k, value: v}` 列表
- `items2dict`：遍历列表，用指定字段构建 map

### 2.8 json_query — JMESPath 查询

**功能**：使用 JMESPath 语法查询 JSON 数据。

```yaml
# 提取所有名称
names: "{{ users | json_query('[*].name') }}"

# 条件过滤
active: "{{ users | json_query('[?status==`active`].name') }}"

# 嵌套访问
ips: "{{ servers | json_query('[*].networkInterfaces[0].ip') }}"
```

**Go 实现思路**：

```go
// 使用第三方库 github.com/jmespath/go-jmespath
func jsonQueryFilter(data any, expr string) (any, error)
```

- 使用 `jmespath.Search(expr, data)` 执行查询
- 需要将数据序列化为 JSON 再反序列化以确保类型兼容

### 2.9 map / select / reject — 列表操作

**功能**：对列表进行映射、选择、拒绝操作。

```yaml
# map — 提取属性
names: "{{ users | map(attribute='name') | list }}"

# select — 过滤（保留匹配项）
active: "{{ users | selectattr('status', 'equalto', 'active') | list }}"

# reject — 过滤（排除匹配项）
inactive: "{{ users | rejectattr('status', 'equalto', 'active') | list }}"
```

**Go 实现思路**：

```go
// map 提取列表中每个元素的指定属性。
func mapFilter(list []any, attribute string) ([]any, error)

// selectattr 按属性值过滤列表，保留匹配项。
func selectAttrFilter(list []any, attr, test string, args ...any) ([]any, error)

// rejectattr 按属性值过滤列表，排除匹配项。
func rejectAttrFilter(list []any, attr, test string, args ...any) ([]any, error)
```

- `map(attribute='x')`：对每个元素取 `x` 属性
- `selectattr(attr, test, val)`：使用指定测试函数筛选
- `rejectattr`：与 `selectattr` 相反

### 2.10 sort / groupby / unique — 列表操作

```yaml
# sort — 排序
sorted: "{{ [3, 1, 2] | sort }}"  # [1, 2, 3]

# sort by attribute
by_name: "{{ users | sort(attribute='name') }}"

# reverse sort
desc: "{{ [3, 1, 2] | sort(reverse=true) }}"  # [3, 2, 1]

# unique — 去重
unique: "{{ [1, 2, 2, 3, 3] | unique }}"  # [1, 2, 3]

# groupby — 按属性分组
grouped: "{{ users | groupby('department') }}"
# 结果: [["engineering", [{user1}, {user2}]], ["sales", [{user3}]]]
```

**Go 实现思路**：

```go
func sortFilter(list []any, reverse bool, attribute string) ([]any, error)
func uniqueFilter(list []any) ([]any, error)
func groupbyFilter(list []any, attribute string) ([][2]any, error)
```

- `sort`：使用 `sort.Slice`，支持按属性排序
- `unique`：使用 map 去重，保持原始顺序
- `groupby`：按属性值分组，返回 `[key, group]` 对列表

### 2.11 difference / intersect / union — 集合操作

```yaml
# difference — 差集（A 有 B 没有）
diff: "{{ list_a | difference(list_b) }}"

# intersect — 交集
common: "{{ list_a | intersect(list_b) }}"

# union — 并集
all: "{{ list_a | union(list_b) }}"
```

**Go 实现思路**：

```go
func differenceFilter(a, b []any) ([]any, error)
func intersectFilter(a, b []any) ([]any, error)
func unionFilter(a, b []any) ([]any, error)
```

- 使用 map 集合实现，时间复杂度 O(n+m)
- 结果保持第一个列表的元素顺序

### 2.12 mandatory — 变量必须存在

**功能**：标记变量为必须存在，未定义时产生错误。

```yaml
# 如果 my_var 未定义，模板渲染失败并报错
value: "{{ my_var | mandatory }}"
```

**Go 实现思路**：

```go
// mandatory 在值为 nil 或零值时返回错误。
func mandatoryFilter(value any) (any, error)
```

- 检查 value 是否为 nil
- 检查字符串是否为空
- 零值时返回明确的错误消息

### 2.13 b64encode / b64decode — Base64 编解码

```yaml
# 编码
encoded: "{{ 'hello world' | b64encode }}"  # "aGVsbG8gd29ybGQ="

# 解码
decoded: "{{ 'aGVsbG8gd29ybGQ=' | b64decode }}"  # "hello world"
```

**Go 实现思路**：

```go
// Sprig 已提供 b64enc/b64dec，但需确保与 Ansible 行为一致。
// 主要差异：Ansible 默认使用标准 Base64（非 URL-safe）。
func b64encodeFilter(value string) (string, error)
func b64decodeFilter(value string) (string, error)
```

- 使用 `encoding/base64` 标准编码
- 确保输出无换行（`StdEncoding` 而非 `StdPadding`）

### 2.14 hash / checksum — 哈希计算

```yaml
# SHA1 哈希
hashed: "{{ 'hello' | hash('sha1') }}"  # "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"

# MD5
md5: "{{ 'hello' | hash('md5') }}"  # "5d41402abc4b2a76b9719d911017c592"

# SHA256
sha256: "{{ 'hello' | hash('sha256') }}"

# checksum（SHA1 快捷方式）
sum: "{{ 'hello' | checksum }}"  # 等同于 hash('sha1')
```

**Go 实现思路**：

```go
func hashFilter(value, hashtype string) (string, error)
func checksumFilter(value string) (string, error)
```

- 支持 `md5`, `sha1`, `sha256`, `sha512`
- 使用 `crypto/sha256` 等标准库
- 输出十六进制字符串

---

## 3. 测试插件

### 3.1 测试语法概述

测试插件在 Go template 中使用 `is` 关键字（通过自定义函数实现）。在 Ansible/Jinja2 中：

```yaml
when: result is success
when: my_var is defined
when: version is version('20.04', '>=')
```

在 ansible-go 的 Go template 中，测试通过管道函数实现：

```yaml
when: "{{ .result | is_success }}"
when: "{{ .my_var | is_defined }}"
when: "{{ .version | is_version '20.04' '>=' }}"
```

或者通过条件函数：

```yaml
when: "{{ is_success .result }}"
when: "{{ is_defined .my_var }}"
when: "{{ is_version .version '20.04' '>=' }}"
```

### 3.2 defined / undefined — 变量是否定义

```yaml
# 变量已定义
when: "{{ is_defined .my_var }}"

# 变量未定义
when: "{{ is_undefined .my_var }}"

# 配合 default 使用
value: "{{ .my_var | d \"fallback\" }}"
```

**实现要点**：
- `defined`：检查变量是否存在且不为 nil
- `undefined`：变量不存在或为 nil
- 区分 "变量不存在" 和 "变量值为空字符串"

### 3.3 success / failed — 执行结果

```yaml
# 任务成功
when: "{{ is_success .register_result }}"

# 任务失败
when: "{{ is_failed .register_result }}"

# 典型用法：根据前一个任务结果决定后续操作
- command: /usr/bin/check.sh
  register: check_result
  ignore_errors: true

- name: Fix issue
  command: /usr/bin/fix.sh
  when: "{{ is_failed check_result }}"
```

**实现要点**：
- 检查 `TaskResult.Status` 是否为 `TaskStatusOk` 或 `TaskStatusChanged`
- `success` 包括 ok 和 changed 状态
- `failed` 仅匹配 `TaskStatusFailed`

### 3.4 changed / skipped — 状态检查

```yaml
# 任务产生了变更
when: "{{ is_changed .register_result }}"

# 任务被跳过
when: "{{ is_skipped .register_result }}"
```

**实现要点**：
- `changed`：检查 `TaskResult.Changed == true`
- `skipped`：检查 `TaskResult.Status == TaskStatusSkipped`

### 3.5 match / search / regex — 模式匹配

```yaml
# match — 从字符串开头匹配（类似 shell 通配符）
when: "{{ is_match .name 'web*' }}"

# search — 在字符串中搜索（任意位置匹配）
when: "{{ is_search .output 'error' }}"

# regex — 正则表达式匹配
when: "{{ is_regex .version '^\\d+\\.\\d+' }}"
```

**实现要点**：

```go
// match: 使用 filepath.Match 或 strings.HasPrefix
func isMatchTest(value, pattern string) bool

// search: 使用 strings.Contains
func isSearchTest(value, pattern string) bool

// regex: 使用 regexp.MatchString
func isRegexTest(value, pattern string) bool
```

- `match`：从开头匹配，支持 glob 通配符（`*`, `?`）
- `search`：子串包含检查
- `regex`：完整正则表达式匹配

### 3.6 version — 版本比较

```yaml
# 版本比较
when: "{{ is_version .ansible_distribution_version '20.04' '>=' }}"

# 支持的比较运算符：==, !=, >=, <=, >, <
when: "{{ is_version .os_version '7.0' '==' }}"
```

**实现要点**：

```go
// 使用语义化版本比较。
func isVersionTest(version, constraint, operator string) bool
```

- 使用 `hashicorp/go-version` 或自研版本比较
- 支持语义化版本（semver）和发行版版本号
- 处理 `20.04`, `7.9`, `1.2.3` 等格式

### 3.7 subset / superset — 集合关系

```yaml
# 是否为子集
when: "{{ is_subset .selected_hosts .all_hosts }}"

# 是否为超集
when: "{{ is_superset .all_hosts .required_hosts }}"
```

**实现要点**：

```go
func isSubsetTest(subset, superset []any) bool
func isSupersetTest(superset, subset []any) bool
```

- 将两个列表转为 set（map），检查包含关系

---

## 4. 查找插件

### 4.1 lookup() 函数

查找插件通过 `lookup()` 函数在模板中调用：

```yaml
# 基本语法
content: "{{ lookup('file', '/etc/hosts') }}"

# 多个 term
items: "{{ lookup('fileglob', '/etc/*.conf') }}"

# 链式使用
data: "{{ lookup('file', '/tmp/data.json') | from_json }}"
```

在 Go template 中的调用方式：

```yaml
content: "{{ lookup 'file' '/etc/hosts' }}"
data: "{{ lookup 'file' '/tmp/data.json' | from_json }}"
```

### 4.2 file — 读取文件内容

**功能**：从控制节点读取文件内容。

```yaml
# 读取单个文件
hosts_content: "{{ lookup('file', '/etc/hosts') }}"

# 读取多个文件
configs: "{{ lookup('file', '/etc/app.conf', '/etc/app2.conf') }}"
```

**实现要点**：
- 在控制节点本地读取文件
- 返回文件内容字符串
- 文件不存在时返回错误

### 4.3 template — 渲染模板返回字符串

**功能**：渲染 Jinja2/Go template 模板文件，返回结果字符串。

```yaml
# 渲染模板
config: "{{ lookup('template', 'my_template.j2') }}"

# 渲染时使用当前变量上下文
rendered: "{{ lookup('templates/nginx.conf.j2') }}"
```

**实现要点**：
- 读取模板文件
- 使用当前变量上下文渲染
- 返回渲染后的字符串
- 与 `template` 模块不同：lookup 返回字符串，模块写入远程文件

### 4.4 pipe — 执行命令获取输出

**功能**：在控制节点执行 shell 命令，返回 stdout。

```yaml
# 获取 git commit hash
commit: "{{ lookup('pipe', 'git rev-parse HEAD') }}"

# 获取系统信息
hostname: "{{ lookup('pipe', 'hostname -f') }}"
```

**实现要点**：
- 使用 `os/exec` 执行命令
- 返回 stdout 内容
- 去除末尾换行符
- 命令失败时返回错误

### 4.5 env — 读取环境变量

```yaml
# 读取 HOME 目录
home: "{{ lookup('env', 'HOME') }}"

# 读取自定义变量
api_key: "{{ lookup('env', 'MY_API_KEY') }}"
```

**实现要点**：

```go
func envLookup(terms []string, variables map[string]any) ([]string, error)
```

- 使用 `os.Getenv()` 读取
- 未设置时返回空字符串

### 4.6 password — 生成/读取密码

**功能**：生成随机密码或从文件读取密码。

```yaml
# 生成 20 位随机密码并保存到文件
db_pass: "{{ lookup('password', '/tmp/db_password length=20') }}"

# 指定字符集
token: "{{ lookup('password', '/tmp/token chars=ascii_letters,digits') }}"

# 使用 encrypt 参数加密
hashed: "{{ lookup('password', '/tmp/pw encrypt=sha256_crypt') }}"
```

**实现要点**：
- 如果密码文件已存在，读取内容
- 如果不存在，生成随机密码并写入文件
- 支持 `length`, `chars`, `encrypt` 参数
- 使用 `crypto/rand` 生成密码

### 4.7 ini — 读取 INI 文件

```yaml
# 读取 INI 文件中的值
user: "{{ lookup('ini', 'user section=defaults file=/etc/app.ini') }}"

# 使用正则匹配键名
port: "{{ lookup('ini', 'port.* section=server type=regexp') }}"
```

**实现要点**：
- 使用 `go-ini` 库或标准库解析
- 支持 `section`, `file`, `type` 参数
- `type=regexp` 时使用正则匹配键名

### 4.8 url — HTTP GET

```yaml
# GET 请求
data: "{{ lookup('url', 'https://api.example.com/data') }}"

# 带请求头
auth_data: "{{ lookup('url', 'https://api.example.com', headers='Authorization: Bearer xxx') }}"
```

**实现要点**：
- 使用 `net/http` 发送 GET 请求
- 支持自定义 headers
- 返回响应体字符串
- 支持 `validate_certs` 参数控制 TLS 验证

### 4.9 fileglob — 文件模式匹配

```yaml
# 匹配文件列表
pem_files: "{{ lookup('fileglob', '/etc/ssl/*.pem') }}"

# 多个模式
configs: "{{ lookup('fileglob', '/etc/app/*.conf', '/etc/app/*.yaml') }}"
```

**实现要点**：

```go
func fileglobLookup(terms []string, variables map[string]any) ([]string, error)
```

- 使用 `filepath.Glob()` 匹配
- 返回匹配的文件路径列表
- 多个 term 时合并结果

### 4.10 dict — 字典迭代

```yaml
# 遍历字典
items: "{{ lookup('dict', my_dict) }}"

# 返回 [{key: "k1", value: "v1"}, ...] 格式
```

**实现要点**：
- 将 map 转为键值对列表
- 返回 `[{key, value}]` 格式

### 4.11 sequence — 生成序列

```yaml
# 生成数字序列
ids: "{{ lookup('sequence', 'start=1 end=10') }}"

# 指定步长
even: "{{ lookup('sequence', 'start=0 end=20 stride=2') }}"

# 格式化
padded: "{{ lookup('sequence', 'start=1 end=100 format=host%03d') }}"

# 十六进制
hex: "{{ lookup('sequence', 'start=0x00 end=0xFF format=%02x') }}"
```

**实现要点**：
- 解析 `start`, `end`, `stride`, `format` 参数
- 生成序列字符串列表
- 支持十进制和十六进制
- `format` 参数使用 `fmt.Sprintf` 格式化

---

## 5. Go 实现要点

### 5.1 核心类型签名

```go
// FilterFunc 是过滤器函数的类型签名。
// value 是管道传入的值，args 是额外参数。
// Go template 管道中，value 是最后一个参数。
type FilterFunc func(value any, args ...any) (any, error)

// TestFunc 是测试函数的类型签名。
// 返回 bool 表示测试是否通过。
type TestFunc func(value any, args ...any) (bool, error)

// LookupFunc 是查找函数的类型签名。
// terms 是查找项列表，variables 是当前变量上下文。
type LookupFunc func(terms []string, variables map[string]any) ([]string, error)
```

### 5.2 注册模式

```go
// FilterRegistry 管理所有过滤器。
type FilterRegistry struct {
    filters map[string]FilterFunc
}

// Register 注册一个过滤器。
func (r *FilterRegistry) Register(name string, fn FilterFunc)

// Get 获取过滤器函数。
func (r *FilterRegistry) Get(name string) (FilterFunc, bool)

// TestRegistry 管理所有测试插件。
type TestRegistry struct {
    tests map[string]TestFunc
}

func (r *TestRegistry) Register(name string, fn TestFunc)
func (r *TestRegistry) Get(name string) (TestFunc, bool)

// LookupRegistry 管理所有查找插件。
type LookupRegistry struct {
    lookups map[string]LookupFunc
}

func (r *LookupRegistry) Register(name string, fn LookupFunc)
func (r *LookupRegistry) Get(name string) (LookupFunc, bool)
```

### 5.3 统一插件管理器

```go
// PluginManager 统一管理过滤器、测试和查找插件。
type PluginManager struct {
    Filters  *FilterRegistry
    Tests    *TestRegistry
    Lookups  *LookupRegistry
}

// NewPluginManager 创建并注册所有内置插件。
func NewPluginManager() *PluginManager {
    pm := &PluginManager{
        Filters:  NewFilterRegistry(),
        Tests:    NewTestRegistry(),
        Lookups:  NewLookupRegistry(),
    }
    pm.registerBuiltinFilters()
    pm.registerBuiltinTests()
    pm.registerBuiltinLookups()
    return pm
}
```

### 5.4 JMESPath 库选择

`json_query` 过滤器需要 JMESPath 实现：

```
推荐库: github.com/jmespath/go-jmespath
```

- 这是 JMESPath 官方 Go 实现
- 支持完整的 JMESPath 规范
- 无需 cgo，纯 Go 实现
- 在 Ansible 生态中广泛使用

### 5.5 版本比较库

`version` 测试需要语义化版本比较：

```
推荐库: github.com/hashicorp/go-version
```

- 支持多种版本格式（semver, 发行版版本号）
- 提供 `GreaterThanOrEqual`, `LessThan` 等方法
- 处理前导零和多段版本号

### 5.6 IP 地址库

`ipaddr` 过滤器使用 Go 标准库：

```
标准库: net
关键函数: net.ParseIP(), net.ParseCIDR(), net.IPNet.Contains()
```

- 无需外部依赖
- 完整支持 IPv4 和 IPv6

### 5.7 模板函数注册

将插件注册到模板引擎的 FuncMap：

```go
// BuildTemplateFuncMap 从 PluginManager 构建模板函数映射。
func (pm *PluginManager) BuildTemplateFuncMap() template.FuncMap {
    fm := template.FuncMap{}

    // 注册过滤器（直接作为函数）
    for name, fn := range pm.Filters.filters {
        fm[name] = fn
    }

    // 注册测试（作为 is_xxx 函数）
    for name, fn := range pm.Tests.tests {
        fm["is_"+name] = fn
    }

    // 注册 lookup 函数
    fm["lookup"] = pm.lookupFunc

    return fm
}
```

---

## 6. 自定义插件扩展

### 6.1 添加自定义过滤器

用户可以通过注册机制添加自定义过滤器：

```go
// 1. 定义过滤器函数
func myCustomFilter(value any, args ...any) (any, error) {
    // 自定义逻辑
    return result, nil
}

// 2. 注册到 FilterRegistry
pluginManager.Filters.Register("my_custom", myCustomFilter)

// 3. 在模板中使用
// {{ .data | my_custom }}
```

### 6.2 添加自定义测试

```go
// 1. 定义测试函数
func isEvenTest(value any, args ...any) (bool, error) {
    n, ok := value.(int)
    if !ok {
        return false, fmt.Errorf("is_even: expected int, got %T", value)
    }
    return n%2 == 0, nil
}

// 2. 注册到 TestRegistry
pluginManager.Tests.Register("even", isEvenTest)

// 3. 在模板中使用
// {{ is_even .number }}
```

### 6.3 添加自定义查找

```go
// 1. 定义查找函数
func consulLookup(terms []string, variables map[string]any) ([]string, error) {
    results := make([]string, 0, len(terms))
    for _, key := range terms {
        value, err := consulClient.Get(key)
        if err != nil {
            return nil, err
        }
        results = append(results, value)
    }
    return results, nil
}

// 2. 注册到 LookupRegistry
pluginManager.Lookups.Register("consul", consulLookup)

// 3. 在模板中使用
// {{ lookup "consul" "service/web/port" }}
```

### 6.4 插件命名规范

| 类型 | 前缀/约定 | 示例 |
|------|----------|------|
| 过滤器 | 无前缀，snake_case | `my_filter` |
| 测试 | `is_` 前缀 | `is_even` |
| 查找 | 无前缀，snake_case | `my_lookup` |

### 6.5 错误处理规范

所有插件函数必须：
- 返回明确的错误消息，包含插件名称和参数
- 不 panic，所有异常通过 error 返回
- 类型断言失败时返回有意义的错误

```go
// 好的错误消息
return nil, fmt.Errorf("filter 'ipaddr': invalid IP address: %s", value)

// 坏的错误消息
return nil, fmt.Errorf("invalid input")
```

---

## 7. 任务拆解

### T13.1 过滤器与测试插件注册

**目标**：实现过滤器和测试插件的注册机制及核心过滤器。

**子任务**：

| 编号 | 任务 | 说明 | 预估 |
|------|------|------|------|
| T13.1.1 | 定义核心类型 | FilterFunc, TestFunc, Registry 类型 | 0.5d |
| T13.1.2 | 实现 FilterRegistry | 注册/查找机制 | 0.25d |
| T13.1.3 | 实现 TestRegistry | 注册/查找机制 | 0.25d |
| T13.1.4 | 实现正则过滤器 | regex_replace, regex_search, regex_findall | 0.5d |
| T13.1.5 | 实现字典过滤器 | combine, dict2items, items2dict | 0.5d |
| T13.1.6 | 实现列表过滤器 | flatten, map, select, reject, sort, groupby, unique | 1d |
| T13.1.7 | 实现集合过滤器 | difference, intersect, union | 0.25d |
| T13.1.8 | 实现 ipaddr 过滤器 | IPv4/IPv6 地址操作 | 1d |
| T13.1.9 | 实现 json_query | 集成 JMESPath 库 | 0.5d |
| T13.1.10 | 实现编码/哈希 | b64encode, b64decode, hash, checksum, mandatory | 0.5d |
| T13.1.11 | 实现测试插件 | defined, success, failed, changed, skipped, match, search, regex, version, subset, superset | 1d |
| T13.1.12 | 模板引擎集成 | 注册到 FuncMap，与模板引擎对接 | 0.5d |
| T13.1.13 | 单元测试 | 所有过滤器和测试的测试用例 | 2d |

**总预估**：9 天

### T13.2 查找插件

**目标**：实现查找插件系统及所有内置查找插件。

**子任务**：

| 编号 | 任务 | 说明 | 预估 |
|------|------|------|------|
| T13.2.1 | 定义 LookupFunc 类型 | 查找函数签名和 Registry | 0.25d |
| T13.2.2 | 实现 file 查找 | 读取控制节点文件 | 0.25d |
| T13.2.3 | 实现 template 查找 | 渲染模板返回字符串 | 0.5d |
| T13.2.4 | 实现 pipe 查找 | 执行命令获取输出 | 0.25d |
| T13.2.5 | 实现 env 查找 | 读取环境变量 | 0.25d |
| T13.2.6 | 实现 password 查找 | 生成/读取密码文件 | 0.5d |
| T13.2.7 | 实现 ini 查找 | 读取 INI 文件 | 0.5d |
| T13.2.8 | 实现 url 查找 | HTTP GET 请求 | 0.5d |
| T13.2.9 | 实现 fileglob 查找 | 文件模式匹配 | 0.25d |
| T13.2.10 | 实现 dict 查找 | 字典迭代 | 0.25d |
| T13.2.11 | 实现 sequence 查找 | 生成序列 | 0.5d |
| T13.2.12 | lookup() 函数集成 | 注册到模板 FuncMap | 0.25d |
| T13.2.13 | 单元测试 | 所有查找插件的测试用例 | 1d |

**总预估**：5 天

**验收标准**：

- [ ] 所有内置过滤器行为与 Ansible 一致
- [ ] 测试插件在条件判断中正确工作
- [ ] 查找插件返回正确结果
- [ ] 自定义插件注册机制正常工作
- [ ] 错误消息清晰、包含上下文
- [ ] 单元测试覆盖率达到 80%+

---

*上一篇：[13-callbacks-and-output.md](13-callbacks-and-output.md) | 下一篇：[15-testing-and-e2e.md](15-testing-and-e2e.md)*
