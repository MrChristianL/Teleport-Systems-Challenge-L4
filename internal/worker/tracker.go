/* tracker.go

Tracker is responsible for keeping track of all jobs spawned on the server and for
generating unique Job IDs for every job.

*/

package worker

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
)

// Manages the lifecycle of all jobs on the server
type Tracker struct {
	mu   sync.Mutex
	jobs map[string]*Job // jobID -> Job
}

func NewTracker() *Tracker {
	return &Tracker{
		jobs: make(map[string]*Job),
	}
}

// genJobID generates a unique ID. Must be called with t.mu held, as done in AddJob
func (t *Tracker) genJobID() string {
	for {
		// This returns a string like "job-XXXXXX", truncated to 6 characters for UX
		id := "job-" + rand.Text()[:6]

		// check if newly generated ID is already assigned to a job.
		// if no, return. if yes, loop continues and generates a new ID
		if _, exists := t.jobs[id]; !exists {
			return id
		}
	}
}

// Generates a unique 6-character ID for a new job, initializes the job based on the commands provided, and adds the job to the tracker
func (t *Tracker) AddJob(command []string) (*Job, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := t.genJobID()

	// create job instance
	job, err := newJob(id, command)
	if err != nil {
		return nil, fmt.Errorf("[%s] creating new job: %w", id, err)
	}

	t.jobs[job.ID] = job // register job with tracker
	return job, nil
}

// Returns a job given that job's ID
func (t *Tracker) GetJob(jobID string) (*Job, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	job, ok := t.jobs[jobID]
	if !ok {
		return nil, errors.New("job not found")
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

---

TODO:
For production, consider using UUID/ULID for generating IDs for jobs. These approaches would provide support for
a more high throughput service. However, any  implementation would require some consideration as to how the CLI
would allow users to type in job IDs.

UUID is 36 char long. While that offers a great amount of variety, the UX for this is terrible.
*/
