// Package main is a demo target program instrumented with the goroscope agent.
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

	jobs := make(chan int)
	results := make(chan int)
	var mu sync.Mutex

	go func() {
		for job := range jobs {
			mu.Lock()
			time.Sleep(15 * time.Millisecond)
			mu.Unlock()
			results <- job * 2
		}
		close(results)
	}()

	go func() {
		for _, job := range []int{1, 2, 3} {
			jobs <- job
			time.Sleep(10 * time.Millisecond)
		}
		close(jobs)
	}()

	sum := 0
	for result := range results {
		sum += result
		time.Sleep(8 * time.Millisecond)
	}

	fmt.Printf("trace demo complete: sum=%d\n", sum)
}
