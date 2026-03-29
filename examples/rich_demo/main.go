// Package main is a rich goroscope demo designed to produce a vivid, wide
// timeline with a healthy mix of all goroutine states:
//
//   - RUNNING  (green)  – heavy CPU computation: sort + multi-pass SHA-256
//   - BLOCKED  (red)    – mutex contention pools (multiple goroutines per lock)
//   - WAITING  (amber)  – channel pipeline stages, tickers, fan-in collector
//   - SYSCALL  (blue)   – HTTP client + server network I/O
//   - RUNNABLE (grey)   – brief scheduling gaps between the above
//
// Architecture:
//
//	HTTP server (:18080)        Mutex pools (4×10 goroutines)
//	  /compute  heavy CPU         each pool shares one mutex
//	  /data     contended lock    holds lock + does CPU work
//	  /health   instant ping
//	       ▲                    Channel pipeline  (3 stages × 8 workers)
//	20 HTTP workers               items flow stage0→stage1→stage2→sink
//	  GET /compute|/data          each stage sorts+hashes on every item
//	  heavy CPU post-processing
//	       │                    4 continuous CPU goroutines
//	fan-in aggregator             spin generating + sorting data non-stop
//	  RWMutex-protected map
//	       │
//	1 leaked goroutine (leak detector demo)
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/agent"
)

// ── Tuning constants ──────────────────────────────────────────────────────────

const (
	httpAddr = "127.0.0.1:18080"

	// HTTP worker pool.
	numHTTPWorkers = 20
	numJobs        = 300

	// Mutex contention pools.
	numMutexPools       = 4
	mutexWorkersPerPool = 10
	mutexJobsPerPool    = 80
	mutexHoldWork       = 8_000 // cpuWork n while holding mutex → red + green
	mutexHoldSleep      = 15 * time.Millisecond

	// Channel pipeline.
	numPipelineStages = 3
	pipelineWorkers   = 8
	numPipelineItems  = 400
	pipelineItemWork  = 6_000 // cpuWork n per stage

	// Continuous CPU goroutines.
	numCPUSpin = 4

	// CPU work sizes.
	httpClientWork = 25_000 // cpuWork n per HTTP worker job
	serverCompute  = 20_000 // cpuWork n in /compute handler
)

// ── CPU work ──────────────────────────────────────────────────────────────────

