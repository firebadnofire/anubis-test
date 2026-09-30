package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

type record struct {
	Client  int     `json:"client"`
	Status  string  `json:"status"`
	SolveMS float64 `json:"solve_ms"`
	TotalMS float64 `json:"total_ms"`
	Error   string  `json:"error,omitempty"`
}
type report struct {
	Difficulty          int       `json:"difficulty"`
	Concurrency         int       `json:"concurrency"`
	Started             time.Time `json:"started"`
	ElapsedSeconds      float64   `json:"elapsed_seconds"`
	Hashes              uint64    `json:"hashes"`
	GPUSeconds          float64   `json:"gpu_batch_seconds"`
	GPUHashesPerSecond  float64   `json:"gpu_hashes_per_second"`
	WallHashesPerSecond float64   `json:"wall_hashes_per_second"`
	Accepted            int       `json:"accepted"`
	Rejected            int       `json:"rejected"`
	Timeouts            int       `json:"timeouts"`
	Failures            int       `json:"failures"`
	Unfinished          int       `json:"unfinished"`
	AcceptedPerSecond   float64   `json:"accepted_per_second"`
	SolveP50MS          float64   `json:"solve_p50_ms"`
	SolveP95MS          float64   `json:"solve_p95_ms"`
	SolveP99MS          float64   `json:"solve_p99_ms"`
	TotalP50MS          float64   `json:"total_p50_ms"`
	TotalP95MS          float64   `json:"total_p95_ms"`
	TotalP99MS          float64   `json:"total_p99_ms"`
	Records             []record  `json:"-"`
}
type solveRequest struct {
	j       gpuJob
	reply   chan solveReply
	started time.Time
}
type solveReply struct {
	result  gpuResult
	elapsed time.Duration
	err     error
}

func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	return v[int(math.Ceil(float64(len(v))*p))-1]
}
func saveReport(dir string, r *report) error {
	if e := os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "summary.json"), b, 0644); e != nil {
		return e
	}
	f, e := os.Create(filepath.Join(dir, "sessions.csv"))
	if e != nil {
		return e
	}
	w := csv.NewWriter(f)
	e = w.Write([]string{"client", "status", "solve_ms", "total_ms", "error"})
	if e != nil {
		f.Close()
		return e
	}
	for _, v := range r.Records {
		if e = w.Write([]string{strconv.Itoa(v.Client), v.Status, fmt.Sprintf("%.3f", v.SolveMS), fmt.Sprintf("%.3f", v.TotalMS), v.Error}); e != nil {
			f.Close()
			return e
		}
	}
	w.Flush()
	if e = w.Error(); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
