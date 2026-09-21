package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/block"
	"github.com/leohteixeira/fleet-pulse/internal/book"
	"github.com/leohteixeira/fleet-pulse/internal/clock"
	"github.com/leohteixeira/fleet-pulse/internal/httpapi"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestHandler_StreamRequiresFleet(t *testing.T) {
	t.Parallel()

	h := httpapi.New(fakeStore{snap: seededSnapshot()}, httpapi.NewHub(), nil, nil).Handler()
	tests := []struct {
		name string
		url  string
		want int
	}{
		{name: "missing fleet", url: "/api/stream", want: http.StatusBadRequest},
		{name: "unknown fleet", url: "/api/stream?fleet=other", want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.url, nil))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestHandler_StreamDoesNotMixFleets(t *testing.T) {
	t.Parallel()

	hub := httpapi.NewHub()
	srv := httptest.NewServer(httpapi.New(fakeStore{snap: seededSnapshot()}, hub, nil, nil).Handler())
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream?fleet=rental", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	hub.Publish(httpapi.Event{
		Name:  "telemetry",
		Fleet: httpapi.FleetLeasing,
		Data:  []byte(`{"vin":"FPULSELSG00000001"}`),
	})
	hub.Publish(httpapi.Event{
		Name:  "telemetry",
		Fleet: httpapi.FleetRental,
		Data:  []byte(`{"vin":"FPULSESAO00000001"}`),
	})

	got, err := readSSE(resp.Body)
	if err != nil {
		t.Fatalf("read sse: %v", err)
	}
	if strings.Contains(got.data, "FPULSELSG") {
		t.Fatalf("rental stream received leasing telem: %q", got.data)
	}
	if !strings.Contains(got.data, "FPULSESAO00000001") {
		t.Fatalf("rental stream missed rental telem: %q", got.data)
	}
}

func TestHandler_StreamDeliversLeasingFleet(t *testing.T) {
	t.Parallel()

	hub := httpapi.NewHub()
	srv := httptest.NewServer(httpapi.New(fakeStore{snap: seededSnapshot()}, hub, nil, nil).Handler())
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream?fleet=leasing", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	hub.Publish(httpapi.Event{
		Name:  "telemetry",
		Fleet: httpapi.FleetRental,
		Data:  []byte(`{"vin":"FPULSESAO00000001"}`),
	})
	hub.Publish(httpapi.Event{
		Name:  "telemetry",
		Fleet: httpapi.FleetLeasing,
		Data:  []byte(`{"vin":"FPULSELSG00000001"}`),
	})

	got, err := readSSE(resp.Body)
	if err != nil {
		t.Fatalf("read sse: %v", err)
	}
	if strings.Contains(got.data, "FPULSESAO") {
		t.Fatalf("leasing stream received rental telem: %q", got.data)
	}
	if !strings.Contains(got.data, "FPULSELSG00000001") {
		t.Fatalf("leasing stream missed leasing telem: %q", got.data)
	}
}

func TestHandler_LeasingVehiclesAndRentalUnmixed(t *testing.T) {
	t.Parallel()

	roster := fakeRoster{vehicles: []store.Vehicle{
		{VIN: "FPULSELSG00000001", DisplayID: "L01"},
		{VIN: "FPULSELSG00000002", DisplayID: "L02"},
	}}
	h := leasingHandler(t, leasingOpts{
		roster: roster,
		book:   &scriptBook{details: map[string]book.Detail{}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/leasing/vehicles", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("leasing vehicles status = %d, want 200", rec.Code)
	}
	var body struct {
		Vehicles []store.Vehicle `json:"vehicles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Vehicles) != 2 {
		t.Fatalf("len = %d, want 2", len(body.Vehicles))
	}
	for _, v := range body.Vehicles {
		if strings.HasPrefix(v.VIN, "FPULSESAO") {
			t.Fatalf("leasing list has rental vin %q", v.VIN)
		}
	}

	snapRec := httptest.NewRecorder()
	h.ServeHTTP(snapRec, httptest.NewRequest(http.MethodGet, "/api/vehicles", nil))
	var snap store.Snapshot
	if err := json.Unmarshal(snapRec.Body.Bytes(), &snap); err != nil {
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

func TestHandler_ContractsFilters(t *testing.T) {
	t.Parallel()

	vin := "FPULSELSG00000050"
	bk := &scriptBook{details: map[string]book.Detail{
		"c-late": {Contract: book.Contract{ID: "c-late", VIN: vin, DaysLate: 20, OverdueBand: book.Band1630}},
		"c-ok":   {Contract: book.Contract{ID: "c-ok", VIN: "FPULSELSG00000051", DaysLate: 0, OverdueBand: book.BandEmDia}},
	}}
	blocks := block.New(nil, nil)
	blocks.NoteTelem(block.LastKnown{VIN: vin})
	if _, err := blocks.Request(t.Context(), vin); err != nil {
		t.Fatalf("request: %v", err)
	}

	h := leasingHandler(t, leasingOpts{book: bk, blocks: blocks})
	assertArmado := func(label string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/contracts?overdueBand=16_30&vehicleState=armado", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", label, rec.Code)
		}
		var body struct {
			Contracts []book.Contract `json:"contracts"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s decode: %v", label, err)
		}
		if len(body.Contracts) != 1 || body.Contracts[0].ID != "c-late" {
			t.Fatalf("%s filtered = %+v, want c-late", label, body.Contracts)
		}
	}
	assertArmado("requested")
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	assertArmado("armed")
	blocks.NoteTelem(block.LastKnown{VIN: vin, Speed: 0, Ignition: false})
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("send: %v", err)
	}
	assertArmado("sent")
}

