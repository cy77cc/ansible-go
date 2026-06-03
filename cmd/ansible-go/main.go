package main

import (
	"fmt"
	"os"

	"github.com/cy77cc/ansible-go/internal/version"
	"github.com/spf13/cobra"
)

// 命令层接口
type Command interface {
	Execute(args []string) error
}

// Ad-hoc 命令
type AdhocCommand struct {
	HostPattern string
	ModuleName  string
	ModuleArgs  string
	Options     GlobalOptions
}

// Playbook 命令
type PlaybookCommand struct {
	PlaybookFiles []string
	Options       GlobalOptions
}

// CLI 层核心结构
type RootCommand struct {
	GlobalOptions GlobalOptions
	SubCommands   map[string]Command
}

type GlobalOptions struct {
	Inventory      string
	User           string
	PrivateKeyFile string
	Become         bool
	BecomeMethod   string
	BecomeUser     string
	Forks          int
	Verbosity      int
	Timeout        int
	Diff           bool
	Check          bool
	Limit          string
	Tags           string
	SkipTags       string
	ExtraVars      []string
}

var rootCmd = &cobra.Command{
	Use:   "ansible-go",
	Short: "Ansible-go is a Go implementation of Ansible, a powerful automation tool for IT tasks.",
	Long: `Ansible-go is a Go implementation of Ansible, a powerful automation tool for IT tasks.
It allows you to automate configuration management, application deployment, and task automation across your infrastructure.`,
	Version: version.VERSION,
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func main() {
	rootCmd.Execute()
}
