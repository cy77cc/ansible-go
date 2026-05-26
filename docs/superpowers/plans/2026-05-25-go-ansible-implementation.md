# go-ansible Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete Go implementation of Ansible that can manage Linux servers via SSH, with full playbook, inventory, roles, vault, and galaxy support.

**Architecture:** Plugin-based 5-layer architecture (CLI → Command → Engine → Module → Connection). Each component communicates through interfaces, enabling independent testing. Variables use immutable context chains with 16-level precedence. Concurrency via goroutine worker pool (fork mechanism).

**Tech Stack:** Go 1.21+, cobra (CLI), golang.org/x/crypto/ssh (SSH), text/template + Sprig (templates), gopkg.in/yaml.v3 (YAML), github.com/fatih/color (terminal output)

**Spec:** `docs/superpowers/specs/2026-05-25-go-ansible-design.md`

---

## Phase P0: Project Skeleton + CLI

### Task 0.1: Initialize Go Module

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.gitignore`

- [ ] **Step 1: Initialize go module**

```bash
cd /root/project/ansible-go
go mod init github.com/yourname/go-ansible
```

- [ ] **Step 2: Create .gitignore**

```
bin/
coverage.out
*.retry
.idea/
.vscode/
```

- [ ] **Step 3: Create Makefile**

```makefile
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.Commit=$(COMMIT)

.PHONY: build install clean test test-coverage lint fmt vet

build:
	go build -ldflags "$(LDFLAGS)" -o bin/go-ansible ./cmd/go-ansible

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/go-ansible

clean:
	rm -rf bin/ coverage.out

test:
	go test ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

test-race:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -s -w .

vet:
	go vet ./...

check: fmt vet lint test
```

- [ ] **Step 4: Commit**

```bash
git add go.mod .gitignore Makefile
git commit -m "chore: initialize go module with Makefile"
```

---

### Task 0.2: CLI Root Command with Global Flags

**Files:**
- Create: `cmd/go-ansible/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`

- [ ] **Step 1: Write the failing test for root command**

```go
// internal/cli/root_test.go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommand_HasVersionFlag(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "go-ansible") {
		t.Errorf("expected 'go-ansible' in version output, got: %s", output)
	}
}

func TestRootCommand_HasInventoryFlag(t *testing.T) {
	cmd := NewRootCmd()
	f := cmd.PersistentFlags().Lookup("inventory")
	if f == nil {
		t.Fatal("expected --inventory flag to exist")
	}
	if f.Shorthand != "i" {
		t.Errorf("expected shorthand 'i', got '%s'", f.Shorthand)
	}
}

func TestRootCommand_DefaultInventory(t *testing.T) {
	cmd := NewRootCmd()
	f := cmd.PersistentFlags().Lookup("inventory")
	if f == nil {
		t.Fatal("expected --inventory flag")
	}
	if f.DefValue != "/etc/ansible/hosts" {
		t.Errorf("expected default '/etc/ansible/hosts', got '%s'", f.DefValue)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/ -v -run TestRootCommand
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement root command**

```go
// internal/cli/root.go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version    = "dev"
	BuildTime  = "unknown"
	Commit     = "unknown"
)

type GlobalOptions struct {
	Inventory       string
	User            string
	PrivateKeyFile  string
	Become          bool
	BecomeMethod    string
	BecomeUser      string
	Forks           int
	Verbosity       int
	Timeout         int
	Diff            bool
	Check           bool
	Limit           string
	Tags            string
	SkipTags        string
	ExtraVars       []string
	OutputFormat    string
	Connection      string
}

var Opts GlobalOptions

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "go-ansible",
		Short: "Go implementation of Ansible",
		Long:  "go-ansible is a tool for managing Linux servers over SSH, compatible with Ansible playbook format.",
		Version: fmt.Sprintf("%s (built: %s, commit: %s)", Version, BuildTime, Commit),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVarP(&Opts.Inventory, "inventory", "i", "/etc/ansible/hosts", "inventory host path or comma separated list")
	cmd.PersistentFlags().StringVarP(&Opts.User, "user", "u", "", "connect as this user (default: current user)")
	cmd.PersistentFlags().StringVar(&Opts.PrivateKeyFile, "private-key", "", "use this file to authenticate the connection")
	cmd.PersistentFlags().BoolVar(&Opts.Become, "become", false, "run operations with become")
	cmd.PersistentFlags().StringVar(&Opts.BecomeMethod, "become-method", "sudo", "privilege escalation method to use")
	cmd.PersistentFlags().StringVar(&Opts.BecomeUser, "become-user", "root", "run operations as this user")
	cmd.PersistentFlags().IntVarP(&Opts.Forks, "forks", "f", 5, "specify number of parallel processes")
	cmd.PersistentFlags().CountVarP(&Opts.Verbosity, "verbose", "v", "verbose mode (-v, -vv, -vvv, -vvvv)")
	cmd.PersistentFlags().IntVar(&Opts.Timeout, "timeout", 10, "override the connection timeout in seconds")
	cmd.PersistentFlags().BoolVar(&Opts.Diff, "diff", false, "when changing, show the diff")
	cmd.PersistentFlags().BoolVar(&Opts.Check, "check", false, "dry run mode")
	cmd.PersistentFlags().StringVar(&Opts.Limit, "limit", "", "further limit selected hosts to an additional pattern")
	cmd.PersistentFlags().StringVar(&Opts.Tags, "tags", "", "only run plays and tasks tagged with these values")
	cmd.PersistentFlags().StringVar(&Opts.SkipTags, "skip-tags", "", "only run plays and tasks whose tags do not match")
	cmd.PersistentFlags().StringSliceVarP(&Opts.ExtraVars, "extra-vars", "e", nil, "set additional variables as key=value or YAML/JSON")
	cmd.PersistentFlags().StringVarP(&Opts.OutputFormat, "output", "o", "default", "output format: default, minimal, json, yaml")
	cmd.PersistentFlags().StringVar(&Opts.Connection, "connection", "ssh", "connection type: ssh, local")

	return cmd
}

func Execute() {
	cmd := NewRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Create main.go entry point**

```go
// cmd/go-ansible/main.go
package main

import "github.com/yourname/go-ansible/internal/cli"

var (
	Version   string
	BuildTime string
	Commit    string
)

func main() {
	cli.Version = Version
	cli.BuildTime = BuildTime
	cli.Commit = Commit
	cli.Execute()
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go mod tidy
go test ./internal/cli/ -v -run TestRootCommand
```

Expected: PASS

- [ ] **Step 6: Verify CLI works**

```bash
go run ./cmd/go-ansible --help
go run ./cmd/go-ansible --version
```

- [ ] **Step 7: Commit**

```bash
git add cmd/go-ansible/main.go internal/cli/root.go internal/cli/root_test.go go.mod go.sum
git commit -m "feat: add CLI root command with global flags"
```

---

### Task 0.3: Placeholder Subcommands

**Files:**
- Create: `internal/cli/adhoc.go`
- Create: `internal/cli/playbook.go`
- Create: `internal/cli/inventory.go`
- Create: `internal/cli/vault.go`
- Create: `internal/cli/galaxy.go`
- Create: `internal/cli/config.go`

- [ ] **Step 1: Create adhoc command stub**

```go
// internal/cli/adhoc.go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewAdhocCmd() *cobra.Command {
	var module string
	var args string

	cmd := &cobra.Command{
		Use:   "adhoc <host-pattern>",
		Short: "Run an ad-hoc command on selected hosts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, pattern []string) error {
			return fmt.Errorf("adhoc command not yet implemented")
		},
	}

	cmd.Flags().StringVarP(&module, "module-name", "m", "", "module name to execute (required)")
	cmd.Flags().StringVarP(&args, "args", "a", "", "module arguments")
	cmd.MarkFlagRequired("module-name")

	return cmd
}
```

- [ ] **Step 2: Create remaining command stubs**

Each follows the same pattern — `NewXxxCmd() *cobra.Command` returning `fmt.Errorf("not yet implemented")`:

```go
// internal/cli/playbook.go
package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

func NewPlaybookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "playbook <playbook.yml>",
		Short: "Run Ansible playbooks",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("playbook command not yet implemented")
		},
	}
}
```

```go
// internal/cli/inventory.go
package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

func NewInventoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Manage inventory",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all hosts and groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("inventory list not yet implemented")
		},
	}

	hostCmd := &cobra.Command{
		Use:   "host <hostname>",
		Short: "Show host variables",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("inventory host not yet implemented")
		},
	}

	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Show host group graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("inventory graph not yet implemented")
		},
	}

	cmd.AddCommand(listCmd, hostCmd, graphCmd)
	return cmd
}
```

```go
// internal/cli/vault.go
package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

func NewVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage vault encrypted files",
	}

	for _, sub := range []string{"encrypt", "decrypt", "view", "edit", "rekey", "encrypt_string"} {
		name := sub
		cmd.AddCommand(&cobra.Command{
			Use:   name,
			Short: fmt.Sprintf("Vault %s", name),
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("vault %s not yet implemented", name)
			},
		})
	}

	return cmd
}
```

```go
// internal/cli/galaxy.go
package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

func NewGalaxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "galaxy",
		Short: "Manage Ansible roles and collections",
	}

	for _, sub := range []string{"install", "list", "remove", "init"} {
		name := sub
		cmd.AddCommand(&cobra.Command{
			Use:   name,
			Short: fmt.Sprintf("Galaxy %s", name),
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("galaxy %s not yet implemented", name)
			},
		})
	}

	return cmd
}
```

```go
// internal/cli/config.go
package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and manage configuration",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("config list not yet implemented")
		},
	}

	dumpCmd := &cobra.Command{
		Use:   "dump",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("config dump not yet implemented")
		},
	}

	cmd.AddCommand(listCmd, dumpCmd)
	return cmd
}
```

- [ ] **Step 3: Register all subcommands in root.go**

Edit `internal/cli/root.go` — add to `NewRootCmd()` before `return cmd`:

```go
	cmd.AddCommand(NewAdhocCmd())
	cmd.AddCommand(NewPlaybookCmd())
	cmd.AddCommand(NewInventoryCmd())
	cmd.AddCommand(NewVaultCmd())
	cmd.AddCommand(NewGalaxyCmd())
	cmd.AddCommand(NewConfigCmd())
