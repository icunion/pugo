package cdb

import (
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

type CommitSitesOptions struct {
	// A site of site Ids for which to commit changes.  If not supplied,
	// then all sites with changes will be committeed
	Ids map[int]bool
	// A short message snippet which will be embedded in the commit message
	// (e.g. "Update admins")
	Message string
	// The name of the command that is being run (e.g. "sync")
	Cmd string
	// If set perform dry run only
	DryRun bool
	// If set force the tree to be updated when dry run is also set
	ForceUpdateTree bool
	// If set commit but don't push to origin
	NoPush bool
}

type sitesCacheStruct struct {
	byId      map[int]*Site
	byName    map[string]*Site
	initOnce  sync.Once
	initError error
	slice     []*Site
}

var sitesCache sitesCacheStruct

func init() {
	viper.SetDefault("cdb.branch", "master")
	viper.SetDefault("cdb.author.name", "pugo")
	viper.SetDefault("cdb.author.email", "pugo@example.com")
}

func CommitSites(opts *CommitSitesOptions) error {
	if err := ensureSitesCacheLoaded(); err != nil {
		return err
	}

	// Ensure correct branch is checked out, clean, and any upstream
	// changes merged
	wt, err := GetWorktree()
	if err != nil {
		return err
	}

	if opts.DryRun {
		log.Warn("cdb: Performing dry run - changes will not be committed to repo.")
		if opts.ForceUpdateTree {
			log.Warn("cdb: ForceUpdateTree in effect - working tree will be updated but not committed.")
		}
	} else {
		if opts.NoPush {
			log.Warn("cdb: NoPush enabled - changes will be committed but not pushed to origin.")
		}
	}

	// Determine sites to process
	siteIds := opts.Ids
	if siteIds == nil {
		siteIds = make(map[int]bool)
		for id, _ := range sitesCache.byId {
			siteIds[id] = true
		}
	}

	// Output sites to work tree
	errors := make(chan error, len(sitesCache.byId))
	filesToStage := make(chan string, len(sitesCache.byId))
	var wg sync.WaitGroup

	sitesChanged := 0
	for id, inSet := range siteIds {
		if !inSet {
			continue
		}
		site := sitesCache.byId[id]
		if site == nil {
			log.Debugf("cdb: Site Id %d not found, skipping", id)
			continue
		}
		if !site.Changed() {
			log.Debugf("cdb: %s unchanged, skipping save", site.Name())
			continue
		}
		sitesChanged++
		wg.Add(1)
		go func(site *Site) {
			var err error
			defer wg.Done()
			if !opts.DryRun || opts.ForceUpdateTree {
				log.Debugf("cdb: Saving %s", site.Name())
				err = site.Save()
				if err == nil {
					filesToStage <- site.FileNameRepo()
				}
			} else {
				log.Debugf("cdb: Dry run, skipping save of %s", site.Name())
				err = nil
			}
			errors <- err
		}(site)
	}

	go func() {
		wg.Wait()
		close(errors)
		close(filesToStage)
	}()

	for err := range errors {
		if err != nil {
			return err
		}
	}

	if !opts.DryRun || opts.ForceUpdateTree {
		log.Infof("cdb: %d changed sites saved to working tree", sitesChanged)
	} else {
		log.Infof("cdb: Dry run, %d changed sites not saved to working tree", sitesChanged)
	}

	// Stage files
	stagedFiles := 0
	if !opts.DryRun {
		log.Debug("cdb: Staging files")
		for fn := range filesToStage {
			log.Debugf("cdb: Staging %s", fn)
			if _, err := wt.Add(fn); err != nil {
				return fmt.Errorf("cdb: Staging %s: %v", fn, err)
			}
			stagedFiles++
		}
	}

	// If working tree is clean after staging files don't bother to commit
	if err := checkWorktreeClean(wt); err == nil {
		if stagedFiles == 0 {
			log.Info("cdb: Working tree is clean, skipping commit")
		} else {
			log.Warnf("cdb: Working tree is clean after staging %d sites, skipping commit", stagedFiles)
		}
		return nil
	}

	// Commit changes
	message := opts.Message
	if message == "" {
		message = "Unspecified changes"
	}
	cmd := "pugo"
	if opts.Cmd != "" {
		cmd = cmd + " " + opts.Cmd
	}
	src := viper.GetString("newerpol.name")
	if src == "" {
		src = viper.GetString("newerpol.database")
	}
	commitMessage := fmt.Sprintf("sites: %s. Sites changed: %d (cmd=%s, src=%s)", message, sitesChanged, cmd, src)
	log.Debugf("cdb: Commit message is '%s'", commitMessage)

	if !opts.DryRun {
		log.Info("cdb: Creating commit")
		_, err := wt.Commit(commitMessage, &git.CommitOptions{
			Author: &object.Signature{
				Name:  viper.GetString("cdb.author.name"),
				Email: viper.GetString("cdb.author.email"),
				When:  time.Now(),
			},
		})
		if err != nil {
			return fmt.Errorf("cdb: Creating commit: %v", err)
		}
	} else {
		log.Info("cdb: Dry run, not committing")
	}

	// Push to origins
	if !opts.DryRun && !opts.NoPush {
		log.Infof("cdb: Pushing to origin/%s", viper.GetString("cdb.branch"))
		repo, err := git.PlainOpen(viper.GetString("cdb.path"))
		if err != nil {
			return fmt.Errorf("cdb: Opening repo at %s: %v", viper.GetString("cdb.path"), err)
		}
		if err := repo.Push(&git.PushOptions{}); err != nil {
			return fmt.Errorf("cdb: Pushing to origin/%s: %v", viper.GetString("cdb.branch"), err)
		}
	} else {
		if opts.DryRun {
			log.Debug("cdb: Dry run, not pushing")
		} else {
			log.Debug("cdb: NoPush enabled, not pushing")
		}
	}

	return nil
}

func GetAllSites() ([]*Site, error) {
	if err := ensureSitesCacheLoaded(); err != nil {
		return nil, err
	}

	return sitesCache.slice, nil
}

func GetSiteById(id int) (*Site, error) {
	if err := ensureSitesCacheLoaded(); err != nil {
		return nil, err
	}

	return sitesCache.byId[id], nil
}

func GetSiteByName(name string) (*Site, error) {
	if err := ensureSitesCacheLoaded(); err != nil {
		return nil, err
	}

	return sitesCache.byName[name], nil
}

func GetWorktree() (*git.Worktree, error) {
	if viper.GetString("cdb.path") == "" {
		return nil, fmt.Errorf("cdb: cdb.path missing in config")
	}

	repo, err := git.PlainOpen(viper.GetString("cdb.path"))
	if err != nil {
		return nil, fmt.Errorf("cdb: Opening repo at %s: %v", viper.GetString("cdb.path"), err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("cdb: Opening worktree: %v", err)
	}

	if err = checkWorktreeClean(wt); err != nil {
		return nil, err
	}

	h, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("cdb: %v", err)
	}

	// Ensure correct branch checked out
	currentBranch := filepath.Base(string(h.Name()))
	if currentBranch != viper.GetString("cdb.branch") {
		log.Infof("cdb: Current branch is '%s', checking out '%s'", currentBranch, viper.GetString("cdb.branch"))
		err = wt.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(viper.GetString("cdb.branch")),
		})
		if err != nil {
			return nil, fmt.Errorf("cdb: Checking out branch '%s': %v", viper.GetString("cdb.branch"), err)
		}
		h, err = repo.Head()
		if err != nil {
			return nil, fmt.Errorf("cdb: %v", err)
		}
		currentBranch = filepath.Base(string(h.Name()))
	}

	// Pull to ensure branch up-to-date
	log.Infof("cdb: Git pulling branch '%s'", currentBranch)
	err = wt.Pull(&git.PullOptions{
		RemoteName:    "origin",
		ReferenceName: plumbing.NewBranchReferenceName(viper.GetString("cdb.branch")),
		SingleBranch:  true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return nil, fmt.Errorf("cdb: Pulling branch '%s': %v", currentBranch, err)
	}

	return wt, nil
}

