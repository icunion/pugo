package cmd

import (
	"fmt"

	"github.com/icunion/pugo/cdb"
	"github.com/icunion/pugo/newerpol"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var adminsCmd = &cobra.Command{
	Use:   "admins",
	Short: "Clear site admins.",
	Long: `Reset site admins back to none. By default only acts on sites
where access is managed through eActivities.`,
	Run: func(cmd *cobra.Command, args []string) {
		resetAdmins(cmd)
	},
}

var allSites bool

func init() {
	resetCmd.AddCommand(adminsCmd)

	adminsCmd.Flags().BoolVar(&allSites, "all", false, "Reset admins for all sites in cdb, not just the sites where access is managed through eActivities")
}

func resetAdmins(cmd *cobra.Command) error {
	log.Info("reset-admins: Starting reset ...")

	siteIdsToCommit := make(map[int]bool)

	// Update sites
	if allSites {
		sites, err := cdb.GetAllSites()
		if err != nil {
			log.Fatalf("reset-admins: Getting all sites: %v", err)
		}

		for _, site := range sites {
			site.Admins = []string{}
			site.MarkAsChanged()
			siteIdsToCommit[site.Id] = true
		}
	} else {
		newerpolDb, err := newerpol.Connect()
		if err != nil {
			log.Fatal(fmt.Errorf("reset-admins: ", err))
		}
		defer newerpolDb.Close()

		managedSiteIds, err := newerpol.GetManagedSiteIds(newerpolDb)
		if err != nil {
			log.Fatalf("reset-admins: Getting managed site ids: %v", err)
		}

		for _, id := range managedSiteIds {
			site, err := cdb.GetSiteById(id)
			if err != nil {
				log.Fatalf("reset-admins: %v", err)
			}
			if site == nil {
				log.Warnf("reset-admins: Unable to reset admins for site %d - site not found in cdb. Skipping", id)
			}

			site.Admins = []string{}
			site.MarkAsChanged()
			siteIdsToCommit[site.Id] = true
		}
	}

	// Commit changes to repo
	commitOpts := &cdb.CommitSitesOptions{
		Ids:             siteIdsToCommit,
		Message:         "Reset admins (eActivities managed sites only)",
		Cmd:             "reset admins",
		DryRun:          globalOpts.dryRun,
		ForceUpdateTree: globalOpts.forceUpdateTree,
		NoPush:          globalOpts.noPush,
	}
	if allSites {
		commitOpts.Message = "Reset admins (all sites)"
	}

	log.WithFields(log.Fields{
		"Ids":             siteIdsToCommit,
		"Message":         commitOpts.Message,
		"Cmd":             "reset admins",
		"DryRun":          globalOpts.dryRun,
		"ForceUpdateTree": globalOpts.forceUpdateTree,
		"NoPush":          globalOpts.noPush,
	}).Debugf("reset-admins: Committing sites")
	if err := cdb.CommitSites(commitOpts); err != nil {
		log.Fatalf("reset-admins: %v", err)
	}

	return nil
}
