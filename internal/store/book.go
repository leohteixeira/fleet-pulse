package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/leohteixeira/fleet-pulse/internal/store/queries"
)

// PaymentSourceSystem is the book ticker / seed origin for payments.
const PaymentSourceSystem = "system"

// PaymentSourceVisitor is a manual payment from an HTTP write.
const PaymentSourceVisitor = "visitor"

// AuditActionNotify is a persisted notify row.
const AuditActionNotify = "notify"

// AuditRow is one append-only audit_log record for a contract.
type AuditRow struct {
	ID          int64
	ContractID  string
	Action      string
	Payload     []byte
	VisitorHash string
	CreatedAt   time.Time
}

// AuditInsert appends one contract audit row.
type AuditInsert struct {
	ContractID  string
	Action      string
	Payload     []byte
	VisitorHash string
}

// NotifyRow is the latest notify for a contract.
type NotifyRow struct {
	ContractID string
	Payload    []byte
	CreatedAt  time.Time
}

// Replay is the first successful write for a contract+action+key.
type Replay struct {
	ContractID string
	Action     string
	Key        string
	ResourceID string
	Status     int
	Body       []byte
}

// LeasingVehicle is a financed VIN that must never enter the rental cache.
type LeasingVehicle struct {
	VIN       string
	DisplayID string
	Plate     string
	Model     string
}

// SeedInstallment is one due row plus whether seed already recorded a payment.
type SeedInstallment struct {
	DueOn  time.Time
	Amount string
	Paid   bool
}

// SeedContract is one active book row to persist atomically with its installments.
type SeedContract struct {
	VIN          string
	CustomerName string
	Profile      string
	Amount       string
	StartedOn    time.Time
	Installments []SeedInstallment
}

// ContractInsert opens a new active contract after seed (floor / lifecycle).
type ContractInsert struct {
	CustomerID string
	VIN        string
	Amount     string
	Total      int
	PaidCount  int
	Profile    string
	StartedOn  time.Time
}

// InstallmentInsert is one due date for an existing contract.
type InstallmentInsert struct {
	ContractID string
	DueOn      time.Time
	Amount     string
}

// PaymentInsert records a system (or later visitor) payment.
type PaymentInsert struct {
	ContractID    string
	InstallmentID string
	Amount        string
	Source        string
	PaidAt        time.Time
}

// ContractRow is an active leasing contract plus client name.
type ContractRow struct {
	ID           string
	VIN          string
	Profile      string
	CustomerName string
	Total        int
	PaidCount    int
	StartedOn    time.Time
}

// InstallmentRow is a due date for an active contract.
type InstallmentRow struct {
	ID         string
	ContractID string
	DueOn      time.Time
	Amount     string
}

// PaymentRow identifies which installment is already paid.
type PaymentRow struct {
	ID            string
	ContractID    string
	InstallmentID string
	Source        string
}

