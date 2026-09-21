package book

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/leohteixeira/fleet-pulse/internal/block"
	"github.com/leohteixeira/fleet-pulse/internal/clock"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

type realNow struct{ now time.Time }

func (r realNow) Now() time.Time { return r.now }

func TestSeed_BookMixAndDeterminism(t *testing.T) {
	t.Setenv("SIM_SEED", "1")
	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)

	first := New(newMemStore(), clk, nil)
	if err := first.Seed(t.Context()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := first.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertBookShape(t, got)

	t.Setenv("SIM_SEED", "1")
	second := New(newMemStore(), clk, nil)
	if err := second.Seed(t.Context()); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	again, err := second.List(t.Context())
	if err != nil {
		t.Fatalf("second list: %v", err)
	}
	if assignmentKey(got) != assignmentKey(again) {
		t.Fatal("same SIM_SEED produced a different first book assignment")
	}

	t.Setenv("SIM_SEED", "2")
	other := New(newMemStore(), clk, nil)
	if err := other.Seed(t.Context()); err != nil {
		t.Fatalf("seed 2: %v", err)
	}
	otherList, err := other.List(t.Context())
	if err != nil {
		t.Fatalf("list seed 2: %v", err)
	}
	if assignmentKey(got) == assignmentKey(otherList) {
		t.Fatal("SIM_SEED=1 and SIM_SEED=2 assigned the same book")
	}
}

func TestSeed_MissingLeasingVINFails(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	st := newMemStore()
	st.failVehicles = true
	b := New(st, clk, nil)
	err := b.Seed(t.Context())
	if err == nil {
		t.Fatal("expected wrapped leasing insert error")
	}
	if !strings.Contains(err.Error(), "leasing") {
		t.Fatalf("error %q should mention leasing vehicles", err)
	}
}

func TestTick_PontualPaysOnDueDate(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	id := mem.mustContract(t, "FPULSELSG00000001", "Ana Costa", ProfilePontual, origin, []store.SeedInstallment{
		{DueOn: origin, Amount: defaultInstallment},
	})

	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	list, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].ID != id || list[0].DaysLate != 0 || list[0].OverdueBand != BandEmDia {
		t.Fatalf("contract = %+v, want daysLate=0 em_dia", list[0])
	}
	if mem.paymentCount() != 1 {
		t.Fatalf("payments = %d, want 1 system payment", mem.paymentCount())
	}
}

func TestTick_InadimplenteStaysLate(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	due := origin.AddDate(0, 0, -10)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	mem.mustContract(t, "FPULSELSG00000002", "Bruno Lima", ProfileInadimplente, due, []store.SeedInstallment{
		{DueOn: due, Amount: defaultInstallment},
	})

	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	list, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].DaysLate != 10 || list[0].OverdueBand != Band115 {
		t.Fatalf("contract = %+v, want daysLate=10 band 1_15", list[0])
	}
	if mem.paymentCount() != 0 {
		t.Fatalf("payments = %d, want 0", mem.paymentCount())
	}
}

func TestTick_RegularizaDoesNotAutoPay(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	due := origin.AddDate(0, 0, -20)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	mem.mustContract(t, "FPULSELSG00000003", "Carla Souza", ProfileRegulariza, due, []store.SeedInstallment{
		{DueOn: due, Amount: defaultInstallment},
	})

	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if mem.paymentCount() != 0 {
		t.Fatal("regulariza_apos_notificacao created a system payment")
	}
	list, _ := b.List(t.Context())
	if list[0].DaysLate != 20 || list[0].OverdueBand != Band1630 {
		t.Fatalf("contract = %+v, want 20d 16_30", list[0])
	}
}

