package cmd

import (
	"fmt"

	"github.com/batrashubham/claudectl/internal/update"
	"github.com/spf13/cobra"
)

var appVersion = "dev"

func SetVersion(v string) {
	appVersion = update.Resolve(v)
	rootCmd.Version = appVersion
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the claudectl version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("claudectl %s\n", appVersion)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
