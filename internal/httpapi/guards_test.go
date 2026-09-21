package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/book"
	"github.com/leohteixeira/fleet-pulse/internal/clock"
	"github.com/leohteixeira/fleet-pulse/internal/httpapi"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestHandler_WriteRateLimitAndReplay(t *testing.T) {
	t.Parallel()

	details := map[string]book.Detail{}
	for i := range 8 {
		id := fmt.Sprintf("c-%d", i)
		details[id] = book.Detail{Contract: book.Contract{ID: id, VIN: fmt.Sprintf("FPULSELSG000000%02d", i+1), DaysLate: 4}}
	}
	bk := &scriptBook{details: details}
	h := leasingHandler(t, leasingOpts{book: bk, keys: httpapiKeys()})

	const ip = "203.0.113.80:9"
	var first *httptest.ResponseRecorder
	for i := range 6 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-"+strconv.Itoa(i)+"/notify", nil)
		req.RemoteAddr = ip
		req.Header.Set("Idempotency-Key", "rate-"+strconv.Itoa(i))
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("write %d status = %d body=%s", i+1, rec.Code, rec.Body.Bytes())
		}
		if i == 0 {
			first = rec
		}
	}

	seventh := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-6/notify", nil)
	req.RemoteAddr = ip
	req.Header.Set("Idempotency-Key", "rate-6")
	h.ServeHTTP(seventh, req)
	if seventh.Code != http.StatusTooManyRequests {
		t.Fatalf("7th write status = %d body=%s, want 429", seventh.Code, seventh.Body.Bytes())
	}
	retry, err := strconv.Atoi(seventh.Header().Get("Retry-After"))
	if err != nil || retry < 1 {
		t.Fatalf("Retry-After = %q, want positive integer", seventh.Header().Get("Retry-After"))
	}

	replay := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-0/notify", nil)
	req.RemoteAddr = ip
	req.Header.Set("Idempotency-Key", "rate-0")
	h.ServeHTTP(replay, req)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d body=%s, want 200", replay.Code, replay.Body.Bytes())
	}
	if replay.Body.String() != first.Body.String() {
		t.Fatalf("replay body %q != first %q", replay.Body.String(), first.Body.String())
	}
	if bk.notifyCount != 6 {
		t.Fatalf("notify writes = %d, want 6 (7th must have no side effect)", bk.notifyCount)
	}

	assertRentalSnapshot(t, h)
}

