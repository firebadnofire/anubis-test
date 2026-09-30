package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const target = "http://127.0.0.1:18080"
const backend = "ANUBIS_BENCH_BACKEND_OK\n"
const passPath = "/.within.website/x/cmd/anubis/api/pass-challenge"

var challengeScript = regexp.MustCompile(`(?s)<script id="anubis_challenge" type="application/json">(.*?)</script>`)
var authCookieName = regexp.MustCompile(`^techaro\.lol-anubis-auth-[0-9a-f]{8}$`)

type challengePage struct {
	Rules struct {
		Algorithm  string `json:"algorithm"`
		Difficulty int    `json:"difficulty"`
	} `json:"rules"`
	Challenge struct {
		ID     string `json:"id"`
		Data   string `json:"randomData"`
		Method string `json:"method"`
		Spent  bool   `json:"spent"`
	} `json:"challenge"`
}

func newClient(t *http.Transport) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Transport: t, Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func get(ctx context.Context, c *http.Client, path string) (int, []byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", target+path, nil)
	if e != nil {
		return 0, nil, e
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 Anubis4090LocalBenchmark/1.0")
	resp, e := c.Do(req)
	if e != nil {
		return 0, nil, e
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if e != nil {
		return 0, nil, e
	}
	if len(b) > 1024*1024 {
		return 0, nil, fmt.Errorf("response exceeded 1 MiB")
	}
	return resp.StatusCode, b, nil
}
func parseChallenge(b []byte, difficulty int) (challengePage, error) {
	var p challengePage
	m := challengeScript.FindSubmatch(b)
	if m == nil {
		return p, fmt.Errorf("fresh session did not receive challenge")
	}
	if e := json.Unmarshal(m[1], &p); e != nil {
		return p, e
	}
	if p.Rules.Algorithm != "fast" || p.Challenge.Method != "fast" || p.Rules.Difficulty != difficulty || p.Challenge.Spent || p.Challenge.ID == "" || len(p.Challenge.Data) != 128 {
		return p, fmt.Errorf("unexpected challenge protocol/difficulty")
	}
	return p, nil
}
func issue(ctx context.Context, c *http.Client, difficulty int) (challengePage, error) {
	u, _ := url.Parse(target)
	if len(c.Jar.Cookies(u)) != 0 {
		return challengePage{}, fmt.Errorf("fresh session contains cookies")
	}
	status, b, e := get(ctx, c, "/")
	if e != nil {
		return challengePage{}, e
	}
	if status != 200 {
		return challengePage{}, fmt.Errorf("challenge HTTP %d", status)
	}
	return parseChallenge(b, difficulty)
}
func proofPath(p challengePage, r gpuResult, elapsed time.Duration) string {
	q := url.Values{"id": {p.Challenge.ID}, "nonce": {strconv.FormatUint(r.Nonce, 10)}, "response": {fmt.Sprintf("%x", r.Hash)}, "elapsedTime": {strconv.FormatFloat(float64(elapsed)/float64(time.Millisecond), 'f', 3, 64)}, "redir": {"/"}}
	return passPath + "?" + q.Encode()
}
func submit(ctx context.Context, c *http.Client, path string) error {
	status, b, e := get(ctx, c, path)
	if e != nil {
		return e
	}
	if status != 302 {
		return fmt.Errorf("proof rejected HTTP %d: %.160s", status, b)
	}
	u, _ := url.Parse(target)
	auth := false
	var names []string
	for _, ck := range c.Jar.Cookies(u) {
		names = append(names, ck.Name)
		if authCookieName.MatchString(ck.Name) && ck.Value != "" {
			auth = true
		}
	}
	if !auth {
		return fmt.Errorf("accepted proof missing authentication cookie (received names: %v)", names)
	}
	status, b, e = get(ctx, c, "/")
	if e != nil {
		return e
	}
	if status != 200 || string(b) != backend {
		return fmt.Errorf("proof did not reach known backend: HTTP %d", status)
	}
	return nil
}
func smoke(ctx context.Context, g *gpu, difficulty int) error {
	t := &http.Transport{}
	defer t.CloseIdleConnections()
	c := newClient(t)
	p, e := issue(ctx, c, difficulty)
	if e != nil {
		return e
	}
	j, _ := job(p.Challenge.Data, difficulty, 0)
	start := time.Now()
	var r gpuResult
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		results, e := g.run([]gpuJob{j}, 65536)
		if e != nil {
			return e
		}
		r = results[0]
		if r.Nonce != ^uint64(0) {
			break
		}
		j.Base += 65536
	}
	if e = verify(j, r); e != nil {
		return e
	}
	path := proofPath(p, r, time.Since(start))
	if e = submit(ctx, c, path); e != nil {
		return e
	}
	status, b, e := get(ctx, c, path)
	if e != nil {
		return e
	}
	if status == 302 || !strings.Contains(string(b), "double_spend") {
		return fmt.Errorf("replay was not explicitly rejected: HTTP %d", status)
	}
	c = newClient(t)
	p, e = issue(ctx, c, difficulty)
	if e != nil {
		return e
	}
	r.Hash = [32]byte{}
	status, b, e = get(ctx, c, proofPath(p, r, time.Millisecond))
	if e != nil {
		return e
	}
	if status == 302 || string(b) == backend || !strings.Contains(string(b), "invalid response") {
		return fmt.Errorf("invalid proof was not explicitly rejected: HTTP %d", status)
	}
	fmt.Printf("Difficulty %d smoke: GPU proof accepted, backend confirmed, invalid proof and replay rejected\n", difficulty)
	return nil
}
