package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var cfgFile string
var LogQuiet bool
var LogVerbose bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "pugo",
	Short: "A tool for managing the ICU student sites configuration database (icu-cdb) and related Sysadmin tasks",
	Long: `A Go implementation of Pete's Update Gizmo. Allows ICU Sysadmins
to perform the following tasks:

* Sync access requests and revocations from eActivities to icu-cdb
* Make a new site
* Fix file permissions
`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initLog, initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.pugo.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&LogQuiet, "quiet", "q", false, "quiet output (warnings only). Ignored if verbose is enabled.")
	rootCmd.PersistentFlags().BoolVarP(&LogVerbose, "verbose", "v", false, "verbose output (debug level)")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			log.Fatal(err)
		}

		// Search config in home directory with name ".pugo" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".pugo")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		log.Info("Using config file:", viper.ConfigFileUsed())
	}
}

// initLog initialises logging (i.e. setting the required log level etc)
func initLog() {
	if LogVerbose {
		LogQuiet = false
		log.SetLevel(log.DebugLevel)
	}
	if LogQuiet {
		log.SetLevel(log.WarnLevel)
	}
}
