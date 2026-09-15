package packagemanager

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunReadOperationsUsesBoundedConcurrencyAndKeepsOrder(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	operations := make([]readOperation[int], 12)
	for i := range operations {
		index := i
		operations[i] = readOperation[int]{run: func(context.Context) (int, error) {
			current := active.Add(1)
			for {
				seen := maximum.Load()
				if current <= seen || maximum.CompareAndSwap(seen, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			return index, nil
		}}
	}
	results := runReadOperations(context.Background(), operations)
	if maximum.Load() > readConcurrency || maximum.Load() < 2 {
		t.Fatalf("maximum concurrency = %d", maximum.Load())
	}
	for i, result := range results {
		if result.value != i {
			t.Fatalf("result %d = %d", i, result.value)
		}
	}
}
