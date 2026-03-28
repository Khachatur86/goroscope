//go:build ignore

// Package badcode contains intentional concurrency anti-patterns used to
// demonstrate goroscope's static analysis detectors (Code tab → Analyze).
//
// Every function here is a textbook mistake; none of this code is meant to
// run correctly.  Point the analyzer at ./examples/badcode to see all rules
// fire at once.
package badcode

import (
	"sync"
	"time"
)

// SA-1: Lock() not followed by defer Unlock().
// If doWork() panics the mutex is never released.
func processWithoutDefer(mu sync.Locker, shared *int) {
	mu.Lock() // SA-1: should be: defer mu.Unlock()
	*shared++
	mu.Unlock()
}

// SA-7: Double-lock — the same mutex is locked twice in one call stack
// without an intervening Unlock.  This will deadlock at runtime.
func doubleLockExample() {
	var mu sync.Mutex
	mu.Lock()
	// ... some work ...
	mu.Lock() // SA-7: deadlock — mu is already held
	mu.Unlock()
	mu.Unlock()
}

// SA-4: sync.Mutex passed by value.
// The copy has an independent (unlocked) state, so the caller's lock is bypassed.
func workerWithCopiedMutex(mu sync.Mutex, counter *int) { // SA-4: use *sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	*counter++
}

// SA-2: Goroutine closure captures the loop variable by reference.
// All spawned goroutines will see the final value of `job` after the loop ends.
func spawnWorkers(jobs []string) {
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func() { // SA-2: captures `job` by reference — use go func(job string){...}(job)
			defer wg.Done()
			_ = job
		}()
	}
	wg.Wait()
}

// SA-3: wg.Add() called after the goroutine has already started.
// There is a race: the goroutine may call wg.Done() before Add increments.
func addAfterGo(items []int) {
	var wg sync.WaitGroup
	for _, v := range items {
		go func(n int) { // SA-3: wg.Add(1) must come before this line
			defer wg.Done()
			_ = n
		}(v)
		wg.Add(1) // SA-3: too late — goroutine may have already finished
	}
	wg.Wait()
}

// SA-5: Unbuffered channel send outside a select.
// The send blocks forever if no receiver is ready.
func sendWithoutBuffer() {
	ch := make(chan int) // unbuffered
	ch <- 42             // SA-5: blocks indefinitely — use make(chan int, 1) or a select
}

// SA-8: time.Sleep inside a goroutine without a context cancellation check.
// The goroutine cannot be stopped cleanly on shutdown.
func pollForever() {
	go func() {
		for {
			time.Sleep(5 * time.Second) // SA-8: no ctx.Done() check — goroutine leaks on shutdown
			// do some polling work
		}
	}()
}
