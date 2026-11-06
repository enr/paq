package httperr

import (
	"net/http"
	"testing"
)

func TestRateLimited(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		header map[string]string
		want   bool
	}{
		{"429", http.StatusTooManyRequests, nil, true},
		{"403 primary limit", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, true},
		{"403 secondary limit", http.StatusForbidden, map[string]string{"Retry-After": "60"}, true},
		{"403 with requests left", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "42"}, false},
		{"403 from a proxy", http.StatusForbidden, nil, false},
		{"404", http.StatusNotFound, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.code, Header: http.Header{}}
			for k, v := range tc.header {
				resp.Header.Set(k, v)
			}
			if got := RateLimited(resp); got != tc.want {
				t.Errorf("RateLimited = %v, want %v", got, tc.want)
			}
		})
	}
}
