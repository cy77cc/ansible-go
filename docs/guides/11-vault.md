# 11 - Vault 加密

> 阶段：P10 | 设计文档引用：第十二章

本章介绍 ansible-go 的 Vault 加密系统。Vault 允许将敏感数据（密码、密钥、证书）
直接存储在版本控制系统中，通过静态加密保护，执行时透明解密。这是生产环境
安全管理的核心基础设施。

---

## 1. Vault 解决什么问题

### 1.1 密钥管理的困境

在基础设施即代码（IaC）的实践中，Playbook 需要引用大量敏感数据：

```yaml
# 这些值不应该明文存储在 Git 中
vars:
  db_password: "SuperSecret123!"
  api_key: "sk-abc123def456"
  ssl_cert: |
    -----BEGIN CERTIFICATE-----
    MIIDXTCCAkWgAwIBAgIJAKJ0...
    -----END CERTIFICATE-----
  ssh_private_key: |
    -----BEGIN RSA PRIVATE KEY-----
    MIIEowIBAAKCAQEA0Z3VS5JJ...
    -----END RSA PRIVATE KEY-----
```

传统做法的问题：

| 方案 | 缺点 |
|------|------|
| 明文存储在 Git | 任何有仓库访问权的人都能看到密钥 |
| 环境变量 | 无法版本管理，部署时需要额外配置 |
| 外部密钥管理（Vault/SM） | 增加依赖，需要额外服务 |
| 手动加密文件 | 每次编辑都需要手动加解密，容易出错 |

### 1.2 Vault 的解决方案

Vault 提供**透明加密**：文件在磁盘上是加密的，但 Playbook 执行时自动解密。

```
Git 仓库中（加密）          执行时（内存中，明文）
├── group_vars/
│   └── all.yml             ├── group_vars/
│       $ANSIBLE_VAULT      │   └── all.yml
│       1.1;AES256          │       db_password: SuperSecret123!
│       a3f8b2c1d4e5...     │       api_key: sk-abc123def456
│       ...                  │
```

**核心优势**：
- 敏感数据可以安全地提交到 Git
- 审计历史完整（谁改了什么密码）
- 执行时透明，Playbook 语法不变
- 支持多环境不同密码

---

## 2. 加密算法详解

### 2.1 整体流程

Vault 使用三层加密架构确保安全性：

```
用户密码（password）
    │
    ▼
┌─────────────────────────────┐
│  PBKDF2-SHA256              │  密钥派生
│  iterations: 10000          │  将用户密码转换为加密密钥
│  salt: 32 字节随机值         │
│  output: 32 字节密钥         │
└─────────────────────────────┘
    │
    ├──▶ AES-256-CTR 密钥     用于加密明文数据
    │
    └──▶ HMAC-SHA256 密钥     用于完整性校验
```

### 2.2 PBKDF2-SHA256 密钥派生

PBKDF2（Password-Based Key Derivation Function 2）将用户输入的密码转换为
密码学安全的密钥：

```
输入：
  password = 用户输入的密码（任意长度）
  salt     = 32 字节随机值
  iterations = 10000

过程：
  DK = PBKDF2(
      PRF = HMAC-SHA256,
      Password = password,
      Salt = salt,
      Iterations = 10000,
      dkLen = 64 字节
  )

输出：
  DK[0:32]  = AES-256 加密密钥
  DK[32:64] = HMAC-SHA256 完整性密钥
```

**为什么用 10000 次迭代**：增加暴力破解成本。每次迭代都是一次 HMAC 计算，
10000 次迭代让离线暴力破解变慢约 10000 倍。

### 2.3 AES-256-CTR 加密

AES-256-CTR（Counter 模式）用于加密实际数据：

```
输入：
  key = PBKDF2 派生的 32 字节密钥
  IV  = 16 字节随机初始化向量
  plaintext = 要加密的明文数据

过程：
  ciphertext = AES-256-CTR(key, IV, plaintext)

输出：
  ciphertext = 与明文等长的密文
```

**CTR 模式的优势**：
- 不需要填充（padding），密文长度等于明文长度
- 支持并行加密/解密
- 随机访问（可以解密任意位置的数据块）

