package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestChallengeContract(t *testing.T) {
	valid := fmt.Sprintf(`<script id="anubis_challenge" type="application/json">{"rules":{"algorithm":"fast","difficulty":5},"challenge":{"id":"test-id","randomData":"%s","method":"fast","spent":false}}</script>`, strings.Repeat("a", 128))
	for _, tt := range []struct {
		name, body string
		difficulty int
		wantError  bool
	}{
		{"valid", valid, 5, false},
		{"wrong difficulty", valid, 4, true},
		{"wrong algorithm", strings.ReplaceAll(valid, `"fast"`, `"sha256"`), 5, true},
		{"spent challenge", strings.ReplaceAll(valid, `"spent":false`, `"spent":true`), 5, true},
		{"backend bypass", backend, 5, true},
		{"malformed JSON", strings.Replace(valid, `"rules"`, `rules`, 1), 5, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseChallenge([]byte(tt.body), tt.difficulty)
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v wantError=%v", err, tt.wantError)
			}
		})
	}
}
func TestPercentiles(t *testing.T) {
	v := []float64{3, 1, 2, 100}
	if percentile(v, .5) != 2 || percentile(v, .95) != 100 || percentile(nil, .99) != 0 {
		t.Fatal("unexpected nearest-rank percentiles")
	}
}
