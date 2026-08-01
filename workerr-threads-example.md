```go


package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	bufferSize  = 10
	workerCount = 3
)

// produce stands in for readPackets: runs continuously in the background,
// generating work items, feeding them into out. A ticker stands in for
// "reading off a live pcap handle" — different source, same shape (loop,
// send, eventually stop).
func produce(ctx context.Context, out chan<- int) error {
	defer close(out)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	item := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			item++
			out <- item // blocking send: "no item left behind"
		}
	}
}

// worker stands in for your worker: ranges over in, doing whatever work
// each item needs. No ctx check needed here — in closing is the shutdown
// signal.
func worker(id int, in <-chan int) error {
	for item := range in {
		fmt.Printf("worker %d processed item %d\n", id, item)
	}
	return nil
}

// run stands in for OngoingCapture: the orchestrator. Builds the channel,
// launches the one producer + N workers via errgroup, waits for all of them.
func run(ctx context.Context) error {
	ch := make(chan int, bufferSize)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return produce(ctx, ch)
	})
	for i := 0; i < workerCount; i++ {
		id := i
		g.Go(func() error {
			return worker(id, ch)
		})
	}

	return g.Wait()
}


```