### 2.4 HMAC-SHA256 完整性校验

HMAC 用于确保密文未被篡改：

```
输入：
  hmac_key = PBKDF2 派生的后 32 字节密钥
  data     = IV + ciphertext

过程：
  hmac = HMAC-SHA256(hmac_key, IV || ciphertext)

输出：
  hmac = 32 字节完整性校验值
```

**解密时的验证顺序**：
1. 计算 HMAC
2. 与文件中存储的 HMAC 比较
3. 如果不匹配，拒绝解密（防止篡改）
4. 如果匹配，执行解密

### 2.5 加密完整流程

```
加密：
  password ──▶ PBKDF2 ──▶ key, hmac_key
  plaintext ──▶ AES-256-CTR(key, IV) ──▶ ciphertext
  HMAC(hmac_key, IV || ciphertext) ──▶ hmac
  输出 = hmac || IV || ciphertext

解密：
  password ──▶ PBKDF2 ──▶ key, hmac_key
  读取 hmac, IV, ciphertext
  验证 HMAC(hmac_key, IV || ciphertext) == hmac
  ciphertext ──▶ AES-256-CTR(key, IV) ──▶ plaintext
```

---

## 3. 文件格式

### 3.1 格式规范

Vault 加密文件的完整格式：

```
$ANSIBLE_VAULT;1.1;AES256
<hex-encoded data: HMAC(32B) + IV(16B) + ciphertext>
```

第一行是头部，包含：
- `$ANSIBLE_VAULT` — 固定标识
- `1.1` — 格式版本
- `AES256` — 加密算法

第二行开始是 hex 编码的加密数据，按以下顺序拼接：
- 前 64 个 hex 字符 = 32 字节 HMAC
- 接下来 32 个 hex 字符 = 16 字节 IV
- 剩余所有字符 = 密文

### 3.2 换行规则

加密数据每行固定 80 个 hex 字符（40 字节），最后一行可以少于 80 个字符：

```
$ANSIBLE_VAULT;1.1;AES256
a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4
e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9
d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4
c5d6e7f8a9b0c1d2e3f4
```

### 3.3 完整文件示例

加密前（明文）：

```yaml
---
db_password: SuperSecret123!
api_key: sk-abc123def456
```

加密后：

```
$ANSIBLE_VAULT;1.1;AES256
613839663134633538643162343935383730633862636139663430373164383766373161
393834626235616332643861303833353036306338623633643930663134373838323763
383837633166613530393539623564373737383236383762656330363861353065663533
333237336665343932383438313464316334336530333835353664396566366536356237
333130313835393135646238616239613034633534306233666231383230346336363435
306233353238376630386264383531643237633830373838636434373938333738356364
313664343064383835383761343438323761313866393063643539636665356661366430
35323364303765363235
```

### 3.4 Vault ID 扩展格式

当使用 Vault ID（多密码）时，头部会包含 ID 标签：

```
$ANSIBLE_VAULT;1.2;AES256;prod
613839663134633538643162...
```

---

## 4. CLI 命令

### 4.1 encrypt — 加密文件

```bash
# 加密单个文件
ansible-go vault encrypt secrets.yml

# 加密多个文件
ansible-go vault encrypt secrets1.yml secrets2.yml

# 加密时指定 Vault ID
ansible-go vault encrypt --vault-id prod@prompt secrets.yml

# 加密时使用密码文件
ansible-go vault encrypt --vault-password-file ~/.vault_pass secrets.yml
```

加密后原地替换文件内容。原文件变为加密格式。

### 4.2 decrypt — 解密文件

```bash
# 解密单个文件
ansible-go vault decrypt secrets.yml

# 解密多个文件
ansible-go vault decrypt secrets1.yml secrets2.yml

# 解密时指定密码
ansible-go vault decrypt --vault-password-file ~/.vault_pass secrets.yml
```

解密后原地替换文件内容。加密格式变为明文 YAML。

### 4.3 view — 查看加密文件

```bash
# 查看加密文件内容（不修改文件）
ansible-go vault view secrets.yml
```

