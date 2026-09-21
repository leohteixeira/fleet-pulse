package store_test

import (
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
