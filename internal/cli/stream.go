package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/api/client"

	"github.com/spf13/cobra"
)

var streamCmd = &cobra.Command{
	Use:   "stream [job-id]",
	Short: "Stream output from a job",
	Long: `Stream output from a running job in real-time.
	
You can provide the job ID as an argument or pipe it from another command.

Examples:
  jobctl stream job-12345
  jobctl start sleep 100 | jobctl stream
	
Press Ctrl + C to stop streaming (job continues running)...`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get job ID from args or stdin
		var jobID string
		if len(args) > 0 {
			jobID = args[0]
		} else {
			// Read from stdin
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				jobID = strings.TrimSpace(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("failed to read job ID from stdin: %v", err)
			}
			if jobID == "" {
				return fmt.Errorf("no job ID provided")
			}
		}

		// Get connection flags
		serverAddr, _ := cmd.Flags().GetString("server")
		certFile, keyFile, caFile := certPathsForRole(cmd)

		// Create client
		c, err := client.NewClient(serverAddr, certFile, keyFile, caFile)
		if err != nil {
			return fmt.Errorf("failed to connect to server: %v", err)
		}
		defer c.Close()

		// Stream output
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = c.StreamOutput(ctx, jobID, func(chunk []byte) error {
			// Write chunk directly to stdout (preserves binary data)
			os.Stdout.Write(chunk)
			return nil
		})

		if err != nil {
			return fmt.Errorf("error streaming output: %v", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(streamCmd)
}