`view` 不修改磁盘上的文件，只在终端显示解密后的内容。

### 4.4 edit — 编辑加密文件

```bash
# 编辑加密文件
ansible-go vault edit secrets.yml
```

`edit` 的内部流程：
1. 解密文件到临时文件
2. 打开编辑器（`$EDITOR` 环境变量）
3. 编辑器关闭后，重新加密临时文件
4. 替换原文件

### 4.5 rekey — 更换密码

```bash
# 更换加密密码
ansible-go vault rekey secrets.yml
```

`rekey` 的内部流程：
1. 提示输入旧密码
2. 解密文件
3. 提示输入新密码
4. 用新密码重新加密
5. 替换原文件

### 4.6 encrypt_string — 加密字符串

```bash
# 加密字符串并输出
ansible-go vault encrypt_string 'SuperSecret123!' --name 'db_password'

# 输出（可直接粘贴到 YAML 中）：
# db_password: !vault |
#   $ANSIBLE_VAULT;1.1;AES256
#   6138396631346335...
```

**内联加密字符串的用途**：不需要为每个密钥创建单独的加密文件，可以直接在
明文 YAML 中嵌入加密值。

---

## 5. Vault ID（多密码支持）

### 5.1 什么是 Vault ID

Vault ID 允许为不同的加密数据使用不同的密码。每个加密文件或字符串可以关联
一个 ID 标签，执行时通过 ID 查找对应密码。

```
场景：
  prod 环境密码 → 加密 prod 相关的 secrets
  dev 环境密码  → 加密 dev 相关的 secrets
  shared 密码   → 加密共享的 secrets
```

### 5.2 加密时指定 Vault ID

```bash
# 使用 prod 密码加密
ansible-go vault encrypt --vault-id prod@prompt prod_secrets.yml

# 使用 dev 密码加密
ansible-go vault encrypt --vault-id dev@prompt dev_secrets.yml

# 使用密码文件加密
ansible-go vault encrypt --vault-id prod@/path/to/prod_pass prod_secrets.yml
```

### 5.3 执行时指定多个 Vault ID

```bash
ansible-go playbook site.yml \
  --vault-id prod@prompt \
  --vault-id dev@prompt \
  --vault-id shared@/path/to/shared_pass
```

执行时，ansible-go 会：
1. 读取加密文件头部的 Vault ID
2. 根据 ID 查找对应的密码
3. 用该密码解密

### 5.4 Vault ID 查找逻辑

```
加密文件头部: $ANSIBLE_VAULT;1.2;AES256;prod
                                         │
                                         ▼
查找 vault-id 列表中 label == "prod" 的条目
                                         │
                                         ▼
使用该条目的密码进行解密
```

如果找不到匹配的 ID，ansible-go 会尝试用所有提供的密码依次尝试。

### 5.5 无 ID 的兼容模式

```bash
# 传统方式：单一密码，无 ID
ansible-go vault encrypt --vault-password-file ~/.vault_pass secrets.yml

# 文件头部：$ANSIBLE_VAULT;1.1;AES256（无 ID 部分）
```

无 ID 的加密文件可以使用任意提供的密码解密（逐一尝试）。

---

## 6. 密码来源

### 6.1 优先级顺序

ansible-go 按以下优先级查找 Vault 密码（从高到低）：

```
1. --vault-password-file 命令行参数
   ├── 文件路径：读取文件内容作为密码
   └── 可执行文件：执行文件，stdout 作为密码

2. ANSIBLE_VAULT_PASSWORD_FILE 环境变量
   ├── 文件路径或可执行文件，同上
   └── 例：export ANSIBLE_VAULT_PASSWORD_FILE=~/.vault_pass

3. ansible.cfg 配置
   [defaults]
   vault_password_file = ~/.vault_pass

4. 交互式提示
   └── 如果以上都没有，提示用户输入密码
```

### 6.2 密码文件类型

**普通文件**：直接读取文件内容（去除首尾空白）：

```bash
# ~/.vault_pass 内容
MyVaultPassword123!
```

**可执行文件**：执行文件并读取 stdout（去除首尾空白）：

