package book

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/store"
)

// Tick applies due payments, the 10% band floor, and the 40–60 lifecycle.
func (b *Book) Tick(ctx context.Context) error {
	if b == nil || b.st == nil || b.clk == nil {
		return fmt.Errorf("book tick: persist and clock are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	views, err := b.load(ctx)
	if err != nil {
		return err
	}
	simDay := dateOnly(b.clk.Simulated())
	if err := b.applyDuePayments(ctx, views, simDay); err != nil {
		return err
	}
	views, err = b.load(ctx)
	if err != nil {
		return err
	}
	if err := b.healFloor(ctx, views, simDay); err != nil {
		return err
	}
	views, err = b.load(ctx)
	if err != nil {
		return err
	}
	b.unlockCleared(ctx, views, simDay)
	if err := b.unlockStale(ctx); err != nil {
		return err
	}
	views, err = b.load(ctx)
	if err != nil {
		return err
	}
	return b.fitRange(ctx, views, simDay)
}

func (b *Book) unlockCleared(ctx context.Context, views []view, simDay time.Time) {
	if b.unblocker == nil {
		return
	}
	for _, v := range views {
		if v.daysLate(simDay) != 0 {
			continue
		}
		if err := b.unblocker.UnlockIfBlocked(ctx, v.vin); err != nil {
			b.log.Error("unlock cleared block", "err", err)
		}
	}
}

func (b *Book) unlockStale(ctx context.Context) error {
	if b.unblocker == nil {
		return nil
	}
	cutoff := b.clk.Simulated().Add(-maxBlockAge)
	if err := b.unblocker.UnlockStale(ctx, cutoff); err != nil {
		b.log.Error("unlock stale blocks", "err", err)
		return nil
	}
	return nil
}

func (b *Book) applyDuePayments(ctx context.Context, views []view, simDay time.Time) error {
	for i := range views {
		v := &views[i]
		for _, inst := range v.installments {
			if v.paid[inst.ID] {
				continue
			}
			if inst.DueOn.After(simDay) {
				continue
			}
			if !shouldPay(v.profile, v.id, inst.DueOn, simDay, b.seed, v.hasNotify) {
				continue
			}
			if v.profile != ProfilePontual && paymentBreaksFloor(views, v.id, inst.ID, simDay) {
				continue
			}
			if err := b.st.InsertPayment(ctx, store.PaymentInsert{
				ContractID:    v.id,
				InstallmentID: inst.ID,
				Amount:        inst.Amount,
				Source:        store.PaymentSourceSystem,
				PaidAt:        b.clk.Real(),
			}); err != nil {
				return err
			}
			v.paid[inst.ID] = true
			v.paidCount++
		}
	}
	return nil
}

func shouldPay(profile, contractID string, due, simDay time.Time, seed uint64, hasNotify bool) bool {
	switch profile {
	case ProfilePontual:
		return !simDay.Before(dateOnly(due))
	case ProfileAtrasa:
		return !simDay.Before(atrasaPayOn(contractID, due, seed))
	case ProfileRegulariza:
		return hasNotify && !simDay.Before(dateOnly(due))
	default:
		return false
	}
}

func atrasaPayOn(contractID string, due time.Time, seed uint64) time.Time {
	dueDay := dateOnly(due)
	rng := rand.New(rand.NewPCG(seed, hash64(contractID+"|"+dueDay.Format(time.DateOnly))))
	if rng.IntN(2) == 0 {
		return dueDay
	}
	return dueDay.AddDate(0, 0, 3+rng.IntN(10))
}

func hash64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func paymentBreaksFloor(views []view, contractID, installmentID string, simDay time.Time) bool {
	need := floorNeed(len(views))
	if need == 0 {
		return false
	}
	before := bandCounts(views, simDay)
	afterViews := cloneViews(views)
	for i := range afterViews {
		if afterViews[i].id != contractID {
			continue
		}
		if afterViews[i].paid == nil {
			afterViews[i].paid = map[string]bool{}
		}
		afterViews[i].paid[installmentID] = true
		break
	}
	after := bandCounts(afterViews, simDay)
	for _, band := range []string{Band115, Band1630, BandAcima30} {
		if before[band] >= need && after[band] < need {
			return true
		}
	}
	return false
}

func cloneViews(views []view) []view {
	out := make([]view, len(views))
	for i, v := range views {
		paid := make(map[string]bool, len(v.paid))
		for k, val := range v.paid {
			paid[k] = val
		}
		out[i] = v
		out[i].paid = paid
	}
	return out
}

func bandCounts(views []view, simDay time.Time) map[string]int {
	counts := map[string]int{
		BandEmDia:   0,
		Band115:     0,
		Band1630:    0,
		BandAcima30: 0,
	}
	for _, v := range views {
		counts[v.band(simDay)]++
	}
	return counts
}

func (b *Book) healFloor(ctx context.Context, views []view, simDay time.Time) error {
	for {
		need := floorNeed(len(views))
		counts := bandCounts(views, simDay)
		progressed := false
		for _, band := range []string{BandEmDia, Band115, Band1630, BandAcima30} {
			for counts[band] < need {
				added, err := b.addBandContract(ctx, views, band, simDay)
				if err != nil {
					return err
				}
				if !added {
					break
				}
				progressed = true
				reloaded, err := b.load(ctx)
				if err != nil {
					return err
				}
				views = reloaded
				counts = bandCounts(views, simDay)
				need = floorNeed(len(views))
			}
		}
		if !progressed {
			return nil
		}
	}
}

func (b *Book) fitRange(ctx context.Context, views []view, simDay time.Time) error {
	n := len(views)
	for n < bookMin {
		added, err := b.addBandContract(ctx, views, BandEmDia, simDay)
		if err != nil {
			return err
		}
		if !added {
			break
		}
		views, err = b.load(ctx)
		if err != nil {
			return err
		}
		n = len(views)
	}
	for n > bookMax {
		before := n
		if err := b.endFromSafest(ctx, views, simDay); err != nil {
			return err
		}
		var err error
		views, err = b.load(ctx)
		if err != nil {
			return err
		}
		n = len(views)
		if n >= before {
			break
		}
	}
	return b.rotatePaid(ctx, views, simDay)
}

func (b *Book) rotatePaid(ctx context.Context, views []view, simDay time.Time) error {
	if len(views) <= bookMin {
		return nil
	}
	for _, v := range views {
		if v.paidCount < v.total || v.total == 0 {
			continue
		}
		if bandCounts(views, simDay)[v.band(simDay)] <= floorNeed(len(views)) {
			continue
		}
		if err := b.st.EndContract(ctx, v.id, simDay); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (b *Book) addBandContract(ctx context.Context, views []view, band string, simDay time.Time) (bool, error) {
	vins, err := b.st.ListFreeLeasingVins(ctx)
	if err != nil {
		return false, err
	}
	if len(vins) == 0 {
		canRotate := len(views) > bookMin || len(views) >= bookMax
		if !canRotate {
			return false, nil
		}
		if err := b.endFromSafest(ctx, views, simDay); err != nil {
			return false, err
		}
		vins, err = b.st.ListFreeLeasingVins(ctx)
		if err != nil {
			return false, err
		}
		if len(vins) == 0 {
			return false, nil
		}
	} else if len(views) >= bookMax {
		if err := b.endFromSafest(ctx, views, simDay); err != nil {
			return false, err
		}
		vins, err = b.st.ListFreeLeasingVins(ctx)
		if err != nil {
			return false, err
		}
		if len(vins) == 0 {
			return false, nil
		}
	}
	rng := rand.New(rand.NewPCG(b.seed, hash64("new|"+band+"|"+simDay.Format(time.DateOnly))))
	name := clientNames[rng.IntN(len(clientNames))]
	profile := profileForBand(band)
	planned := newContractInBand(vins[0], name, profile, band, simDay, rng)
	customerID, err := b.st.InsertCustomer(ctx, planned.CustomerName)
	if err != nil {
		return false, err
	}
	contractID, err := b.st.InsertContract(ctx, store.ContractInsert{
		CustomerID: customerID,
		VIN:        planned.VIN,
		Amount:     planned.Amount,
		Total:      len(planned.Installments),
		Profile:    planned.Profile,
		StartedOn:  planned.StartedOn,
	})
	if err != nil {
		return false, err
	}
	for _, inst := range planned.Installments {
		if _, err := b.st.InsertInstallment(ctx, store.InstallmentInsert{
			ContractID: contractID,
			DueOn:      inst.DueOn,
			Amount:     inst.Amount,
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (b *Book) endFromSafest(ctx context.Context, views []view, simDay time.Time) error {
	need := floorNeed(len(views) - 1)
	counts := bandCounts(views, simDay)
	bestBand := ""
	bestCount := -1
	for band, n := range counts {
		if n-1 < need {
			continue
		}
		if n > bestCount {
			bestBand = band
			bestCount = n
		}
	}
	if bestBand == "" {
		return nil
	}
	for _, v := range views {
		if v.band(simDay) != bestBand {
			continue
		}
		if v.profile == ProfileInadimplente && bestBand != BandEmDia {
			continue
		}
		return b.st.EndContract(ctx, v.id, simDay)
	}
	for _, v := range views {
		if v.band(simDay) == bestBand {
			return b.st.EndContract(ctx, v.id, simDay)
		}
	}
	return nil
}
