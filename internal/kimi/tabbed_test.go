package kimi

import (
	"sync"
	"testing"
)

func TestThreadClose_NeverOpened(t *testing.T) {
	tc := NewTabbedClient("test", "Test")
	th := tc.NewThread("a")

	if err := th.Close(); err != nil {
		t.Errorf("Close on unopened thread should not error: %v", err)
	}

	tc.mu.Lock()
	_, exists := tc.threads["a"]
	tc.mu.Unlock()
	if exists {
		t.Errorf("thread should be removed from registry after Close")
	}
}

func TestThreadClose_Idempotent(t *testing.T) {
	tc := NewTabbedClient("test", "Test")
	th := tc.NewThread("a")

	if err := th.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := th.Close(); err != nil {
		t.Errorf("second Close should be no-op, got: %v", err)
	}
}

func TestThreadClose_RemovesOnlyItsOwnEntry(t *testing.T) {
	tc := NewTabbedClient("test", "Test")
	thA := tc.NewThread("a")
	_ = tc.NewThread("b")

	if err := thA.Close(); err != nil {
		t.Fatalf("Close a: %v", err)
	}

	tc.mu.Lock()
	_, hasA := tc.threads["a"]
	_, hasB := tc.threads["b"]
	tc.mu.Unlock()

	if hasA {
		t.Errorf("thread a should be removed")
	}
	if !hasB {
		t.Errorf("thread b should still be present")
	}
}

func TestThreadName(t *testing.T) {
	tc := NewTabbedClient("test", "Test")
	th := tc.NewThread("worker-1")
	if got := th.Name(); got != "worker-1" {
		t.Errorf("Name() = %q, want %q", got, "worker-1")
	}
}

func TestThreadClose_ConcurrentSafe(t *testing.T) {
	tc := NewTabbedClient("test", "Test")
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := string(rune('a' + idx))
			th := tc.NewThread(name)
			_ = th.Close()
		}(i)
	}

	wg.Wait()

	tc.mu.Lock()
	remaining := len(tc.threads)
	tc.mu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 threads after concurrent close, got %d", remaining)
	}
}