```bash
#!/bin/bash
# ~/.vault_pass.sh
echo "MyVaultPassword123!"
```

```bash
chmod +x ~/.vault_pass.sh
ansible-go vault encrypt --vault-password-file ~/.vault_pass.sh secrets.yml
```

可执行文件的优势：可以从外部密钥管理系统动态获取密码。

### 6.3 密码来源签名

```go
// PasswordSource 密码来源接口
type PasswordSource interface {
    // GetPassword 获取密码
    GetPassword(vaultID string) (string, error)
}

// PasswordSourceType 密码来源类型
type PasswordSourceType int

const (
    PasswordSourceCLI         PasswordSourceType = iota // --vault-password-file
    PasswordSourceEnvVar                               // ANSIBLE_VAULT_PASSWORD_FILE
    PasswordSourceConfig                               // ansible.cfg
    PasswordSourcePrompt                               // 交互式提示
)

// CLIFileSource 命令行文件来源
type CLIFileSource struct {
    Path string
}

func (s *CLIFileSource) GetPassword(vaultID string) (string, error)

// EnvVarSource 环境变量来源
type EnvVarSource struct {
    EnvKey string
}

func (s *EnvVarSource) GetPassword(vaultID string) (string, error)

// ConfigSource 配置文件来源
type ConfigSource struct {
    ConfigPath string
}

func (s *ConfigSource) GetPassword(vaultID string) (string, error)

// PromptSource 交互式提示来源
type PromptSource struct{}

func (s *PromptSource) GetPassword(vaultID string) (string, error)

// PasswordManager 密码管理器
type PasswordManager struct {
    sources []PasswordSource
}

// NewPasswordManager 创建密码管理器，按优先级注册来源
func NewPasswordManager(cfg *Config) *PasswordManager

// GetPassword 按优先级尝试获取密码
func (m *PasswordManager) GetPassword(vaultID string) (string, error)
```

---

## 7. Playbook 集成

### 7.1 vars_files 透明解密

当 Playbook 引用的变量文件是 Vault 加密的，ansible-go 自动解密：

```yaml
# site.yml
---
- hosts: all
  vars_files:
    - vars/secrets.yml          # 加密文件，自动解密
    - vars/regular.yml          # 明文文件，正常读取

  tasks:
    - name: Use decrypted variable
      debug:
        msg: "DB password is {{ db_password }}"
```

`vars/secrets.yml` 内容（加密）：

```
$ANSIBLE_VAULT;1.1;AES256
6138396631346335...
```

解密后等效于：

```yaml
db_password: SuperSecret123!
api_key: sk-abc123def456
```

### 7.2 group_vars / host_vars 中的加密文件

```
inventory/
├── hosts.yml
├── group_vars/
│   ├── all.yml              # 明文变量
│   └── all_vault.yml        # 加密变量（自动解密）
└── host_vars/
    └── db1/
        └── vault.yml        # 加密的主机变量
```

ansible-go 在加载 inventory 时，自动识别并解密 `$ANSIBLE_VAULT` 头的文件。

### 7.3 内联加密字符串

使用 `encrypt_string` 生成的加密值可以直接嵌入明文 YAML：

```yaml
# vars/config.yml（明文文件）
---
app_name: myapp
db_host: db.example.com
db_port: 5432
db_password: !vault |
  $ANSIBLE_VAULT;1.1;AES256
  613839663134633538643162343935383730633862636139663430373164383766373161
  393834626235616332643861303833353036306338623633643930663134373838323763
  383837633166613530393539623564373737383236383762656330363861353065663533
  333237336665343932383438313464316334336530333835353664396566366536356237
  333130313835393135646238616239613034633534306233666231383230346336363435
  306233353238376630386264383531643237633830373838636434373938333738356364
  313664343064383835383761343438323761313866393063643539636665356661366430
  35323364303765363235
api_key: !vault |
  $ANSIBLE_VAULT;1.1;AES256
  ...
```

ansible-go 需要在 YAML 解析阶段识别 `!vault` 标签并解密。

### 7.4 加密整个文件 vs 内联加密字符串

