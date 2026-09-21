// Package book seeds and ticks the leasing contract portfolio.
package book

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
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

	// OriginVisitor is a hashed anonymous visitor write.
	OriginVisitor = "VISITANTE"
	// OriginSystem is a machine or ticker write.
	OriginSystem = "SISTEMA"

	// ActionNotify is the audit action for a delinquency notice.
	ActionNotify = "notify"
	// ActionPayment is the audit action for a recorded payment.
	ActionPayment = "payment"

	paymentHorizonDays = 5
)

var (
	// ErrNotFound is returned when Get/Notify/Pay cannot find the contract.
	ErrNotFound = errors.New("book: contract not found")
	// ErrPaymentNotDue is returned when Pay has no overdue or soon-due installment.
	ErrPaymentNotDue = errors.New("book: payment not due")
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
	GetContract(ctx context.Context, id string) (store.ContractRow, bool, error)
	ListInstallmentsByContract(ctx context.Context, id string) ([]store.InstallmentRow, error)
	ListPaymentsByContract(ctx context.Context, id string) ([]store.PaymentRow, error)
	ListAuditByContract(ctx context.Context, id string) ([]store.AuditRow, error)
	InsertAudit(ctx context.Context, in store.AuditInsert) (string, error)
	LastNotify(ctx context.Context, id string) (store.NotifyRow, bool, error)
	ListLastNotifies(ctx context.Context) ([]store.NotifyRow, error)
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

// Detail is GET /api/contracts/{id}: contract plus installments and audit.
type Detail struct {
	Contract
	Installments []InstallmentView `json:"installments"`
	Audit        []AuditView       `json:"audit"`
	LastNotify   time.Time         `json:"-"`
}

// InstallmentView is one due date on a contract detail.
type InstallmentView struct {
	ID     string `json:"id"`
	DueOn  string `json:"dueOn"`
	Amount string `json:"amount"`
	Paid   bool   `json:"paid"`
}

// AuditView is one append-only audit row. Visitor hash is present; never an IP.
type AuditView struct {
	ID          int64  `json:"id,omitempty"`
	Action      string `json:"action"`
	Origin      string `json:"origin"`
	VisitorHash string `json:"visitorHash,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// NotifyInput is a visitor or system notify write.
type NotifyInput struct {
	ID          string
	Origin      string
	VisitorHash string
}

// PayInput is a visitor payment write.
type PayInput struct {
	ID          string
	Origin      string
	VisitorHash string
}

// WriteResult is the id of a persisted notify or audit row.
type WriteResult struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

// PayResult is a recorded payment plus the resulting days late.
type PayResult struct {
	ID            string `json:"id"`
	InstallmentID string `json:"installmentId"`
	VIN           string `json:"vin"`
	DaysLate      int    `json:"daysLate"`
}

// AuditInput appends a visitor audit row after a leasing write.
type AuditInput struct {
	ContractID  string
	Action      string
	Origin      string
	VisitorHash string
	Fields      map[string]string
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
	hasNotify    bool
	lastNotify   time.Time
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
	notifies, err := b.st.ListLastNotifies(ctx)
	if err != nil {
		return nil, err
	}
	lastByContract := make(map[string]time.Time, len(notifies))
	for _, n := range notifies {
		if t, ok := simulatedOf(n.Payload); ok {
			lastByContract[n.ContractID] = t
			continue
		}
		lastByContract[n.ContractID] = n.CreatedAt
	}
	out := make([]view, 0, len(contracts))
	for _, c := range contracts {
		insts := byContract[c.ID]
		if insts == nil {
			insts = []store.InstallmentRow{}
		}
		last, hasNotify := lastByContract[c.ID]
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
			hasNotify:    hasNotify,
			lastNotify:   last,
		})
	}
	return out, nil
}

func simulatedOf(payload []byte) (time.Time, bool) {
	if len(payload) == 0 {
		return time.Time{}, false
	}
	var body struct {
		Simulated string `json:"simulated"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.Simulated == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, body.Simulated)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func originOf(payload []byte) string {
	if len(payload) == 0 {
		return OriginSystem
	}
	var body struct {
		Origin string `json:"origin"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.Origin == "" {
		return OriginSystem
	}
	return body.Origin
}

// FilterByBand keeps contracts in overdueBand. Empty band returns list unchanged.
func FilterByBand(list []Contract, overdueBand string) []Contract {
	if overdueBand == "" {
		return list
	}
	out := make([]Contract, 0, len(list))
	for _, c := range list {
		if c.OverdueBand == overdueBand {
			out = append(out, c)
		}
	}
	return out
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