// SeedBook inserts leasing vehicles, customers, contracts, and installments
// in one transaction. An empty leasing roster or a vehicle insert error fails
// the call. Idempotent when active contracts already exist.
func (p *Postgres) SeedBook(ctx context.Context, vehicles []LeasingVehicle, contracts []SeedContract) error {
	if p.pool == nil {
		return fmt.Errorf("seed book: %w", errPoolClosed())
	}
	if len(vehicles) == 0 {
		return fmt.Errorf("insert leasing vehicles: empty roster")
	}

	n, err := p.CountActiveContracts(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		if err := p.insertLeasingVehicles(ctx, p.q, vehicles); err != nil {
			return err
		}
		return nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin book seed: %w", err)
	}
	defer tx.Rollback(context.Background())

	q := p.q.WithTx(tx)
	if err := p.insertLeasingVehicles(ctx, q, vehicles); err != nil {
		return err
	}
	count, err := q.CountLeasingVehicles(ctx)
	if err != nil {
		return fmt.Errorf("count leasing vehicles: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("insert leasing vehicles: none persisted")
	}
	for _, c := range contracts {
		if err := insertSeedContract(ctx, q, c); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit book seed: %w", err)
	}
	return nil
}

func (p *Postgres) insertLeasingVehicles(ctx context.Context, q *queries.Queries, vehicles []LeasingVehicle) error {
	for _, v := range vehicles {
		if v.VIN == "" {
			return fmt.Errorf("insert leasing vehicles: missing vin")
		}
		if err := q.InsertLeasingVehicle(ctx, queries.InsertLeasingVehicleParams{
			Vin:       v.VIN,
			DisplayID: v.DisplayID,
			Plate:     v.Plate,
			Model:     v.Model,
		}); err != nil {
			return fmt.Errorf("insert leasing vehicles: %w", err)
		}
	}
	return nil
}

func insertSeedContract(ctx context.Context, q *queries.Queries, c SeedContract) error {
	customerID, err := q.InsertCustomer(ctx, c.CustomerName)
	if err != nil {
		return fmt.Errorf("insert customer: %w", err)
	}
	amount, err := numericOf(c.Amount)
	if err != nil {
		return err
	}
	var paid int32
	for _, inst := range c.Installments {
		if inst.Paid {
			paid++
		}
	}
	contractID, err := q.InsertContract(ctx, queries.InsertContractParams{
		CustomerID:        customerID,
		Vin:               c.VIN,
		InstallmentValue:  amount,
		TotalInstallments: int32(len(c.Installments)),
		PaidCount:         paid,
		PayerProfile:      c.Profile,
		StartedOn:         dateOf(c.StartedOn),
	})
	if err != nil {
		return fmt.Errorf("insert contract: %w", err)
	}
	for _, inst := range c.Installments {
		instAmount, err := numericOf(inst.Amount)
		if err != nil {
			return err
		}
		instID, err := q.InsertInstallment(ctx, queries.InsertInstallmentParams{
			ContractID: contractID,
			DueOn:      dateOf(inst.DueOn),
			Amount:     instAmount,
		})
		if err != nil {
			return fmt.Errorf("insert installment: %w", err)
		}
		if !inst.Paid {
			continue
		}
		if err := q.InsertPayment(ctx, queries.InsertPaymentParams{
			ContractID:    contractID,
			InstallmentID: instID,
			Amount:        instAmount,
			Source:        PaymentSourceSystem,
			PaidAt:        timestamptzOf(time.Now()),
		}); err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}
	}
	return nil
}

// CountActiveContracts returns active (ended_on IS NULL) contracts.
func (p *Postgres) CountActiveContracts(ctx context.Context) (int, error) {
	n, err := p.q.CountActiveContracts(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active contracts: %w", err)
	}
	return int(n), nil
}

// CountLeasingVehicles returns financed VINs.
func (p *Postgres) CountLeasingVehicles(ctx context.Context) (int, error) {
	n, err := p.q.CountLeasingVehicles(ctx)
	if err != nil {
		return 0, fmt.Errorf("count leasing vehicles: %w", err)
	}
	return int(n), nil
}

// InsertCustomer persists a leasing client and returns its id.
func (p *Postgres) InsertCustomer(ctx context.Context, name string) (string, error) {
	id, err := p.q.InsertCustomer(ctx, name)
	if err != nil {
		return "", fmt.Errorf("insert customer: %w", err)
	}
	return uuidString(id), nil
}

// InsertContract persists a new active contract.
func (p *Postgres) InsertContract(ctx context.Context, in ContractInsert) (string, error) {
	customerID, err := parseUUID(in.CustomerID)
	if err != nil {
		return "", err
	}
	amount, err := numericOf(in.Amount)
	if err != nil {
		return "", err
	}
	id, err := p.q.InsertContract(ctx, queries.InsertContractParams{
		CustomerID:        customerID,
		Vin:               in.VIN,
		InstallmentValue:  amount,
		TotalInstallments: int32(in.Total),
		PaidCount:         int32(in.PaidCount),
		PayerProfile:      in.Profile,
		StartedOn:         dateOf(in.StartedOn),
	})
	if err != nil {
		return "", fmt.Errorf("insert contract: %w", err)
	}
	return uuidString(id), nil
}

// InsertInstallment persists one due date.
func (p *Postgres) InsertInstallment(ctx context.Context, in InstallmentInsert) (string, error) {
	contractID, err := parseUUID(in.ContractID)
	if err != nil {
		return "", err
	}
	amount, err := numericOf(in.Amount)
	if err != nil {
		return "", err
	}
	id, err := p.q.InsertInstallment(ctx, queries.InsertInstallmentParams{
		ContractID: contractID,
		DueOn:      dateOf(in.DueOn),
		Amount:     amount,
	})
	if err != nil {
		return "", fmt.Errorf("insert installment: %w", err)
	}
	return uuidString(id), nil
}