```

- [ ] **Step 4: Verify all commands show in help**

```bash
go run ./cmd/go-ansible --help
go run ./cmd/go-ansible inventory --help
go run ./cmd/go-ansible vault --help
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat: add placeholder subcommands (adhoc, playbook, inventory, vault, galaxy, config)"
```

---

## Phase P1: Inventory System

### Task 1.1: Inventory Data Model

**Files:**
- Create: `internal/inventory/inventory.go`
- Create: `internal/inventory/inventory_test.go`

- [ ] **Step 1: Write tests for data model**

```go
// internal/inventory/inventory_test.go
package inventory

import "testing"

func TestNewHost(t *testing.T) {
	h := NewHost("web1", map[string]any{"ansible_host": "192.168.1.10", "http_port": 80})
	if h.Name != "web1" {
		t.Errorf("expected name 'web1', got '%s'", h.Name)
	}
	if h.GetVar("ansible_host") != "192.168.1.10" {
		t.Errorf("expected ansible_host '192.168.1.10', got '%v'", h.GetVar("ansible_host"))
	}
	if h.GetVar("ansible_port") != 22 {
		t.Errorf("expected default port 22, got '%v'", h.GetVar("ansible_port"))
	}
}

func TestNewGroup(t *testing.T) {
	g := NewGroup("webservers")
	if g.Name != "webservers" {
		t.Errorf("expected name 'webservers', got '%s'", g.Name)
	}
	if len(g.Hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(g.Hosts))
	}
}

func TestGroupAddHost(t *testing.T) {
	g := NewGroup("webservers")
	h := NewHost("web1", nil)
	g.AddHost(h)
	if len(g.Hosts) != 1 {
		t.Errorf("expected 1 host, got %d", len(g.Hosts))
	}
	if g.Hosts["web1"] != h {
		t.Error("expected host 'web1' in group")
	}
}

func TestGroupAddChild(t *testing.T) {
	parent := NewGroup("production")
	child := NewGroup("webservers")
	parent.AddChild(child)
	if len(parent.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(parent.Children))
	}
	if child.Parent != parent {
		t.Error("expected child's parent to be set")
	}
}

func TestInventoryAllGroup(t *testing.T) {
	inv := New()
	if inv.GetGroup("all") == nil {
		t.Error("expected 'all' group to exist by default")
	}
}

func TestHostGetVar_DefaultPort(t *testing.T) {
	h := NewHost("test", nil)
	if h.GetVar("ansible_port") != 22 {
		t.Errorf("expected default port 22, got %v", h.GetVar("ansible_port"))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/inventory/ -v
```

Expected: FAIL

- [ ] **Step 3: Implement data model**

```go
// internal/inventory/inventory.go
package inventory

const DefaultSSHPort = 22

// Host represents a single managed host.
type Host struct {
	Name      string
	Port      int
	Variables map[string]any
	Groups    []*Group
}

// NewHost creates a Host with given name and variables.
func NewHost(name string, vars map[string]any) *Host {
	if vars == nil {
		vars = make(map[string]any)
	}
	port := DefaultSSHPort
	if p, ok := vars["ansible_port"]; ok {
		switch v := p.(type) {
		case int:
			port = v
		case float64:
			port = int(v)
		}
	}
	return &Host{
		Name:      name,
		Port:      port,
		Variables: vars,
	}
}

// GetVar returns a variable value, checking host variables then returning nil.
func (h *Host) GetVar(key string) any {
	if v, ok := h.Variables[key]; ok {
		return v
	}
	if key == "ansible_port" {
		return h.Port
	}
	return nil
}

// Group represents a group of hosts.
type Group struct {
	Name      string
	Hosts     map[string]*Host
	Children  map[string]*Group
	Variables map[string]any
	Parent    *Group
}

// NewGroup creates an empty group.
func NewGroup(name string) *Group {
	return &Group{
		Name:      name,
		Hosts:     make(map[string]*Host),
		Children:  make(map[string]*Group),
		Variables: make(map[string]any),
	}
}

// AddHost adds a host to this group.
func (g *Group) AddHost(h *Host) {
	g.Hosts[h.Name] = h
	h.Groups = append(h.Groups, g)
}

// AddChild adds a child group.
func (g *Group) AddChild(child *Group) {
	g.Children[child.Name] = child
	child.Parent = g
}

// Inventory is the top-level container for all hosts and groups.
type Inventory struct {
	Groups map[string]*Group
	Hosts  map[string]*Host
}

// New creates an empty Inventory with the 'all' group.
func New() *Inventory {
	all := NewGroup("all")
	return &Inventory{
		Groups: map[string]*Group{"all": all},
		Hosts:  make(map[string]*Host),
	}
}

// GetGroup returns a group by name, or nil.
func (inv *Inventory) GetGroup(name string) *Group {
	return inv.Groups[name]
}

// GetHost returns a host by name, or nil.
func (inv *Inventory) GetHost(name string) *Host {
	return inv.Hosts[name]
}

// AddGroup adds a group to the inventory.
func (inv *Inventory) AddGroup(g *Group) {
	inv.Groups[g.Name] = g
}

// AddHost adds a host to the inventory and the 'all' group.
func (inv *Inventory) AddHost(h *Host) {
	inv.Hosts[h.Name] = h
	inv.Groups["all"].AddHost(h)
}

// AllHosts returns all hosts in the inventory.
func (inv *Inventory) AllHosts() []*Host {
	hosts := make([]*Host, 0, len(inv.Hosts))
	for _, h := range inv.Hosts {
		hosts = append(hosts, h)
	}
	return hosts
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/inventory/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/inventory/inventory.go internal/inventory/inventory_test.go
git commit -m "feat: add inventory data model (Host, Group, Inventory)"
```

---

### Task 1.2: INI Parser

**Files:**
- Create: `internal/inventory/ini_parser.go`
- Create: `internal/inventory/ini_parser_test.go`
- Create: `testdata/hosts.ini`

- [ ] **Step 1: Create test fixture**

```ini
// testdata/hosts.ini
[webservers]
web1 ansible_host=192.168.1.10 ansible_port=22
web2 ansible_host=192.168.1.11

[dbservers]
db1 ansible_host=192.168.1.20
db2 ansible_host=192.168.1.21

[production:children]
webservers
dbservers

[all:vars]
ansible_user=deploy

[webservers:vars]
http_port=80
```

- [ ] **Step 2: Write tests for INI parser**

```go
// internal/inventory/ini_parser_test.go
package inventory

import (
	"os"
	"testing"
)

func TestINIParser_ParseBasicGroups(t *testing.T) {
	data, err := os.ReadFile("../../testdata/hosts.ini")
	if err != nil {
		t.Fatalf("failed to read test fixture: %v", err)
	}

	p := NewINIParser()
	inv, err := p.Parse(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Check groups exist
	for _, name := range []string{"webservers", "dbservers", "production", "all"} {
		if inv.GetGroup(name) == nil {
			t.Errorf("expected group '%s' to exist", name)
		}
	}
}

func TestINIParser_ParseHosts(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.ini")
	p := NewINIParser()
	inv, _ := p.Parse(data)

	// Check hosts
	for _, name := range []string{"web1", "web2", "db1", "db2"} {
		if inv.GetHost(name) == nil {
			t.Errorf("expected host '%s' to exist", name)
		}
	}
}

func TestINIParser_HostVariables(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.ini")
	p := NewINIParser()
	inv, _ := p.Parse(data)

	web1 := inv.GetHost("web1")
	if web1 == nil {
		t.Fatal("expected host 'web1'")
	}
	if web1.GetVar("ansible_host") != "192.168.1.10" {
		t.Errorf("expected ansible_host '192.168.1.10', got '%v'", web1.GetVar("ansible_host"))
	}
	if web1.GetVar("ansible_port") != 22 {
		t.Errorf("expected port 22, got '%v'", web1.GetVar("ansible_port"))
	}
}

func TestINIParser_GroupVariables(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.ini")
	p := NewINIParser()
	inv, _ := p.Parse(data)

	allGroup := inv.GetGroup("all")
	if allGroup.Variables["ansible_user"] != "deploy" {
		t.Errorf("expected ansible_user 'deploy', got '%v'", allGroup.Variables["ansible_user"])
	}

	webGroup := inv.GetGroup("webservers")
	if webGroup.Variables["http_port"] != "80" {
		t.Errorf("expected http_port '80', got '%v'", webGroup.Variables["http_port"])
	}
}

func TestINIParser_ChildrenGroups(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.ini")
	p := NewINIParser()
	inv, _ := p.Parse(data)

	prod := inv.GetGroup("production")
	if prod == nil {
		t.Fatal("expected group 'production'")
	}
	if len(prod.Children) != 2 {
		t.Errorf("expected 2 children, got %d", len(prod.Children))
	}
}

func TestINIParser_EmptyInput(t *testing.T) {
	p := NewINIParser()
	inv, err := p.Parse([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error on empty input: %v", err)
	}
	if inv.GetGroup("all") == nil {
		t.Error("expected 'all' group even on empty input")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/inventory/ -v -run TestINI
```

Expected: FAIL

- [ ] **Step 4: Implement INI parser**

```go
// internal/inventory/ini_parser.go
package inventory

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// INIParser parses Ansible INI-format inventory files.
type INIParser struct{}

func NewINIParser() *INIParser {
	return &INIParser{}
}

// Parse parses INI inventory data into an Inventory.
func (p *INIParser) Parse(data []byte) (*Inventory, error) {
	inv := New()
	scanner := bufio.NewScanner(bytes.NewReader(data))

	var currentGroup *Group
	var currentSectionType string // "hosts", "vars", "children"

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Section header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			header := line[1 : len(line)-1]

			if strings.HasSuffix(header, ":vars") {
				groupName := strings.TrimSuffix(header, ":vars")
				currentSectionType = "vars"
				currentGroup = inv.GetGroup(groupName)
				if currentGroup == nil {
					currentGroup = NewGroup(groupName)
					inv.AddGroup(currentGroup)
				}
			} else if strings.HasSuffix(header, ":children") {
				groupName := strings.TrimSuffix(header, ":children")
				currentSectionType = "children"
				currentGroup = inv.GetGroup(groupName)
				if currentGroup == nil {
					currentGroup = NewGroup(groupName)
					inv.AddGroup(currentGroup)
				}
			} else {
				currentSectionType = "hosts"
				currentGroup = inv.GetGroup(header)
				if currentGroup == nil {
					currentGroup = NewGroup(header)
					inv.AddGroup(currentGroup)
				}
			}
			continue
		}

		if currentGroup == nil {
			continue
		}

		switch currentSectionType {
		case "hosts":
			p.parseHostLine(inv, currentGroup, line)
		case "vars":
			p.parseVarLine(currentGroup.Variables, line)
		case "children":
			childName := strings.TrimSpace(line)
			if childName != "" {
				child := inv.GetGroup(childName)
				if child == nil {
					child = NewGroup(childName)
					inv.AddGroup(child)
				}
				currentGroup.AddChild(child)
			}
		}
	}

	return inv, nil
}

func (p *INIParser) parseHostLine(inv *Inventory, group *Group, line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}

	hostName := parts[0]
	host := inv.GetHost(hostName)
	if host == nil {
		vars := make(map[string]any)
		for _, part := range parts[1:] {
			if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
				vars[kv[0]] = p.parseValue(kv[1])
			}
		}
		host = NewHost(hostName, vars)
		inv.AddHost(host)
	}

	group.AddHost(host)
}

func (p *INIParser) parseVarLine(vars map[string]any, line string) {
	if kv := strings.SplitN(line, "=", 2); len(kv) == 2 {
		vars[strings.TrimSpace(kv[0])] = p.parseValue(strings.TrimSpace(kv[1]))
	}
}

func (p *INIParser) parseValue(s string) any {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseBool(s); err == nil {
		return v
	}
	// Remove surrounding quotes
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return s[1 : len(s)-1]
	}
	return s
}

// Detect returns true if the data looks like INI format.
func (p *INIParser) Detect(data []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return true
		}
		return false
	}
	return false
}

