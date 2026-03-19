// Package main is a demo target program instrumented with the goroscope agent.
// Demonstrates a worker-pool pattern with multiple goroutines for timeline visualization.
package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/agent"
)

const (
	numWorkers = 8
	// numJobs is large enough that the demo runs for ~6-8 seconds so that
	// the live-streaming UI can show goroutines in BLOCKED state while the
	// program is still running.  With 8 workers serialised by a single mutex
	// and each job holding the lock for 100 ms, 7 out of 8 workers are
	// blocked at any given moment — making the "blocked" filter immediately
	// useful without requiring split-second timing.
	numJobs = 60
)

func main() {
	stopTrace, err := agent.StartFromEnv()
	if err != nil {
		log.Fatalf("start goroscope agent: %v", err)
	}
	defer func() {
		if err := stopTrace(); err != nil {
			log.Fatalf("stop goroscope agent: %v", err)
		}
	}()

	jobs := make(chan int, numJobs)
	results := make(chan int, numJobs)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				mu.Lock()
				// Hold the mutex for 100 ms so that 7/8 workers are visibly
				// BLOCKED at any moment — long enough for the streaming UI to
				// capture and display the state before the next poll cycle.
				time.Sleep(100 * time.Millisecond)
				mu.Unlock()
				results <- job * 2
			}
		}()
	}

	go func() {
		for i := 1; i <= numJobs; i++ {
			jobs <- i
			time.Sleep(20 * time.Millisecond)
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	sum := 0
	for result := range results {
		sum += result
		time.Sleep(6 * time.Millisecond)
	}

	fmt.Printf("trace demo complete: sum=%d\n", sum)
}