// InsertPayment records a payment and increments paid_count.
func (p *Postgres) InsertPayment(ctx context.Context, in PaymentInsert) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin payment: %w", err)
	}
	defer tx.Rollback(context.Background())

	q := p.q.WithTx(tx)
	contractID, err := parseUUID(in.ContractID)
	if err != nil {
		return err
	}
	installmentID, err := parseUUID(in.InstallmentID)
	if err != nil {
		return err
	}
	amount, err := numericOf(in.Amount)
	if err != nil {
		return err
	}
	source := in.Source
	if source == "" {
		source = PaymentSourceSystem
	}
	paidAt := in.PaidAt
	if paidAt.IsZero() {
		paidAt = time.Now()
	}
	if err := q.InsertPayment(ctx, queries.InsertPaymentParams{
		ContractID:    contractID,
		InstallmentID: installmentID,
		Amount:        amount,
		Source:        source,
		PaidAt:        timestamptzOf(paidAt),
	}); err != nil {
		return fmt.Errorf("insert payment: %w", err)
	}
	if err := q.IncrementPaidCount(ctx, contractID); err != nil {
		return fmt.Errorf("increment paid count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit payment: %w", err)
	}
	return nil
}

// EndContract marks a contract finished on the simulated date.
func (p *Postgres) EndContract(ctx context.Context, id string, endedOn time.Time) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := p.q.EndContract(ctx, queries.EndContractParams{
		ID:      uid,
		EndedOn: dateOf(endedOn),
	}); err != nil {
		return fmt.Errorf("end contract: %w", err)
	}
	return nil
}

// ListActiveContracts returns active leasing contracts.
func (p *Postgres) ListActiveContracts(ctx context.Context) ([]ContractRow, error) {
	rows, err := p.q.ListActiveContracts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	out := make([]ContractRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, ContractRow{
			ID:           uuidString(r.ID),
			VIN:          r.Vin,
			Profile:      r.PayerProfile,
			CustomerName: r.CustomerName,
			Total:        int(r.TotalInstallments),
			PaidCount:    int(r.PaidCount),
			StartedOn:    timeOfDate(r.StartedOn),
		})
	}
	return out, nil
}

// ListActiveInstallments returns dues for active contracts.
func (p *Postgres) ListActiveInstallments(ctx context.Context) ([]InstallmentRow, error) {
	rows, err := p.q.ListActiveInstallments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list installments: %w", err)
	}
	out := make([]InstallmentRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, InstallmentRow{
			ID:         uuidString(r.ID),
			ContractID: uuidString(r.ContractID),
			DueOn:      timeOfDate(r.DueOn),
			Amount:     numericString(r.Amount),
		})
	}
	return out, nil
}

// ListActivePayments returns payments for active contracts.
func (p *Postgres) ListActivePayments(ctx context.Context) ([]PaymentRow, error) {
	rows, err := p.q.ListActivePayments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	out := make([]PaymentRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, PaymentRow{
			ID:            uuidString(r.ID),
			ContractID:    uuidString(r.ContractID),
			InstallmentID: uuidString(r.InstallmentID),
			Source:        r.Source,
		})
	}
	return out, nil
}

// ListFreeLeasingVins returns financed VINs with no active contract.
func (p *Postgres) ListFreeLeasingVins(ctx context.Context) ([]string, error) {
	vins, err := p.q.ListFreeLeasingVins(ctx)
	if err != nil {
		return nil, fmt.Errorf("list free leasing vins: %w", err)
	}
	return vins, nil
}

// GetContract returns one leasing contract by id.
func (p *Postgres) GetContract(ctx context.Context, id string) (ContractRow, bool, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return ContractRow{}, false, err
	}
	row, err := p.q.GetContract(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ContractRow{}, false, nil
		}
		return ContractRow{}, false, fmt.Errorf("get contract: %w", err)
	}
	return ContractRow{
		ID:           uuidString(row.ID),
		VIN:          row.Vin,
		Profile:      row.PayerProfile,
		CustomerName: row.CustomerName,
		Total:        int(row.TotalInstallments),
		PaidCount:    int(row.PaidCount),
		StartedOn:    timeOfDate(row.StartedOn),
	}, true, nil
}

