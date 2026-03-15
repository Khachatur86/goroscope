// Package main demonstrates a worker-pool pattern with goroscope tracing.
// Run with: goroscope run ./examples/worker_pool --open-browser
package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/agent"
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

	const numWorkers = 5
	const numJobs = 20

	jobs := make(chan int, numJobs)
	results := make(chan int, numJobs)
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range jobs {
				time.Sleep(10 * time.Millisecond)
				results <- j * 2
			}
		}(w)
	}

	go func() {
		for j := 1; j <= numJobs; j++ {
			jobs <- j
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	sum := 0
	for r := range results {
		sum += r
	}

	fmt.Printf("worker pool complete: sum=%d\n", sum)
}
