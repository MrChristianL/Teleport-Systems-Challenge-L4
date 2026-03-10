package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/api/client"

	"github.com/spf13/cobra"
)

var exitCodeMessages = map[int32]string{ // some exit codes may never be reached but are listed here for completeness
	1:   "general error",
	2:   "executable not found",
	13:  "permission denied",
	126: "command cannot execute (permission problem or not an executable)",
	127: "command not found",
	128: "invalid argument to exit",
	137: "stopped by user (SIGKILL)",
}

func exitCodeMessage(code int32, fallback string) string {
	if msg, ok := exitCodeMessages[code]; ok {
		return msg
	}
	if code > 128 {
		return fmt.Sprintf("killed by signal %d", code-128)
	}
	return fallback
}

var statusCmd = &cobra.Command{
	Use:   "status [job-id]",
	Short: "Get status of a job",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var jobID string

		// Get job ID from arguments or stdin
		if len(args) > 0 {
			jobID = args[0]
		} else {
			// Read from stdin
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				jobID = strings.TrimSpace(scanner.Text())
			}
			if jobID == "" {
				return errors.New("job ID not provided as argument or via stdin")
			}
		}

		// Get connection flags
		serverAddr, _ := cmd.Flags().GetString("server")
		certFile, keyFile, caFile := certPathsForRole(cmd)

		// Create client
		c, err := client.NewClient(serverAddr, certFile, keyFile, caFile)
		if err != nil {
			return fmt.Errorf("connecting to server: %w", err)
		}
		defer c.Close()

		// Get job status
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		status, exitCode, message, err := c.GetStatus(ctx, jobID)
		if err != nil {
			return fmt.Errorf("GetStatus: %w", err)
		}

		fmt.Printf("Job ID: %s\n", jobID)
		fmt.Printf("Status: %s\n", status.String())
		if status.String() == "FINISHED" || status.String() == "FAILED" {
			fmt.Printf("Message: %s\n", exitCodeMessage(exitCode, message))
			fmt.Printf("Exit Code: %d\n", exitCode)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
