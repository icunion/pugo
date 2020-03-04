package cmd

import (
	"fmt"
	"sync"

	"github.com/icunion/pugo/cdb"
	"github.com/icunion/pugo/email"
	"github.com/icunion/pugo/newerpol"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// syncCmd represents the sync command
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync pending access requests and revocations from eActivities",
	Long: `Process pending access requests and revocations from
eActivities. The requests will be committed into the configuration database,
and if this succeeds (and the push to the remote succeeds), eActivities will
be updated and the users in question notified.`,
	Run: func(cmd *cobra.Command, args []string) {
		doSync(cmd)
	},
}

type syncOptions struct {
	dryRun            bool
	forceUpdateTree   bool
	noPush            bool
	noEmail           bool
	recipientOverride string
}

var syncOpts syncOptions

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().BoolVar(&syncOpts.dryRun, "dry-run", false, "Perform dry run: don't commit to cdb, update Newerpol, or send emails.")
	syncCmd.Flags().BoolVar(&syncOpts.forceUpdateTree, "force-update-tree", false, "Force the cdb tree to be updated when performing a dry run (e.g. to inspect changes in repo before manually committing).")
	syncCmd.Flags().BoolVar(&syncOpts.noPush, "no-push", false, "Don't push to origin after committing. Implied by dry-run.")
	syncCmd.Flags().BoolVar(&syncOpts.noEmail, "no-email", false, "Don't send emails. Implied by dry-run.")
	syncCmd.Flags().StringVar(&syncOpts.recipientOverride, "recipient-override-email", "", "If set, sends all generated emails to the specified address instead of the real recipients.")
	syncCmd.Flags().String("branch", "master", "Commit to the named branch instead of the default or config specified branch.")
	viper.BindPFlag("cdb.branch", syncCmd.Flags().Lookup("branch"))
}