func TestTick_AtrasaPaysOnScheduledDay(t *testing.T) {
	t.Parallel()

	const seed = uint64(1)
	due := clock.Origin
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	mem := newMemStore()
	id := mem.mustContract(t, "FPULSELSG00000004", "Diego Alves", ProfileAtrasa, due, []store.SeedInstallment{
		{DueOn: due, Amount: defaultInstallment},
	})
	payOn := atrasaPayOn(id, due, seed)
	mem.mustContract(t, "FPULSELSG00000005", "Eva Nunes", ProfileInadimplente, payOn.AddDate(0, 0, -10), []store.SeedInstallment{
		{DueOn: payOn.AddDate(0, 0, -10), Amount: defaultInstallment},
	})
	before := payOn.AddDate(0, 0, -1)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), before, real0)
	b := &Book{st: mem, clk: clk, seed: seed, log: New(mem, clk, nil).log}

	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick before pay day: %v", err)
	}
	if mem.paymentCount() != 0 {
		t.Fatalf("payments before pay day = %d, want 0", mem.paymentCount())
	}

	clk.SetReal(real0.Add(6 * time.Second))
	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick on pay day: %v", err)
	}
	list, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if mem.paymentCount() != 1 {
		t.Fatalf("payments on pay day = %d, want 1", mem.paymentCount())
	}
	var got Contract
	for _, c := range list {
		if c.ID == id {
			got = c
			break
		}
	}
	if got.ID != id || got.DaysLate != 0 || got.PayerProfile != ProfileAtrasa {
		t.Fatalf("contract = %+v, want id=%s atrasa daysLate=0", list, id)
	}
}

func TestTick_FloorHoldsAfterBandsAge(t *testing.T) {
	t.Setenv("SIM_SEED", "1")
	origin := clock.Origin
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, real0)
	b := New(newMemStore(), clk, nil)
	if err := b.Seed(t.Context()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	clk.SetReal(real0.Add(16 * 6 * time.Second))
	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	list, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	n := len(list)
	if n < bookMin || n > bookMax {
		t.Fatalf("active contracts = %d, want 40–60", n)
	}
	need := floorNeed(n)
	bands := map[string]int{}
	for _, c := range list {
		bands[c.OverdueBand]++
	}
	for _, band := range []string{BandEmDia, Band115, Band1630, BandAcima30} {
		if bands[band] < need {
			t.Fatalf("band %s = %d, want >= %d of %d after aging", band, bands[band], need, n)
		}
	}
}

func TestTick_PersistErrorDoesNotAdvanceLogic(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	mem.failPayments = true
	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	mem.mustContract(t, "FPULSELSG00000001", "Ana Costa", ProfilePontual, origin, []store.SeedInstallment{
		{DueOn: origin, Amount: defaultInstallment},
	})

	err := b.Tick(t.Context())
	if err == nil {
		t.Fatal("expected persist error")
	}
	if mem.paymentCount() != 0 {
		t.Fatalf("payments = %d, want 0 after failed tick", mem.paymentCount())
	}
	list, listErr := b.List(t.Context())
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(list) != 1 || list[0].PayerProfile != ProfilePontual {
		t.Fatalf("contract = %+v, want unpaid pontual still listed", list)
	}
	for _, c := range mem.contracts {
		if c.PaidCount != 0 {
			t.Fatalf("paid_count = %d, want 0", c.PaidCount)
		}
	}
}

