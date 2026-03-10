package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/api/client"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <job-id>",
	Short: "Stop a running job",
	Args:  cobra.ExactArgs(1), // requires exactly 1 argument
	RunE: func(cmd *cobra.Command, arg []string) error {
		jobID := arg[0]

		// get connection flags
		serverAddr, _ := cmd.Flags().GetString("server")
		certFile, keyFile, caFile := certPathsForRole(cmd)

		// Create client
		c, err := client.NewClient(serverAddr, certFile, keyFile, caFile)
		if err != nil {
			return fmt.Errorf("failed to connect to server: %v", err)
		}
		defer c.Close()

		// Stop the job
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		success, message, err := c.StopJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("failed to stop job: %v", err)
		}

		if !success {
			return fmt.Errorf("Error: %s", message)
		}

		fmt.Println(message)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
