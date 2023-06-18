package command

import (
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
			cfg := config.DefaultConfig()
			cmd.Print(config.Marshal(cfg))
		},
	}
	return cmd
}