| 场景 | 推荐方式 | 理由 |
|------|---------|------|
| 大量敏感变量 | 加密整个文件 | 减少管理开销 |
| 少量敏感变量 | 内联加密字符串 | 可以和明文变量在同一文件 |
| 不同密码保护 | 按文件加密 | 每个文件可以用不同的 Vault ID |
| 审计需求 | 按文件加密 | Git diff 能看到哪个文件被修改 |

---

## 8. Go 实现要点

### 8.1 Vault 接口

```go
// Vault 加解密引擎接口
type Vault interface {
    // Encrypt 加密明文数据
    Encrypt(plaintext []byte, password string) ([]byte, error)

    // Decrypt 解密加密数据
    Decrypt(ciphertext []byte, password string) ([]byte, error)

    // IsEncrypted 检查数据是否为 Vault 加密格式
    IsEncrypted(data []byte) bool
}

// VaultWithID 带 ID 的 Vault
type VaultWithID interface {
    Vault

    // EncryptWithID 使用指定 ID 加密
    EncryptWithID(plaintext []byte, password string, vaultID string) ([]byte, error)

    // GetVaultID 从加密数据中提取 Vault ID
    GetVaultID(data []byte) string
}
```

### 8.2 PBKDF2 密钥派生

```go
// DeriveKey 使用 PBKDF2-SHA256 从密码派生密钥
// 返回 64 字节：前 32 字节为加密密钥，后 32 字节为 HMAC 密钥
func DeriveKey(password string, salt []byte) ([]byte, error)

// 具体实现使用 golang.org/x/crypto/pbkdf2
// iterations = 10000
// keyLen = 64
```

### 8.3 AES-256-CTR 加解密

```go
// EncryptAES256CTR 使用 AES-256-CTR 模式加密
func EncryptAES256CTR(key []byte, plaintext []byte) (iv []byte, ciphertext []byte, err error)

// DecryptAES256CTR 使用 AES-256-CTR 模式解密
func DecryptAES256CTR(key []byte, iv []byte, ciphertext []byte) ([]byte, error)

// 具体实现使用 crypto/aes + crypto/cipher
// IV 长度 = aes.BlockSize = 16 字节
```

### 8.4 HMAC-SHA256 完整性

```go
// ComputeHMAC 计算 HMAC-SHA256
func ComputeHMAC(key []byte, data []byte) []byte

// VerifyHMAC 验证 HMAC-SHA256
func VerifyHMAC(key []byte, data []byte, expectedMAC []byte) bool

// 使用 crypto/hmac + crypto/sha256
```

### 8.5 文件格式处理

```go
// VaultHeader Vault 文件头部
type VaultHeader struct {
    Version  string // "1.1" or "1.2"
    Cipher   string // "AES256"
    VaultID  string // 可选
}

// ParseHeader 解析 Vault 文件头部
func ParseHeader(data []byte) (*VaultHeader, error)

// FormatHeader 格式化 Vault 文件头部
func FormatHeader(header VaultHeader) []byte

// EncodePayload 将 HMAC + IV + ciphertext 编码为 hex 格式（80 字符换行）
func EncodePayload(hmac, iv, ciphertext []byte) []byte

// DecodePayload 从 hex 格式解码出 HMAC + IV + ciphertext
func DecodePayload(data []byte) (hmac, iv, ciphertext []byte, err error)
```

### 8.6 密码管理器

```go
// PasswordManager 管理 Vault 密码来源
type PasswordManager struct {
    sources []PasswordSource
}

// PasswordSource 密码来源接口
type PasswordSource interface {
    GetPassword(vaultID string) (string, error)
    SourceType() PasswordSourceType
}

// NewPasswordManager 创建密码管理器
func NewPasswordManager(cfg *Config) *PasswordManager

// GetPassword 获取密码（按优先级尝试）
func (m *PasswordManager) GetPassword(vaultID string) (string, error)

// GetAllPasswords 获取所有可用密码（用于无 ID 文件的解密尝试）
func (m *PasswordManager) GetAllPasswords() ([]string, error)
```

### 8.7 测试要点

