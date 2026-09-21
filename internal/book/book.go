// Package book seeds and ticks the leasing contract portfolio.
package book

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

const (
	// ProfilePontual pays on the simulated due day.
	ProfilePontual = "pontual"
	// ProfileAtrasa pays on the due day about half the time, else 3–12 days late.
	ProfileAtrasa = "atrasa_ocasionalmente"
	// ProfileRegulariza stays unpaid until a notify exists (none this story).
	ProfileRegulariza = "regulariza_apos_notificacao"
	// ProfileInadimplente never auto-pays.
	ProfileInadimplente = "inadimplente"

	// BandEmDia is zero days late against the simulated date.
	BandEmDia = "em_dia"
	// Band115 is 1–15 simulated days late.
	Band115 = "1_15"
	// Band1630 is 16–30 simulated days late.
	Band1630 = "16_30"
	// BandAcima30 is more than 30 simulated days late.
	BandAcima30 = "acima_30"

	bookMin            = 40
	bookMax            = 60
	bookSize           = 50
	installmentCount   = 12
	floorPct           = 10
	defaultTickEvery   = 500 * time.Millisecond
	defaultInstallment = "1290.00"
)

// Clock is the simulated calendar the book reads. Declared by this consumer.
type Clock interface {
	Simulated() time.Time
	Real() time.Time
}

type persist interface {
	SeedBook(ctx context.Context, vehicles []store.LeasingVehicle, contracts []store.SeedContract) error
	CountActiveContracts(ctx context.Context) (int, error)
	InsertCustomer(ctx context.Context, name string) (string, error)
	InsertContract(ctx context.Context, in store.ContractInsert) (string, error)
	InsertInstallment(ctx context.Context, in store.InstallmentInsert) (string, error)
	InsertPayment(ctx context.Context, in store.PaymentInsert) error
	EndContract(ctx context.Context, id string, endedOn time.Time) error
	ListActiveContracts(ctx context.Context) ([]store.ContractRow, error)
	ListActiveInstallments(ctx context.Context) ([]store.InstallmentRow, error)
	ListActivePayments(ctx context.Context) ([]store.PaymentRow, error)
	ListFreeLeasingVins(ctx context.Context) ([]string, error)
}

var _ persist = (*store.Postgres)(nil)

// Contract is one active book row for GET /api/contracts.
type Contract struct {
	ID           string `json:"id"`
	VIN          string `json:"vin"`
	ClientName   string `json:"clientName"`
	PayerProfile string `json:"payerProfile"`
	DaysLate     int    `json:"daysLate"`
	OverdueBand  string `json:"overdueBand"`
}

// Book seeds the 40–60 contract portfolio and ticks payments as the calendar moves.
type Book struct {
	st        persist
	clk       Clock
	log       *slog.Logger
	seed      uint64
	tickEvery time.Duration
	fleet     []sim.Vehicle
}

// New wires persistence and the simulated clock. Seed comes from SIM_SEED.
func New(st persist, clk Clock, log *slog.Logger) *Book {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Book{
		st:        st,
		clk:       clk,
		log:       log,
		seed:      sim.Seed(),
		tickEvery: defaultTickEvery,
		fleet:     sim.NewLeasingFleet(),
	}
}

// Seed inserts 50 leasing vehicles and a 40–60 contract book when the table is empty.
func (b *Book) Seed(ctx context.Context) error {
	if b == nil || b.st == nil {
		return fmt.Errorf("seed book: persist is required")
	}
	if b.clk == nil {
		return fmt.Errorf("seed book: clock is required")
	}
	origin := dateOnly(b.clk.Simulated())
	vehicles, contracts := planBook(origin, b.fleet, b.seed)
	if err := b.st.SeedBook(ctx, vehicles, contracts); err != nil {
		return fmt.Errorf("seed book: %w", err)
	}
	return nil
}

