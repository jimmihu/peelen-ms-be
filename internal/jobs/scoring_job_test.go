package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingCycle struct{ runs atomic.Int32 }

func (c *countingCycle) RunJobCycle(context.Context) error { c.runs.Add(1); return nil }

func TestStartScoringJobRunsImmediatelyAndOnTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cycle := &countingCycle{}
	StartScoringJob(ctx, cycle, 20*time.Millisecond)
	time.Sleep(70 * time.Millisecond)
	cancel()
	if n := cycle.runs.Load(); n < 2 {
		t.Fatalf("runs = %d, want at least 2", n)
	}
}
