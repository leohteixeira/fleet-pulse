package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/command"
	"github.com/leohteixeira/fleet-pulse/internal/httpapi"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

type fakeStore struct {
	snap store.Snapshot
}

func (f fakeStore) Snapshot() store.Snapshot {
	return f.snap
}

func seededSnapshot() store.Snapshot {
	mem := store.New()
	fleet := sim.NewFleet()
	vehicles := make([]store.Vehicle, 0, len(fleet))
	for _, v := range fleet {
		vehicles = append(vehicles, store.Vehicle{
			VIN:       v.VIN,
			DisplayID: v.DisplayID,
		})
	}
	mem.Seed(vehicles)
	return mem.Snapshot()
}

func TestHandler_Snapshot(t *testing.T) {
	t.Parallel()

	h := httpapi.New(fakeStore{snap: seededSnapshot()}, httpapi.NewHub(), nil).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/vehicles", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var snap store.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.Polygon.South != -23.585 || snap.Polygon.North != -23.525 || snap.Polygon.West != -46.685 || snap.Polygon.East != -46.60 {
		t.Fatalf("polygon = %+v, want south=-23.585 north=-23.525 west=-46.685 east=-46.60", snap.Polygon)
	}
	if len(snap.Vehicles) != sim.FleetSize {
		t.Fatalf("len(vehicles) = %d, want %d", len(snap.Vehicles), sim.FleetSize)
	}
	var hasOffline bool
	for _, v := range snap.Vehicles {
		if v.VIN == "" || v.DisplayID == "" {
			t.Fatalf("vehicle missing vin or displayId: %+v", v)
		}
		if v.VIN == sim.OfflineVIN {
			hasOffline = true
		}
	}
	if !hasOffline {
		t.Fatalf("snapshot missing offline vin %q", sim.OfflineVIN)
	}
}

