package main

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type gpuJob struct {
	Data               [256]byte
	Base               uint64
	Length, Difficulty uint32
}
type gpuResult struct {
	Nonce uint64
	Hash  [32]byte
}
type gpu struct {
	dll                     *syscall.DLL
	batch, close, errorProc *syscall.Proc
}

func openGPU() (*gpu, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dll, err := syscall.LoadDLL(filepath.Join(filepath.Dir(exe), "anubis_cuda.dll"))
	if err != nil {
		return nil, err
	}
	g := &gpu{dll: dll}
	init, e := dll.FindProc("GPUInit")
	if e != nil {
		return nil, e
	}
	g.batch, e = dll.FindProc("GPUBatch")
	if e != nil {
		return nil, e
	}
	g.close, e = dll.FindProc("GPUClose")
	if e != nil {
		return nil, e
	}
	g.errorProc, e = dll.FindProc("GPUError")
	if e != nil {
		return nil, e
	}
	status, _, _ := init.Call()
	if status != 0 {
		return nil, g.err()
	}
	return g, nil
}
func (g *gpu) err() error {
	var buffer [256]byte
	status, _, _ := g.errorProc.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	if status != 0 {
		return fmt.Errorf("CUDA failed without error text")
	}
	length := 0
	for length < len(buffer) && buffer[length] != 0 {
		length++
	}
	return fmt.Errorf("CUDA: %s", buffer[:length])
}
func (g *gpu) shutdown() error {
	status, _, _ := g.close.Call()
	if status != 0 {
		return g.err()
	}
	return g.dll.Release()
}
func (g *gpu) run(jobs []gpuJob, attempts uint32) ([]gpuResult, error) {
	if len(jobs) == 0 || len(jobs) > 32 || attempts == 0 || attempts > 262144 {
		return nil, fmt.Errorf("invalid GPU batch")
	}
	results := make([]gpuResult, len(jobs))
	status, _, _ := g.batch.Call(uintptr(unsafe.Pointer(&jobs[0])), uintptr(len(jobs)), uintptr(attempts), uintptr(unsafe.Pointer(&results[0])))
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(results)
	if status != 0 {
		return nil, g.err()
	}
	return results, nil
}
func job(data string, difficulty int, base uint64) (gpuJob, error) {
	var j gpuJob
	if len(data) > 256 || difficulty < 0 || difficulty > 8 {
		return j, fmt.Errorf("unsupported challenge length/difficulty")
	}
	copy(j.Data[:], data)
	j.Length = uint32(len(data))
	j.Difficulty = uint32(difficulty)
	j.Base = base
	return j, nil
}
func verify(j gpuJob, r gpuResult) error {
	h := sha256.Sum256([]byte(string(j.Data[:j.Length]) + strconv.FormatUint(r.Nonce, 10)))
	if h != r.Hash || !strings.HasPrefix(fmt.Sprintf("%x", h), strings.Repeat("0", int(j.Difficulty))) {
		return fmt.Errorf("CPU/GPU proof mismatch at nonce %d", r.Nonce)
	}
	return nil
}
func selftest(g *gpu) error {
	if unsafe.Sizeof(gpuJob{}) != 272 || unsafe.Sizeof(gpuResult{}) != 40 {
		return fmt.Errorf("GPU ABI size mismatch")
	}
	cases := 0
	for _, length := range []int{0, 1, 54, 55, 56, 63, 64, 65, 119, 120, 127, 128, 129, 255, 256} {
		for _, base := range []uint64{0, 9, 10, 99, 100, 999, 1000, 999999999, 1000000000, math.MaxInt64 - 262144} {
			j, _ := job(strings.Repeat("a", length), 0, base)
			r, e := g.run([]gpuJob{j}, 1)
			if e != nil {
				return e
			}
			if r[0].Nonce != base {
				return fmt.Errorf("nonce mismatch")
			}
			if e = verify(j, r[0]); e != nil {
				return e
			}
			cases++
		}
	}
	// Compare the entire search interval against a separate CPU implementation.
	jobs := make([]gpuJob, 32)
	for i := range jobs {
		jobs[i], _ = job(strings.Repeat(strconv.Itoa(i%10), 128), i%9, uint64(i*10000))
	}
	results, e := g.run(jobs, 4096)
	if e != nil {
		return e
	}
	for i, j := range jobs {
		want := uint64(math.MaxUint64)
		for n := j.Base; n < j.Base+4096; n++ {
			h := sha256.Sum256([]byte(string(j.Data[:j.Length]) + strconv.FormatUint(n, 10)))
			if strings.HasPrefix(fmt.Sprintf("%x", h), strings.Repeat("0", int(j.Difficulty))) {
				want = n
				break
			}
		}
		if results[i].Nonce != want {
			return fmt.Errorf("search mismatch for difficulty %d: got %d want %d", j.Difficulty, results[i].Nonce, want)
		}
		if want != math.MaxUint64 {
			if e = verify(j, results[i]); e != nil {
				return e
			}
		}
		cases++
	}
	fmt.Printf("CPU/GPU correctness: %d cases passed\n", cases)
	return nil
}