func TestHandler_ContractDetailUnknown(t *testing.T) {
	t.Parallel()

	h := leasingHandler(t, leasingOpts{book: &scriptBook{details: map[string]book.Detail{
		"c1": {
			Contract:     book.Contract{ID: "c1", VIN: "FPULSELSG00000001", ClientName: "Ana", DaysLate: 3, OverdueBand: book.Band115},
			Installments: []book.InstallmentView{{ID: "i1", DueOn: "2026-01-01", Amount: "1290.00", Paid: false}},
			Audit:        []book.AuditView{{Action: book.ActionNotify, Origin: book.OriginVisitor, VisitorHash: "deadbeef", CreatedAt: "2026-01-01T00:00:00Z"}},
		},
	}}})
	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/api/contracts/c1", nil))
	if ok.Code != http.StatusOK {
		t.Fatalf("detail status = %d", ok.Code)
	}
	var detail book.Detail
	if err := json.Unmarshal(ok.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.ID != "c1" || len(detail.Installments) != 1 || len(detail.Audit) != 1 || detail.Audit[0].VisitorHash != "deadbeef" {
		t.Fatalf("detail = %+v", detail)
	}

	miss := httptest.NewRecorder()
	h.ServeHTTP(miss, httptest.NewRequest(http.MethodGet, "/api/contracts/missing", nil))
	if miss.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", miss.Code)
	}
}

func TestHandler_NotifyPolicyAndReplay(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	bk := &scriptBook{details: map[string]book.Detail{
		"c-late": {
			Contract:   book.Contract{ID: "c-late", VIN: "FPULSELSG00000001", DaysLate: 10, OverdueBand: book.Band115},
			LastNotify: time.Time{},
			Audit:      []book.AuditView{},
		},
		"c-day": {
			Contract: book.Contract{ID: "c-day", VIN: "FPULSELSG00000002", DaysLate: 0, OverdueBand: book.BandEmDia},
		},
	}}
	h := leasingHandler(t, leasingOpts{book: bk, clock: clk, keys: httpapiKeys()})

	day := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-day/notify", nil)
	req.Header.Set("Idempotency-Key", "n-day")
	h.ServeHTTP(day, req)
	assertPolicy(t, day, http.StatusUnprocessableEntity, "in_day")

	ok := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-late/notify", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Idempotency-Key", "n-1")
	h.ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("notify status = %d body=%s", ok.Code, ok.Body.Bytes())
	}
	if bk.notifyCount != 1 || bk.lastHash == "" {
		t.Fatalf("notify writes = %d hash=%q", bk.notifyCount, bk.lastHash)
	}
	if strings.Contains(ok.Body.String(), "203.0.113.10") {
		t.Fatal("response leaked raw ip")
	}

	replay := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-late/notify", nil)
	req.Header.Set("Idempotency-Key", "n-1")
	h.ServeHTTP(replay, req)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d", replay.Code)
	}
	if bk.notifyCount != 1 {
		t.Fatalf("second notify created audit; count = %d", bk.notifyCount)
	}
	if replay.Body.String() != ok.Body.String() {
		t.Fatalf("replay body %q != first %q", replay.Body.String(), ok.Body.String())
	}

	cool := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-late/notify", nil)
	req.Header.Set("Idempotency-Key", "n-2")
	h.ServeHTTP(cool, req)
	assertPolicy(t, cool, http.StatusUnprocessableEntity, "notify_cooldown")
}

