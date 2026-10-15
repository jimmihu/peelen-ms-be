package jobs

import (
	"context"
	"log"
	"time"
)

type jobCycle interface {
	RunJobCycle(ctx context.Context) error
}

// StartScoringJob runs one cycle immediately, then every interval, until ctx ends.
func StartScoringJob(ctx context.Context, u jobCycle, interval time.Duration) {
	go func() {
		run := func() {
			runCtx, cancel := context.WithTimeout(ctx, interval)
			defer cancel()
			if err := u.RunJobCycle(runCtx); err != nil {
				log.Printf("scoring job: %v", err)
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
