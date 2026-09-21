package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/leohteixeira/fleet-pulse/internal/block"
	"github.com/leohteixeira/fleet-pulse/internal/book"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

const (
	maxBodyBytes = 8 * 1024

	actionNotify  = "notify"
	actionBlock   = "block"
	actionCancel  = "cancel"
	actionPayment = "payment"
)

// Roster is the leasing snapshot port declared by HTTP.
type Roster interface {
	ListLeasing(ctx context.Context) ([]store.Vehicle, error)
}

// Blocker is the leasing machine port declared by HTTP.
type Blocker interface {
	Request(ctx context.Context, vin string) (block.Record, error)
	Cancel(ctx context.Context, id string) (block.Record, error)
	Unlock(ctx context.Context, vin string) (block.Record, error)
	Get(id string) (block.Record, bool)
	Online(vin string) bool
	Blocked(vin string) bool
	VehicleState(vin string) string
	ActiveBlock(vin string) (block.Record, bool)
}

// WithRoster wires GET /api/leasing/vehicles.
func WithRoster(r Roster) Option {
	return func(s *Server) {
		s.roster = r
	}
}

// WithBlocks wires leasing writes (notify/block/cancel/payment side effects).
func WithBlocks(b Blocker) Option {
	return func(s *Server) {
		s.blocks = b
	}
}

// WithIdempotency stores first-write replays for leasing POSTs.
func WithIdempotency(k Idempotency) Option {
	return func(s *Server) {
		s.keys = k
	}
}

// WithAuditHash configures the visitor hash secret and optional X-Forwarded-For trust.
func WithAuditHash(secret string, trustForwarded bool) Option {
	return func(s *Server) {
		if secret != "" {
			s.auditSecret = secret
		}
		s.trustForwarded = trustForwarded
	}
}

func (s *Server) leasingVehicles(w http.ResponseWriter, r *http.Request) {
	if s.roster == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	list, err := s.roster.ListLeasing(r.Context())
	if err != nil {
		slog.Error("leasing vehicles", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []store.Vehicle{}
	}
	if err := writeJSON(w, http.StatusOK, leasingVehiclesBody{Vehicles: list}); err != nil {
		slog.Error("leasing vehicles", "err", err)
	}
}

func (s *Server) contractDetail(w http.ResponseWriter, r *http.Request) {
	if s.contracts == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	detail, err := s.contracts.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeContractError(w, err)
		return
	}
	if detail.Installments == nil {
		detail.Installments = []book.InstallmentView{}
	}
	if detail.Audit == nil {
		detail.Audit = []book.AuditView{}
	}
	if err := writeJSON(w, http.StatusOK, detail); err != nil {
		slog.Error("contract detail", "err", err)
	}
}

func (s *Server) notifyContract(w http.ResponseWriter, r *http.Request) {
	s.leasingWrite(w, r, actionNotify, func(ctx context.Context, id, hash string, _ []byte) (int, any, string, error) {
		detail, err := s.contracts.Get(ctx, id)
		if err != nil {
			return 0, nil, "", err
		}
		if s.clock == nil {
			return http.StatusInternalServerError, nil, "", errors.New("clock is required")
		}
		if err := notifyPolicy(detail.DaysLate, detail.LastNotify, s.clock.Snapshot().Simulated); err != nil {
			return http.StatusUnprocessableEntity, err, "", err
		}
		res, err := s.contracts.Notify(ctx, book.NotifyInput{
			ID:          id,
			Origin:      book.OriginVisitor,
			VisitorHash: hash,
		})
		if err != nil {
			return 0, nil, "", err
		}
		return http.StatusOK, res, res.ID, nil
	})
}

