package book

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/store"
)

// Get returns one leasing contract with installments and audit. Unknown id is ErrNotFound.
func (b *Book) Get(ctx context.Context, id string) (Detail, error) {
	if b == nil || b.st == nil || b.clk == nil {
		return Detail{}, fmt.Errorf("get contract: persist and clock are required")
	}
	row, ok, err := b.st.GetContract(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if !ok {
		return Detail{}, ErrNotFound
	}
	installments, err := b.st.ListInstallmentsByContract(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	payments, err := b.st.ListPaymentsByContract(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	paid := make(map[string]bool, len(payments))
	for _, p := range payments {
		if p.InstallmentID != "" {
			paid[p.InstallmentID] = true
		}
	}
	v := view{
		id:           row.ID,
		vin:          row.VIN,
		name:         row.CustomerName,
		profile:      row.Profile,
		installments: installments,
		paid:         paid,
	}
	simDay := dateOnly(b.clk.Simulated())
	days := v.daysLate(simDay)
	instViews := make([]InstallmentView, 0, len(installments))
	for _, inst := range installments {
		instViews = append(instViews, InstallmentView{
			ID:     inst.ID,
			DueOn:  dateOnly(inst.DueOn).Format(time.DateOnly),
			Amount: inst.Amount,
			Paid:   paid[inst.ID],
		})
	}
	audits, err := b.st.ListAuditByContract(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	auditViews := make([]AuditView, 0, len(audits))
	var lastNotify time.Time
	for _, a := range audits {
		if a.Action == ActionNotify {
			if t, ok := simulatedOf(a.Payload); ok {
				lastNotify = t
			} else {
				lastNotify = a.CreatedAt
			}
		}
		auditViews = append(auditViews, AuditView{
			ID:          a.ID,
			Action:      a.Action,
			Origin:      originOf(a.Payload),
			VisitorHash: a.VisitorHash,
			CreatedAt:   a.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return Detail{
		Contract: Contract{
			ID:           row.ID,
			VIN:          row.VIN,
			ClientName:   row.CustomerName,
			PayerProfile: row.Profile,
			DaysLate:     days,
			OverdueBand:  bandOf(days),
		},
		Installments: instViews,
		Audit:        auditViews,
		LastNotify:   lastNotify,
	}, nil
}

// Notify records a visitor notify. Policy is evaluated by HTTP, not here.
func (b *Book) Notify(ctx context.Context, in NotifyInput) (WriteResult, error) {
	if b == nil || b.st == nil || b.clk == nil {
		return WriteResult{}, fmt.Errorf("notify: persist and clock are required")
	}
	if _, ok, err := b.st.GetContract(ctx, in.ID); err != nil {
		return WriteResult{}, err
	} else if !ok {
		return WriteResult{}, ErrNotFound
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginVisitor
	}
	id, err := b.AppendAudit(ctx, AuditInput{
		ContractID:  in.ID,
		Action:      ActionNotify,
		Origin:      origin,
		VisitorHash: in.VisitorHash,
	})
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{ID: id, Action: ActionNotify}, nil
}

// Pay records a payment on the oldest unpaid installment that is overdue or due within 5 days.
func (b *Book) Pay(ctx context.Context, in PayInput) (PayResult, error) {
	if b == nil || b.st == nil || b.clk == nil {
		return PayResult{}, fmt.Errorf("pay: persist and clock are required")
	}
	detail, err := b.Get(ctx, in.ID)
	if err != nil {
		return PayResult{}, err
	}
	simDay := dateOnly(b.clk.Simulated())
	horizon := simDay.AddDate(0, 0, paymentHorizonDays)
	installments, err := b.st.ListInstallmentsByContract(ctx, in.ID)
	if err != nil {
		return PayResult{}, err
	}
	payments, err := b.st.ListPaymentsByContract(ctx, in.ID)
	if err != nil {
		return PayResult{}, err
	}
	paid := make(map[string]bool, len(payments))
	for _, p := range payments {
		if p.InstallmentID != "" {
			paid[p.InstallmentID] = true
		}
	}
	target, ok := oldestPayable(installments, paid, horizon)
	if !ok {
		return PayResult{}, ErrPaymentNotDue
	}
	source := store.PaymentSourceVisitor
	if in.Origin == OriginSystem {
		source = store.PaymentSourceSystem
	}
	if err := b.st.InsertPayment(ctx, store.PaymentInsert{
		ContractID:    in.ID,
		InstallmentID: target.ID,
		Amount:        target.Amount,
		Source:        source,
		PaidAt:        b.clk.Real(),
	}); err != nil {
		return PayResult{}, err
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginVisitor
	}
	auditID, err := b.AppendAudit(ctx, AuditInput{
		ContractID:  in.ID,
		Action:      ActionPayment,
		Origin:      origin,
		VisitorHash: in.VisitorHash,
		Fields: map[string]string{
			"installmentId": target.ID,
		},
	})
	if err != nil {
		return PayResult{}, err
	}
	paid[target.ID] = true
	days := view{installments: installments, paid: paid}.daysLate(simDay)
	return PayResult{
		ID:            auditID,
		InstallmentID: target.ID,
		VIN:           detail.VIN,
		DaysLate:      days,
	}, nil
}

// AppendAudit writes one append-only row. Origin and simulated time live in payload.
func (b *Book) AppendAudit(ctx context.Context, in AuditInput) (string, error) {
	if b == nil || b.st == nil || b.clk == nil {
		return "", fmt.Errorf("append audit: persist and clock are required")
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginSystem
	}
	payload := map[string]string{
		"origin":     origin,
		"simulated":  b.clk.Simulated().UTC().Format(time.RFC3339),
		"action":     in.Action,
		"contractId": in.ContractID,
	}
	for k, v := range in.Fields {
		if k == "" || v == "" {
			continue
		}
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode audit: %w", err)
	}
	id, err := b.st.InsertAudit(ctx, store.AuditInsert{
		ContractID:  in.ContractID,
		Action:      in.Action,
		Payload:     raw,
		VisitorHash: in.VisitorHash,
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// Payable reports whether an unpaid installment is overdue or due within 5 simulated days.
func Payable(installments []store.InstallmentRow, paid map[string]bool, simDay time.Time) bool {
	horizon := dateOnly(simDay).AddDate(0, 0, paymentHorizonDays)
	_, ok := oldestPayable(installments, paid, horizon)
	return ok
}

func oldestPayable(installments []store.InstallmentRow, paid map[string]bool, horizon time.Time) (store.InstallmentRow, bool) {
	var best store.InstallmentRow
	var found bool
	for _, inst := range installments {
		if paid[inst.ID] {
			continue
		}
		due := dateOnly(inst.DueOn)
		if due.After(horizon) {
			continue
		}
		if !found || due.Before(dateOnly(best.DueOn)) {
			best = inst
			found = true
		}
	}
	return best, found
}
