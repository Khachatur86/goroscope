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
	// numJobs × lockDuration = total runtime (~1 min with the defaults).
	// Because only one goroutine holds the mutex at a time, 7 out of 8 workers
	// are always visibly BLOCKED — the "blocked" filter works for the entire run.
	numJobs = 60
	// lockDuration is how long each worker holds the mutex per job.
	// 1 s gives a ~60 s total demo: the mutex is serial, so 60 jobs × 1 s = ~1 min.
	// 7/8 workers are visibly BLOCKED at any moment for the entire run.
	lockDuration = 1 * time.Second
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
				time.Sleep(lockDuration)
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
