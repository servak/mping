package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/servak/mping/internal/command"
)

var (
	Version   string
	Revision  string
	GoVersion = runtime.Version()
)

func Execute() {
	cmd := command.NewPingCmd()
	cmd.Version = Version
	cmd.Flags().BoolP("version", "v", false, "Display version")
	cmd.SetVersionTemplate(fmt.Sprintf("mping, version: {{ .Version }} (revision: %s, goversion: %s)", Revision, GoVersion))
	cmd.AddCommand(
		command.NewPingBatchCmd(),
		command.NewConfigCmd(),
	)
	cmd.CompletionOptions.HiddenDefaultCmd = true
	cmd.SetOutput(os.Stdout)

	if err := cmd.Execute(); err != nil {
		cmd.SetOutput(os.Stderr)
		cmd.Println(err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
