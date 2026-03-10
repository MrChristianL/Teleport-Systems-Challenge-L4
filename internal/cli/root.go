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

// certPathsForRole returns cert paths based on --cert flag
// Supports convenience values: "admin", "user", "unknown"
// Or full paths: "certs/custom-cert.pem"
func certPathsForRole(cmd *cobra.Command) (certPath, keyFile, caFile string) {
	certFlag, _ := cmd.Flags().GetString("cert")
	caFile, _ = cmd.Flags().GetString("ca")
	
	// Check if user provided a convenience name (admin, user, unknown)
	// or a full path
	switch certFlag {
	case "admin", "user", "unknown":
		// Convenience: --cert=admin expands to certs/admin-cert.pem
		certPath = fmt.Sprintf("certs/%s-cert.pem", certFlag)
		keyFile = fmt.Sprintf("certs/%s-key.pem", certFlag)
	default:
		// Full path provided: --cert=certs/custom-cert.pem
		certPath = certFlag
		
		// Try to infer key path by replacing -cert.pem with -key.pem
		if strings.HasSuffix(certPath, "-cert.pem") {
			keyFile = strings.Replace(certPath, "-cert.pem", "-key.pem", 1)
		} else if strings.HasSuffix(certPath, ".pem") {
			// Fallback: replace .pem with -key.pem
			keyFile = strings.Replace(certPath, ".pem", "-key.pem", 1)
		} else {
			// Can't infer, user must provide --key explicitly
			keyFile, _ = cmd.Flags().GetString("key")
		}
	}
	
	// Allow explicit --key override
	if cmd.Flags().Changed("key") {
		keyFile, _ = cmd.Flags().GetString("key")
	}
	
	return certPath, keyFile, caFile
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Certificate flags
	rootCmd.PersistentFlags().String("cert", "admin", "Client certificate (admin, user, unknown, or path to cert file)")
	rootCmd.PersistentFlags().String("key", "", "Path to client key (optional, auto-detected from --cert)")
	rootCmd.PersistentFlags().String("ca", "certs/ca-cert.pem", "Path to CA certificate")
	
	// Server address
	rootCmd.PersistentFlags().String("server", "localhost:50051", "Server address")
}