// List returns active leasing contracts with days late against the simulated date.
func (b *Book) List(ctx context.Context) ([]Contract, error) {
	if b == nil || b.clk == nil {
		return nil, fmt.Errorf("list contracts: clock is required")
	}
	views, err := b.load(ctx)
	if err != nil {
		return nil, err
	}
	simDay := dateOnly(b.clk.Simulated())
	out := make([]Contract, 0, len(views))
	for _, v := range views {
		days := v.daysLate(simDay)
		out = append(out, Contract{
			ID:           v.id,
			VIN:          v.vin,
			ClientName:   v.name,
			PayerProfile: v.profile,
			DaysLate:     days,
			OverdueBand:  bandOf(days),
		})
	}
	slices.SortFunc(out, func(a, c Contract) int {
		if a.DaysLate != c.DaysLate {
			return cmp.Compare(c.DaysLate, a.DaysLate)
		}
		if n := cmp.Compare(a.ClientName, c.ClientName); n != 0 {
			return n
		}
		return cmp.Compare(a.VIN, c.VIN)
	})
	return out, nil
}

// Run ticks the book until ctx is cancelled. Persist errors are logged.
func (b *Book) Run(ctx context.Context) {
	if b == nil {
		return
	}
	every := b.tickEvery
	if every <= 0 {
		every = defaultTickEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.Tick(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				b.log.Error("book tick failed", "err", err)
			}
		}
	}
}

type view struct {
	id           string
	vin          string
	name         string
	profile      string
	total        int
	paidCount    int
	startedOn    time.Time
	installments []store.InstallmentRow
	paid         map[string]bool
}

func (v view) daysLate(simDay time.Time) int {
	var late int
	for _, inst := range v.installments {
		if v.paid[inst.ID] {
			continue
		}
		d := daysLate(inst.DueOn, simDay)
		if d > late {
			late = d
		}
	}
	return late
}

func (v view) band(simDay time.Time) string {
	return bandOf(v.daysLate(simDay))
}

func (b *Book) load(ctx context.Context) ([]view, error) {
	contracts, err := b.st.ListActiveContracts(ctx)
	if err != nil {
		return nil, err
	}
	installments, err := b.st.ListActiveInstallments(ctx)
	if err != nil {
		return nil, err
	}
	payments, err := b.st.ListActivePayments(ctx)
	if err != nil {
		return nil, err
	}
	byContract := make(map[string][]store.InstallmentRow, len(contracts))
	for _, inst := range installments {
		byContract[inst.ContractID] = append(byContract[inst.ContractID], inst)
	}
	paid := make(map[string]bool, len(payments))
	for _, p := range payments {
		if p.InstallmentID != "" {
			paid[p.InstallmentID] = true
		}
	}
	out := make([]view, 0, len(contracts))
	for _, c := range contracts {
		insts := byContract[c.ID]
		if insts == nil {
			insts = []store.InstallmentRow{}
		}
		out = append(out, view{
			id:           c.ID,
			vin:          c.VIN,
			name:         c.CustomerName,
			profile:      c.Profile,
			total:        c.Total,
			paidCount:    c.PaidCount,
			startedOn:    c.StartedOn,
			installments: insts,
			paid:         paid,
		})
	}
	return out, nil
}

func dateOnly(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func daysLate(due, simDay time.Time) int {
	d := dateOnly(simDay).Sub(dateOnly(due))
	if d <= 0 {
		return 0
	}
	return int(d / (24 * time.Hour))
}

func bandOf(days int) string {
	switch {
	case days <= 0:
		return BandEmDia
	case days <= 15:
		return Band115
	case days <= 30:
		return Band1630
	default:
		return BandAcima30
	}
}

func floorNeed(n int) int {
	if n <= 0 {
		return 0
	}
	need := (n * floorPct) / 100
	if (n*floorPct)%100 != 0 {
		need++
	}
	if need < 1 {
		return 1
	}
	return need
}