func checkWorktreeClean(wt *git.Worktree) error {
	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("cdb: %v", err)
	}
	if !status.IsClean() {
		return fmt.Errorf("cdb: Working tree not clean")
	}

	return nil
}

func ensureSitesCacheLoaded() error {
	sitesCache.initOnce.Do(func() {
		sitesCache.initError = initSitesCache()
	})
	return sitesCache.initError
}

func initSitesCache() error {
	if viper.GetString("cdb.path") == "" {
		return fmt.Errorf("cdb: cdb.path missing in config")
	}

	sitesDir := path.Join(viper.GetString("cdb.path"), "sites")
	dirEnts, err := ioutil.ReadDir(sitesDir)
	if err != nil {
		return fmt.Errorf("cdb: %v", err)
	}

	type item struct {
		site *Site
		err  error
	}
	ch := make(chan item, len(dirEnts))

	for _, entry := range dirEnts {
		go func(siteFileName string) {
			log.Debugf("cdb: Loading %s", siteFileName)
			var it item

			// Ensure file under consideration is a YAML file, skip if not
			_, fn := path.Split(siteFileName)
			if path.Ext(fn) != ".yaml" {
				ch <- it
				return
			}

			it.site, it.err = LoadSite(siteFileName)
			ch <- it
		}(entry.Name())
	}

	sitesCache.byId = make(map[int]*Site)
	sitesCache.byName = make(map[string]*Site)

	for range dirEnts {
		it := <-ch
		if it.err != nil {
			return it.err
		}
		if it.site != nil {
			sitesCache.byId[it.site.Id] = it.site
			sitesCache.byName[it.site.name] = it.site
			sitesCache.slice = append(sitesCache.slice, it.site)
		}
	}

	return nil
}