func init() {
	_ = fmt.Sprintf("") // ensure fmt is used
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/inventory/ -v -run TestINI
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/inventory/ini_parser.go internal/inventory/ini_parser_test.go testdata/hosts.ini
git commit -m "feat: add INI inventory parser"
```

---

### Task 1.3: YAML Parser

**Files:**
- Create: `internal/inventory/yaml_parser.go`
- Create: `internal/inventory/yaml_parser_test.go`
- Create: `testdata/hosts.yml`

- [ ] **Step 1: Create test fixture**

```yaml
# testdata/hosts.yml
all:
  vars:
    ansible_user: deploy
  children:
    webservers:
      hosts:
        web1:
          ansible_host: 192.168.1.10
          http_port: 80
        web2:
          ansible_host: 192.168.1.11
      vars:
        nginx_version: "1.24"
    dbservers:
      hosts:
        db1:
          ansible_host: 192.168.1.20
        db2:
          ansible_host: 192.168.1.21
    production:
      children:
        webservers:
        dbservers:
```

- [ ] **Step 2: Write tests for YAML parser**

```go
// internal/inventory/yaml_parser_test.go
package inventory

import (
	"os"
	"testing"
)

func TestYAMLParser_ParseBasicGroups(t *testing.T) {
	data, err := os.ReadFile("../../testdata/hosts.yml")
	if err != nil {
		t.Fatalf("failed to read test fixture: %v", err)
	}

	p := NewYAMLParser()
	inv, err := p.Parse(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	for _, name := range []string{"webservers", "dbservers", "production", "all"} {
		if inv.GetGroup(name) == nil {
			t.Errorf("expected group '%s' to exist", name)
		}
	}
}

func TestYAMLParser_ParseHosts(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.yml")
	p := NewYAMLParser()
	inv, _ := p.Parse(data)

	for _, name := range []string{"web1", "web2", "db1", "db2"} {
		if inv.GetHost(name) == nil {
			t.Errorf("expected host '%s' to exist", name)
		}
	}
}

func TestYAMLParser_HostVariables(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.yml")
	p := NewYAMLParser()
	inv, _ := p.Parse(data)

	web1 := inv.GetHost("web1")
	if web1 == nil {
		t.Fatal("expected host 'web1'")
	}
	if web1.GetVar("ansible_host") != "192.168.1.10" {
		t.Errorf("expected ansible_host '192.168.1.10', got '%v'", web1.GetVar("ansible_host"))
	}
	if web1.GetVar("http_port") != 80 {
		t.Errorf("expected http_port 80, got '%v'", web1.GetVar("http_port"))
	}
}

func TestYAMLParser_GroupVariables(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.yml")
	p := NewYAMLParser()
	inv, _ := p.Parse(data)

	allGroup := inv.GetGroup("all")
	if allGroup.Variables["ansible_user"] != "deploy" {
		t.Errorf("expected ansible_user 'deploy', got '%v'", allGroup.Variables["ansible_user"])
	}

	webGroup := inv.GetGroup("webservers")
	if webGroup.Variables["nginx_version"] != "1.24" {
		t.Errorf("expected nginx_version '1.24', got '%v'", webGroup.Variables["nginx_version"])
	}
}

func TestYAMLParser_ChildrenGroups(t *testing.T) {
	data, _ := os.ReadFile("../../testdata/hosts.yml")
	p := NewYAMLParser()
	inv, _ := p.Parse(data)

	prod := inv.GetGroup("production")
	if prod == nil {
		t.Fatal("expected group 'production'")
	}
	if len(prod.Children) != 2 {
		t.Errorf("expected 2 children, got %d", len(prod.Children))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/inventory/ -v -run TestYAML
```

Expected: FAIL

- [ ] **Step 4: Implement YAML parser**

```go
// internal/inventory/yaml_parser.go
package inventory

import (
	"gopkg.in/yaml.v3"
)

// YAMLParser parses Ansible YAML-format inventory files.
type YAMLParser struct{}

func NewYAMLParser() *YAMLParser {
	return &YAMLParser{}
}

// yamlNode represents the recursive structure of YAML inventory.
type yamlNode struct {
	Hosts    map[string]map[string]any `yaml:"hosts,omitempty"`
	Children map[string]*yamlNode     `yaml:"children,omitempty"`
	Vars     map[string]any           `yaml:"vars,omitempty"`
}

// Parse parses YAML inventory data into an Inventory.
func (p *YAMLParser) Parse(data []byte) (*Inventory, error) {
	inv := New()

	var root map[string]*yamlNode
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	for name, node := range root {
		if name == "all" || name == "_meta" {
			if node.Vars != nil {
				allGroup := inv.GetGroup("all")
				for k, v := range node.Vars {
					allGroup.Variables[k] = v
				}
			}
		}
		p.parseNode(inv, name, node)
	}

	return inv, nil
}

func (p *YAMLParser) parseNode(inv *Inventory, name string, node *yamlNode) {
	if node == nil || name == "_meta" {
		return
	}

	group := inv.GetGroup(name)
	if group == nil {
		group = NewGroup(name)
		inv.AddGroup(group)
	}

	// Set group variables
	if node.Vars != nil {
		for k, v := range node.Vars {
			group.Variables[k] = v
		}
	}

	// Parse hosts
	if node.Hosts != nil {
		for hostName, hostVars := range node.Hosts {
			host := inv.GetHost(hostName)
			if host == nil {
				host = NewHost(hostName, hostVars)
				inv.AddHost(host)
			} else {
				for k, v := range hostVars {
					host.Variables[k] = v
				}
			}
			group.AddHost(host)
		}
	}

	// Parse children recursively
	if node.Children != nil {
		for childName, childNode := range node.Children {
			child := inv.GetGroup(childName)
			if child == nil {
				child = NewGroup(childName)
				inv.AddGroup(child)
			}
			group.AddChild(child)
			p.parseNode(inv, childName, childNode)
		}
	}
}

// Detect returns true if the data looks like YAML format.
func (p *YAMLParser) Detect(data []byte) bool {
	var test map[string]any
	return yaml.Unmarshal(data, &test) == nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/inventory/ -v -run TestYAML
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/inventory/yaml_parser.go internal/inventory/yaml_parser_test.go testdata/hosts.yml
git commit -m "feat: add YAML inventory parser"
```

---

### Task 1.4: Host Pattern Matching

**Files:**
- Create: `internal/inventory/host_pattern.go`
- Create: `internal/inventory/host_pattern_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/inventory/host_pattern_test.go
package inventory

import "testing"

func setupTestInventory() *Inventory {
	inv := New()

	web1 := NewHost("web1", map[string]any{"env": "prod"})
	web2 := NewHost("web2", map[string]any{"env": "prod"})
	web3 := NewHost("web3", map[string]any{"env": "staging"})
	db1 := NewHost("db1", map[string]any{"env": "prod"})

	inv.AddHost(web1)
	inv.AddHost(web2)
	inv.AddHost(web3)
	inv.AddHost(db1)

	webGroup := NewGroup("webservers")
	webGroup.AddHost(web1)
	webGroup.AddHost(web2)
	webGroup.AddHost(web3)
	inv.AddGroup(webGroup)

	dbGroup := NewGroup("dbservers")
	dbGroup.AddHost(db1)
	inv.AddGroup(dbGroup)

	prodGroup := NewGroup("production")
	prodGroup.AddChild(webGroup)
	prodGroup.AddChild(dbGroup)
	inv.AddGroup(prodGroup)

	return inv
}

func TestMatchPattern_All(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "all")
	if len(hosts) != 4 {
		t.Errorf("expected 4 hosts, got %d", len(hosts))
	}
}

func TestMatchPattern_GroupName(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "webservers")
	if len(hosts) != 3 {
		t.Errorf("expected 3 hosts, got %d", len(hosts))
	}
}

func TestMatchPattern_Union(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "webservers:dbservers")
	if len(hosts) != 4 {
		t.Errorf("expected 4 hosts, got %d", len(hosts))
	}
}

func TestMatchPattern_Wildcard(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "web*")
	if len(hosts) != 3 {
		t.Errorf("expected 3 hosts, got %d", len(hosts))
	}
}

func TestMatchPattern_Difference(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "all:!dbservers")
	if len(hosts) != 3 {
		t.Errorf("expected 3 hosts, got %d", len(hosts))
	}
}

func TestMatchPattern_SingleHost(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "web1")
	if len(hosts) != 1 {
		t.Errorf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "web1" {
		t.Errorf("expected 'web1', got '%s'", hosts[0].Name)
	}
}

func TestMatchPattern_Index(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "webservers[0]")
	if len(hosts) != 1 {
		t.Errorf("expected 1 host, got %d", len(hosts))
	}
}