func bench(parent context.Context, g *gpu, difficulty, clients int, duration time.Duration, dir string) error {
	ctx, cancel := context.WithTimeout(parent, duration)
	defer cancel()
	r := report{Difficulty: difficulty, Concurrency: clients, Started: time.Now().UTC()}
	transport := &http.Transport{MaxIdleConns: 128, MaxIdleConnsPerHost: 64, MaxConnsPerHost: 64}
	defer transport.CloseIdleConnections()
	requests := make(chan *solveRequest, 32)
	var mu sync.Mutex
	var fatal error
	var wg sync.WaitGroup
	add := func(v record, e error) {
		mu.Lock()
		defer mu.Unlock()
		r.Records = append(r.Records, v)
		if e != nil && fatal == nil {
			fatal = e
			cancel()
		}
	}
	for id := 0; id < clients; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for ctx.Err() == nil {
				start := time.Now()
				v := record{Client: id}
				c := newClient(transport)
				p, e := issue(ctx, c, difficulty)
				var solve time.Duration
				if e == nil {
					j, _ := job(p.Challenge.Data, difficulty, 0)
					q := &solveRequest{j: j, reply: make(chan solveReply, 1), started: time.Now()}
					select {
					case requests <- q:
					case <-ctx.Done():
						e = ctx.Err()
					}
					if e == nil {
						select {
						case ans := <-q.reply:
							e = ans.err
							solve = ans.elapsed
							if e == nil {
								e = submit(ctx, c, proofPath(p, ans.result, solve))
							}
						case <-ctx.Done():
							e = ctx.Err()
						}
					}
				}
				v.SolveMS = float64(solve) / float64(time.Millisecond)
				v.TotalMS = float64(time.Since(start)) / float64(time.Millisecond)
				if e == nil {
					v.Status = "accepted"
					add(v, nil)
				} else if ctx.Err() != nil {
					v.Status = "unfinished"
					v.Error = e.Error()
					add(v, nil)
					return
				} else {
					v.Status = "failure"
					var n net.Error
					if errors.As(e, &n) && n.Timeout() {
						v.Status = "timeout"
					}
					if len(e.Error()) >= 14 && e.Error()[:14] == "proof rejected" {
						v.Status = "rejected"
					}
					v.Error = e.Error()
					add(v, e)
					return
				}
			}
		}(id)
	}
	active := make([]*solveRequest, 0, 32)
	lastProgress := time.Now()
	for ctx.Err() == nil {
		if len(active) == 0 {
			select {
			case q := <-requests:
				active = append(active, q)
			case <-ctx.Done():
				continue
			}
		}
	drain:
		for len(active) < 32 {
			select {
			case q := <-requests:
				active = append(active, q)
			default:
				break drain
			}
		}
		jobs := make([]gpuJob, len(active))
		for i, q := range active {
			jobs[i] = q.j
		}
		start := time.Now()
		results, e := g.run(jobs, 65536)
		r.GPUSeconds += time.Since(start).Seconds()
		if e != nil {
			mu.Lock()
			fatal = e
			mu.Unlock()
			cancel()
			break
		}
		r.Hashes += uint64(len(jobs)) * 65536
		keep := active[:0]
		for i, q := range active {
			ans := results[i]
			if ans.Nonce == math.MaxUint64 {
				q.j.Base += 65536
				keep = append(keep, q)
				continue
			}
			e = verify(q.j, ans)
			q.reply <- solveReply{result: ans, elapsed: time.Since(q.started), err: e}
			if e != nil {
				mu.Lock()
				fatal = e
				mu.Unlock()
				cancel()
				break
			}
		}
		active = keep
		if time.Since(lastProgress) >= 10*time.Second {
			mu.Lock()
			n := 0
			for _, v := range r.Records {
				if v.Status == "accepted" {
					n++
				}
			}
			mu.Unlock()
			fmt.Printf("difficulty=%d elapsed=%.0fs accepted=%d active=%d GPU=%.2f MH/s\n", difficulty, time.Since(r.Started).Seconds(), n, len(active), float64(r.Hashes)/r.GPUSeconds/1e6)
			lastProgress = time.Now()
		}
	}
	cancel()
	wg.Wait()
	r.ElapsedSeconds = time.Since(r.Started).Seconds()
	var solves, totals []float64
	for _, v := range r.Records {
		switch v.Status {
		case "accepted":
			r.Accepted++
			solves = append(solves, v.SolveMS)
			totals = append(totals, v.TotalMS)
		case "unfinished":
			r.Unfinished++
		case "timeout":
			r.Timeouts++
		case "rejected":
			r.Rejected++
		default:
			r.Failures++
		}
	}
	if r.GPUSeconds > 0 {
		r.GPUHashesPerSecond = float64(r.Hashes) / r.GPUSeconds
	}
	r.WallHashesPerSecond = float64(r.Hashes) / r.ElapsedSeconds
	r.AcceptedPerSecond = float64(r.Accepted) / r.ElapsedSeconds
	r.SolveP50MS = percentile(solves, .5)
	r.SolveP95MS = percentile(solves, .95)
	r.SolveP99MS = percentile(solves, .99)
	r.TotalP50MS = percentile(totals, .5)
	r.TotalP95MS = percentile(totals, .95)
	r.TotalP99MS = percentile(totals, .99)
	if e := saveReport(dir, &r); e != nil {
		return e
	}
	fmt.Printf("Stage complete: accepted=%d unfinished=%d wall=%.2f MH/s reports=%s\n", r.Accepted, r.Unfinished, r.WallHashesPerSecond/1e6, dir)
	if fatal != nil {
		return fatal
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	return nil
}
func warmup(ctx context.Context, g *gpu, duration time.Duration) error {
	start := time.Now()
	jobs := make([]gpuJob, 32)
	for i := range jobs {
		jobs[i], _ = job(fmt.Sprintf("%0128d", i), 8, 0)
	}
	var hashes uint64
	for time.Since(start) < duration {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, e := g.run(jobs, 65536)
		if e != nil {
			return e
		}
		hashes += 32 * 65536
		for i := range jobs {
			jobs[i].Base += 65536
		}
	}
	r := map[string]any{"hashes": hashes, "seconds": time.Since(start).Seconds(), "hashes_per_second": float64(hashes) / time.Since(start).Seconds()}
	b, _ := json.Marshal(r)
	fmt.Printf("GPU warmup %s\n", b)
	return nil
}
func run() error {
	mode := flag.String("mode", "selftest", "selftest, smoke, warmup, bench")
	d := flag.Int("difficulty", 4, "fast difficulty 4..8")
	clients := flag.Int("clients", 32, "independent sessions 1..32")
	seconds := flag.Int("seconds", 600, "bounded duration")
	out := flag.String("out", "reports/manual", "report directory")
	flag.Parse()
	if *d < 4 || *d > 8 || *clients < 1 || *clients > 32 || *seconds < 1 || *seconds > 3600 {
		return fmt.Errorf("invalid difficulty, clients or duration")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	g, e := openGPU()
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch *mode {
	case "selftest":
		e = selftest(g)
	case "smoke":
		s, c := context.WithTimeout(ctx, 120*time.Second)
		defer c()
		e = smoke(s, g, *d)
	case "warmup":
		e = warmup(ctx, g, time.Duration(*seconds)*time.Second)
	case "bench":
		e = bench(ctx, g, *d, *clients, time.Duration(*seconds)*time.Second, *out)
	default:
		e = fmt.Errorf("unknown mode %q", *mode)
	}
	closeErr := g.shutdown()
	if e != nil {
		return e
	}
	return closeErr
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "Benchmark stopped:", e)
		os.Exit(1)
	}
}
