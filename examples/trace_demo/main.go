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
	numJobs    = 24
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
				time.Sleep(10 * time.Millisecond)
				mu.Unlock()
				results <- job * 2
			}
		}()
	}

	go func() {
		for i := 1; i <= numJobs; i++ {
			jobs <- i
			time.Sleep(10 * time.Millisecond)
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
