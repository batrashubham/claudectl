package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/batrashubham/claudectl/internal/app"
	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	syncWatch    bool
	syncInterval time.Duration
	syncWait     bool
	syncQuiet    bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Back up sessions from every enabled agent",
	Long: `Copy session data from every enabled agent into this machine's subtree of
the backup, commit it, and push if git_push is enabled. Pushing first rebases
onto whatever other machines have pushed, so several machines can share one
remote.

Use --watch to run continuously (e.g., in a tmux pane). Default interval is
5 minutes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if syncWatch {
			return runSyncWatch()
		}
		return runSyncOnce()
	},
}

func init() {
	syncCmd.Flags().BoolVarP(&syncWatch, "watch", "w", false, "Run continuously at an interval")
	syncCmd.Flags().DurationVarP(&syncInterval, "interval", "i", 5*time.Minute, "Sync interval (with --watch)")
	syncCmd.Flags().BoolVar(&syncWait, "wait", false, "Wait for a running sync to finish instead of failing")
	syncCmd.Flags().BoolVarP(&syncQuiet, "quiet", "q", false, "Only print warnings and errors")
	rootCmd.AddCommand(syncCmd)
}

func lockWait() time.Duration {
	if syncWait {
		return 2 * time.Minute
	}
	return 0
}

func enabledNames() string {
	var names []string
	for _, h := range env.Enabled {
		names = append(names, h.DisplayName())
	}
	if len(names) == 0 {
		return "(no agents found)"
	}
	return strings.Join(names, ", ")
}

func runSyncOnce() error {
	if !syncQuiet {
		fmt.Printf("Syncing %s → %s (machine: %s)\n", enabledNames(), cfg.BackupDir, env.Machine)
	}

	out, err := env.Sync(lockWait())
	if err != nil {
		return err
	}
	if !syncQuiet {
		printSyncResult(out)
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}
	return nil
}

func printSyncResult(out *app.SyncOutcome) {
	r := out.Result
	fmt.Printf("Done: %d new, %d updated (%s)", r.NewFiles, r.UpdatedFiles, humanize.Bytes(uint64(r.TotalBytes)))
	if len(r.PerHarness) > 0 {
		var parts []string
		for name, n := range r.PerHarness {
			parts = append(parts, fmt.Sprintf("%s %d", harness.DisplayName(name), n))
		}
		sort.Strings(parts)
		fmt.Printf(" — %s", strings.Join(parts, ", "))
	}
	fmt.Println()
	if r.Migrated {
		fmt.Printf("Moved existing backup into machines/%s/claude/ (multi-machine layout).\n", env.Machine)
	}
	if out.Pushed {
		fmt.Println("Pushed to remote.")
	}
}

func runSyncWatch() error {
	fmt.Printf("Watching every %s: %s → %s (machine: %s)\n", syncInterval, enabledNames(), cfg.BackupDir, env.Machine)
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		stamp := time.Now().Format("15:04:05")
		out, err := env.Sync(lockWait())
		switch {
		case err != nil:
			fmt.Printf("[%s] error: %v\n", stamp, err)
		case out.Result.Changed():
			fmt.Printf("[%s] synced: %d new, %d updated (%s)\n", stamp,
				out.Result.NewFiles, out.Result.UpdatedFiles, humanize.Bytes(uint64(out.Result.TotalBytes)))
		}
		if out != nil {
			for _, w := range out.Warnings {
				fmt.Printf("[%s] warning: %s\n", stamp, w)
			}
		}

		select {
		case <-stop:
			fmt.Println("Stopped.")
			return nil
		case <-ticker.C:
		}
	}
}