func TestList_NoRentalVIN(t *testing.T) {
	t.Setenv("SIM_SEED", "1")
	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	b := New(newMemStore(), clk, nil)
	if err := b.Seed(t.Context()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	list, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, c := range list {
		if strings.HasPrefix(c.VIN, "FPULSESAO") || !strings.HasPrefix(c.VIN, "FPULSELSG") {
			t.Fatalf("rental or unexpected vin %q", c.VIN)
		}
	}
}

func TestGet_UnknownAndDetail(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	id := mem.mustContract(t, "FPULSELSG00000010", "Ana Costa", ProfileInadimplente, origin.AddDate(0, 0, -10), []store.SeedInstallment{
		{DueOn: origin.AddDate(0, 0, -10), Amount: defaultInstallment},
		{DueOn: origin.AddDate(0, 1, 0), Amount: defaultInstallment},
	})

	if _, err := b.Get(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown get err = %v, want ErrNotFound", err)
	}
	got, err := b.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != id || got.DaysLate != 10 || got.OverdueBand != Band115 {
		t.Fatalf("detail = %+v, want 10d 1_15", got.Contract)
	}
	if len(got.Installments) != 2 {
		t.Fatalf("installments = %d, want 2", len(got.Installments))
	}
	if got.Audit == nil {
		t.Fatal("audit slice is nil")
	}
}

func TestNotify_ThenRegularizaPaysOnTick(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	due := origin.AddDate(0, 0, -20)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	id := mem.mustContract(t, "FPULSELSG00000011", "Carla Souza", ProfileRegulariza, due, []store.SeedInstallment{
		{DueOn: due, Amount: defaultInstallment},
	})
	mem.mustContract(t, "FPULSELSG00000015", "Gina Prado", ProfileRegulariza, due, []store.SeedInstallment{
		{DueOn: due, Amount: defaultInstallment},
	})
	mem.mustContract(t, "FPULSELSG00000016", "Hugo Dias", ProfileInadimplente, due, []store.SeedInstallment{
		{DueOn: due, Amount: defaultInstallment},
	})

	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick before notify: %v", err)
	}
	if mem.paymentCount() != 0 {
		t.Fatal("regulariza paid before notify")
	}

	res, err := b.Notify(t.Context(), NotifyInput{ID: id, Origin: OriginVisitor, VisitorHash: "abc"})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if res.Action != ActionNotify || res.ID == "" {
		t.Fatalf("notify result = %+v", res)
	}
	detail, err := b.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get after notify: %v", err)
	}
	if len(detail.Audit) != 1 || detail.Audit[0].Origin != OriginVisitor || detail.Audit[0].VisitorHash != "abc" {
		t.Fatalf("audit = %+v, want VISITANTE hash abc", detail.Audit)
	}
	if detail.LastNotify.IsZero() {
		t.Fatal("last notify is zero")
	}

	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick after notify: %v", err)
	}
	if mem.paymentCount() != 1 {
		t.Fatalf("payments = %d, want 1 after regulariza notify", mem.paymentCount())
	}
	list, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var paid Contract
	for _, c := range list {
		if c.ID == id {
			paid = c
			break
		}
	}
	if paid.ID != id || paid.DaysLate != 0 {
		t.Fatalf("after pay = %+v, want id=%s daysLate=0", list, id)
	}
}

func TestPay_OldestOverdueAndHorizon(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	overdue := origin.AddDate(0, 0, -3)
	soon := origin.AddDate(0, 0, 4)
	later := origin.AddDate(0, 0, 20)
	id := mem.mustContract(t, "FPULSELSG00000012", "Diego Alves", ProfileInadimplente, overdue, []store.SeedInstallment{
		{DueOn: later, Amount: defaultInstallment},
		{DueOn: overdue, Amount: defaultInstallment},
		{DueOn: soon, Amount: defaultInstallment},
	})

	first, err := b.Pay(t.Context(), PayInput{ID: id, Origin: OriginVisitor, VisitorHash: "h1"})
	if err != nil {
		t.Fatalf("pay overdue: %v", err)
	}
	if first.DaysLate != 0 {
		t.Fatalf("daysLate after oldest pay = %d, want 0 (remaining dues are future)", first.DaysLate)
	}

	id2 := mem.mustContract(t, "FPULSELSG00000013", "Eva Nunes", ProfileInadimplente, later, []store.SeedInstallment{
		{DueOn: later, Amount: defaultInstallment},
	})
	if _, err := b.Pay(t.Context(), PayInput{ID: id2}); !errors.Is(err, ErrPaymentNotDue) {
		t.Fatalf("far due pay err = %v, want ErrPaymentNotDue", err)
	}

	id3 := mem.mustContract(t, "FPULSELSG00000014", "Fábio Reis", ProfileInadimplente, soon, []store.SeedInstallment{
		{DueOn: soon, Amount: defaultInstallment},
	})
	got, err := b.Pay(t.Context(), PayInput{ID: id3, Origin: OriginVisitor})
	if err != nil {
		t.Fatalf("pay due within 5 days: %v", err)
	}
	if got.VIN != "FPULSELSG00000014" || got.DaysLate != 0 {
		t.Fatalf("horizon pay = %+v", got)
	}
}