func doSync(cmd *cobra.Command) error {
	log.Info("sync: Starting sync ...")

	newerpolDb, err := newerpol.Connect()
	if err != nil {
		log.Fatal(fmt.Errorf("sync: ", err))
	}
	defer newerpolDb.Close()

	grants := make(map[string]map[int][]newerpol.AccessRecord)
	// Get grants to add grouped by site id
	grants["add"], err = newerpol.GetGrantsToAdd(newerpolDb)
	if err != nil {
		log.Fatal(fmt.Errorf("sync: ", err))
	}
	log.WithFields(log.Fields{
		"grantsToAdd": grants["add"],
	}).Debug("sync: Got grants to add")

	// Get grants to revoke grouped by site id
	grants["revoke"], err = newerpol.GetGrantsToRevoke(newerpolDb)
	if err != nil {
		log.Fatal(fmt.Errorf("sync: ", err))
	}
	log.WithFields(log.Fields{
		"grantsToRevoke": grants["revoke"],
	}).Debug("sync: Got grants to revoke")

	// Determine total number of grants pending
	var totalGrants int
	for _, verb := range []string{"add", "revoke"} {
		for _, grantRecords := range grants[verb] {
			totalGrants += len(grantRecords)
		}
	}

	// Process grants
	var wg sync.WaitGroup
	siteIdsChanged := make(chan int, totalGrants)
	grantsProcessed := make(chan newerpol.AccessRecord, totalGrants)
	for _, verb := range []string{"add", "revoke"} {
		log.Infof("sync: Processing grants to %s for %d sites", verb, len(grants[verb]))
		for id, grantRecords := range grants[verb] {
			site, err := cdb.GetSiteById(id)
			if err != nil {
				log.Fatalf("sync: %v", err)
			}
			if site == nil {
				log.Warnf("sync: Unable to %s grants for site %d - site not found in cdb. Skipping", verb, id)
				continue
			}

			wg.Add(1)
			go func(verb string, site *cdb.Site, grantRecords []newerpol.AccessRecord) {
				log.WithFields(log.Fields{
					"id":           site.Id,
					"name":         site.Name(),
					"grantRecords": grantRecords,
				}).Debug("sync: Processing grants for site")

				for _, accessRecord := range grantRecords {
					log.WithFields(log.Fields{
						"accessRecord": accessRecord,
					}).Debug("sync: Processing access record")
					switch verb {
					case "add":
						log.Infof("sync: Adding %s to %s", accessRecord.Login, site.Name())
						site.AddAdmin(accessRecord.Login)
					case "revoke":
						log.Infof("sync: Revoking %s from %s", accessRecord.Login, site.Name())
						site.RemoveAdmin(accessRecord.Login)
					}
					if site.Changed() {
						log.Debugf("sync: %s changed", site.Name())
						siteIdsChanged <- site.Id
					}
					grantsProcessed <- accessRecord
				}
				wg.Done()
			}(verb, site, grantRecords)
		}
	}
	go func() {
		wg.Wait()
		close(grantsProcessed)
		close(siteIdsChanged)
	}()

	siteIdsToCommit := make(map[int]bool)
	for id := range siteIdsChanged {
		siteIdsToCommit[id] = true
	}

	// Commit changes to repo
	commitOpts := &cdb.CommitSitesOptions{
		Ids:             siteIdsToCommit,
		Message:         "Update admins",
		Cmd:             "sync",
		DryRun:          syncOpts.dryRun,
		ForceUpdateTree: syncOpts.forceUpdateTree,
		NoPush:          syncOpts.noPush,
	}
	log.WithFields(log.Fields{
		"Ids":             siteIdsToCommit,
		"Message":         "Update admins",
		"Cmd":             "sync",
		"DryRun":          syncOpts.dryRun,
		"ForceUpdateTree": syncOpts.forceUpdateTree,
		"NoPush":          syncOpts.noPush,
	}).Debugf("sync: Committing sites")
	if err = cdb.CommitSites(commitOpts); err != nil {
		log.Fatalf("sync: %v", err)
	}

	// Update eActivities and email user when access granted
	sendEmails := !syncOpts.dryRun && !syncOpts.noEmail
	if sendEmails {
		if syncOpts.recipientOverride != "" {
			log.Infof("sync: Email override in effect - all emails will be sent to %s", syncOpts.recipientOverride)
		}
		if err := email.StartWorker(); err != nil {
			log.Warn("sync: %v", err)
			log.Warn("sync: Unable to start email worker, emails will not be sent")
			sendEmails = false
		}
	} else {
		log.Info("sync: Performing dry run or --no-email in effect - emails will not be sent.")
	}

	for accessRecord := range grantsProcessed {
		log.WithFields(log.Fields{
			"accessRecord": accessRecord,
		}).Debug("sync: Finishing grant")

		if syncOpts.dryRun {
			log.WithFields(log.Fields{
				"accessRecord": accessRecord,
			}).Debug("sync: Dry run, skipping newerpol.FinishGrant")
			continue
		}

		updated, err := newerpol.FinishGrant(accessRecord, newerpolDb)
		if err != nil {
			log.Fatalf("sync: %v", err)
		}

		if updated && sendEmails {
			// Perpare options ...
			site, err := cdb.GetSiteById(accessRecord.WebsiteId)
			if err != nil || site == nil {
				log.WithFields(log.Fields{
					"accessRecord": accessRecord,
				}).Warn("sync: Unable to load site %d - skipping email", accessRecord.WebsiteId)
				continue
			}

			emailOpts := &email.EmailOptions{
				FirstName: accessRecord.FirstName,
				EmailName: accessRecord.LookupName,
				Email:     accessRecord.Email,
				CSP:       accessRecord.CSP,
				Folder:    site.Name(),
			}

			if emailOpts.Email == "" {
				log.WithFields(log.Fields{
					"emailOpts": emailOpts,
				}).Warn("sync: No email address - skipping email")
				continue
			}

			switch accessRecord.RequestStatus {
			case newerpol.AccessGrantPending:
				emailOpts.Subject = "Website Access Granted"
				emailOpts.Type = "granted"
			case newerpol.AccessRevokePending:
				emailOpts.Subject = "Website Access Removed"
				emailOpts.Type = "revoked"
			}

			if syncOpts.recipientOverride != "" {
				emailOpts.Email = syncOpts.recipientOverride
			}

			// Now actually send the actual email for actual
			if err := email.SendEmail(emailOpts); err != nil {
				log.WithFields(log.Fields{
					"emailOpts": emailOpts,
				}).Warn("sync: Error attempting to send email: %v", err)
				continue
			}
		}
	}

	if sendEmails {
		email.ShutdownWorker()
	}

	return nil
}