func (s *Server) blockContract(w http.ResponseWriter, r *http.Request) {
	s.leasingWrite(w, r, actionBlock, func(ctx context.Context, id, hash string, raw []byte) (int, any, string, error) {
		if s.blocks == nil {
			return http.StatusInternalServerError, nil, "", errors.New("block machine is required")
		}
		detail, err := s.contracts.Get(ctx, id)
		if err != nil {
			return 0, nil, "", err
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				return http.StatusUnprocessableEntity, &policyError{Code: codeReasonInvalid, Message: "motivo invalido"}, "", err
			}
		}
		if s.clock == nil {
			return http.StatusInternalServerError, nil, "", errors.New("clock is required")
		}
		if err := blockPolicy(detail.DaysLate, detail.LastNotify, s.clock.Snapshot().Simulated, s.blocks.Online(detail.VIN), body.Reason); err != nil {
			return http.StatusUnprocessableEntity, err, "", err
		}
		rec, err := s.blocks.Request(ctx, detail.VIN)
		if err != nil {
			if errors.Is(err, block.ErrInTransit) {
				return http.StatusUnprocessableEntity, inTransitError(), "", err
			}
			return http.StatusInternalServerError, nil, "", err
		}
		if _, err := s.contracts.AppendAudit(ctx, book.AuditInput{
			ContractID:  id,
			Action:      actionBlock,
			Origin:      book.OriginVisitor,
			VisitorHash: hash,
			Fields: map[string]string{
				"commandId": rec.ID,
				"reason":    strings.TrimSpace(body.Reason),
			},
		}); err != nil {
			if _, cxlErr := s.blocks.Cancel(ctx, rec.ID); cxlErr != nil {
				slog.Error("cancel block after audit fail", "err", cxlErr)
			}
			return http.StatusInternalServerError, nil, "", err
		}
		return http.StatusAccepted, commandBody{ID: rec.ID, State: rec.State}, rec.ID, nil
	})
}

func (s *Server) cancelBlock(w http.ResponseWriter, r *http.Request) {
	s.leasingWrite(w, r, actionCancel, func(ctx context.Context, id, hash string, _ []byte) (int, any, string, error) {
		if s.blocks == nil {
			return http.StatusInternalServerError, nil, "", errors.New("block machine is required")
		}
		detail, err := s.contracts.Get(ctx, id)
		if err != nil {
			return 0, nil, "", err
		}
		if rec, ok := s.blocks.ActiveBlock(detail.VIN); ok {
			if rec.State == block.StateSent {
				return http.StatusUnprocessableEntity, inTransitError(), "", block.ErrInTransit
			}
			cancelled, err := s.blocks.Cancel(ctx, rec.ID)
			if err != nil {
				if errors.Is(err, block.ErrInTransit) {
					return http.StatusUnprocessableEntity, inTransitError(), "", err
				}
				return http.StatusInternalServerError, nil, "", err
			}
			if _, err := s.contracts.AppendAudit(ctx, book.AuditInput{
				ContractID:  id,
				Action:      actionCancel,
				Origin:      book.OriginVisitor,
				VisitorHash: hash,
				Fields:      map[string]string{"commandId": cancelled.ID},
			}); err != nil {
				return http.StatusInternalServerError, nil, "", err
			}
			return http.StatusOK, commandBody{ID: cancelled.ID, State: cancelled.State}, cancelled.ID, nil
		}
		if s.blocks.Blocked(detail.VIN) {
			unlocked, err := s.blocks.Unlock(ctx, detail.VIN)
			if err != nil {
				if errors.Is(err, block.ErrInTransit) {
					return http.StatusUnprocessableEntity, inTransitError(), "", err
				}
				return http.StatusInternalServerError, nil, "", err
			}
			if _, err := s.contracts.AppendAudit(ctx, book.AuditInput{
				ContractID:  id,
				Action:      actionCancel,
				Origin:      book.OriginVisitor,
				VisitorHash: hash,
				Fields:      map[string]string{"commandId": unlocked.ID},
			}); err != nil {
				return http.StatusInternalServerError, nil, "", err
			}
			return http.StatusOK, commandBody{ID: unlocked.ID, State: unlocked.State}, unlocked.ID, nil
		}
		return http.StatusNotFound, nil, "", book.ErrNotFound
	})
}

func (s *Server) payContract(w http.ResponseWriter, r *http.Request) {
	s.leasingWrite(w, r, actionPayment, func(ctx context.Context, id, hash string, _ []byte) (int, any, string, error) {
		res, err := s.contracts.Pay(ctx, book.PayInput{
			ID:          id,
			Origin:      book.OriginVisitor,
			VisitorHash: hash,
		})
		if err != nil {
			if errors.Is(err, book.ErrPaymentNotDue) {
				return http.StatusUnprocessableEntity, paymentNotDueError(), "", err
			}
			return 0, nil, "", err
		}
		if res.DaysLate == 0 {
			if err := s.settleDebt(ctx, res.VIN); err != nil {
				return http.StatusInternalServerError, nil, "", err
			}
		}
		return http.StatusOK, res, res.ID, nil
	})
}

