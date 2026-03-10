package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "jobctl",
	Short: "Job control CLI for managing remote processes",
	Long: `jobctl is a CLI tool for managing and performing remote job execution on Linux machines. This includes:

	jobctl start ...
	jobctl stop ...
	jobctl status ...
	jobctl stream...`,
	SilenceUsage:  true, // don't print usage on runtime errors, only argument errors
	SilenceErrors: true, // don't print errors twice
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// helper function that allows users to select cert levels (role) for running commands
// DEFAULT: Admin user
func certPathsForRole(cmd *cobra.Command) (certPath, keyFile, caFile string) {
	cert, _ := cmd.Flags().GetString("cert")
	caFile, _ = cmd.Flags().GetString("ca")

	// if cert/key were explicitly provided, use those
	certPath, _ = cmd.Flags().GetString("cert")
	keyFile, _ = cmd.Flags().GetString("key")

	// if role was explicitly set, override cert/key
	if cmd.Flags().Changed("role") {
		certPath = fmt.Sprintf("certs/%s-cert.pem", cert)
		keyFile = fmt.Sprintf("certs/%s-key.pem", cert)
	}

	return certPath, keyFile, caFile
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.PersistentFlags().String("role", "admin", "Role to use (admin, user, unknown)")
}