func TestHandler_ContractBusy(t *testing.T) {
	t.Parallel()

	bk := &scriptBook{details: map[string]book.Detail{
		"c-busy": {Contract: book.Contract{ID: "c-busy", VIN: "FPULSELSG00000011", DaysLate: 8}},
	}}
	h := leasingHandler(t, leasingOpts{book: bk, keys: httpapiKeys()})

	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-busy/notify", nil)
	req.Header.Set("Idempotency-Key", "busy-1")
	h.ServeHTTP(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.Bytes())
	}

	second := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-busy/notify", nil)
	req.Header.Set("Idempotency-Key", "busy-2")
	h.ServeHTTP(second, req)
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d body=%s, want 409", second.Code, second.Body.Bytes())
	}
	var body struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		RetryAfter int    `json:"retryAfter"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "contract_busy" || body.Message == "" || body.RetryAfter < 1 {
		t.Fatalf("busy body = %+v", body)
	}

	replay := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-busy/notify", nil)
	req.Header.Set("Idempotency-Key", "busy-1")
	h.ServeHTTP(replay, req)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200", replay.Code)
	}
	if bk.notifyCount != 1 {
		t.Fatalf("notify writes = %d, want 1 after busy+replay", bk.notifyCount)
	}
}

func TestHandler_ContractBusyConcurrent(t *testing.T) {
	t.Parallel()

	bk := &scriptBook{details: map[string]book.Detail{
		"c-race": {Contract: book.Contract{ID: "c-race", VIN: "FPULSELSG00000021", DaysLate: 8}},
	}}
	h := leasingHandler(t, leasingOpts{book: bk, keys: httpapiKeys()})

	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-race/notify", nil)
			req.Header.Set("Idempotency-Key", fmt.Sprintf("race-%d", i))
			h.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	ok, busy := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			busy++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if ok != 1 || busy != n-1 {
		t.Fatalf("status counts ok=%d busy=%d codes=%v, want 1 and %d", ok, busy, codes, n-1)
	}
	if bk.notifyCount != 1 {
		t.Fatalf("notify writes = %d, want 1", bk.notifyCount)
	}
}

func TestHandler_StreamCap(t *testing.T) {
	t.Parallel()

	hub := httpapi.NewHub()
	h := httpapi.New(fakeStore{snap: seededSnapshot()}, hub, nil, nil).Handler()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	t.Cleanup(cancel)
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}

	type conn struct {
		resp *http.Response
	}
	miss := httptest.NewRecorder()
	h.ServeHTTP(miss, httptest.NewRequest(http.MethodGet, "/api/stream", nil))
	if miss.Code != http.StatusBadRequest {
		t.Fatalf("missing fleet status = %d, want 400", miss.Code)
	}

	open := make([]conn, 0, 64)
	t.Cleanup(func() {
		for _, c := range open {
			_ = c.resp.Body.Close()
		}
	})

	for i := range 64 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream?fleet=rental", nil)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET stream %d: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream %d status = %d, want 200", i, resp.StatusCode)
		}
		open = append(open, conn{resp: resp})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream?fleet=leasing", nil)
	if err != nil {
		t.Fatalf("65th request: %v", err)
	}
	extra, err := client.Do(req)
	if err != nil {
		t.Fatalf("65th GET: %v", err)
	}
	t.Cleanup(func() { _ = extra.Body.Close() })
	if extra.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(extra.Body)
		t.Fatalf("65th status = %d body=%s, want 429", extra.StatusCode, body)
	}
	var refused struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(extra.Body).Decode(&refused); err != nil {
		t.Fatalf("decode 65th: %v", err)
	}
	if refused.Code != "stream_full" || refused.Message == "" {
		t.Fatalf("65th body = %+v", refused)
	}

	hub.Publish(httpapi.Event{
		Name:  "telemetry",
		Fleet: httpapi.FleetRental,
		Data:  []byte(`{"vin":"FPULSESAO00000001"}`),
	})
	got, err := readSSE(open[0].resp.Body)
	if err != nil {
		t.Fatalf("original client read: %v", err)
	}
	if !strings.Contains(got.data, "FPULSESAO00000001") {
		t.Fatalf("original client missed event: %q", got.data)
	}
	gotLast, err := readSSE(open[len(open)-1].resp.Body)
	if err != nil {
		t.Fatalf("64th client read: %v", err)
	}
	if !strings.Contains(gotLast.data, "FPULSESAO00000001") {
		t.Fatalf("64th client missed event: %q", gotLast.data)
	}

	assertRentalSnapshot(t, h)
}

func TestHandler_SameOriginCORS(t *testing.T) {
	t.Parallel()

	bk := &scriptBook{details: map[string]book.Detail{
		"c1": {Contract: book.Contract{ID: "c1", VIN: "FPULSELSG00000001", DaysLate: 3}},
	}}
	h := leasingHandler(t, leasingOpts{book: bk, keys: httpapiKeys()})

	ok := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c1/notify", nil)
	req.Host = "demo.local:8300"
	req.Header.Set("Origin", "http://demo.local:8300")
	req.Header.Set("Idempotency-Key", "cors-1")
	h.ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("same-origin status = %d body=%s", ok.Code, ok.Body.Bytes())
	}
	if got := ok.Header().Get("Access-Control-Allow-Origin"); got != "http://demo.local:8300" {
		t.Fatalf("Allow-Origin = %q", got)
	}

	deny := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c1/notify", nil)
	req.Host = "demo.local:8300"
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Idempotency-Key", "cors-2")
	h.ServeHTTP(deny, req)
	if deny.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", deny.Code)
	}

	portless := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c1/notify", nil)
	req.Host = "demo.local:8300"
	req.Header.Set("Origin", "http://demo.local")
	req.Header.Set("Idempotency-Key", "cors-port")
	h.ServeHTTP(portless, req)
	if portless.Code != http.StatusForbidden {
		t.Fatalf("portless origin status = %d, want 403", portless.Code)
	}

	pre := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodOptions, "/api/contracts/c1/notify", nil)
	req.Host = "demo.local:8300"
	req.Header.Set("Origin", "http://demo.local:8300")
	h.ServeHTTP(pre, req)
	if pre.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", pre.Code)
	}
	if !strings.Contains(pre.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key") {
		t.Fatalf("preflight headers = %q", pre.Header().Get("Access-Control-Allow-Headers"))
	}
	if !strings.Contains(pre.Header().Get("Access-Control-Allow-Headers"), "Content-Type") {
		t.Fatalf("preflight missing Content-Type: %q", pre.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestHandler_VehiclesStayRentalAfterGuards(t *testing.T) {
	t.Parallel()

	h := leasingHandler(t, leasingOpts{
		book:  &scriptBook{details: map[string]book.Detail{}},
		clock: clock.Fixed(clock.ParseRate(clock.Rate4h), clock.Origin, clock.Origin),
	})
	assertRentalSnapshot(t, h)
}

func assertRentalSnapshot(t *testing.T, h http.Handler) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/vehicles", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("vehicles status = %d", rec.Code)
	}
	var snap store.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(snap.Vehicles) != sim.FleetSize {
		t.Fatalf("rental snapshot = %d, want %d", len(snap.Vehicles), sim.FleetSize)
	}
	for _, v := range snap.Vehicles {
		if strings.HasPrefix(v.VIN, "FPULSELSG") {
			t.Fatalf("rental snapshot has leasing vin %q", v.VIN)
		}
	}
}