func TestTick_UnlocksStaleAckedBlock(t *testing.T) {
	t.Parallel()

	origin := clock.Origin
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, real0)
	mem := newMemStore()
	vin := "FPULSELSG00000020"
	mem.mustContract(t, vin, "Ana Costa", ProfileInadimplente, origin.AddDate(0, 0, -10), []store.SeedInstallment{
		{DueOn: origin.AddDate(0, 0, -10), Amount: defaultInstallment},
	})

	blocks := block.New(nil, nil, block.WithClock(realNow{now: real0}), block.WithCalendar(clk))
	rec, err := blocks.Request(t.Context(), vin)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	blocks.NoteTelem(block.LastKnown{VIN: vin, Speed: 0, Ignition: false})
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := blocks.Tick(t.Context()); err != nil {
		t.Fatalf("send: %v", err)
	}
	blocks.ApplyAck(rec.ID, true)
	if !blocks.Blocked(vin) {
		t.Fatal("want blocked before tick")
	}

	b := &Book{st: mem, clk: clk, seed: 1, log: New(mem, clk, nil).log}
	b.SetUnblocker(blocks)
	clk.SetReal(real0.Add(181 * time.Second))
	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if blocks.VehicleState(vin) != block.VehicleUnlockPending && blocks.Blocked(vin) {
		t.Fatalf("after tick state = %s blocked=%v, want pending unlock or unblocked", blocks.VehicleState(vin), blocks.Blocked(vin))
	}
}

func TestTick_HealFloorAfterVisitorPay(t *testing.T) {
	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	mem := newMemStore()
	var lateID string
	for i := range 36 {
		vin := fmt.Sprintf("FPULSELSG000000%02d", i+1)
		mem.mustContract(t, vin, "Ana Costa", ProfilePontual, origin, []store.SeedInstallment{
			{DueOn: origin.AddDate(0, 0, 10), Amount: defaultInstallment},
		})
	}
	for i := range 4 {
		vin := fmt.Sprintf("FPULSELSG000000%02d", 37+i)
		id := mem.mustContract(t, vin, "Bruno Lima", ProfileInadimplente, origin.AddDate(0, 0, -10), []store.SeedInstallment{
			{DueOn: origin.AddDate(0, 0, -10), Amount: defaultInstallment},
		})
		if lateID == "" {
			lateID = id
		}
	}
	for i := range 24 {
		mem.vehicles = append(mem.vehicles, store.LeasingVehicle{VIN: fmt.Sprintf("FPULSELSG000001%02d", i+1)})
	}

	b := New(mem, clk, nil)
	beforePay, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	need := floorNeed(len(beforePay))
	if need == 0 || bandCount(beforePay, Band115) != need {
		t.Fatalf("setup 1_15 = %d need=%d n=%d", bandCount(beforePay, Band115), need, len(beforePay))
	}

	if _, err := b.Pay(t.Context(), PayInput{ID: lateID, Origin: OriginVisitor, VisitorHash: "visitor"}); err != nil {
		t.Fatalf("visitor pay: %v", err)
	}
	afterPay, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list after pay: %v", err)
	}
	if bandCount(afterPay, Band115) >= need {
		t.Fatalf("after visitor pay 1_15 = %d, want below floor %d", bandCount(afterPay, Band115), need)
	}

	if err := b.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	list, err := b.List(t.Context())
	if err != nil {
		t.Fatalf("list after tick: %v", err)
	}
	n := len(list)
	if n < bookMin || n > bookMax {
		t.Fatalf("active contracts = %d, want 40–60", n)
	}
	need = floorNeed(n)
	for _, band := range []string{BandEmDia, Band115, Band1630, BandAcima30} {
		if bandCount(list, band) < need {
			t.Fatalf("band %s = %d, want >= %d of %d after visitor pay + healFloor", band, bandCount(list, band), need, n)
		}
	}
}

