package command

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/servak/mping/internal/config"
	"github.com/spf13/cobra"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "management config",
	}
	cmd.AddCommand(
		NewPrintConfigCmd(),
		NewInitConfigCmd(),
	)
	return cmd
}

func NewPrintConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "print",
		Short: "Loads the configuration file, combines it with the default settings, and outputs the actual resulting configuration.",
		Long:  `This command reads the given configuration file and merges it with the system's default settings. The final output displays the combined result which will be the actual configuration that gets applied.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			path, err := flags.GetString("config")
			cfg, _ := config.LoadFile(path)
			if err != nil {
				return err
			}
			cmd.Print(config.Marshal(cfg))
			return nil
		},
	}
	flags := cmd.Flags()
	flags.StringP("config", "c", "~/.mping.yml", "config path")
	return cmd
}

func NewInitConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generates initial settings.",
		Long:  `This command generates the initial settings for the system or application.`,
		Run: func(cmd *cobra.Command, args []string) {
			flags := cmd.Flags()
			path, _ := flags.GetString("output")
			if path == "" {
				cmd.PrintErr("Error: output path is required\n")
				return
			}
			if strings.HasPrefix(path, "~") {
				usr, err := user.Current()
				if err == nil {
					path = strings.Replace(path, "~", usr.HomeDir, 1)
				}
			}
			path, _ = filepath.Abs(path)
			if _, err := os.Stat(path); err == nil {
				cmd.PrintErrf("Error: output file %s already exists\n", path)
				return
			} else if !os.IsNotExist(err) {
				cmd.PrintErrf("Error checking output file %s: %v\n", path, err)
				return
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				cmd.PrintErrf("Error creating directory for output file %s: %v\n", path, err)
				return
			}
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				cmd.PrintErrf("Error opening output file %s: %v\n", path, err)
				return
			}
			defer f.Close()
			if _, err := fmt.Fprint(f, config.Marshal(config.DefaultConfig())); err != nil {
				cmd.PrintErrf("Error writing config to %s: %v\n", path, err)
				return
			}
			cmd.Printf("Configuration initialized and written to %s\n", path)
		},
	}
	flags := cmd.Flags()
	flags.StringP("output", "o", "~/.mping.yml", "output path")
	return cmd
}
