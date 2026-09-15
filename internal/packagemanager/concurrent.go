package packagemanager

import "context"

const readConcurrency = 4

type readOperation[T any] struct {
	managerIndex int
	manager      string
	executable   string
	scope        Scope
	run          func(context.Context) (T, error)
}

type readResult[T any] struct {
	operation readOperation[T]
	value     T
	err       error
}

func runReadOperations[T any](ctx context.Context, operations []readOperation[T]) []readResult[T] {
	results := make([]readResult[T], len(operations))
	semaphore := make(chan struct{}, readConcurrency)
	done := make(chan int, len(operations))
	for i, operation := range operations {
		go func() {
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[i] = readResult[T]{operation: operation, err: ctx.Err()}
				done <- i
				return
			}
			value, err := operation.run(ctx)
			results[i] = readResult[T]{operation: operation, value: value, err: err}
			done <- i
		}()
	}
	for range operations {
		<-done
	}
	return results
}