func TestHandler_BlockPolicyAndAccept(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, real0)
	vin := "FPULSELSG00000020"
	bk := &scriptBook{details: map[string]book.Detail{
		"c-block": {
			Contract:   book.Contract{ID: "c-block", VIN: vin, DaysLate: 20, OverdueBand: book.Band1630},
			LastNotify: origin,
		},
		"c-early": {
			Contract: book.Contract{ID: "c-early", VIN: "FPULSELSG00000021", DaysLate: 10, OverdueBand: book.Band115},
		},
	}}
	blocks := block.New(nil, nil)
	h := leasingHandler(t, leasingOpts{book: bk, clock: clk, blocks: blocks, keys: httpapiKeys()})

	missingKey := httptest.NewRecorder()
	h.ServeHTTP(missingKey, httptest.NewRequest(http.MethodPost, "/api/contracts/c-block/block", strings.NewReader(`{"reason":"too many days late"}`)))
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing key status = %d, want 400", missingKey.Code)
	}

	short := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-block/block", strings.NewReader(`{"reason":"short"}`))
	req.Header.Set("Idempotency-Key", "b-short")
	h.ServeHTTP(short, req)
	assertPolicy(t, short, http.StatusUnprocessableEntity, "reason_invalid")

	early := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-early/block", strings.NewReader(`{"reason":"atraso longo demais"}`))
	req.Header.Set("Idempotency-Key", "b-early")
	h.ServeHTTP(early, req)
	assertPolicy(t, early, http.StatusUnprocessableEntity, "late_below_16")
	if _, ok := blocks.ActiveBlock(vin); ok {
		t.Fatal("refused block created a command")
	}

	noNotify := httptest.NewRecorder()
	bk.details["c-early"] = book.Detail{
		Contract: book.Contract{ID: "c-early", VIN: "FPULSELSG00000021", DaysLate: 20, OverdueBand: book.Band1630},
	}
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-early/block", strings.NewReader(`{"reason":"atraso longo demais"}`))
	req.Header.Set("Idempotency-Key", "b-nonotify")
	h.ServeHTTP(noNotify, req)
	assertPolicy(t, noNotify, http.StatusUnprocessableEntity, "notify_required")

	clk.SetReal(real0.Add(6 * time.Second)) // 24 simulated hours, still < 48
	recent := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-block/block", strings.NewReader(`{"reason":"atraso longo demais"}`))
	req.Header.Set("Idempotency-Key", "b-recent")
	h.ServeHTTP(recent, req)
	assertPolicy(t, recent, http.StatusUnprocessableEntity, "notify_too_recent")

	clk.SetReal(real0.Add(13 * time.Second)) // 52 simulated hours
	offline := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-block/block", strings.NewReader(`{"reason":"atraso longo demais"}`))
	req.Header.Set("Idempotency-Key", "b-off")
	h.ServeHTTP(offline, req)
	assertPolicy(t, offline, http.StatusUnprocessableEntity, "device_offline")

	blocks.NoteTelem(block.LastKnown{VIN: vin, Speed: 0})
	ok := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-block/block", strings.NewReader(`{"reason":"atraso longo demais"}`))
	req.Header.Set("Idempotency-Key", "b-ok")
	h.ServeHTTP(ok, req)
	if ok.Code != http.StatusAccepted {
		t.Fatalf("block status = %d body=%s", ok.Code, ok.Body.Bytes())
	}
	var created struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" || created.State != block.StateRequested {
		t.Fatalf("created = %+v", created)
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/commands/"+created.ID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("command status = %d", get.Code)
	}
	var rec block.Record
	if err := json.Unmarshal(get.Body.Bytes(), &rec); err != nil {
		t.Fatalf("decode command: %v", err)
	}
	if rec.Action != block.ActionBlock || rec.VIN != vin {
		t.Fatalf("command = %+v", rec)
	}

	replay := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-block/block", strings.NewReader(`{"reason":"atraso longo demais"}`))
	req.Header.Set("Idempotency-Key", "b-ok")
	h.ServeHTTP(replay, req)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay status = %d", replay.Code)
	}
	var again struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(replay.Body.Bytes(), &again)
	if again.ID != created.ID {
		t.Fatalf("replay id = %q, want %q", again.ID, created.ID)
	}
	if bk.auditCount != 1 {
		t.Fatalf("audit writes = %d, want 1", bk.auditCount)
	}
}