func bandCount(list []Contract, band string) int {
	n := 0
	for _, c := range list {
		if c.OverdueBand == band {
			n++
		}
	}
	return n
}

func TestFilterByBand(t *testing.T) {
	t.Parallel()

	list := []Contract{
		{ID: "a", OverdueBand: BandEmDia},
		{ID: "b", OverdueBand: Band1630},
		{ID: "c", OverdueBand: Band1630},
	}
	got := FilterByBand(list, Band1630)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("filter = %+v, want b and c", got)
	}
	if len(FilterByBand(list, "")) != 3 {
		t.Fatal("empty band should keep all")
	}
}

func TestBandOfAndFloorNeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		days int
		want string
	}{
		{days: 0, want: BandEmDia},
		{days: 1, want: Band115},
		{days: 15, want: Band115},
		{days: 16, want: Band1630},
		{days: 30, want: Band1630},
		{days: 31, want: BandAcima30},
	}
	for _, tt := range tests {
		t.Run(tt.want+"/"+fmt.Sprintf("%d", tt.days), func(t *testing.T) {
			t.Parallel()
			if got := bandOf(tt.days); got != tt.want {
				t.Fatalf("bandOf(%d) = %s, want %s", tt.days, got, tt.want)
			}
		})
	}
	if got := floorNeed(50); got != 5 {
		t.Fatalf("floorNeed(50) = %d, want 5", got)
	}
}

func assertBookShape(t *testing.T, list []Contract) {
	t.Helper()
	n := len(list)
	if n < bookMin || n > bookMax {
		t.Fatalf("active contracts = %d, want 40–60", n)
	}
	bands := map[string]int{}
	profiles := map[string]int{}
	for _, c := range list {
		if c.ID == "" || c.ClientName == "" || c.VIN == "" {
			t.Fatalf("incomplete contract: %+v", c)
		}
		if strings.HasPrefix(c.VIN, "FPULSESAO") {
			t.Fatalf("rental vin %q in book", c.VIN)
		}
		bands[c.OverdueBand]++
		profiles[c.PayerProfile]++
	}
	need := floorNeed(n)
	for _, band := range []string{BandEmDia, Band115, Band1630, BandAcima30} {
		if bands[band] < need {
			t.Fatalf("band %s = %d, want >= %d of %d", band, bands[band], need, n)
		}
	}
	if profiles[ProfilePontual] < 15 || profiles[ProfileAtrasa] < 10 {
		t.Fatalf("profiles = %v, want ~40/30/20/10 on a 50-book", profiles)
	}
	if profiles[ProfileRegulariza] < 8 || profiles[ProfileInadimplente] < 3 {
		t.Fatalf("profiles = %v, want regulariza and inadimplente present", profiles)
	}
}

func assignmentKey(list []Contract) string {
	parts := make([]string, 0, len(list))
	for _, c := range list {
		parts = append(parts, c.VIN+"|"+c.PayerProfile+"|"+c.ClientName+"|"+c.OverdueBand)
	}
	return strings.Join(parts, ";")
}

type memStore struct {
	vehicles     []store.LeasingVehicle
	customers    map[string]string
	contracts    map[string]store.ContractRow
	installments []store.InstallmentRow
	payments     []store.PaymentRow
	audits       []store.AuditRow
	auditSeq     int64
	failVehicles bool
	failPayments bool
}

func newMemStore() *memStore {
	return &memStore{
		vehicles:  []store.LeasingVehicle{},
		customers: map[string]string{},
		contracts: map[string]store.ContractRow{},
	}
}

