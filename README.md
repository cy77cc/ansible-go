# ansible-go

A Go reimplementation of Ansible — a single static binary for Linux server automation via SSH.

## Why ansible-go?

| | Ansible (Python) | ansible-go (Go) |
|---|---|---|
| **Runtime** | Python 3.8+ + pip dependencies | Single static binary (~15MB) |
| **Concurrency** | fork-based, GIL-limited | goroutine worker pool, true parallelism |
| **Template** | Jinja2 | Go `text/template` + Sprig |
| **Modules** | Python modules (SSH + transfer + execute) | Remote SSH command execution |
| **Platform** | Cross-platform | Linux only (control + target) |

## Architecture

ansible-go uses a 5-layer architecture:

```
CLI Layer          Cobra command parsing, global flags
    ↓
Command Layer      Orchestrates engine components per subcommand
    ↓
Engine Layer       Inventory, Playbook, Variables, Template, Facts, Handlers
    ↓
Module Layer       ping, shell, command, copy, file, template, yum, apt, service, user ...
    ↓
Connection Layer   SSH (x/crypto/ssh), Local, become (privilege escalation)
```

## Project Status

**Early stage** — design and documentation are complete, implementation is in progress.

| Phase | Description | Status |
|-------|-------------|--------|
| P0.1 | Project skeleton (go.mod, Makefile) | Done |
| P0.2 | Root command + CLI structure | Done |
| P0.3 | Placeholder subcommands | Next |
| P1-P14 | Core implementation | Planned |

See [implementation plan](docs/superpowers/plans/2026-05-25-ansible-go-implementation.md) for full details.

## Quick Start

### Prerequisites

- Go 1.26+

### Build

```bash
make build
```

The binary is output to `bin/ansible-go`.

### Install

```bash
make install
```

### Planned Usage

```bash
# Ad-hoc commands
ansible-go all -m ping -i inventory/hosts
ansible-go webservers -m shell -a "uptime" -i inventory/hosts
ansible-go db -m service -a "name=mysql state=restarted" --become

# Playbook execution
ansible-go playbook site.yml -i inventory/production
ansible-go playbook deploy.yml --limit webservers --tags deploy
ansible-go playbook site.yml --check --diff

# Inventory
ansible-go inventory list -i inventory/hosts
ansible-go inventory host web1 -i inventory/hosts

# Vault
ansible-go vault encrypt secrets.yml
ansible-go vault decrypt secrets.yml

# Galaxy
ansible-go galaxy install username.rolename

# Configuration
ansible-go config dump
```

## Development

### Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Compile binary to `bin/` |
| `make install` | Install to `$GOPATH/bin` |
| `make test` | Run all tests |
| `make test-coverage` | Generate coverage report |
| `make test-race` | Run tests with race detector |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code with gofmt |
| `make vet` | Run go vet |
| `make check` | Full quality pipeline (fmt + vet + lint + test) |
| `make clean` | Remove build artifacts |

### Build Metadata

Version, build time, and commit hash are injected via ldflags:

```bash
ansible-go version
# ansible-go v0.1.0 (abc1234) built 2026-05-25T12:00:00Z
```

## Dependencies

**Current:**
- [cobra](https://github.com/spf13/cobra) — CLI framework

**Planned:**
- `golang.org/x/crypto` — SSH implementation
- `gopkg.in/yaml.v3` — YAML parsing
- `github.com/Masterminds/sprig/v3` — Template functions
- `github.com/pkg/sftp` — SFTP file transfer
- `github.com/fatih/color` — Terminal color output

## Documentation

Detailed design and implementation docs are in [`docs/`](docs/):

- [Project Overview](docs/guides/00-overview.md)
- [Architecture](docs/guides/01-architecture.md)
- [Design Specification](docs/superpowers/specs/2026-05-25-ansible-go-design.md)
- [Implementation Plan](docs/superpowers/plans/2026-05-25-ansible-go-implementation.md)
- [MVP Milestones](docs/guides/16-minimal-viable-path.md)

Guide series (02-16) covers each subsystem: inventory, connections, variables, templates, modules, playbook engine, roles, handlers, async, vault, collections, callbacks, and filters.

## Compatibility

ansible-go aims for Ansible-compatible YAML playbook syntax with these differences:

| Feature | Ansible | ansible-go |
|---------|---------|------------|
| Template engine | Jinja2 | Go text/template + Sprig |
| Variable syntax | `{{ var }}` | `{{ .var }}` (auto-preprocessed) |
| Modules | Python | Go (remote SSH execution) |
| Plugin system | Python classes | Go interfaces |

## License

[Apache License 2.0](LICENSE)