func TestHandler_CancelArmedAndSent(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	vin := "FPULSELSG00000030"
	bk := &scriptBook{details: map[string]book.Detail{
		"c-cxl": {Contract: book.Contract{ID: "c-cxl", VIN: vin, DaysLate: 20}},
	}}
	blocks := block.New(nil, nil)
	rec, err := blocks.Request(t.Context(), vin)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	armed, ok := blocks.Get(rec.ID)
	if !ok || armed.State != block.StateArmed {
		t.Fatalf("state = %+v, want ARMED", armed)
	}

	h := leasingHandler(t, leasingOpts{book: bk, clock: clk, blocks: blocks, keys: httpapiKeys()})
	cxl := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-cxl/block/cancel", nil)
	req.Header.Set("Idempotency-Key", "cxl-1")
	h.ServeHTTP(cxl, req)
	if cxl.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body=%s", cxl.Code, cxl.Body.Bytes())
	}
	var body struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal(cxl.Body.Bytes(), &body)
	if body.State != block.StateCancelled {
		t.Fatalf("state = %q, want CANCELLED", body.State)
	}

	sent, err := blocks.Request(t.Context(), vin)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	blocks.NoteTelem(block.LastKnown{VIN: vin, Speed: 0, Ignition: false})
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("send: %v", err)
	}
	live, _ := blocks.Get(sent.ID)
	if live.State != block.StateSent {
		t.Fatalf("state = %q, want SENT", live.State)
	}
	refuse := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-cxl/block/cancel", nil)
	req.Header.Set("Idempotency-Key", "cxl-2")
	h.ServeHTTP(refuse, req)
	assertPolicy(t, refuse, http.StatusUnprocessableEntity, "in_transit")
}

func TestHandler_PaymentClearsLateAndUnlocks(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	vin := "FPULSELSG00000040"
	bk := &scriptBook{
		details: map[string]book.Detail{
			"c-pay": {Contract: book.Contract{ID: "c-pay", VIN: vin, DaysLate: 20}},
		},
		pay: book.PayResult{ID: "pay-1", InstallmentID: "i1", VIN: vin, DaysLate: 0},
	}
	blocks := block.New(nil, nil)
	rec, err := blocks.Request(t.Context(), vin)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	h := leasingHandler(t, leasingOpts{book: bk, clock: clk, blocks: blocks, keys: httpapiKeys()})
	pay := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-pay/payments", nil)
	req.Header.Set("Idempotency-Key", "pay-1")
	h.ServeHTTP(pay, req)
	if pay.Code != http.StatusOK {
		t.Fatalf("pay status = %d body=%s", pay.Code, pay.Body.Bytes())
	}
	got, ok := blocks.Get(rec.ID)
	if !ok || got.State != block.StateCancelled {
		t.Fatalf("after pay command = %+v, want CANCELLED", got)
	}

	blockedVIN := "FPULSELSG00000041"
	bk2 := &scriptBook{
		details: map[string]book.Detail{
			"c-unl": {Contract: book.Contract{ID: "c-unl", VIN: blockedVIN, DaysLate: 16}},
		},
		pay: book.PayResult{ID: "pay-2", InstallmentID: "i2", VIN: blockedVIN, DaysLate: 0},
	}
	blocks2 := block.New(nil, nil)
	armed, err := blocks2.Request(t.Context(), blockedVIN)
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	blocks2.NoteTelem(block.LastKnown{VIN: blockedVIN, Speed: 0, Ignition: false})
	if err := blocks2.Tick(t.Context()); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := blocks2.Tick(t.Context()); err != nil {
		t.Fatalf("send: %v", err)
	}
	blocks2.ApplyAck(armed.ID, true)
	if !blocks2.Blocked(blockedVIN) {
		t.Fatal("want blocked vin before payment")
	}
	hUnlock := leasingHandler(t, leasingOpts{book: bk2, clock: clk, blocks: blocks2, keys: httpapiKeys()})
	unl := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-unl/payments", nil)
	req.Header.Set("Idempotency-Key", "pay-unl")
	hUnlock.ServeHTTP(unl, req)
	if unl.Code != http.StatusOK {
		t.Fatalf("pay-unblock status = %d body=%s", unl.Code, unl.Body.Bytes())
	}
	if blocks2.VehicleState(blockedVIN) != block.VehicleUnlockPending {
		t.Fatalf("vehicle state = %s, want desbloqueio_pendente after payment unlock", blocks2.VehicleState(blockedVIN))
	}

	notDue := &scriptBook{
		details: map[string]book.Detail{
			"c-ok": {Contract: book.Contract{ID: "c-ok", VIN: vin, DaysLate: 0}},
		},
		payErr: book.ErrPaymentNotDue,
	}
	h2 := leasingHandler(t, leasingOpts{book: notDue, clock: clk, keys: httpapiKeys()})
	refuse := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/contracts/c-ok/payments", nil)
	req.Header.Set("Idempotency-Key", "pay-2")
	h2.ServeHTTP(refuse, req)
	assertPolicy(t, refuse, http.StatusUnprocessableEntity, "payment_not_due")
}