func (m *memStore) SeedBook(_ context.Context, vehicles []store.LeasingVehicle, contracts []store.SeedContract) error {
	if m.failVehicles {
		return errors.New("insert leasing vehicles: write failed")
	}
	if len(vehicles) == 0 {
		return errors.New("insert leasing vehicles: empty roster")
	}
	if len(m.contracts) > 0 {
		m.vehicles = append([]store.LeasingVehicle{}, vehicles...)
		return nil
	}
	m.vehicles = append([]store.LeasingVehicle{}, vehicles...)
	for _, c := range contracts {
		m.mustAdd(c)
	}
	return nil
}

func (m *memStore) mustAdd(c store.SeedContract) {
	customerID := uuid.NewString()
	m.customers[customerID] = c.CustomerName
	id := uuid.NewString()
	var paid int
	for range c.Installments {
		// counted after insert
	}
	for _, inst := range c.Installments {
		instID := uuid.NewString()
		m.installments = append(m.installments, store.InstallmentRow{
			ID:         instID,
			ContractID: id,
			DueOn:      inst.DueOn,
			Amount:     inst.Amount,
		})
		if inst.Paid {
			paid++
			m.payments = append(m.payments, store.PaymentRow{
				ID:            uuid.NewString(),
				ContractID:    id,
				InstallmentID: instID,
				Source:        store.PaymentSourceSystem,
			})
		}
	}
	m.contracts[id] = store.ContractRow{
		ID:           id,
		VIN:          c.VIN,
		Profile:      c.Profile,
		CustomerName: c.CustomerName,
		Total:        len(c.Installments),
		PaidCount:    paid,
		StartedOn:    c.StartedOn,
	}
}

func (m *memStore) mustContract(t *testing.T, vin, name, profile string, started time.Time, insts []store.SeedInstallment) string {
	t.Helper()
	m.vehicles = append(m.vehicles, store.LeasingVehicle{VIN: vin})
	before := len(m.contracts)
	m.mustAdd(store.SeedContract{
		VIN:          vin,
		CustomerName: name,
		Profile:      profile,
		Amount:       defaultInstallment,
		StartedOn:    started,
		Installments: insts,
	})
	if len(m.contracts) != before+1 {
		t.Fatal("failed to insert contract")
	}
	for id, c := range m.contracts {
		if c.VIN == vin {
			return id
		}
	}
	t.Fatal("missing contract id")
	return ""
}

func (m *memStore) paymentCount() int { return len(m.payments) }

func (m *memStore) CountActiveContracts(context.Context) (int, error) {
	return len(m.contracts), nil
}

func (m *memStore) InsertCustomer(_ context.Context, name string) (string, error) {
	id := uuid.NewString()
	m.customers[id] = name
	return id, nil
}

func (m *memStore) InsertContract(_ context.Context, in store.ContractInsert) (string, error) {
	id := uuid.NewString()
	m.contracts[id] = store.ContractRow{
		ID:           id,
		VIN:          in.VIN,
		Profile:      in.Profile,
		CustomerName: m.customers[in.CustomerID],
		Total:        in.Total,
		PaidCount:    in.PaidCount,
		StartedOn:    in.StartedOn,
	}
	return id, nil
}

func (m *memStore) InsertInstallment(_ context.Context, in store.InstallmentInsert) (string, error) {
	id := uuid.NewString()
	m.installments = append(m.installments, store.InstallmentRow{
		ID:         id,
		ContractID: in.ContractID,
		DueOn:      in.DueOn,
		Amount:     in.Amount,
	})
	return id, nil
}

func (m *memStore) InsertPayment(_ context.Context, in store.PaymentInsert) error {
	if m.failPayments {
		return errors.New("insert payment: write failed")
	}
	m.payments = append(m.payments, store.PaymentRow{
		ID:            uuid.NewString(),
		ContractID:    in.ContractID,
		InstallmentID: in.InstallmentID,
		Source:        in.Source,
	})
	c := m.contracts[in.ContractID]
	c.PaidCount++
	m.contracts[in.ContractID] = c
	return nil
}