// ListInstallmentsByContract returns dues for one contract.
func (p *Postgres) ListInstallmentsByContract(ctx context.Context, id string) ([]InstallmentRow, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return nil, err
	}
	rows, err := p.q.ListInstallmentsByContract(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list installments: %w", err)
	}
	out := make([]InstallmentRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, InstallmentRow{
			ID:         uuidString(r.ID),
			ContractID: uuidString(r.ContractID),
			DueOn:      timeOfDate(r.DueOn),
			Amount:     numericString(r.Amount),
		})
	}
	return out, nil
}

// ListPaymentsByContract returns payments for one contract.
func (p *Postgres) ListPaymentsByContract(ctx context.Context, id string) ([]PaymentRow, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return nil, err
	}
	rows, err := p.q.ListPaymentsByContract(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	out := make([]PaymentRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, PaymentRow{
			ID:            uuidString(r.ID),
			ContractID:    uuidString(r.ContractID),
			InstallmentID: uuidString(r.InstallmentID),
			Source:        r.Source,
		})
	}
	return out, nil
}

// ListAuditByContract returns append-only audit rows for one contract.
func (p *Postgres) ListAuditByContract(ctx context.Context, id string) ([]AuditRow, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return nil, err
	}
	rows, err := p.q.ListAuditByContract(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	out := make([]AuditRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, auditFromRow(r))
	}
	return out, nil
}

// InsertAudit appends one contract audit row and returns its id.
func (p *Postgres) InsertAudit(ctx context.Context, in AuditInsert) (string, error) {
	uid, err := parseUUID(in.ContractID)
	if err != nil {
		return "", err
	}
	payload := in.Payload
	if payload == nil {
		payload = []byte("{}")
	}
	id, err := p.q.InsertContractAudit(ctx, queries.InsertContractAuditParams{
		ContractID:  uid,
		Action:      in.Action,
		Payload:     payload,
		VisitorHash: textOf(in.VisitorHash),
	})
	if err != nil {
		return "", fmt.Errorf("insert audit: %w", err)
	}
	return fmt.Sprintf("%d", id), nil
}

// LastNotify returns the latest notify audit for a contract.
func (p *Postgres) LastNotify(ctx context.Context, id string) (NotifyRow, bool, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return NotifyRow{}, false, err
	}
	row, err := p.q.GetLastNotify(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return NotifyRow{}, false, nil
		}
		return NotifyRow{}, false, fmt.Errorf("last notify: %w", err)
	}
	return NotifyRow{
		ContractID: uuidString(row.ContractID),
		Payload:    row.Payload,
		CreatedAt:  timeOfTimestamptz(row.CreatedAt),
	}, true, nil
}

// ListLastNotifies returns the latest notify per active-or-any contract.
func (p *Postgres) ListLastNotifies(ctx context.Context) ([]NotifyRow, error) {
	rows, err := p.q.ListLastNotifies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list last notifies: %w", err)
	}
	out := make([]NotifyRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, NotifyRow{
			ContractID: uuidString(r.ContractID),
			Payload:    r.Payload,
			CreatedAt:  timeOfTimestamptz(r.CreatedAt),
		})
	}
	return out, nil
}

func auditFromRow(r queries.AuditLog) AuditRow {
	return AuditRow{
		ID:          r.ID,
		ContractID:  uuidString(r.ContractID),
		Action:      r.Action,
		Payload:     r.Payload,
		VisitorHash: textString(r.VisitorHash),
		CreatedAt:   timeOfTimestamptz(r.CreatedAt),
	}
}

func textOf(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func textString(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func errPoolClosed() error {
	return errors.New("store: pool is closed")
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

func parseUUID(s string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse uuid %q: %w", s, err)
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func dateOf(t time.Time) pgtype.Date {
	utc := t.UTC()
	return pgtype.Date{
		Time:  time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC),
		Valid: true,
	}
}

func timeOfDate(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return time.Date(d.Time.Year(), d.Time.Month(), d.Time.Day(), 0, 0, 0, 0, time.UTC)
}

func timestamptzOf(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func numericOf(s string) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("numeric %q: %w", s, err)
	}
	return n, nil
}

func numericString(n pgtype.Numeric) string {
	if !n.Valid {
		return ""
	}
	v, err := n.Value()
	if err != nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