func TestHandler_BlockCancelsWhenAuditFails(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, real0)
	clk.SetReal(real0.Add(13 * time.Second))
	vin := "FPULSELSG00000060"
	bk := &scriptBook{
		details: map[string]book.Detail{
			"c-aud": {
				Contract:   book.Contract{ID: "c-aud", VIN: vin, DaysLate: 20, OverdueBand: book.Band1630},
				LastNotify: origin,
			},
		},
		auditErr: errors.New("audit down"),
	}
	blocks := block.New(nil, nil)
	blocks.NoteTelem(block.LastKnown{VIN: vin, Speed: 0})
	h := leasingHandler(t, leasingOpts{book: bk, clock: clk, blocks: blocks, keys: httpapiKeys()})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-aud/block", strings.NewReader(`{"reason":"atraso longo demais"}`))
	req.Header.Set("Idempotency-Key", "b-aud")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.Bytes())
	}
	if active, ok := blocks.ActiveBlock(vin); ok {
		t.Fatalf("command still active after audit fail: %+v", active)
	}
}

func TestHandler_PaymentStillLateDoesNotSettle(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	vin := "FPULSELSG00000070"
	bk := &scriptBook{
		details: map[string]book.Detail{
			"c-part": {Contract: book.Contract{ID: "c-part", VIN: vin, DaysLate: 20}},
		},
		pay: book.PayResult{ID: "pay-late", InstallmentID: "i1", VIN: vin, DaysLate: 8},
	}
	blocks := block.New(nil, nil)
	rec, err := blocks.Request(t.Context(), vin)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	h := leasingHandler(t, leasingOpts{book: bk, clock: clk, blocks: blocks, keys: httpapiKeys()})
	pay := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c-part/payments", nil)
	req.Header.Set("Idempotency-Key", "pay-late")
	h.ServeHTTP(pay, req)
	if pay.Code != http.StatusOK {
		t.Fatalf("pay status = %d body=%s", pay.Code, pay.Body.Bytes())
	}
	got, ok := blocks.Get(rec.ID)
	if !ok || got.State == block.StateCancelled {
		t.Fatalf("after late pay command = %+v, want still in flight", got)
	}
}

