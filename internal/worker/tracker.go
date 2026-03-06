/* tracker.go

Tracker is responsible for keeping track of all jobs spawned on the server and for
generating unique Job IDs for every job.

*/

package worker

import (
	"crypto/rand"
	"fmt"
	"sync"
)

// Manages the lifecycle of all jobs on the server
type Tracker struct {
	mu   sync.RWMutex
	jobs map[string]*Job // jobID -> Job
}

func NewTracker() *Tracker {
	return &Tracker{
		jobs: make(map[string]*Job),
	}
}

// genJobID generates a unique ID. Must be called with t.mu held
func (t *Tracker) genJobID() (string, error) {
	const charset = "0123456789abcdefghijklmnopqrstuvwxyz"
	const length = 6

	for {
		// generate ID core - randomly assign bytes
		core := make([]byte, length)
		if _, err := rand.Read(core); err != nil {
			return "", fmt.Errorf("failed to generated Job ID core: %w", err)
		}

		// map random bytes to charset
		for i := range core {
			core[i] = charset[int(core[i])%len(charset)] // assign corresponding character to core
		}

		id := "job-" + string(core)

		// check if newly generated ID is already assigned to a job.
		// if no, return. if yes, loop continues and generates a new ID
		if _, exists := t.jobs[id]; !exists {
			return id, nil
		}
	}
}

// Generates a unique 6-character ID for a new job, initializes the job based on the commands provided, and adds the job to the tracker
func (t *Tracker) AddJob(command []string) (*Job, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	id, err := t.genJobID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate Job ID: %w", err)
	}

	// create job instance
	job, err := newJob(id, command)
	if err != nil {
		return nil, fmt.Errorf("[%s] failed to create new job: %w", id, err)
	}

	t.jobs[job.ID] = job // register job with tracker
	return job, nil
}

// Returns a job given that job's ID
func (t *Tracker) GetJob(jobID string) (*Job, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	job, ok := t.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found")
	}

	return job, nil
}

/* TODO:
Add cleanup for finished/failed/stopped jobs.

Some options include:
- TTL-based cleanup that removes jobs oler than X amount of time
- Provide support for an explicit call from CLI (with sufficient permissions) to cleanup non-running jobs.

For now, jobs persist until the server restarts and /tmp is cleaned up. This allows clients to view the
status and output of jobs, even after finishing.

TODO:
For production, consider using UUID/ULID for generating IDs for jobs. These approaches would provide support for
a more high throughput service. However, any  implementation would require some consideration as to how the CLI
would allow users to type in job IDs.

UUID is 36 char long. While that offers a great amount of variety, the UX for this is terrible.
*/