func TestHandler_Healthz(t *testing.T) {
	t.Parallel()

	h := httpapi.New(fakeStore{}, httpapi.NewHub(), nil).Handler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHandler_UnknownMethod(t *testing.T) {
	t.Parallel()

	h := httpapi.New(fakeStore{snap: seededSnapshot()}, httpapi.NewHub(), nil).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/vehicles", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandler_SSEEventAndNoGzip(t *testing.T) {
	t.Parallel()

	hub := httpapi.NewHub()
	srv := httptest.NewServer(httpapi.New(fakeStore{snap: seededSnapshot()}, hub, nil).Handler())
	t.Cleanup(srv.Close)

	ctx := t.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{
		Transport: &http.Transport{DisableCompression: true},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	if enc := resp.Header.Get("Content-Encoding"); strings.EqualFold(enc, "gzip") {
		t.Fatalf("content-encoding = %q, want no gzip", enc)
	}

	payload := []byte(`{"vin":"FPULSESAO00000001","lat":-23.55,"lng":-46.63}`)
	hub.Publish(httpapi.Event{Name: "telemetry", Data: payload})

	got, err := readSSE(resp.Body)
	if err != nil {
		t.Fatalf("read sse: %v", err)
	}
	if got.name != "telemetry" {
		t.Fatalf("event = %q, want telemetry", got.name)
	}
	if !strings.Contains(got.data, `"vin":"FPULSESAO00000001"`) {
		t.Fatalf("data %q missing vin", got.data)
	}
}

func TestHandler_Unlock(t *testing.T) {
	t.Parallel()

	const vin = "FPULSESAO00000001"

	tests := []struct {
		name       string
		vin        string
		key        string
		unlocker   *fakeUnlocker
		wantStatus int
		wantID     string
		wantState  string
	}{
		{
			name: "accepted",
			vin:  vin,
			key:  "k1",
			unlocker: &fakeUnlocker{
				rec: command.Record{ID: "cmd-1", State: command.StateSent, VIN: vin},
			},
			wantStatus: http.StatusAccepted,
			wantID:     "cmd-1",
			wantState:  command.StateSent,
		},
		{
			name:       "missing key",
			vin:        vin,
			key:        "",
			unlocker:   &fakeUnlocker{err: command.ErrMissingKey},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown vin",
			vin:        "UNKNOWN",
			key:        "k1",
			unlocker:   &fakeUnlocker{err: command.ErrUnknownVIN},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "in-flight conflict",
			vin:  vin,
			key:  "other",
			unlocker: &fakeUnlocker{err: &command.ConflictError{
				Current: command.Record{ID: "cmd-1", State: command.StateSent},
			}},
			wantStatus: http.StatusConflict,
			wantID:     "cmd-1",
			wantState:  command.StateSent,
		},
		{
			name: "publish failure still 202",
			vin:  vin,
			key:  "k-fail",
			unlocker: &fakeUnlocker{
				rec: command.Record{ID: "cmd-fail", State: command.StateFailed, VIN: vin},
				err: fmt.Errorf("publish command: broker down"),
			},
			wantStatus: http.StatusAccepted,
			wantID:     "cmd-fail",
			wantState:  command.StateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := httpapi.New(fakeStore{snap: seededSnapshot()}, httpapi.NewHub(), tt.unlocker).Handler()
			req := httptest.NewRequest(http.MethodPost, "/api/vehicles/"+tt.vin+"/unlock", nil)
			if tt.key != "" {
				req.Header.Set("Idempotency-Key", tt.key)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.unlocker.key != tt.key {
				t.Fatalf("unlocker key = %q, want %q", tt.unlocker.key, tt.key)
			}
			if tt.wantID == "" {
				return
			}
			var body struct {
				ID    string `json:"id"`
				State string `json:"state"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.ID != tt.wantID || body.State != tt.wantState {
				t.Fatalf("body = %+v, want id=%s state=%s", body, tt.wantID, tt.wantState)
			}
			if tt.unlocker.vin != tt.vin || tt.unlocker.key != tt.key {
				t.Fatalf("unlocker got vin=%q key=%q", tt.unlocker.vin, tt.unlocker.key)
			}
		})
	}
}

func TestHandler_UnlockWithMachine(t *testing.T) {
	t.Parallel()

	mem := store.New()
	mem.Seed([]store.Vehicle{{VIN: "FPULSESAO00000001", DisplayID: "V01"}})
	pub := &ackPublisher{}
	svc := command.New(pub, mem, nil)
	pub.svc = svc
	h := httpapi.New(mem, httpapi.NewHub(), svc).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/vehicles/FPULSESAO00000001/unlock", nil)
	req.Header.Set("Idempotency-Key", "machine-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusAccepted, rec.Body.Bytes())
	}
	var created struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Fatal("missing command id")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/commands/"+created.ID, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got command.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if got.State != command.StateAcked {
		t.Fatalf("state = %q, want ACKED", got.State)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/vehicles/FPULSESAO00000001/unlock", nil)
	req.Header.Set("Idempotency-Key", "machine-1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var replay struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &replay); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replay.ID != created.ID {
		t.Fatalf("replay id = %q, want %q", replay.ID, created.ID)
	}
}

type ackPublisher struct {
	svc *command.Service
}

func (a *ackPublisher) Publish(_ context.Context, _ string, payload []byte) error {
	var cmd struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &cmd); err != nil {
		return err
	}
	if a.svc != nil {
		a.svc.Apply(cmd.ID, true)
	}
	return nil
}

func TestHandler_GetCommand(t *testing.T) {
	t.Parallel()

	unlocker := &fakeUnlocker{rec: command.Record{
		ID:            "cmd-1",
		VIN:           "FPULSESAO00000001",
		Action:        command.ActionUnlock,
		State:         command.StateAcked,
		CorrelationID: "corr-1",
	}}
	h := httpapi.New(fakeStore{}, httpapi.NewHub(), unlocker).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/commands/cmd-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body command.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID != "cmd-1" || body.State != command.StateAcked || body.CorrelationID != "corr-1" {
		t.Fatalf("body = %+v", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/commands/missing", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHub_DropOldest(t *testing.T) {
	t.Parallel()

	hub := httpapi.NewHub()
	events, unsubscribe := hub.Subscribe()
	t.Cleanup(unsubscribe)

	const extra = 1
	const overflow = 32 + extra
	for i := range overflow {
		hub.Publish(httpapi.Event{
			Name: "telemetry",
			Data: fmt.Appendf(nil, `{"i":%d}`, i),
		})
	}

	got := make([]int, 0, 32)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event channel closed")
			}
			var body struct {
				I int `json:"i"`
			}
			if err := json.Unmarshal(ev.Data, &body); err != nil {
				t.Fatalf("decode event: %v", err)
			}
			got = append(got, body.I)
		default:
			if len(got) != 32 {
				t.Fatalf("queued = %d, want 32 after drop-oldest", len(got))
			}
			if got[0] != extra {
				t.Fatalf("oldest kept = %d, want %d (0 was dropped)", got[0], extra)
			}
			if got[len(got)-1] != overflow-1 {
				t.Fatalf("newest = %d, want %d", got[len(got)-1], overflow-1)
			}
			return
		}
	}
}

type fakeUnlocker struct {
	rec command.Record
	err error
	vin string
	key string
}

func (f *fakeUnlocker) Unlock(_ context.Context, vin, key string) (command.Record, error) {
	f.vin = vin
	f.key = key
	return f.rec, f.err
}

func (f *fakeUnlocker) Get(id string) (command.Record, bool) {
	if f.rec.ID == id {
		return f.rec, true
	}
	return command.Record{}, false
}

type sseFrame struct {
	name string
	data string
}

func readSSE(r io.Reader) (sseFrame, error) {
	sc := bufio.NewScanner(r)
	var frame sseFrame
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			frame.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			frame.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if frame.name != "" || frame.data != "" {
				return frame, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return sseFrame{}, err
	}
	return sseFrame{}, io.EOF
}