func (m *memStore) EndContract(_ context.Context, id string, _ time.Time) error {
	delete(m.contracts, id)
	return nil
}

func (m *memStore) ListActiveContracts(context.Context) ([]store.ContractRow, error) {
	out := make([]store.ContractRow, 0, len(m.contracts))
	for _, c := range m.contracts {
		out = append(out, c)
	}
	return out, nil
}

func (m *memStore) ListActiveInstallments(context.Context) ([]store.InstallmentRow, error) {
	out := make([]store.InstallmentRow, 0, len(m.installments))
	for _, inst := range m.installments {
		if _, ok := m.contracts[inst.ContractID]; ok {
			out = append(out, inst)
		}
	}
	return out, nil
}

func (m *memStore) ListActivePayments(context.Context) ([]store.PaymentRow, error) {
	out := make([]store.PaymentRow, 0, len(m.payments))
	for _, p := range m.payments {
		if _, ok := m.contracts[p.ContractID]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memStore) ListFreeLeasingVins(context.Context) ([]string, error) {
	used := map[string]bool{}
	for _, c := range m.contracts {
		used[c.VIN] = true
	}
	out := []string{}
	for _, v := range m.vehicles {
		if !used[v.VIN] {
			out = append(out, v.VIN)
		}
	}
	return out, nil
}

func (m *memStore) GetContract(_ context.Context, id string) (store.ContractRow, bool, error) {
	c, ok := m.contracts[id]
	return c, ok, nil
}

func (m *memStore) ListInstallmentsByContract(_ context.Context, id string) ([]store.InstallmentRow, error) {
	out := []store.InstallmentRow{}
	for _, inst := range m.installments {
		if inst.ContractID == id {
			out = append(out, inst)
		}
	}
	return out, nil
}

func (m *memStore) ListPaymentsByContract(_ context.Context, id string) ([]store.PaymentRow, error) {
	out := []store.PaymentRow{}
	for _, p := range m.payments {
		if p.ContractID == id {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memStore) ListAuditByContract(_ context.Context, id string) ([]store.AuditRow, error) {
	out := []store.AuditRow{}
	for _, a := range m.audits {
		if a.ContractID == id {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memStore) InsertAudit(_ context.Context, in store.AuditInsert) (string, error) {
	m.auditSeq++
	row := store.AuditRow{
		ID:          m.auditSeq,
		ContractID:  in.ContractID,
		Action:      in.Action,
		Payload:     in.Payload,
		VisitorHash: in.VisitorHash,
		CreatedAt:   time.Now().UTC(),
	}
	m.audits = append(m.audits, row)
	return fmt.Sprintf("%d", row.ID), nil
}

func (m *memStore) LastNotify(_ context.Context, id string) (store.NotifyRow, bool, error) {
	for i := len(m.audits) - 1; i >= 0; i-- {
		a := m.audits[i]
		if a.ContractID == id && a.Action == store.AuditActionNotify {
			return store.NotifyRow{ContractID: id, Payload: a.Payload, CreatedAt: a.CreatedAt}, true, nil
		}
	}
	return store.NotifyRow{}, false, nil
}

func (m *memStore) ListLastNotifies(context.Context) ([]store.NotifyRow, error) {
	seen := map[string]store.NotifyRow{}
	for _, a := range m.audits {
		if a.Action != store.AuditActionNotify {
			continue
		}
		seen[a.ContractID] = store.NotifyRow{
			ContractID: a.ContractID,
			Payload:    a.Payload,
			CreatedAt:  a.CreatedAt,
		}
	}
	out := make([]store.NotifyRow, 0, len(seen))
	for _, n := range seen {
		out = append(out, n)
	}
	return out, nil
}
