package cli

import (
	"flag"
	"fmt"
	"os"

	"ccam/internal/accounts"
	"ccam/internal/config"
)

// cmdPrune reclaims the disk the de-isolation migration left behind: once an
// account's transcripts were copied into the shared ~/.claude, its own
// directory still holds the originals. This deletes those, keeping only the
// credential and identity files. It previews by default and only deletes with
// --yes, because it removes the last per-account copy of already-shared data.
func cmdPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "actually delete (without this, prune only previews)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ids := fs.Args()

	accountsFile, err := config.AccountsFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	manager := accounts.NewManager(accounts.NewStore(accountsFile), accountsDir)

	report, err := manager.Prune(ids, !*yes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	if len(report.Accounts) == 0 {
		fmt.Println("Nothing to prune: no migrated (credentials-only) accounts with reclaimable data.")
		return 0
	}

	for _, a := range report.Accounts {
		if a.Err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", a.ID, a.Err)
			continue
		}
		fmt.Printf("  %s: %s across %d item(s)\n", a.ID, humanBytes(a.Bytes), len(a.Removed))
	}
	fmt.Printf("Total: %s\n", humanBytes(report.TotalBytes()))

	if report.DryRun {
		fmt.Println("\nThis was a preview. Re-run with --yes to delete (the transcripts already live in ~/.claude).")
	} else {
		fmt.Println("\nDone. Credentials and account identity were kept; everything else came from your shared ~/.claude.")
	}
	return 0
}

// humanBytes formats a byte count for the prune summary.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
