// Package main is a complex goroscope demo target that showcases ~200 goroutines
// across two concurrency patterns running in parallel for ~60 seconds.
//
// Pattern 1 – Mutex pools (5 pools × 20 workers = 100 goroutines):
//
//	Each pool serializes work through a per-pool mutex.  At any moment
//	19 out of 20 workers per pool (95 total) are visibly BLOCKED waiting
//	for the lock — the "blocked" filter returns a dense timeline.
//
// Pattern 2 – Channel pipeline (3 stages × 30 workers = 90 goroutines):
//
//	Items flow producer→stage0→stage1→stage2→sink through buffered channels.
//	Because the producer is the bottleneck (100 ms per item), workers in every
//	stage spend most of their time BLOCKED on channel receive, giving the
//	timeline a second, distinct blocking-reason column to explore.
//
// Total user goroutines: ~195 + runtime goroutines.
package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/agent"
)

// Mutex pool parameters.
// 5 pools × 20 workers = 100 goroutines; 19/20 per pool always BLOCKED.
const (
	numPools       = 5
	workersPerPool = 20
	jobsPerPool    = 60
	holdDuration   = 1 * time.Second // serial mutex → one worker running per pool
)

// Pipeline parameters.
// 3 stages × 30 workers = 90 goroutines; workers block on channel receive.
const (
	numStages       = 3
	workersPerStage = 30
	numItems        = 500
	produceDelay    = 100 * time.Millisecond // 500 × 100 ms ≈ 50 s total production
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

	var wg sync.WaitGroup
	wg.Add(2)
	go runMutexPools(&wg)
	go runPipeline(&wg)
	wg.Wait()

	fmt.Println("goroscope complex demo complete")
}

// runMutexPools starts numPools independent worker pools concurrently.
// Each pool serializes work through its own mutex so that
// workersPerPool−1 goroutines are visibly BLOCKED at any moment.
func runMutexPools(outer *sync.WaitGroup) {
	defer outer.Done()

	var wg sync.WaitGroup
	for range numPools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runPool()
		}()
	}
	wg.Wait()
}

// runPool runs a single worker pool: workersPerPool goroutines compete for a
// shared mutex.  A job is queued every 20 ms so the pool stays busy for
// approximately jobsPerPool × holdDuration ≈ 60 s.
func runPool() {
	jobs := make(chan int, jobsPerPool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for range workersPerPool {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				mu.Lock()
				time.Sleep(holdDuration)
				mu.Unlock()
			}
		}()
	}

	for i := range jobsPerPool {
		jobs <- i
		time.Sleep(20 * time.Millisecond)
	}
	close(jobs) // CC-1: sender closes
	wg.Wait()
}

// runPipeline runs a numStages-stage processing pipeline.  Each stage has
// workersPerStage goroutines reading from an upstream channel and writing to a
// downstream channel.  The producer is the bottleneck so workers spend most of
// their time BLOCKED on channel receive, making their state visible in the
// timeline alongside the mutex-blocked pool workers.
func runPipeline(outer *sync.WaitGroup) {
	defer outer.Done()

	// chans[0] is the pipeline input; chans[numStages] is the final sink channel.
	chans := make([]chan int, numStages+1)
	for i := range chans {
		chans[i] = make(chan int, workersPerStage)
	}

	var wg sync.WaitGroup

	for s := range numStages {
		in := chans[s]
		out := chans[s+1]
		stageID := s

		var stageWG sync.WaitGroup
		for range workersPerStage {
			stageWG.Add(1)
			wg.Add(1)
			go func() {
				defer stageWG.Done()
				defer wg.Done()
				for v := range in {
					// Variable processing time: 10–60 ms.
					//nolint:gosec // demo jitter does not require a cryptographic RNG.
					time.Sleep(time.Duration(10+rand.IntN(50)) * time.Millisecond)
					out <- v*2 + stageID
				}
			}()
		}

		// CC-1: close the output channel once all workers for this stage are done.
		go func() {
			stageWG.Wait()
			close(out)
		}()
	}

	// Producer: feeds numItems into stage 0 at produceDelay intervals.
	go func() {
		for i := range numItems {
			chans[0] <- i
			time.Sleep(produceDelay)
		}
		close(chans[0]) // CC-1: sender closes
	}()

	// Sink: drain the final stage output so workers are never blocked sending.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range chans[numStages] {
		}
	}()

	wg.Wait()
}