// cpuWork sorts n random ints, then makes multiple SHA-256 passes over the
// result.  Deliberately CPU-heavy to produce wide RUNNING segments.
func cpuWork(n int) string {
	data := make([]int, n)
	for i := range data {
		data[i] = rand.IntN(10_000_000) //nolint:gosec
	}
	sort.Ints(data)

	// Three hash passes so RUNNING stays long even for moderate n.
	h := sha256.New()
	limit := n
	if limit > 512 {
		limit = 512
	}
	for pass := range 3 {
		h.Reset()
		for _, v := range data[:limit] {
			_, _ = fmt.Fprintf(h, "%d:%d,", pass, v)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// ── cache ─────────────────────────────────────────────────────────────────────

type cache struct {
	mu      sync.RWMutex
	entries map[string]string
	maxSize int
}

func newCache(maxSize int) *cache {
	return &cache{entries: make(map[string]string, maxSize), maxSize: maxSize}
}

func (c *cache) get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *cache) set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxSize {
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	c.entries[key] = value
}

// ── stats ─────────────────────────────────────────────────────────────────────

type stats struct {
	mu      sync.Mutex
	total   int
	ok      int
	errored int
	totalMS int64
}

func (s *stats) record(success bool, ms int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if success {
		s.ok++
	} else {
		s.errored++
	}
	s.totalMS += ms
}

func (s *stats) snapshot() (total, ok, errored int, avgMS float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total, ok, errored = s.total, s.ok, s.errored
	if total > 0 {
		avgMS = float64(s.totalMS) / float64(total)
	}
	return
}

// ── HTTP server ───────────────────────────────────────────────────────────────

type serverDeps struct {
	cache *cache
	stats *stats
}

type startServerInput struct {
	addr string
	deps serverDeps
	log  *slog.Logger
}

func startServer(input startServerInput) *http.Server {
	var slowMu sync.Mutex // deliberately contended

	mux := http.NewServeMux()

	// /compute: heavy CPU work → RUNNING in server goroutine; RWMutex write → BLOCKED briefly.
	mux.HandleFunc("/compute", func(w http.ResponseWriter, _ *http.Request) {
		n := serverCompute + rand.IntN(serverCompute/2) //nolint:gosec
		result := cpuWork(n)
		input.deps.cache.set("compute:"+result[:4], result)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": result})
	})

	// /data: holds slowMu while doing CPU work — forces BLOCKED state in callers.
	mux.HandleFunc("/data", func(w http.ResponseWriter, _ *http.Request) {
		slowMu.Lock()
		time.Sleep(mutexHoldSleep)
		row := rand.IntN(10_000) //nolint:gosec
		slowMu.Unlock()

		cached, _ := input.deps.cache.get(fmt.Sprintf("compute:%d", row%16))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"row": row, "cached": cached})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		total, ok, errored, avgMS := input.deps.stats.snapshot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": total, "ok": ok, "errors": errored,
			"avg_ms": fmt.Sprintf("%.2f", avgMS),
		})
	})

	srv := &http.Server{
		Addr:         input.addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			input.log.Error("HTTP server error", "err", err)
		}
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	for i := range 30 {
		resp, err := client.Get("http://" + input.addr + "/health")
		if err == nil {
			_ = resp.Body.Close()
			input.log.Info("HTTP server ready", "addr", input.addr)
			return srv
		}
		if i == 29 {
			panic("server never became ready at " + input.addr)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return srv
}

// ── HTTP workers ──────────────────────────────────────────────────────────────

type jobResult struct {
	hash string
	ms   int64
}

type workerInput struct {
	id     int
	jobs   <-chan int
	out    chan<- jobResult
	st     *stats
	client *http.Client
	addr   string
}

// runWorker fetches from the HTTP server then does heavy CPU post-processing.
// The cycle is: SYSCALL (HTTP) → RUNNING (cpuWork) → repeat.
func runWorker(ctx context.Context, input workerInput) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-input.jobs:
			if !ok {
				return
			}
			start := time.Now()

			var url string
			if job%3 == 0 {
				url = "http://" + input.addr + "/compute"
			} else {
				url = "http://" + input.addr + "/data"
			}

			resp, err := input.client.Get(url) //nolint:noctx
			ms := time.Since(start).Milliseconds()
			if err != nil {
				input.st.record(false, ms)
				continue
			}
			_ = resp.Body.Close()

			// Heavy CPU post-processing: two rounds for a longer RUNNING burst.
			n := httpClientWork + rand.IntN(httpClientWork/2) //nolint:gosec
			hash := cpuWork(n)
			_ = cpuWork(n / 2) // second pass

			input.st.record(true, ms)
			select {
			case input.out <- jobResult{hash: hash, ms: ms}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// ── Mutex contention pools ────────────────────────────────────────────────────

// runMutexPool runs mutexWorkersPerPool goroutines that compete for a single
// mutex.  Each holder does CPU work (RUNNING) then sleeps (WAITING) while
// holding the lock, making the others BLOCKED.
func runMutexPool(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	jobs := make(chan int, mutexJobsPerPool)
	var mu sync.Mutex
	var innerWG sync.WaitGroup

	for range mutexWorkersPerPool {
		innerWG.Add(1)
		go func() {
			defer innerWG.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-jobs:
					if !ok {
						return
					}
					mu.Lock()
					_ = cpuWork(mutexHoldWork) // RUNNING while holding lock
					time.Sleep(mutexHoldSleep) // WAITING while holding lock
					mu.Unlock()
				}
			}
		}()
	}

	for i := range mutexJobsPerPool {
		select {
		case <-ctx.Done():
			close(jobs)
			innerWG.Wait()
			return
		case jobs <- i:
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(jobs) // CC-1
	innerWG.Wait()
}

// ── Channel pipeline ──────────────────────────────────────────────────────────

// runPipeline runs a numPipelineStages-stage pipeline.  Each stage worker does
// CPU work per item (RUNNING) then blocks on the next channel (WAITING).
func runPipeline(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	chans := make([]chan int, numPipelineStages+1)
	for i := range chans {
		chans[i] = make(chan int, pipelineWorkers*2)
	}

	var stageWGs [numPipelineStages]sync.WaitGroup
	for s := range numPipelineStages {
		in, out := chans[s], chans[s+1]
		stageWGs[s].Add(pipelineWorkers) //nolint:gosec // s < numPipelineStages, array size matches
		for range pipelineWorkers {
			go func() {
				defer stageWGs[s].Done()
				for {
					select {
					case <-ctx.Done():
						return
					case v, ok := <-in:
						if !ok {
							return
						}
						_ = cpuWork(pipelineItemWork) // RUNNING
						select {
						case out <- v*2 + s:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
		}
		// CC-1: close output when all workers of this stage exit.
		go func(swg *sync.WaitGroup, ch chan int) {
			swg.Wait()
			close(ch)
		}(&stageWGs[s], out)
	}

	// Producer.
	go func() {
		for i := range numPipelineItems {
			select {
			case <-ctx.Done():
				close(chans[0])
				return
			case chans[0] <- i:
			}
		}
		close(chans[0]) // CC-1
	}()

	// Drain sink so workers are never blocked sending.
	for range chans[numPipelineStages] {
	}
}

// ── Continuous CPU spinners ───────────────────────────────────────────────────

// runCPUSpin loops forever doing heavy computation.
// These goroutines are almost always in RUNNING state, filling the timeline
// with solid green.
func runCPUSpin(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_ = cpuWork(40_000 + rand.IntN(20_000)) //nolint:gosec
		}
	}
}

// ── Aggregator ────────────────────────────────────────────────────────────────

type aggregator struct {
	mu      sync.RWMutex
	topHash map[string]int
}

func newAggregator() *aggregator {
	return &aggregator{topHash: make(map[string]int, 128)}
}

func (a *aggregator) run(ctx context.Context, results <-chan jobResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case r, ok := <-results:
			if !ok {
				return
			}
			a.mu.Lock()
			a.topHash[r.hash[:8]]++
			a.mu.Unlock()
		}
	}
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	stopTrace, err := agent.StartFromEnv()
	if err != nil {
		log.Error("start goroscope agent", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := stopTrace(); err != nil {
			log.Error("stop goroscope agent", "err", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := newCache(128)
	st := &stats{}

	// ── HTTP server ───────────────────────────────────────────────────────────
	srv := startServer(startServerInput{addr: httpAddr, deps: serverDeps{cache: c, stats: st}, log: log})
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// ── Goroutine leak ────────────────────────────────────────────────────────
	leaked := make(chan struct{})
	go func() {
		log.Info("[leak] goroutine waiting forever — will appear as LEAKED in UI")
		<-leaked
	}()

	// ── Continuous CPU spinners ───────────────────────────────────────────────
	// These fill the timeline with solid RUNNING (green) blocks.
	for range numCPUSpin {
		go runCPUSpin(ctx)
	}

	// ── Health checker ────────────────────────────────────────────────────────
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		client := &http.Client{Timeout: time.Second}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				resp, err := client.Get("http://" + httpAddr + "/health")
				if err == nil {
					_ = resp.Body.Close()
				}
			}
		}
	}()

	// ── Stats reporter ────────────────────────────────────────────────────────
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				total, ok, errored, avgMS := st.snapshot()
				log.Info("stats", "total", total, "ok", ok, "errors", errored, "avg_ms", fmt.Sprintf("%.1f", avgMS))
			}
		}
	}()

	// ── Mutex contention pools ────────────────────────────────────────────────
	var poolWG sync.WaitGroup
	poolWG.Add(numMutexPools)
	for range numMutexPools {
		go runMutexPool(ctx, &poolWG)
	}

	// ── Channel pipeline ──────────────────────────────────────────────────────
	var pipeWG sync.WaitGroup
	pipeWG.Add(1)
	go runPipeline(ctx, &pipeWG)

	// ── HTTP worker pool ──────────────────────────────────────────────────────
	jobs := make(chan int, numJobs)
	results := make(chan jobResult, numHTTPWorkers*4)
	client := &http.Client{Timeout: 30 * time.Second}

	var workerWG sync.WaitGroup
	for w := range numHTTPWorkers {
		workerWG.Add(1)
		go func(id int) {
			defer workerWG.Done()
			runWorker(ctx, workerInput{
				id: id, jobs: jobs, out: results,
				st: st, client: client, addr: httpAddr,
			})
		}(w)
	}

	// Job producer — no sleep: keep workers maximally busy. CC-1: sender closes.
	go func() {
		for i := range numJobs {
			select {
			case <-ctx.Done():
				close(jobs)
				return
			case jobs <- i:
			}
		}
		close(jobs) // CC-1
	}()

	go func() {
		workerWG.Wait()
		close(results) // CC-1
	}()

	// ── Aggregator ────────────────────────────────────────────────────────────
	agg := newAggregator()
	var aggDone sync.WaitGroup
	aggDone.Add(1)
	go func() {
		defer aggDone.Done()
		agg.run(ctx, results)
	}()

	// Wait for HTTP workers and aggregator to finish, then pools and pipeline.
	aggDone.Wait()
	poolWG.Wait()
	pipeWG.Wait()

	total, ok, errored, avgMS := st.snapshot()
	log.Info("rich demo complete",
		"total", total, "ok", ok, "errors", errored,
		"avg_ms", fmt.Sprintf("%.1f", avgMS),
	)
}
