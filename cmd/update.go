package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/update"
	"github.com/spf13/cobra"
)

var (
	updateCheck bool
	updateForce bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update claudectl to the latest release",
	RunE: func(cmd *cobra.Command, args []string) error {
		latest, err := update.Latest()
		if err != nil {
			return err
		}
		latestV := strings.TrimPrefix(latest, "v")

		if appVersion != "dev" && !update.Newer(latestV, appVersion) && !updateForce {
			fmt.Printf("claudectl %s is up to date\n", appVersion)
			return nil
		}
		if updateCheck {
			fmt.Printf("Update available: %s → %s (run 'claudectl update')\n", appVersion, latestV)
			return nil
		}
		if appVersion == "dev" && !updateForce {
			return fmt.Errorf("this is a development build; use --force to replace it with %s", latestV)
		}

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate current binary: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}

		fmt.Printf("Updating %s → %s (%s)\n", appVersion, latestV, exe)
		if err := update.Install(latest, exe); err != nil {
			if errors.Is(err, update.ErrPermission) {
				return fmt.Errorf("no write access to %s; run: sudo claudectl update", filepath.Dir(exe))
			}
			return err
		}
		fmt.Printf("✓ claudectl %s installed\n", latestV)
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "only check whether an update is available")
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "reinstall even if up to date or a dev build")
	rootCmd.AddCommand(updateCmd)
}
