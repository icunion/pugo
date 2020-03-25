package cmd

import (
	"fmt"
	"time"

	"github.com/icunion/pugo/cdb"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var expiryCmd = &cobra.Command{
	Use:   "expiry [yyyy-mm-dd]",
	Short: "Reset user expiry date",
	Long:  `Reset user expiry date on all sites to the specified date`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("Requires a single date argument in the form yyyy-mm-dd")
		}
		_, err := time.Parse("2006-01-02", args[0])
		if err != nil {
			return fmt.Errorf("Invalid date specified: %s", args[0])
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		date, _ := time.Parse("2006-01-02", args[0])
		resetExpiry(cmd, date)
	},
}

func init() {
	resetCmd.AddCommand(expiryCmd)
}

func resetExpiry(cmd *cobra.Command, date time.Time) error {
	log.Infof("reset-expiry: Starting reset of expiry date to '%s' ...", date.Format("2006-01-02"))

	if date.Before(time.Now()) {
		log.Warn("reset-expiry: new expiry date is in the past. This probably isn't a good idea.")
	}
	if !(date.Month() == 7 && date.Day() == 31) {
		log.Warn("reset-expiry: new expiry date does not co-incide with year end (31 July)")
	}

	siteIdsToCommit := make(map[int]bool)

	// Update sites
	sites, err := cdb.GetAllSites()
	if err != nil {
		log.Fatalf("reset-expiry: Getting all sites: %v", err)
	}

	for _, site := range sites {
		site.Expiry = date.Format("2006-01-02")
		site.MarkAsChanged()
		siteIdsToCommit[site.Id] = true
	}

	// Commit changes to repo
	commitOpts := &cdb.CommitSitesOptions{
		Ids:             siteIdsToCommit,
		Message:         fmt.Sprintf("Reset expiry date to %s", date.Format("2006-01-02")),
		Cmd:             "reset expiry",
		DryRun:          globalOpts.dryRun,
		ForceUpdateTree: globalOpts.forceUpdateTree,
		NoPush:          globalOpts.noPush,
	}

	log.WithFields(log.Fields{
		"Ids":             siteIdsToCommit,
		"Message":         commitOpts.Message,
		"Cmd":             "reset admins",
		"DryRun":          globalOpts.dryRun,
		"ForceUpdateTree": globalOpts.forceUpdateTree,
		"NoPush":          globalOpts.noPush,
	}).Debugf("reset-expiry: Committing sites")
	if err := cdb.CommitSites(commitOpts); err != nil {
		log.Fatalf("reset-expiry: %v", err)
	}

	return nil
}