func (s *Server) settleDebt(ctx context.Context, vin string) error {
	if s.blocks == nil || vin == "" {
		return nil
	}
	if rec, ok := s.blocks.ActiveBlock(vin); ok {
		switch rec.State {
		case block.StateRequested, block.StateArmed:
			if _, err := s.blocks.Cancel(ctx, rec.ID); err != nil {
				return err
			}
		}
	}
	if s.blocks.Blocked(vin) {
		if _, err := s.blocks.Unlock(ctx, vin); err != nil {
			return err
		}
	}
	return nil
}

func matchesVehicleState(b Blocker, vin, want string) bool {
	if want == "" {
		return true
	}
	if b.VehicleState(vin) == want {
		return true
	}
	if want != block.VehicleArmado {
		return false
	}
	rec, ok := b.ActiveBlock(vin)
	if !ok {
		return false
	}
	switch rec.State {
	case block.StateRequested, block.StateArmed, block.StateSent:
		return true
	default:
		return false
	}
}

type writeFunc func(ctx context.Context, contractID, hash string, body []byte) (status int, payload any, resourceID string, err error)

func (s *Server) leasingWrite(w http.ResponseWriter, r *http.Request, action string, fn writeFunc) {
	if s.contracts == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(key) == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if rec, ok, err := s.lookupReplay(r.Context(), id, action, key); err != nil {
		slog.Error("idempotency lookup", "err", err, "action", action)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	} else if ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.Status)
		if _, err := w.Write(rec.Body); err != nil {
			slog.Error("idempotency replay", "err", err, "action", action)
		}
		return
	}

	raw, err := readLimitedBody(w, r)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	prev, reserved, rej := s.guards.reserveInterval(id)
	if rej != nil {
		writeGuardReject(w, rej)
		return
	}
	if rej := s.guards.checkRate(visitorIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), s.trustForwarded)); rej != nil {
		s.guards.releaseInterval(id, prev, reserved)
		writeGuardReject(w, rej)
		return
	}

	hash := hashVisitor(s.auditSecret, r.RemoteAddr, r.Header.Get("X-Forwarded-For"), s.trustForwarded)
	status, payload, resourceID, err := fn(r.Context(), id, hash, raw)
	keepInterval := err == nil && status >= 200 && status < 300
	if !keepInterval {
		s.guards.releaseInterval(id, prev, reserved)
	}
	if status == http.StatusUnprocessableEntity {
		if pol, ok := payload.(*policyError); ok {
			if writeErr := writeJSON(w, http.StatusUnprocessableEntity, pol); writeErr != nil {
				slog.Error("policy response", "err", writeErr, "action", action)
			}
			return
		}
	}
	if err != nil {
		s.writeContractError(w, err)
		return
	}
	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if s.keys != nil && resourceID != "" {
		if storeErr := s.keys.Store(r.Context(), Replay{
			ContractID: id,
			Action:     action,
			Key:        key,
			ResourceID: resourceID,
			Status:     status,
			Body:       body,
		}); storeErr != nil {
			if rec, ok, lookErr := s.lookupReplay(r.Context(), id, action, key); lookErr == nil && ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(rec.Status)
				if _, err := w.Write(rec.Body); err != nil {
					slog.Error("idempotency replay", "err", err, "action", action)
				}
				return
			}
			slog.Error("idempotency store", "err", storeErr, "action", action)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Error("leasing write", "err", err, "action", action)
	}
}

func (s *Server) lookupReplay(ctx context.Context, contractID, action, key string) (Replay, bool, error) {
	if s.keys == nil {
		return Replay{}, false, nil
	}
	return s.keys.Lookup(ctx, contractID, action, key)
}

func (s *Server) writeContractError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, book.ErrNotFound):
		http.NotFound(w, nil)
	case errors.Is(err, book.ErrPaymentNotDue):
		if writeErr := writeJSON(w, http.StatusUnprocessableEntity, paymentNotDueError()); writeErr != nil {
			slog.Error("payment not due", "err", writeErr)
		}
	default:
		if strings.Contains(err.Error(), "parse uuid") {
			http.NotFound(w, nil)
			return
		}
		slog.Error("leasing write failed", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

var errBodyTooLarge = errors.New("httpapi: body too large")

func readLimitedBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.ContentLength > maxBodyBytes {
		return nil, errBodyTooLarge
	}
	if r.Body == nil {
		return []byte{}, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errBodyTooLarge
		}
		return nil, err
	}
	return body, nil
}

type leasingVehiclesBody struct {
	Vehicles []store.Vehicle `json:"vehicles"`
}
