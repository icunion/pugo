package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset various elements of cdb",
	Long: `Reset things in cdb, such as clearing admins from all sites
or resetting the user expiry date.`,
	Run: func(cmd *cobra.Command, args []string) {
		log.Fatal("reset: Must be run with subcommand")
	},
}

func init() {
	rootCmd.AddCommand(resetCmd)
}
