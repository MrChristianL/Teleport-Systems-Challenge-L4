package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/api/client"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var startCmd = &cobra.Command{
	Use:   "start [command] [args...]",
	Short: "Start a new job",
	Long: `Start a new job with the specified command and arguments.
    
Example:
  jobctl start sleep 100
  jobctl start python3 script.py --input=data.csv`,
	Args: cobra.MinimumNArgs(1), // Require at least one argument
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get connection flags
		serverAddr, _ := cmd.Flags().GetString("server")
		certFile, keyFile, caFile := certPathsForRole(cmd)

		// Create client
		c, err := client.NewClient(serverAddr, certFile, keyFile, caFile)
		if err != nil {
			return fmt.Errorf("connecting to server: %w", err)
		}
		defer c.Close()

		// Start the job
		startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer startCancel()

		jobID, err := c.StartJob(startCtx, args)
		if err != nil {
			return fmt.Errorf("Error: %s", nicerErrors(err))
		}

		fmt.Printf("%s\n", jobID)
		return nil
	},
}

func nicerErrors(err error) error {
	if s, ok := status.FromError(err); ok {
		return fmt.Errorf("%s", s.Message())
	}
	return err
}

func init() {
	rootCmd.AddCommand(startCmd)

	// Command-specific flags
	startCmd.Flags().BoolP("follow", "f", false, "Automatically stream output after starting job")
}
