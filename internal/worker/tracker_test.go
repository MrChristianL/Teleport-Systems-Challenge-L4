/* tracker_test.go

Tracker_test.go tests the job tracker's abiltity to add and track jobs
*/

package worker

import (
	"sync"
	"testing"
)

// TestTrackerJobLifecycle verifies the tracker can add/get jobs and assign job IDs
func TestTrackerJobLifecycle(t *testing.T) {
	tracker := NewTracker()

	// add job to tracker
	job, err := tracker.AddJob([]string{"sleep", "3"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}

	// job should have been generated an ID
	if len(job.ID) != 10 {
		t.Errorf("job ID should be of format 'job-XXXXXX', got %q", job.ID)
	}

	// get job
	jobGet, err := tracker.GetJob(job.ID)
	if err != nil {
		t.Errorf("tracker failed to get job %q: %v", job.ID, err)
	}

	if jobGet.ID != job.ID {
		t.Errorf("job ID should be the same for both, got %s, %s", job.ID, jobGet.ID)
	}
}

// TestGetMissingJob verifies that tracker will return err if GetJob is given an incorrect ID
func TestGetMissingJob(t *testing.T) {
	tracker := NewTracker()

	// get job with false ID
	if _, err := tracker.GetJob("job-123abc"); err == nil {
		t.Errorf("NewTracker() = %v, wanted err", err)
	}
}

// TestConcurrentAddJob verifies the tracker can handle concurrent AddJob requests
func TestConcurrentAddJob(t *testing.T) {
	tracker := NewTracker()

	var mu sync.Mutex // protects IDs map
	IDs := make(map[string]bool)
	var wg sync.WaitGroup

	// add 50 jobs
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			job, err := tracker.AddJob([]string{"sleep", "1"})
			if err != nil {
				t.Errorf("[%d] failed to start job: %v", index, err)
				return
			}
			mu.Lock()
			IDs[job.ID] = true
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if len(IDs) != 50 {
		t.Errorf("expected 50 unique IDs, got %d", len(IDs))
	}

	// verify all IDs are actually unique
	seen := make(map[string]bool)
	for id := range IDs {
		if seen[id] {
			t.Errorf("duplicate job ID: %s", id)
		}
		seen[id] = true
	}
}