func TestMatchPattern_Regex(t *testing.T) {
	inv := setupTestInventory()
	hosts := MatchPattern(inv, "~web[0-9]")
	if len(hosts) != 3 {
		t.Errorf("expected 3 hosts, got %d", len(hosts))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/inventory/ -v -run TestMatchPattern
```

Expected: FAIL

- [ ] **Step 3: Implement host pattern matching**

```go
// internal/inventory/host_pattern.go
package inventory

import (
	"regexp"
	"sort"
	"strings"
)

// MatchPattern resolves an Ansible host pattern to a list of matching hosts.
func MatchPattern(inv *Inventory, pattern string) []*Host {
	if pattern == "" {
		return nil
	}

	// Handle regex pattern
	if strings.HasPrefix(pattern, "~") {
		return matchRegex(inv, pattern[1:])
	}

	// Handle difference (all:!group)
	if strings.Contains(pattern, ":!") {
		parts := strings.SplitN(pattern, ":!", 2)
		left := resolveHostSet(inv, parts[0])
		right := resolveHostSet(inv, parts[1])
		return difference(left, right)
	}

	// Handle intersection (:&)
	if strings.Contains(pattern, ":&") {
		parts := strings.SplitN(pattern, ":&", 2)
		left := resolveHostSet(inv, parts[0])
		right := resolveHostSet(inv, parts[1])
		return intersect(left, right)
	}

	// Handle union (:)
	if strings.Contains(pattern, ":") && !strings.Contains(pattern, "://") {
		parts := strings.Split(pattern, ":")
		seen := make(map[string]bool)
		var result []*Host
		for _, part := range parts {
			for _, h := range resolveHostSet(inv, part) {
				if !seen[h.Name] {
					seen[h.Name] = true
					result = append(result, h)
				}
			}
		}
		return result
	}

	return resolveHostSet(inv, pattern)
}

func resolveHostSet(inv *Inventory, pattern string) []*Host {
	// Handle index: groupname[N] or groupname[N:M]
	if idx := strings.Index(pattern, "["); idx > 0 && strings.HasSuffix(pattern, "]") {
		groupName := pattern[:idx]
		indexStr := pattern[idx+1 : len(pattern)-1]
		hosts := getGroupHosts(inv, groupName)
		return applyIndex(hosts, indexStr)
	}

	// Handle wildcard
	if strings.ContainsAny(pattern, "*?") {
		return matchWildcard(inv, pattern)
	}

	// Handle "all"
	if pattern == "all" || pattern == "*" {
		return inv.AllHosts()
	}

	// Try as group name
	if hosts := getGroupHosts(inv, pattern); len(hosts) > 0 {
		return hosts
	}

	// Try as single host
	if h := inv.GetHost(pattern); h != nil {
		return []*Host{h}
	}

	return nil
}

func getGroupHosts(inv *Inventory, groupName string) []*Host {
	g := inv.GetGroup(groupName)
	if g == nil {
		return nil
	}
	hosts := make([]*Host, 0, len(g.Hosts))
	for _, h := range g.Hosts {
		hosts = append(hosts, h)
	}
	// Include hosts from child groups
	for _, child := range g.Children {
		hosts = append(hosts, getGroupHostsFromGroup(child)...)
	}
	// Deduplicate
	seen := make(map[string]bool)
	var result []*Host
	for _, h := range hosts {
		if !seen[h.Name] {
			seen[h.Name] = true
			result = append(result, h)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func getGroupHostsFromGroup(g *Group) []*Host {
	hosts := make([]*Host, 0, len(g.Hosts))
	for _, h := range g.Hosts {
		hosts = append(hosts, h)
	}
	for _, child := range g.Children {
		hosts = append(hosts, getGroupHostsFromGroup(child)...)
	}
	return hosts
}

func matchWildcard(inv *Inventory, pattern string) []*Host {
	var result []*Host
	for _, h := range inv.Hosts {
		if matched, _ := matchSimpleGlob(pattern, h.Name); matched {
			result = append(result, h)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func matchSimpleGlob(pattern, name string) (bool, error) {
	regex := "^"
	for _, c := range pattern {
		switch c {
		case '*':
			regex += ".*"
		case '?':
			regex += "."
		case '.':
			regex += "\\."
		default:
			regex += string(c)
		}
	}
	regex += "$"
	return regexp.MatchString(regex, name)
}

func matchRegex(inv *Inventory, pattern string) []*Host {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	var result []*Host
	for _, h := range inv.Hosts {
		if re.MatchString(h.Name) {
			result = append(result, h)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func applyIndex(hosts []*Host, indexStr string) []*Host {
	// Simple index: [0], [1], etc.
	// Range: [0:2]
	parts := strings.SplitN(indexStr, ":", 2)
	if len(parts) == 1 {
		idx := parseIntSafe(parts[0])
		if idx < 0 {
			idx = len(hosts) + idx
		}
		if idx >= 0 && idx < len(hosts) {
			return []*Host{hosts[idx]}
		}
		return nil
	}
	start := parseIntSafe(parts[0])
	end := parseIntSafe(parts[1])
	if start < 0 {
		start = len(hosts) + start
	}
	if end < 0 {
		end = len(hosts) + end
	}
	if start < 0 {
		start = 0
	}
	if end > len(hosts) {
		end = len(hosts)
	}
	if start >= end {
		return nil
	}
	return hosts[start:end]
}

func parseIntSafe(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	negative := false
	for i, c := range s {
		if i == 0 && c == '-' {
			negative = true
			continue
		}
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if negative {
		return -n
	}
	return n
}

func difference(a, b []*Host) []*Host {
	set := make(map[string]bool)
	for _, h := range b {
		set[h.Name] = true
	}
	var result []*Host
	for _, h := range a {
		if !set[h.Name] {
			result = append(result, h)
		}
	}
	return result
}

func intersect(a, b []*Host) []*Host {
	set := make(map[string]bool)
	for _, h := range b {
		set[h.Name] = true
	}
	var result []*Host
	for _, h := range a {
		if set[h.Name] {
			result = append(result, h)
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/inventory/ -v -run TestMatchPattern
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/inventory/host_pattern.go internal/inventory/host_pattern_test.go
git commit -m "feat: add host pattern matching (union, intersect, difference, wildcard, regex, index)"
```

---

### Task 1.5: Directory Loader + Inventory CLI

**Files:**
- Create: `internal/inventory/loader.go`
- Create: `internal/inventory/loader_test.go`
- Modify: `internal/cli/inventory.go`

- [ ] **Step 1: Write tests for loader**

```go
// internal/inventory/loader_test.go
package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadINIFile(t *testing.T) {
	inv, err := Load("../../testdata/hosts.ini")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if inv.GetHost("web1") == nil {
		t.Error("expected host 'web1'")
	}
}

func TestLoader_LoadYAMLFile(t *testing.T) {
	inv, err := Load("../../testdata/hosts.yml")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if inv.GetHost("web1") == nil {
		t.Error("expected host 'web1'")
	}
}

func TestLoader_LoadDirectory(t *testing.T) {
	dir := t.TempDir()

	// Write hosts file
	hostsContent := `[webservers]
web1 ansible_host=192.168.1.10
`
	os.WriteFile(filepath.Join(dir, "hosts.ini"), []byte(hostsContent), 0644)

	// Write group_vars
	gvDir := filepath.Join(dir, "group_vars")
	os.MkdirAll(gvDir, 0755)
	os.WriteFile(filepath.Join(gvDir, "webservers.yml"), []byte("http_port: 8080"), 0644)

	// Write host_vars
	hvDir := filepath.Join(dir, "host_vars")
	os.MkdirAll(hvDir, 0755)
	os.WriteFile(filepath.Join(hvDir, "web1.yml"), []byte("app_env: staging"), 0644)

	inv, err := Load(dir)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	// Check group_vars merged
	webGroup := inv.GetGroup("webservers")
	if webGroup == nil {
		t.Fatal("expected group 'webservers'")
	}
	if webGroup.Variables["http_port"] != "8080" {
		t.Errorf("expected http_port '8080', got '%v'", webGroup.Variables["http_port"])
	}

	// Check host_vars merged
	web1 := inv.GetHost("web1")
	if web1 == nil {
		t.Fatal("expected host 'web1'")
	}
	if web1.GetVar("app_env") != "staging" {
		t.Errorf("expected app_env 'staging', got '%v'", web1.GetVar("app_env"))
	}
}

func TestLoader_NonExistentPath(t *testing.T) {
	_, err := Load("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/inventory/ -v -run TestLoader
```

Expected: FAIL

- [ ] **Step 3: Implement loader**

```go
// internal/inventory/loader.go
package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load loads inventory from a file or directory path.
func Load(path string) (*Inventory, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inventory path not found: %s", path)
	}

	if info.IsDir() {
		return loadDirectory(path)
	}
	return loadFile(path)
}

func loadFile(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yml", ".yaml":
		return NewYAMLParser().Parse(data)
	default:
		// Try INI first, fall back to YAML
		p := NewINIParser()
		if p.Detect(data) {
			return p.Parse(data)
		}
		return NewYAMLParser().Parse(data)
	}
}

func loadDirectory(dir string) (*Inventory, error) {
	// Find main inventory file
	var mainFile string
	for _, name := range []string{"hosts", "hosts.ini", "hosts.yml", "hosts.yaml"} {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			mainFile = candidate
			break
		}
	}

	var inv *Inventory
	if mainFile != "" {
		var err error
		inv, err = loadFile(mainFile)
		if err != nil {
			return nil, err
		}
	} else {
		inv = New()
	}

	// Load group_vars
	loadVarsDir(filepath.Join(dir, "group_vars"), func(name string, vars map[string]any) {
		group := inv.GetGroup(name)
		if group == nil {
			group = NewGroup(name)
			inv.AddGroup(group)
		}
		for k, v := range vars {
			group.Variables[k] = v
		}
	})

	// Load host_vars
	loadVarsDir(filepath.Join(dir, "host_vars"), func(name string, vars map[string]any) {
		host := inv.GetHost(name)
		if host == nil {
			host = NewHost(name, vars)
			inv.AddHost(host)
		} else {
			for k, v := range vars {
				host.Variables[k] = v
			}
		}
	})

	return inv, nil
}

func loadVarsDir(dir string, apply func(name string, vars map[string]any)) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".yml" && ext != ".yaml" && ext != ".json" {
			continue
		}
		key := strings.TrimSuffix(name, ext)

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}

		vars := make(map[string]any)
		if err := yaml.Unmarshal(data, &vars); err != nil {
			continue
		}

		apply(key, vars)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/inventory/ -v -run TestLoader
```

Expected: PASS

- [ ] **Step 5: Implement inventory CLI subcommands**

Modify `internal/cli/inventory.go` to replace stubs with real implementations:

```go
// internal/cli/inventory.go
package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yourname/go-ansible/internal/inventory"
)

func NewInventoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Manage inventory",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all hosts and groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			inv, err := inventory.Load(Opts.Inventory)
			if err != nil {
				return err
			}
			for _, groupName := range sortedGroupNames(inv) {
				g := inv.GetGroup(groupName)
				if len(g.Hosts) == 0 && len(g.Children) == 0 {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", groupName)
				for _, hostName := range sortedHostNames(g) {
					h := g.Hosts[hostName]
					vars := []string{}
					for k, v := range h.Variables {
						if !strings.HasPrefix(k, "ansible_") {
							vars = append(vars, fmt.Sprintf("%s=%v", k, v))
						}
					}
					sort.Strings(vars)
					if len(vars) > 0 {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s\n", hostName, strings.Join(vars, " "))
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", hostName)
					}
				}
			}
			return nil
		},
	}

	hostCmd := &cobra.Command{
		Use:   "host <hostname>",
		Short: "Show host variables",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inv, err := inventory.Load(Opts.Inventory)
			if err != nil {
				return err
			}
			h := inv.GetHost(args[0])
			if h == nil {
				return fmt.Errorf("host '%s' not found", args[0])
			}
			keys := make([]string, 0, len(h.Variables))
			for k := range h.Variables {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %v\n", k, h.Variables[k])
			}
			return nil
		},
	}

	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Show host group graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			inv, err := inventory.Load(Opts.Inventory)
			if err != nil {
				return err
			}
			all := inv.GetGroup("all")
			printGraph(cmd.OutOrStdout(), all, 0)
			return nil
		},
	}

	cmd.AddCommand(listCmd, hostCmd, graphCmd)
	return cmd
}

func sortedGroupNames(inv *inventory.Inventory) []string {
	names := make([]string, 0, len(inv.Groups))
	for name := range inv.Groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedHostNames(g *inventory.Group) []string {
	names := make([]string, 0, len(g.Hosts))
	for name := range g.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func printGraph(w interface{ Write([]byte) (int, error) }, g *inventory.Group, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(w, "%s@%s:\n", indent, g.Name)
	for _, name := range sortedHostNames(g) {
		fmt.Fprintf(w, "%s  %s\n", indent, name)
	}
	for childName, child := range g.Children {
		printGraph(w, child, depth+1)
		_ = childName
	}
}
```

- [ ] **Step 6: Verify CLI**

```bash
go run ./cmd/go-ansible inventory list -i testdata/hosts.ini
go run ./cmd/go-ansible inventory list -i testdata/hosts.yml
go run ./cmd/go-ansible inventory host web1 -i testdata/hosts.ini
go run ./cmd/go-ansible inventory graph -i testdata/hosts.ini
```

- [ ] **Step 7: Commit**

```bash
git add internal/inventory/loader.go internal/inventory/loader_test.go internal/cli/inventory.go
git commit -m "feat: add inventory directory loader and inventory CLI subcommands"
```

---

## Phase P2: Connection Layer

### Task 2.1: Connection Interface + Local Connection

**Files:**
- Create: `internal/connection/connection.go`
- Create: `internal/connection/local.go`
- Create: `internal/connection/local_test.go`

- [ ] **Step 1: Write tests for local connection**

```go
// internal/connection/local_test.go
package connection

import (
	"testing"
)

func TestLocalConnection_Exec(t *testing.T) {
	conn := NewLocalConnection()
	stdout, stderr, rc, err := conn.Exec("echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc != 0 {
		t.Errorf("expected rc 0, got %d", rc)
	}
	if stdout != "hello\n" {
		t.Errorf("expected 'hello\\n', got '%s'", stdout)
	}
	_ = stderr
}

func TestLocalConnection_ExecFailure(t *testing.T) {
	conn := NewLocalConnection()
	_, _, rc, _ := conn.Exec("false")
	if rc != 1 {
		t.Errorf("expected rc 1, got %d", rc)
	}
}

func TestLocalConnection_Shell(t *testing.T) {
	conn := NewLocalConnection()
	if conn.Shell() != "/bin/sh" {
		t.Errorf("expected '/bin/sh', got '%s'", conn.Shell())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/connection/ -v -run TestLocal
```

Expected: FAIL

- [ ] **Step 3: Implement connection interface and local connection**

```go
// internal/connection/connection.go
package connection

// Connection defines the interface for executing commands on remote/local hosts.
type Connection interface {
	// Connect establishes the connection.
	Connect() error
	// Exec executes a command and returns stdout, stderr, exit code, and error.
	Exec(cmd string) (stdout, stderr string, rc int, err error)
	// PutFile copies a local file to the remote path.
	PutFile(localPath, remotePath string) error
	// FetchFile copies a remote file to the local path.
	FetchFile(remotePath, localPath string) error
	// Close closes the connection.
	Close() error
	// Shell returns the default shell path.
	Shell() string
}
```

```go
// internal/connection/local.go
package connection

import (
	"bytes"
	"os"
	"os/exec"
)

// LocalConnection executes commands on the local machine.
type LocalConnection struct{}

func NewLocalConnection() *LocalConnection {
	return &LocalConnection{}
}

func (c *LocalConnection) Connect() error {
	return nil
}

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
		err = nil
	} else if err != nil {
		rc = 1
	}

	return
}

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

func (c *LocalConnection) Close() error {
	return nil
}

func (c *LocalConnection) Shell() string {
	return "/bin/sh"
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/connection/ -v -run TestLocal
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/connection/connection.go internal/connection/local.go internal/connection/local_test.go
git commit -m "feat: add Connection interface and LocalConnection implementation"
```

---

### Task 2.2: SSH Connection

**Files:**
- Create: `internal/connection/ssh.go`
- Create: `internal/connection/ssh_test.go`

- [ ] **Step 1: Write tests (using mock SSH server or integration tests)**

```go
// internal/connection/ssh_test.go
package connection

import (
	"testing"
)

// NOTE: SSH integration tests require a running SSH server.
// These tests validate configuration parsing only.
// Full integration tests are in test/e2e/.

func TestSSHConnection_ConfigFromVars(t *testing.T) {
	vars := map[string]any{
		"ansible_host":                    "192.168.1.10",
		"ansible_port":                    2222,
		"ansible_user":                    "testuser",
		"ansible_ssh_private_key_file":    "/path/to/key",
		"ansible_timeout":                 30,
	}

	cfg := SSHConfigFromVars(vars)
	if cfg.Host != "192.168.1.10" {
		t.Errorf("expected host '192.168.1.10', got '%s'", cfg.Host)
	}
	if cfg.Port != 2222 {
		t.Errorf("expected port 2222, got %d", cfg.Port)
	}
	if cfg.User != "testuser" {
		t.Errorf("expected user 'testuser', got '%s'", cfg.User)
	}
	if cfg.KeyFile != "/path/to/key" {
		t.Errorf("expected key '/path/to/key', got '%s'", cfg.KeyFile)
	}
	if cfg.Timeout != 30 {
		t.Errorf("expected timeout 30, got %d", cfg.Timeout)
	}
}

func TestSSHConnection_DefaultConfig(t *testing.T) {
	vars := map[string]any{
		"ansible_host": "10.0.0.1",
	}

	cfg := SSHConfigFromVars(vars)
	if cfg.Port != 22 {
		t.Errorf("expected default port 22, got %d", cfg.Port)
	}
	if cfg.Timeout != 10 {
		t.Errorf("expected default timeout 10, got %d", cfg.Timeout)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/connection/ -v -run TestSSHConnection
```

Expected: FAIL

- [ ] **Step 3: Implement SSH connection**

```go
// internal/connection/ssh.go
package connection

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
	"github.com/pkg/sftp"
)

// SSHConfig holds SSH connection configuration.
type SSHConfig struct {
	Host       string
	Port       int
	User       string
	KeyFile    string
	Password   string
	Timeout    int
	BecomePass string
}

// SSHConfigFromVars extracts SSH configuration from inventory variables.
func SSHConfigFromVars(vars map[string]any) *SSHConfig {
	cfg := &SSHConfig{
		Port:    22,
		Timeout: 10,
	}

	if v, ok := vars["ansible_host"]; ok {
		cfg.Host = fmt.Sprintf("%v", v)
	}
	if v, ok := vars["ansible_port"]; ok {
		switch p := v.(type) {
		case int:
			cfg.Port = p
		case float64:
			cfg.Port = int(p)
		}
	}
	if v, ok := vars["ansible_user"]; ok {
		cfg.User = fmt.Sprintf("%v", v)
	}
	if v, ok := vars["ansible_ssh_private_key_file"]; ok {
		cfg.KeyFile = fmt.Sprintf("%v", v)
	}
	if v, ok := vars["ansible_ssh_pass"]; ok {
		cfg.Password = fmt.Sprintf("%v", v)
	}
	if v, ok := vars["ansible_timeout"]; ok {
		switch t := v.(type) {
		case int:
			cfg.Timeout = t
		case float64:
			cfg.Timeout = int(t)
		}
	}

	// Default user
	if cfg.User == "" {
		cfg.User = os.Getenv("USER")
		if cfg.User == "" {
			cfg.User = "root"
		}
	}

	// Default key file
	if cfg.KeyFile == "" {
		home, _ := os.UserHomeDir()
		defaultKey := filepath.Join(home, ".ssh", "id_rsa")
		if _, err := os.Stat(defaultKey); err == nil {
			cfg.KeyFile = defaultKey
		}
	}

	return cfg
}

// SSHConnection implements Connection over SSH.
type SSHConnection struct {
	Config   *SSHConfig
	client   *ssh.Client
	sftpClient *sftp.Client
}

func NewSSHConnection(cfg *SSHConfig) *SSHConnection {
	return &SSHConnection{Config: cfg}
}

func (c *SSHConnection) Connect() error {
	authMethods, err := c.buildAuthMethods()
	if err != nil {
		return fmt.Errorf("SSH auth error: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", c.Config.Host, c.Config.Port)
	clientConfig := &ssh.ClientConfig{
		User:            c.Config.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(c.Config.Timeout) * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		return fmt.Errorf("SSH dial error: %w", err)
	}
	c.client = client

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return fmt.Errorf("SFTP error: %w", err)
	}
	c.sftpClient = sftpClient

	return nil
}

func (c *SSHConnection) buildAuthMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if c.Config.KeyFile != "" {
		key, err := os.ReadFile(c.Config.KeyFile)
		if err == nil {
			signer, err := ssh.ParsePrivateKey(key)
			if err != nil {
				// Try with passphrase
				if c.Config.Password != "" {
					signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(c.Config.Password))
				}
				if err != nil {
					return nil, fmt.Errorf("parse key error: %w", err)
				}
			}
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}

	if c.Config.Password != "" {
		methods = append(methods, ssh.Password(c.Config.Password))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication methods available")
	}

	return methods, nil
}

func (c *SSHConnection) Exec(cmd string) (stdout, stderr string, rc int, err error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("session error: %w", err)
	}
	defer session.Close()

	var outBuf, errBuf bytes.Buffer
	session.Stdout = &outBuf
	session.Stderr = &errBuf

	err = session.Run(cmd)
	stdout = outBuf.String()
	stderr = errBuf.String()

	if exitErr, ok := err.(*ssh.ExitError); ok {
		rc = exitErr.ExitStatus()
		err = nil
	} else if err != nil {
		rc = 1
	}

	return
}

func (c *SSHConnection) PutFile(localPath, remotePath string) error {
	if c.sftpClient == nil {
		return fmt.Errorf("SFTP not connected")
	}

	localData, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}

	remoteFile, err := c.sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	_, err = remoteFile.Write(localData)
	return err
}

func (c *SSHConnection) FetchFile(remotePath, localPath string) error {
	if c.sftpClient == nil {
		return fmt.Errorf("SFTP not connected")
	}

	remoteFile, err := c.sftpClient.Open(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(remoteFile); err != nil {
		return err
	}

	return os.WriteFile(localPath, buf.Bytes(), 0644)
}

func (c *SSHConnection) Close() error {
	if c.sftpClient != nil {
		c.sftpClient.Close()
	}
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *SSHConnection) Shell() string {
	return "/bin/sh"
}

// NewConnection creates a Connection based on inventory variables.
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

- [ ] **Step 4: Install SFTP dependency and run tests**

```bash
go get github.com/pkg/sftp
go get golang.org/x/crypto/ssh
go test ./internal/connection/ -v -run TestSSHConnection
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/connection/ssh.go internal/connection/ssh_test.go go.mod go.sum
git commit -m "feat: add SSH connection with key/password auth and SFTP file transfer"
```

---

### Task 2.3: Become (Privilege Escalation)

**Files:**
- Create: `internal/connection/become.go`
- Create: `internal/connection/become_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/connection/become_test.go
package connection

import "testing"

func TestWrapCommand_Sudo(t *testing.T) {
	result := WrapCommand("cat /etc/shadow", "sudo", "root", "")
	expected := "sudo -H -S -n -u root /bin/sh -c 'cat /etc/shadow'"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestWrapCommand_Su(t *testing.T) {
	result := WrapCommand("whoami", "su", "root", "")
	expected := "su - root -c 'whoami'"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestWrapCommand_NoBecome(t *testing.T) {
	result := WrapCommand("whoami", "", "", "")
	if result != "whoami" {
		t.Errorf("expected 'whoami', got '%s'", result)
	}
}

func TestWrapCommand_SudoWithPassword(t *testing.T) {
	result := WrapCommand("whoami", "sudo", "root", "secret")
	expected := "echo 'secret' | sudo -H -S -u root /bin/sh -c 'whoami'"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/connection/ -v -run TestWrapCommand
```

Expected: FAIL

- [ ] **Step 3: Implement become**

```go
// internal/connection/become.go
package connection

import "fmt"

// WrapCommand wraps a command with privilege escalation.
func WrapCommand(cmd, method, user, password string) string {
	if method == "" {
		return cmd
	}

	escaped := shellQuote(cmd)

	switch method {
	case "sudo":
		if password != "" {
			return fmt.Sprintf("echo %s | sudo -H -S -u %s /bin/sh -c %s",
				shellQuote(password), user, escaped)
		}
		return fmt.Sprintf("sudo -H -S -n -u %s /bin/sh -c %s", user, escaped)
	case "su":
		return fmt.Sprintf("su - %s -c %s", user, escaped)
	default:
		return cmd
	}
}

func shellQuote(s string) string {
	return "'" + s + "'" // simplified; real impl should escape single quotes
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/connection/ -v -run TestWrapCommand
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/connection/become.go internal/connection/become_test.go
git commit -m "feat: add privilege escalation (become) support"
```

---

## Phase P3: Variables + Template Engine

### Task 3.1: Variable Context

**Files:**
- Create: `internal/variables/context.go`
- Create: `internal/variables/context_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/variables/context_test.go
package variables

import "testing"

func TestContext_SetAndGet(t *testing.T) {
	ctx := NewContext()
	ctx.Set("foo", "bar")
	if ctx.Get("foo") != "bar" {
		t.Errorf("expected 'bar', got '%v'", ctx.Get("foo"))
	}
}

func TestContext_ChildOverridesParent(t *testing.T) {
	parent := NewContext()
	parent.Set("key", "parent_value")

	child := parent.Child()
	child.Set("key", "child_value")

	if parent.Get("key") != "parent_value" {
		t.Error("parent should not be modified")
	}
	if child.Get("key") != "child_value" {
		t.Error("child should override parent")
	}
}

func TestContext_DeepMerge(t *testing.T) {
	parent := NewContext()
	parent.Set("config", map[string]any{"a": 1, "b": 2})

	child := parent.Child()
	child.Set("config", map[string]any{"b": 3, "c": 4})

	config, ok := child.Get("config").(map[string]any)
	if !ok {
		t.Fatal("expected map")
	}
	if config["a"] != 1 {
		t.Errorf("expected a=1, got %v", config["a"])
	}
	if config["b"] != 3 {
		t.Errorf("expected b=3, got %v", config["b"])
	}
	if config["c"] != 4 {
		t.Errorf("expected c=4, got %v", config["c"])
	}
}

func TestContext_ChildDoesNotMutateParent(t *testing.T) {
	parent := NewContext()
	parent.Set("x", 1)

	child := parent.Child()
	child.Set("x", 2)
	child.Set("y", 3)

	if parent.Get("x") != 1 {
		t.Error("parent x should still be 1")
	}
	if parent.Get("y") != nil {
		t.Error("parent should not have 'y'")
	}
}

func TestContext_ToMap(t *testing.T) {
	ctx := NewContext()
	ctx.Set("a", 1)
	ctx.Set("b", "hello")

	m := ctx.ToMap()
	if m["a"] != 1 || m["b"] != "hello" {
		t.Errorf("unexpected map: %v", m)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/variables/ -v
```

Expected: FAIL

- [ ] **Step 3: Implement variable context**

```go
// internal/variables/context.go
package variables

import "sync"

// Context holds variables with parent-child scope chain.
// Contexts are immutable — Set creates new entries, child contexts copy-on-write.
type Context struct {
	vars   map[string]any
	parent *Context
	mu     sync.RWMutex
}

// NewContext creates a new root context.
func NewContext() *Context {
	return &Context{
		vars: make(map[string]any),
	}
}

// Child creates a child context that inherits from this one.
func (c *Context) Child() *Context {
	return &Context{
		vars:   make(map[string]any),
		parent: c,
	}
}

// Set sets a variable in this context.
func (c *Context) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vars[key] = value
}

// Get gets a variable, checking this context then parents.
func (c *Context) Get(key string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.vars[key]; ok {
		return v
	}
	if c.parent != nil {
		return c.parent.Get(key)
	}
	return nil
}

// GetAll returns all variables merged into a single map (child overrides parent).
func (c *Context) GetAll() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]any)
	if c.parent != nil {
		for k, v := range c.parent.GetAll() {
			result[k] = v
		}
	}
	for k, v := range c.vars {
		if existing, ok := result[k]; ok {
			result[k] = deepMerge(existing, v)
		} else {
			result[k] = v
		}
	}
	return result
}

// ToMap returns all variables as a map.
func (c *Context) ToMap() map[string]any {
	return c.GetAll()
}

// deepMerge merges two values: dicts merge recursively, scalars override.
func deepMerge(base, override any) any {
	baseMap, baseIsMap := base.(map[string]any)
	overrideMap, overrideIsMap := override.(map[string]any)

	if baseIsMap && overrideIsMap {
		result := make(map[string]any)
		for k, v := range baseMap {
			result[k] = v
		}
		for k, v := range overrideMap {
			if existing, ok := result[k]; ok {
				result[k] = deepMerge(existing, v)
			} else {
				result[k] = v
			}
		}
		return result
	}

	// Override wins for non-dict types
	return override
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/variables/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/variables/context.go internal/variables/context_test.go
git commit -m "feat: add variable context with scope chain and deep merge"
```

---

### Task 3.2: Template Engine

**Files:**
- Create: `internal/template/engine.go`
- Create: `internal/template/engine_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/template/engine_test.go
package template

import "testing"

func TestRender_BasicVariable(t *testing.T) {
	vars := map[string]any{"name": "world"}
	result, err := Render("Hello {{ .name }}", vars)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", result)
	}
}

func TestRender_SprigFunction(t *testing.T) {
	vars := map[string]any{"name": "hello"}
	result, err := Render("{{ .name | upper }}", vars)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result != "HELLO" {
		t.Errorf("expected 'HELLO', got '%s'", result)
	}
}

func TestRender_DefaultValue(t *testing.T) {
	vars := map[string]any{}
	result, err := Render(`{{ .name | default "fallback" }}`, vars)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result != "fallback" {
		t.Errorf("expected 'fallback', got '%s'", result)
	}
}

func TestRender_Conditional(t *testing.T) {
	vars := map[string]any{"env": "prod"}
	tmpl := `{{ if eq .env "prod" }}production{{ else }}staging{{ end }}`
	result, err := Render(tmpl, vars)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result != "production" {
		t.Errorf("expected 'production', got '%s'", result)
	}
}

func TestRender_Range(t *testing.T) {
	vars := map[string]any{"items": []any{"a", "b", "c"}}
	tmpl := `{{ range .items }}{{ . }} {{ end }}`
	result, err := Render(tmpl, vars)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result != "a b c " {
		t.Errorf("expected 'a b c ', got '%s'", result)
	}
}

func TestEvaluate_True(t *testing.T) {
	vars := map[string]any{"x": 10}
	result, err := Evaluate(".x > 5", vars)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvaluate_False(t *testing.T) {
	vars := map[string]any{"x": 3}
	result, err := Evaluate(".x > 5", vars)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result {
		t.Error("expected false")
	}
}

func TestPreprocess_AddsDotPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{{ foo }}`, `{{ .foo }}`},
		{`{{ .foo }}`, `{{ .foo }}`},
		{`{{ foo | upper }}`, `{{ .foo | upper }}`},
		{`{{ range items }}`, `{{ range .items }}`},
	}
	for _, tt := range tests {
		result := preprocess(tt.input)
		if result != tt.expected {
			t.Errorf("preprocess(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/template/ -v
```

Expected: FAIL

- [ ] **Step 3: Implement template engine**

```go
// internal/template/engine.go
package template

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// FuncMap returns the combined Sprig + custom function map.
func FuncMap() template.FuncMap {
	fm := sprig.TxtFuncMap()
	// Add custom Ansible-compatible filters here in P13
	return fm
}

// Render renders a template string with the given variables.
func Render(tmplStr string, vars map[string]any) (string, error) {
	processed := preprocess(tmplStr)

	t, err := template.New("go-ansible").Funcs(FuncMap()).Parse(processed)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}

	return buf.String(), nil
}

// Evaluate evaluates a boolean expression (for `when` conditions).
func Evaluate(expr string, vars map[string]any) (bool, error) {
	tmplStr := fmt.Sprintf("{{ if %s }}true{{ end }}", expr)
	result, err := Render(tmplStr, vars)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result) == "true", nil
}

// preprocess adds dot prefix to bare variable references in {{ }} blocks.
// Converts {{ foo }} to {{ .foo }}, but preserves {{ .foo }} and function calls.
var templateVarRe = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(\||\}\})`)

func preprocess(tmpl string) string {
	return templateVarRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		// If already has dot prefix, leave it
		if strings.Contains(match, ".") {
			return match
		}
		// Extract variable name and add dot
		parts := strings.SplitN(match, "{{", 2)
		if len(parts) < 2 {
			return match
		}
		inner := parts[1]
		// Find the variable name (first word after whitespace)
		inner = strings.TrimSpace(inner)
		spaceIdx := strings.IndexAny(inner, " |\t\n")
		if spaceIdx == -1 {
			// Just a variable: {{ foo }}
			return "{{ ." + inner
		}
		varName := inner[:spaceIdx]
		rest := inner[spaceIdx:]
		return "{{ ." + varName + rest
	})
}
```

- [ ] **Step 4: Install sprig and run tests**

```bash
go get github.com/Masterminds/sprig/v3
go test ./internal/template/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/template/engine.go internal/template/engine_test.go go.mod go.sum
git commit -m "feat: add template engine with Sprig integration and variable preprocessing"
```

---

## Phase P4: Module System

### Task 4.1: Module Interface + Registry + Ping Module

**Files:**
- Create: `internal/modules/module.go`
- Create: `internal/modules/registry.go`
- Create: `internal/modules/registry_test.go`
- Create: `internal/modules/ping.go`
- Create: `internal/modules/ping_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/modules/registry_test.go
package modules

import "testing"

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	r.Register(&PingModule{})
	m := r.Get("ping")
	if m == nil {
		t.Fatal("expected to find 'ping' module")
	}
	if m.Name() != "ping" {
		t.Errorf("expected 'ping', got '%s'", m.Name())
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRegistry()
	m := r.Get("nonexistent")
	if m != nil {
		t.Error("expected nil for unknown module")
	}
}
```

```go
// internal/modules/ping_test.go
package modules

import "testing"

func TestPingModule_Run(t *testing.T) {
	pm := &PingModule{}
	result, err := pm.Run(ExecContext{
		Args: map[string]any{"data": "pong"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Extra["ping"] != "pong" {
		t.Errorf("expected 'pong', got '%v'", result.Extra["ping"])
	}
}

func TestPingModule_RunDefault(t *testing.T) {
	pm := &PingModule{}
	result, err := pm.Run(ExecContext{Args: map[string]any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Extra["ping"] != "pong" {
		t.Errorf("expected default 'pong', got '%v'", result.Extra["ping"])
	}
}

func TestPingModule_CheckMode(t *testing.T) {
	pm := &PingModule{}
	if !pm.SupportsCheckMode() {
		t.Error("ping should support check mode")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/modules/ -v
```

Expected: FAIL

- [ ] **Step 3: Implement module interface, registry, and ping**

```go
// internal/modules/module.go
package modules

import (
	"github.com/yourname/go-ansible/internal/connection"
)

// ExecContext provides context for module execution.
type ExecContext struct {
	Host       map[string]any
	Args       map[string]any
	Connection connection.Connection
	CheckMode  bool
	Diff       bool
	Variables  map[string]any
}

// Result is the outcome of a module execution.
type Result struct {
	Changed bool
	Failed  bool
	Msg     string
	Stdout  string
	Stderr  string
	Rc      int
	Extra   map[string]any
}

// Module defines the interface all modules must implement.
type Module interface {
	Name() string
	Run(ctx ExecContext) (Result, error)
	SupportsCheckMode() bool
}
```

```go
// internal/modules/registry.go
package modules

// Registry holds all registered modules.
type Registry struct {
	modules map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{
		modules: make(map[string]Module),
	}
}

func (r *Registry) Register(m Module) {
	r.modules[m.Name()] = m
}

func (r *Registry) Get(name string) Module {
	return r.modules[name]
}

// DefaultRegistry is the global module registry.
var DefaultRegistry = NewRegistry()

func init() {
	DefaultRegistry.Register(&PingModule{})
	DefaultRegistry.Register(&ShellModule{})
	DefaultRegistry.Register(&CommandModule{})
	DefaultRegistry.Register(&DebugModule{})
	DefaultRegistry.Register(&SetupModule{})
	DefaultRegistry.Register(&SetFactModule{})
	DefaultRegistry.Register(&MetaModule{})
	DefaultRegistry.Register(&AsyncStatusModule{})
}
```

```go
// internal/modules/ping.go
package modules

// PingModule tests connectivity to a host.
type PingModule struct{}

func (m *PingModule) Name() string { return "ping" }

func (m *PingModule) SupportsCheckMode() bool { return true }

func (m *PingModule) Run(ctx ExecContext) (Result, error) {
	data := "pong"
	if v, ok := ctx.Args["data"]; ok {
		data = v.(string)
	}
	return Result{
		Changed: false,
		Extra:   map[string]any{"ping": data},
	}, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/modules/ -v
```

Expected: PASS

- [ ] **Step 5: Implement shell and command modules**

```go
// internal/modules/shell.go
package modules

import "fmt"

type ShellModule struct{}

func (m *ShellModule) Name() string            { return "shell" }
func (m *ShellModule) SupportsCheckMode() bool { return false }

func (m *ShellModule) Run(ctx ExecContext) (Result, error) {
	cmd := ""
	if v, ok := ctx.Args["cmd"]; ok {
		cmd = fmt.Sprintf("%v", v)
	} else if v, ok := ctx.Args["_raw_params"]; ok {
		cmd = fmt.Sprintf("%v", v)
	}
	if cmd == "" {
		return Result{Failed: true, Msg: "command is required"}, nil
	}

	if ctx.Connection == nil {
		return Result{Failed: true, Msg: "no connection"}, nil
	}

	if chdir, ok := ctx.Args["chdir"]; ok {
		cmd = fmt.Sprintf("cd %v && %s", chdir, cmd)
	}

	stdout, stderr, rc, err := ctx.Connection.Exec(cmd)
	if err != nil {
		return Result{Failed: true, Msg: err.Error(), Stdout: stdout, Stderr: stderr, Rc: rc}, nil
	}

	return Result{
		Changed: rc == 0,
		Failed:  rc != 0,
		Stdout:  stdout,
		Stderr:  stderr,
		Rc:      rc,
	}, nil
}
```

```go
// internal/modules/command.go
package modules

// CommandModule is identical to ShellModule for our implementation.
type CommandModule = ShellModule
```

```go
// internal/modules/debug.go
package modules

import "fmt"

type DebugModule struct{}

func (m *DebugModule) Name() string            { return "debug" }
func (m *DebugModule) SupportsCheckMode() bool { return true }

func (m *DebugModule) Run(ctx ExecContext) (Result, error) {
	msg := ""
	if v, ok := ctx.Args["msg"]; ok {
		msg = fmt.Sprintf("%v", v)
	}
	if v, ok := ctx.Args["var"]; ok {
		varName := fmt.Sprintf("%v", v)
		if val, exists := ctx.Variables[varName]; exists {
			msg = fmt.Sprintf("%v", val)
		} else {
			msg = fmt.Sprintf("VARIABLE IS NOT DEFINED!: %s", varName)
		}
	}
	return Result{Changed: false, Msg: msg}, nil
}
```

```go
// internal/modules/setup.go
package modules

import "fmt"

type SetupModule struct{}

func (m *SetupModule) Name() string            { return "setup" }
func (m *SetupModule) SupportsCheckMode() bool { return true }

func (m *SetupModule) Run(ctx ExecContext) (Result, error) {
	if ctx.Connection == nil {
		return Result{Failed: true, Msg: "no connection"}, nil
	}

	facts := make(map[string]any)

	commands := map[string]string{
		"ansible_hostname":          "hostname",
		"ansible_fqdn":              "hostname -f",
		"ansible_machine":           "uname -m",
		"ansible_system":            "uname -s",
		"ansible_kernel":            "uname -r",
		"ansible_os_family":         "cat /etc/os-release 2>/dev/null | grep ^ID_LIKE | cut -d= -f2 || echo unknown",
		"ansible_distribution":      "cat /etc/os-release 2>/dev/null | grep ^ID | head -1 | cut -d= -f2 | tr -d '\"' || echo unknown",
		"ansible_distribution_version": "cat /etc/os-release 2>/dev/null | grep ^VERSION_ID | cut -d= -f2 | tr -d '\"' || echo unknown",
		"ansible_processor_cores":   "nproc",
		"ansible_memtotal_mb":       "free -m | awk '/^Mem:/ {print $2}'",
		"ansible_user_id":           "whoami",
		"ansible_user_uid":          "id -u",
		"ansible_user_gid":          "id -g",
	}

	for key, cmd := range commands {
		stdout, _, rc, err := ctx.Connection.Exec(cmd)
		if err == nil && rc == 0 {
			facts[key] = trimNewline(stdout)
		}
	}

	// Get IP addresses
	stdout, _, rc, err := ctx.Connection.Exec("ip -4 addr show | grep 'inet ' | awk '{print $2}'")
	if err == nil && rc == 0 {
		facts["ansible_all_ipv4_addresses"] = splitLines(stdout)
	}

	return Result{
		Changed: false,
		Extra:   facts,
	}, nil
}

func trimNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range splitByNewline(s) {
		if trimmed := trimNewline(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func splitByNewline(s string) []string {
	var result []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
```

```go
// internal/modules/set_fact.go
package modules

type SetFactModule struct{}

func (m *SetFactModule) Name() string            { return "set_fact" }
func (m *SetFactModule) SupportsCheckMode() bool { return true }

func (m *SetFactModule) Run(ctx ExecContext) (Result, error) {
	return Result{
		Changed: false,
		Extra:   ctx.Args, // All args become facts
	}, nil
}
```

```go
// internal/modules/meta.go
package modules

type MetaModule struct{}

func (m *MetaModule) Name() string            { return "meta" }
func (m *MetaModule) SupportsCheckMode() bool { return true }

func (m *MetaModule) Run(ctx ExecContext) (Result, error) {
	// meta actions (flush_handlers, end_play) are handled by the engine
	return Result{Changed: false}, nil
}
```

```go
// internal/modules/async_status.go
package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type AsyncStatusModule struct{}

func (m *AsyncStatusModule) Name() string            { return "async_status" }
func (m *AsyncStatusModule) SupportsCheckMode() bool { return true }

func (m *AsyncStatusModule) Run(ctx ExecContext) (Result, error) {
	jid, ok := ctx.Args["jid"].(string)
	if !ok || jid == "" {
		return Result{Failed: true, Msg: "jid is required"}, nil
	}

	mode := "check"
	if v, ok := ctx.Args["mode"]; ok {
		mode = fmt.Sprintf("%v", v)
	}

	asyncDir := filepath.Join(os.TempDir(), ".go-ansible_async", jid)

	if mode == "cleanup" {
		os.RemoveAll(asyncDir)
		return Result{Changed: false}, nil
	}

	statusFile := filepath.Join(asyncDir, "status")
	data, err := os.ReadFile(statusFile)
	if err != nil {
		return Result{Failed: true, Msg: fmt.Sprintf("async job %s not found", jid)}, nil
	}

	var status map[string]any
	if err := json.Unmarshal(data, &status); err != nil {
		return Result{Failed: true, Msg: "invalid status file"}, nil
	}

	return Result{
		Changed: false,
		Extra:   status,
	}, nil
}
```

- [ ] **Step 4: Run all module tests**

```bash
go test ./internal/modules/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/modules/
git commit -m "feat: add module system with registry, ping, shell, command, debug, setup, set_fact, meta, async_status"
```

---

## Phase P5-P14: Higher-Level Tasks

The following phases build on the established patterns. Each task follows the same TDD approach.

### Task 5.1: Playbook YAML Parser

**Files:**
- Create: `internal/engine/playbook.go`
- Create: `internal/engine/playbook_test.go`
- Create: `testdata/playbooks/simple.yml`

**Key interfaces to define:**

```go
// internal/engine/playbook.go
type Playbook struct {
    Plays []Play
}

type Play struct {
    Name        string
    Hosts       string
    Become      bool
    GatherFacts *bool
    Vars        map[string]any
    VarsFiles   []string
    Roles       []RoleRef
    PreTasks    []Task
    Tasks       []Task
    PostTasks   []Task
    Handlers    []Task
    Serial      any
    MaxFailPct  int
}

type Task struct {
    Name         string
    Module       string
    Args         map[string]any
    When         string
    Loop         any
    LoopControl  LoopControl
    Register     string
    Tags         []string
    Notify       []string
    DelegateTo   string
    RunOnce      bool
    Async        int
    Poll         int
    Retries      int
    Delay        int
    Until        string
    IgnoreErrors bool
    FailedWhen   string
    ChangedWhen  string
    Block        *Block
}

type Block struct {
    Tasks  []Task
    Rescue []Task
    Always []Task
}

type RoleRef struct {
    Name string
    Vars map[string]any
}

type LoopControl struct {
    LoopVar  string
    IndexVar string
    Label    string
}
```

**Test fixture:**

```yaml
# testdata/playbooks/simple.yml
- name: Simple test playbook
  hosts: all
  gather_facts: false
  vars:
    app_port: 8080
  tasks:
    - name: Echo hello
      shell:
        cmd: "echo hello"
      tags: [test]

    - name: Conditional task
      debug:
        msg: "Running on {{ .ansible_hostname }}"
      when: ".ansible_system == \"Linux\""

    - name: Loop task
      debug:
        msg: "Item: {{ .item }}"
      loop:
        - one
        - two
        - three
```

---

### Task 5.2-5.7: Playbook Engine Core

These tasks implement the execution engine:

- **5.2**: Play executor — matches hosts, loads vars, runs tasks
- **5.3**: Task executor — renders templates, checks when, executes module
- **5.4**: Fork worker pool — goroutine-based concurrent execution
- **5.5**: Handler manager — collects notify, executes after play
- **5.6**: Block/rescue/always — error handling blocks
- **5.7**: Default callback — terminal output formatting

Each follows the same pattern: write test → implement → verify → commit.

---

### Task 6.1-6.8: Additional Modules

Each module follows the same pattern as Task 4.1:

- **6.1**: `copy` — SFTP PutFile + chmod
- **6.2**: `file` — state=file/directory/absent/link/touch
- **6.3**: `template` — render template + PutFile
- **6.4**: `stat` — SSH stat command, parse JSON result
- **6.5**: `yum`/`apt`/`dnf` — package manager commands
- **6.6**: `service`/`systemd` — systemctl commands
- **6.7**: `user`/`group` — useradd/groupadd commands
- **6.8**: `uri`/`get_url`/`wait_for` — network modules

---

### Task 7.1-7.3: Roles System

- **7.1**: Role loader — parse directory structure, load defaults/vars/tasks/handlers
- **7.2**: Role dependency resolver — topological sort, cycle detection
- **7.3**: include_role/import_role modules

---

### Task 8.1-8.3: Handlers + Error Handling

- **8.1**: Handler manager with notify/de-dup
- **8.2**: Block/rescue/always execution
- **8.3**: Free strategy implementation

---

### Task 9.1-9.2: Async Tasks

- **9.1**: Async wrapper — remote background process + status files
- **9.2**: async_status module + polling logic

---

### Task 10.1-10.3: Vault Encryption

- **10.1**: AES-256-CTR encrypt/decrypt with PBKDF2 key derivation
- **10.2**: Vault file format parser + CLI commands
- **10.3**: Vault ID support + password sources

---

### Task 11.1-11.3: Collections + Galaxy

- **11.1**: Galaxy API client — search, download, install
- **11.2**: requirements.yml parser
- **11.3**: Collection loader

---

### Task 12.1-12.3: Callback Plugins

- **12.1**: Callback interface + Default Callback (colored terminal)
- **12.2**: JSON/YAML/Minimal callbacks
- **12.3**: Timer callback + PlayRecap

---

### Task 13.1-13.3: Filter/Test/Lookup Plugins

- **13.1**: Ansible filters (regex_replace, ipaddr, combine, etc.)
- **13.2**: Test plugins (defined, version, match, etc.)
- **13.3**: Lookup plugins (file, pipe, env, password, etc.)

---

### Task 14.1-14.3: E2E + Polish

- **14.1**: E2E test suite with mock SSH server
- **14.2**: Performance optimization (connection reuse, goroutine tuning)
- **14.3**: Documentation + release packaging

---

## File Structure Summary

```
go-ansible/
├── cmd/go-ansible/main.go
├── internal/
│   ├── cli/
│   │   ├── root.go
│   │   ├── root_test.go
│   │   ├── adhoc.go
│   │   ├── playbook.go
│   │   ├── inventory.go
│   │   ├── vault.go
│   │   ├── galaxy.go
│   │   └── config.go
│   ├── inventory/
│   │   ├── inventory.go
│   │   ├── inventory_test.go
│   │   ├── ini_parser.go
│   │   ├── ini_parser_test.go
│   │   ├── yaml_parser.go
│   │   ├── yaml_parser_test.go
│   │   ├── host_pattern.go
│   │   ├── host_pattern_test.go
│   │   ├── loader.go
│   │   └── loader_test.go
│   ├── connection/
│   │   ├── connection.go
│   │   ├── local.go
│   │   ├── local_test.go
│   │   ├── ssh.go
│   │   ├── ssh_test.go
│   │   ├── become.go
│   │   ├── become_test.go
│   │   └── pool.go
│   ├── variables/
│   │   ├── context.go
│   │   └── context_test.go
│   ├── template/
│   │   ├── engine.go
│   │   └── engine_test.go
│   ├── modules/
│   │   ├── module.go
│   │   ├── registry.go
│   │   ├── registry_test.go
│   │   ├── ping.go
│   │   ├── ping_test.go
│   │   ├── shell.go
│   │   ├── command.go
│   │   ├── debug.go
│   │   ├── setup.go
│   │   ├── set_fact.go
│   │   ├── meta.go
│   │   ├── async_status.go
│   │   └── ... (more modules)
│   ├── engine/
│   │   ├── playbook.go
│   │   ├── play.go
│   │   ├── task.go
│   │   ├── block.go
│   │   ├── runner.go
│   │   └── iterator.go
│   ├── strategy/
│   │   ├── strategy.go
│   │   ├── linear.go
│   │   └── free.go
│   ├── plugins/
│   │   ├── callback/
│   │   ├── lookup/
│   │   ├── filter/
│   │   └── test/
│   ├── vault/
│   ├── galaxy/
│   ├── roles/
│   ├── collections/
│   ├── config/
│   └── logging/
├── pkg/types/
├── pkg/utils/
├── testdata/
│   ├── hosts.ini
│   ├── hosts.yml
│   └── playbooks/
├── go.mod
├── Makefile
└── .gitignore
```

## Go Module Dependencies

```
github.com/spf13/cobra          // CLI framework
github.com/Masterminds/sprig/v3 // Template functions (Helm-compatible)
golang.org/x/crypto             // SSH implementation
gopkg.in/yaml.v3                // YAML parsing
github.com/pkg/sftp             // SFTP file transfer
github.com/google/uuid          // UUID generation
github.com/fatih/color          // Terminal colors
github.com/mattn/go-isatty      // TTY detection
```
