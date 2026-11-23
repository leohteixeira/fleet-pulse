package store_test

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestRetentionWindow_IsSevenDays(t *testing.T) {
	t.Parallel()
	if store.RetentionWindow != 7*24*time.Hour {
		t.Fatalf("RetentionWindow = %s, want 7 days", store.RetentionWindow)
	}
}

func TestPostgres_PurgeDeletesOldFinishedRows(t *testing.T) {
	dsn := startPostgres(t)
	ctx := t.Context()
	pg, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pg.Close)

	const vin = "FPULSELSG00000001"
	if err := pg.SeedBook(ctx, []store.LeasingVehicle{{
		VIN:       vin,
		DisplayID: "L01",
		Plate:     "LCS0A01",
		Model:     "Fiat Argo",
	}}, nil); err != nil {
		t.Fatalf("seed leasing: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("inspect pool: %v", err)
	}
	t.Cleanup(pool.Close)

	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-24 * time.Hour)

	insertCommand := func(id, state string, at time.Time) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO commands (id, vin, fleet, action, state, correlation_id, created_at, updated_at)
			VALUES ($1, $2, 'leasing', 'block', $3, 'corr', $4, $4)
		`, id, vin, state, at)
		if err != nil {
			t.Fatalf("insert command %s: %v", id, err)
		}
	}
	insertAudit := func(action string, at time.Time) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO audit_log (action, payload, created_at)
			VALUES ($1, '{}', $2)
		`, action, at)
		if err != nil {
			t.Fatalf("insert audit %s: %v", action, err)
		}
	}

	insertCommand("cmd-old-acked", "ACKED", old)
	insertCommand("cmd-old-failed", "FAILED", old)
	insertCommand("cmd-old-timeout", "TIMEOUT", old)
	insertCommand("cmd-old-cancelled", "CANCELLED", old)
	insertCommand("cmd-old-armed", "ARMED", old)
	insertCommand("cmd-recent-acked", "ACKED", recent)
	insertAudit("old.audit", old)
	insertAudit("recent.audit", recent)

	if err := pg.Purge(ctx, now.Add(-store.RetentionWindow)); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	var commands []string
	rows, err := pool.Query(ctx, `SELECT id FROM commands ORDER BY id`)
	if err != nil {
		t.Fatalf("list commands: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan command: %v", err)
		}
		commands = append(commands, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate commands: %v", err)
	}
	wantCommands := []string{"cmd-old-armed", "cmd-recent-acked"}
	if len(commands) != len(wantCommands) {
		t.Fatalf("commands = %v, want %v", commands, wantCommands)
	}
	for i, id := range wantCommands {
		if commands[i] != id {
			t.Fatalf("commands = %v, want %v", commands, wantCommands)
		}
	}

	var audits []string
	arows, err := pool.Query(ctx, `SELECT action FROM audit_log ORDER BY action`)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	defer arows.Close()
	for arows.Next() {
		var action string
		if err := arows.Scan(&action); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		audits = append(audits, action)
	}
	if err := arows.Err(); err != nil {
		t.Fatalf("iterate audit: %v", err)
	}
	if len(audits) != 1 || audits[0] != "recent.audit" {
		t.Fatalf("audit = %v, want [recent.audit]", audits)
	}
}

