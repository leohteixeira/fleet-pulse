package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	writeRateLimit    = 6
	writeRateWindow   = 60 * time.Second
	contractInterval  = 20 * time.Second
	codeContractBusy  = "contract_busy"
	codeRateLimited   = "rate_limited"
	codeStreamFull    = "stream_full"
	maxStreamClients  = 64
	streamFullMessage = "too many stream connections"
	busyMessage       = "aguarde antes de outra acao neste contrato"
	rateMessage       = "limite de escritas por IP atingido"
)

type writeGuards struct {
	mu        sync.Mutex
	ipHits    map[string][]time.Time
	contracts map[string]time.Time
	now       func() time.Time
}

func newWriteGuards() *writeGuards {
	return &writeGuards{
		ipHits:    make(map[string][]time.Time),
		contracts: make(map[string]time.Time),
		now:       time.Now,
	}
}

type guardReject struct {
	status     int
	retryAfter int
	code       string
	message    string
}

func (g *writeGuards) checkRate(ip string) *guardReject {
	if g == nil {
		return nil
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()

	cutoff := now.Add(-writeRateWindow)
	hits := pruneTimes(g.ipHits[ip], cutoff)
	if len(hits) >= writeRateLimit {
		retry := secondsUntil(hits[0].Add(writeRateWindow), now)
		return &guardReject{
			status:     http.StatusTooManyRequests,
			retryAfter: retry,
			code:       codeRateLimited,
			message:    rateMessage,
		}
	}
	g.ipHits[ip] = append(hits, now)
	return nil
}

func (g *writeGuards) reserveInterval(contractID string) (prev time.Time, had bool, rej *guardReject) {
	if g == nil || contractID == "" {
		return time.Time{}, false, nil
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()

	last, ok := g.contracts[contractID]
	if ok {
		until := last.Add(contractInterval)
		if now.Before(until) {
			return last, ok, &guardReject{
				status:     http.StatusConflict,
				retryAfter: secondsUntil(until, now),
				code:       codeContractBusy,
				message:    busyMessage,
			}
		}
	}
	g.contracts[contractID] = now
	return last, ok, nil
}

func (g *writeGuards) releaseInterval(contractID string, prev time.Time, had bool) {
	if g == nil || contractID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if had {
		g.contracts[contractID] = prev
		return
	}
	delete(g.contracts, contractID)
}

func pruneTimes(in []time.Time, cutoff time.Time) []time.Time {
	out := make([]time.Time, 0, len(in))
	for _, t := range in {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

func secondsUntil(deadline, now time.Time) int {
	sec := int(math.Ceil(deadline.Sub(now).Seconds()))
	if sec < 1 {
		return 1
	}
	return sec
}

func visitorIP(remoteAddr, forwardedFor string, trustForwarded bool) string {
	ip := stripHostPort(remoteAddr)
	if trustForwarded {
		if hop := firstForwardedHop(forwardedFor); hop != "" {
			ip = hop
		}
	}
	return ip
}

func writeGuardReject(w http.ResponseWriter, rej *guardReject) {
	if rej.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(rej.retryAfter))
	}
	if rej.status == http.StatusConflict {
		if err := writeJSON(w, http.StatusConflict, contractBusyBody{
			Code:       rej.code,
			Message:    rej.message,
			RetryAfter: rej.retryAfter,
		}); err != nil {
			return
		}
		return
	}
	if err := writeJSON(w, rej.status, contractBusyBody{
		Code:       rej.code,
		Message:    rej.message,
		RetryAfter: rej.retryAfter,
	}); err != nil {
		return
	}
}

type contractBusyBody struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retryAfter"`
}