func TestHandler_BodyTooLarge(t *testing.T) {
	t.Parallel()

	h := leasingHandler(t, leasingOpts{
		book: &scriptBook{details: map[string]book.Detail{
			"c1": {Contract: book.Contract{ID: "c1", VIN: "FPULSELSG00000001", DaysLate: 10}},
		}},
		keys: httpapiKeys(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c1/notify", bytes.NewReader(bytes.Repeat([]byte("a"), 9*1024)))
	req.Header.Set("Idempotency-Key", "big")
	req.ContentLength = 9 * 1024
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestHashVisitor_StableAndIgnoresUntrustedForwarded(t *testing.T) {
	t.Parallel()

	a := hashViaNotify(t, "1.2.3.4:80", "9.9.9.9", false)
	b := hashViaNotify(t, "1.2.3.4:80", "8.8.8.8", false)
	if a == "" || a != b {
		t.Fatalf("untrusted forwarded changed hash: %q vs %q", a, b)
	}
	c := hashViaNotify(t, "1.2.3.4:80", "9.9.9.9", true)
	if c == a {
		t.Fatal("trusted forwarded hop should change hash")
	}
}

func hashViaNotify(t *testing.T, remote, forwarded string, trust bool) string {
	t.Helper()
	bk := &scriptBook{details: map[string]book.Detail{
		"c1": {Contract: book.Contract{ID: "c1", VIN: "FPULSELSG00000001", DaysLate: 2}},
	}}
	h := httpapi.New(
		fakeStore{snap: seededSnapshot()},
		httpapi.NewHub(),
		nil,
		nil,
		httpapi.WithClock(clock.Fixed(clock.ParseRate(clock.Rate4h), clock.Origin, clock.Origin)),
		httpapi.WithContracts(bk),
		httpapi.WithIdempotency(httpapi.NewMemoryKeys()),
		httpapi.WithAuditHash("secret", trust),
	).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/contracts/c1/notify", nil)
	req.RemoteAddr = remote
	req.Header.Set("X-Forwarded-For", forwarded)
	req.Header.Set("Idempotency-Key", "h-"+remote+forwarded)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.Bytes())
	}
	return bk.lastHash
}

type leasingOpts struct {
	book   *scriptBook
	clock  *clock.Clock
	blocks *block.Service
	roster fakeRoster
	keys   httpapi.Idempotency
}

func leasingHandler(t *testing.T, opts leasingOpts) http.Handler {
	t.Helper()
	clk := opts.clock
	if clk == nil {
		clk = clock.Fixed(clock.ParseRate(clock.Rate4h), clock.Origin, clock.Origin)
	}
	var extra []httpapi.Option
	extra = append(extra,
		httpapi.WithClock(clk),
		httpapi.WithContracts(opts.book),
		httpapi.WithRoster(opts.roster),
		httpapi.WithIdempotency(opts.keys),
	)
	if opts.blocks != nil {
		extra = append(extra, httpapi.WithBlocks(opts.blocks), httpapi.WithLeasingCommands(opts.blocks))
	}
	return httpapi.New(fakeStore{snap: seededSnapshot()}, httpapi.NewHub(), nil, nil, extra...).Handler()
}

func httpapiKeys() httpapi.Idempotency {
	return httpapi.NewMemoryKeys()
}

func assertPolicy(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, status, rec.Body.Bytes())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode policy: %v body=%s", err, rec.Body.Bytes())
	}
	if body.Code != code {
		t.Fatalf("code = %q, want %q", body.Code, code)
	}
}

type fakeRoster struct {
	vehicles []store.Vehicle
}

func (f fakeRoster) ListLeasing(context.Context) ([]store.Vehicle, error) {
	if f.vehicles == nil {
		return []store.Vehicle{}, nil
	}
	return f.vehicles, nil
}

type scriptBook struct {
	mu          sync.Mutex
	details     map[string]book.Detail
	notifyCount int
	auditCount  int
	lastHash    string
	pay         book.PayResult
	payErr      error
	auditErr    error
}

func (s *scriptBook) List(context.Context) ([]book.Contract, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]book.Contract, 0, len(s.details))
	for _, d := range s.details {
		out = append(out, d.Contract)
	}
	return out, nil
}

func (s *scriptBook) Get(_ context.Context, id string) (book.Detail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.details[id]
	if !ok {
		return book.Detail{}, book.ErrNotFound
	}
	if d.Installments == nil {
		d.Installments = []book.InstallmentView{}
	}
	if d.Audit == nil {
		d.Audit = []book.AuditView{}
	}
	return d, nil
}

func (s *scriptBook) Notify(_ context.Context, in book.NotifyInput) (book.WriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.details[in.ID]
	if !ok {
		return book.WriteResult{}, book.ErrNotFound
	}
	s.notifyCount++
	s.lastHash = in.VisitorHash
	d.LastNotify = time.Now()
	d.Audit = append(d.Audit, book.AuditView{Action: book.ActionNotify, Origin: in.Origin, VisitorHash: in.VisitorHash})
	s.details[in.ID] = d
	return book.WriteResult{ID: "n-1", Action: book.ActionNotify}, nil
}

func (s *scriptBook) Pay(context.Context, book.PayInput) (book.PayResult, error) {
	if s.payErr != nil {
		return book.PayResult{}, s.payErr
	}
	if s.pay.ID != "" {
		return s.pay, nil
	}
	return book.PayResult{}, book.ErrPaymentNotDue
}

func (s *scriptBook) AppendAudit(_ context.Context, in book.AuditInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auditErr != nil {
		return "", s.auditErr
	}
	s.auditCount++
	s.lastHash = in.VisitorHash
	return "a-1", nil
}
