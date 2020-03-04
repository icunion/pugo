package cdb

import (
	"fmt"
	"io/ioutil"
	"path"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Site struct {
	Id             int
	FullName       string `yaml:"full-name"`
	Email          string
	DisplayEmail   string `yaml:"display-email,omitempty"`
	Admins         []string
	ImmortalAdmins []string `yaml:"immortal-admins,omitempty"`
	Expiry         string
	Paths          []string
	Domains        []interface{} `yaml:"domains,omitempty"`
	Disabled       bool
	DisabledReason string `yaml:"disabled_reason,omitempty"`
	Php            bool
	PhpVersion     int `yaml:"php-version,omitempty"`
	Passenger      bool
	Subpaths       bool
	name           string
	mu             sync.Mutex
	changed        bool
}

func NewSite() *Site {
	site := Site{}
	site.Disabled = false
	site.Php = true
	site.Passenger = false
	site.changed = false
	return &site
}

func LoadSite(siteFileName string) (*Site, error) {
	// Ensure file under consideration is a YAML file, skip if not
	_, fn := path.Split(siteFileName)
	if path.Ext(fn) != ".yaml" {
		return nil, fmt.Errorf("cdb: %s not a YAML file", siteFileName)
	}

	site := NewSite()
	site.name = strings.TrimSuffix(fn, path.Ext(fn))
	yamlData, err := ioutil.ReadFile(path.Join(viper.GetString("cdb.path"), "sites", fn))
	if err != nil {
		return nil, fmt.Errorf("cdb: Reading %s: %v", siteFileName, err)
	}

	if err = yaml.Unmarshal(yamlData, site); err != nil {
		return nil, fmt.Errorf("cdb: Unmarshalling %s: %v", siteFileName, err)
	}

	return site, nil
}

func (s *Site) Changed() bool {
	return s.changed
}

func (s *Site) MarkAsChanged() {
	s.changed = true
}

func (s *Site) Name() string {
	return s.name
}

func (s *Site) FileName() string {
	return path.Join(viper.GetString("cdb.path"), "sites", s.name+".yaml")
}

func (s *Site) FileNameRepo() string {
	return path.Join("sites", s.name+".yaml")
}

func (s *Site) AddAdmin(username string) {
	log.WithFields(log.Fields{
		"s.Admins": s.Admins,
		"username": username,
	}).Debug("cdb: AddAdmin start")

	// Don't attempt to add an empty username
	if username == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sort.Strings(s.Admins)
	pos := sort.SearchStrings(s.Admins, username)
	if pos < len(s.Admins) && s.Admins[pos] == username {
		// Username already exists in admins, nothing to do
		return
	}
	if pos == len(s.Admins) {
		s.Admins = append(s.Admins, username)
	} else {
		s.Admins = append(s.Admins, "")
		copy(s.Admins[pos+1:], s.Admins[pos:])
		s.Admins[pos] = username
	}
	log.WithFields(log.Fields{
		"s.Admins": s.Admins,
	}).Debug("cdb: AddAdmin after change")
	s.changed = true

	return
}

func (s *Site) RemoveAdmin(username string) {
	log.WithFields(log.Fields{
		"s.Admins": s.Admins,
		"username": username,
	}).Debug("cdb: RemoveAdmin")

	// Don't attempt to remove an empty username
	if username == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sort.Strings(s.Admins)
	pos := sort.SearchStrings(s.Admins, username)
	if pos < len(s.Admins) && s.Admins[pos] == username {
		if pos < len(s.Admins)-1 {
			copy(s.Admins[pos:], s.Admins[pos+1:])
		}
		s.Admins[len(s.Admins)-1] = ""
		s.Admins = s.Admins[:len(s.Admins)-1]
		log.WithFields(log.Fields{
			"s.Admins": s.Admins,
		}).Debug("cdb: RemoveAdmin after change")
		s.changed = true
	}

	return
}

func (s *Site) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	yamlData, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("cdb: Unable to marshall %s: %v", s.name, err)
	}
	if err = ioutil.WriteFile(s.FileName(), []byte(yamlData), 0644); err != nil {
		return fmt.Errorf("cdb: Unable to write %s.yaml: %v", s.name, err)
	}
	s.changed = false
	return nil
}