```go
// 测试向量：确保与 Ansible 兼容
func TestVaultCompatibility(t *testing.T) {
    // 使用已知的密码和明文，验证加密结果能被 Ansible 解密
    // 使用 Ansible 加密的文件，验证能被 ansible-go 解密
}

// 测试 round-trip：加密 → 解密 → 原文一致
func TestVaultRoundTrip(t *testing.T) {
    plaintext := []byte("db_password: SuperSecret123!")
    password := "testpassword"
    
    encrypted, err := vault.Encrypt(plaintext, password)
    assert.NoError(t, err)
    
    decrypted, err := vault.Decrypt(encrypted, password)
    assert.NoError(t, err)
    
    assert.Equal(t, plaintext, decrypted)
}

// 测试错误密码
func TestVaultWrongPassword(t *testing.T) {
    encrypted, _ := vault.Encrypt(plaintext, "correct_password")
    _, err := vault.Decrypt(encrypted, "wrong_password")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "HMAC")
}
```

---

## 9. 任务拆解

### T10.1 Vault 加解密实现

| 子任务 | 描述 | 依赖 | 验收标准 |
|--------|------|------|----------|
| T10.1.1 | PBKDF2 密钥派生 | 无 | 与 Ansible 生成的密钥一致 |
| T10.1.2 | AES-256-CTR 加解密 | T10.1.1 | round-trip 测试通过 |
| T10.1.3 | HMAC-SHA256 完整性 | T10.1.1 | 计算和验证逻辑正确 |
| T10.1.4 | 文件格式编解码 | T10.1.2, T10.1.3 | 头部解析、hex 编解码、80 字符换行 |
| T10.1.5 | Vault 主接口 | T10.1.4 | Encrypt/Decrypt/IsEncrypted 通过测试 |
| T10.1.6 | Ansible 兼容性 | T10.1.5 | 能解密 Ansible 加密的文件 |
| T10.1.7 | CLI 命令实现 | T10.1.5 | encrypt/decrypt/view/edit/rekey 命令 |
| T10.1.8 | Vault ID 支持 | T10.1.5, T10.1.7 | 多密码标签查找正确 |
| T10.1.9 | 密码来源实现 | 无 | CLI/ENV/Config/Prompt 四种来源 |
| T10.1.10 | Playbook 集成 | T10.1.5, P5 引擎 | vars_files 自动解密 |
| T10.1.11 | encrypt_string 命令 | T10.1.5, T10.1.7 | 字符串加密并输出 !vault 格式 |
| T10.1.12 | 内联加密字符串解析 | T10.1.5, P3 模板 | !vault 标签自动解密 |

**单元测试覆盖**：
- PBKDF2：与已知测试向量对比
- AES-256-CTR：加密解密往返一致
- HMAC：篡改检测
- 文件格式：各种版本和 ID 的头部解析
- Round-trip：完整加密 → 解密流程
- 错误密码：验证 HMAC 校验失败
- 密码来源：优先级正确

**兼容性测试**：
- 用 Ansible 加密的文件，ansible-go 能解密
- 用 ansible-go 加密的文件，Ansible 能解密

---

## 附录：安全最佳实践

### 密码管理

```bash
# 推荐：使用密码文件而非交互式输入
echo "MyVaultPassword" > ~/.vault_pass
chmod 600 ~/.vault_pass

# 更好：使用可执行文件从密钥管理系统获取
#!/bin/bash
# ~/.vault_pass.sh
security find-generic-password -s "ansible-vault" -w 2>/dev/null || \
    vault kv get -field=password secret/ansible/vault
chmod 700 ~/.vault_pass.sh
```

### .gitignore 配置

```gitignore
# 不要提交密码文件
.vault_pass
.vault_pass.sh
*.vault_pass

# 加密文件可以提交（这就是 Vault 的意义）
# group_vars/all_vault.yml    ← 可以提交
```

### 多环境密码分离

```
group_vars/
├── production/
│   ├── vars.yml              # 明文变量
│   └── vault.yml             # 用 prod 密码加密
├── staging/
│   ├── vars.yml
│   └── vault.yml             # 用 staging 密码加密
└── development/
    ├── vars.yml
    └── vault.yml             # 用 dev 密码加密
```
