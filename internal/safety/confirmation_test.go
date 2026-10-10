package safety

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfirmationExpiry(t *testing.T) {
	c, err := NewConfirmations(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	args := json.RawMessage(`{"bucket":"bucket","key":"key"}`)
	state, err := c.Issue("s3_delete_object", args)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Minute)
	if err := c.Consume("s3_delete_object", args, state); err == nil {
		t.Fatal("STATE_EXPIRED: expired approval accepted")
	}
}

func TestConfirmationConcurrentConsumption(t *testing.T) {
	c, err := NewConfirmations(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"key":"key","bucket":"bucket"}`)
	state, err := c.Issue("s3_delete_object", args)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for range 20 {
		wg.Go(func() {
			if c.Consume("s3_delete_object", args, state) == nil {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("STATE_CONCURRENT: accepted %d times", accepted.Load())
	}
}

func TestConfirmationToolBinding(t *testing.T) {
	c, err := NewConfirmations(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"bucket":"bucket","key":"key"}`)
	state, err := c.Issue("s3_delete_object", args)
	if err != nil {
		t.Fatal(err)
	}
	if c.Consume("s3_presign_put", args, state) == nil {
		t.Fatal("STATE_TOOL: approval reused for another tool")
	}
}