func TestPostgres_PurgeDeletesOldEndedContracts(t *testing.T) {
	dsn := startPostgres(t)
	ctx := t.Context()
	pg, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pg.Close)

	vins := []string{"FPULSELSG00000001", "FPULSELSG00000002", "FPULSELSG00000003", "FPULSELSG00000004"}
	vehicles := make([]store.LeasingVehicle, 0, len(vins))
	for i, vin := range vins {
		vehicles = append(vehicles, store.LeasingVehicle{
			VIN:       vin,
			DisplayID: "L0" + string(rune('1'+i)),
			Plate:     "LCS0A0" + string(rune('1'+i)),
			Model:     "Fiat Argo",
		})
	}
	if err := pg.SeedBook(ctx, vehicles, nil); err != nil {
		t.Fatalf("seed leasing: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("inspect pool: %v", err)
	}
	t.Cleanup(pool.Close)

	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-24 * time.Hour)

	// insertContract adds a customer named after the contract, the contract
	// and one installment, and returns the contract and installment ids.
	insertContract := func(name, vin string, ended bool, at time.Time) (string, string) {
		t.Helper()
		var customerID, contractID, installmentID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO customers (name, created_at) VALUES ($1, $2) RETURNING id::text
		`, name, at).Scan(&customerID); err != nil {
			t.Fatalf("insert customer %s: %v", name, err)
		}
		var endedOn *time.Time
		if ended {
			endedOn = &at
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO contracts (
				customer_id, vin, installment_value, total_installments,
				payer_profile, started_on, ended_on, created_at
			) VALUES ($1, $2, 1290.00, 1, 'pontual', $3, $4, $5)
			RETURNING id::text
		`, customerID, vin, at, endedOn, at).Scan(&contractID); err != nil {
			t.Fatalf("insert contract %s: %v", name, err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO installments (contract_id, due_on, amount)
			VALUES ($1, $2, 1290.00)
			RETURNING id::text
		`, contractID, at).Scan(&installmentID); err != nil {
			t.Fatalf("insert installment %s: %v", name, err)
		}
		return contractID, installmentID
	}
	exec := func(what, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	goneID, goneInstallment := insertContract("old-ended", vins[0], true, old)
	exec("insert payment", `
		INSERT INTO payments (contract_id, installment_id, amount, source, paid_at)
		VALUES ($1, $2, 1290.00, 'system', $3)
	`, goneID, goneInstallment, old)
	exec("insert idempotency key", `
		INSERT INTO idempotency_keys (contract_id, action, key, resource_id, status_code, created_at)
		VALUES ($1, 'notify', 'k1', 'r1', 201, $2)
	`, goneID, old)

	auditedID, _ := insertContract("old-ended-audited", vins[1], true, old)
	exec("insert audit", `
		INSERT INTO audit_log (contract_id, action, payload, created_at)
		VALUES ($1, 'notify', '{}', $2)
	`, auditedID, recent)

	insertContract("old-active", vins[2], false, old)
	insertContract("recent-ended", vins[3], true, recent)

	// A backlog larger than one purge batch, with installments and payments,
	// so the batch loop has to run more than once and stop on its own.
	const backlog = 2500
	exec("insert ended backlog", `
		WITH cu AS (
			INSERT INTO customers (name, created_at)
			SELECT 'backlog-' || g, $2 FROM generate_series(1, $1::int) g
			RETURNING id
		), co AS (
			INSERT INTO contracts (
				customer_id, vin, installment_value, total_installments,
				payer_profile, started_on, ended_on, created_at
			)
			SELECT id, $3, 1290.00, 1, 'pontual', $2::date, $2::date, $2 FROM cu
			RETURNING id
		), ins AS (
			INSERT INTO installments (contract_id, due_on, amount)
			SELECT id, $2::date, 1290.00 FROM co
			RETURNING id, contract_id
		)
		INSERT INTO payments (contract_id, installment_id, amount, source, paid_at)
		SELECT contract_id, id, 1290.00, 'backlog', $2 FROM ins
	`, backlog, old, vins[0])

	exec("insert old orphan customer", `INSERT INTO customers (name, created_at) VALUES ('old-orphan', $1)`, old)
	exec("insert recent orphan customer", `INSERT INTO customers (name, created_at) VALUES ('recent-orphan', $1)`, recent)

	// The second run finds nothing left to delete and must not fail.
	for run := range 2 {
		if err := pg.Purge(ctx, now.Add(-store.RetentionWindow)); err != nil {
			t.Fatalf("Purge run %d: %v", run+1, err)
		}
	}

	names := func(sql string) []string {
		t.Helper()
		rows, err := pool.Query(ctx, sql)
		if err != nil {
			t.Fatalf("query %q: %v", sql, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan %q: %v", sql, err)
			}
			out = append(out, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate %q: %v", sql, err)
		}
		return out
	}
	want := func(what string, got, wanted []string) {
		t.Helper()
		if !slices.Equal(got, wanted) {
			t.Fatalf("%s = %v, want %v", what, got, wanted)
		}
	}

	kept := []string{"old-active", "old-ended-audited", "recent-ended"}
	want("contracts", names(`
		SELECT cu.name FROM contracts c JOIN customers cu ON cu.id = c.customer_id ORDER BY 1
	`), kept)
	want("installments", names(`
		SELECT cu.name FROM installments i
		JOIN contracts c ON c.id = i.contract_id
		JOIN customers cu ON cu.id = c.customer_id
		ORDER BY 1
	`), kept)
	want("customers", names(`SELECT name FROM customers ORDER BY 1`),
		[]string{"old-active", "old-ended-audited", "recent-ended", "recent-orphan"})
	want("payments", names(`SELECT source FROM payments`), nil)
	want("idempotency keys", names(`SELECT key FROM idempotency_keys`), nil)
	want("audit", names(`SELECT action FROM audit_log`), []string{"notify"})
}
