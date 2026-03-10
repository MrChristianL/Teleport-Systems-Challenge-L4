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
	// Get the base flag values
	role, _ := cmd.Flags().GetString("role")
	caFile, _ = cmd.Flags().GetString("ca")
	certPath, _ = cmd.Flags().GetString("cert")
	keyFile, _ = cmd.Flags().GetString("key")

	// If the user ONLY provided --role, override the cert/key paths automatically
	// But if they provided specific --cert or --key flags, respect those.
	if cmd.Flags().Changed("role") && !cmd.Flags().Changed("cert") && !cmd.Flags().Changed("key") {
		certPath = fmt.Sprintf("certs/%s-cert.pem", role)
		keyFile = fmt.Sprintf("certs/%s-key.pem", role)
	}

	return certPath, keyFile, caFile
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// 1. Define the role
	rootCmd.PersistentFlags().String("role", "admin", "Role to use (admin, user, unknown)")

	// 2. Define the paths with sensible defaults
	rootCmd.PersistentFlags().String("ca", "certs/ca-cert.pem", "Path to CA certificate")
	rootCmd.PersistentFlags().String("cert", "certs/admin-cert.pem", "Path to client certificate")
	rootCmd.PersistentFlags().String("key", "certs/admin-key.pem", "Path to client key")
	rootCmd.PersistentFlags().String("server", "localhost:50051", "Server address")
}
