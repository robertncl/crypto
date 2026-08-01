package store_test

import (
	"errors"
	"testing"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

// seedActiveEarn creates a funded pool and an active flexible position of
// `principal` USDT for the given user, mirroring what SubscribeEarn produces.
func seedActiveEarn(t *testing.T, st *store.Store, user int64, id, principal string) *models.EarnPosition {
	t.Helper()
	// Fund the pool so payouts never overdraw it (as Service.Init does).
	if err := st.ApplyPostings("pool_seed:"+id, 100, []store.Posting{
		{UserID: store.EarnPoolID, Asset: "USDT", DeltaAvailable: num.MustParse("1000000"), Reason: "seed", Ref: "test"},
	}); err != nil {
		t.Fatal(err)
	}
	// Give the user the principal, then subscribe (user -> pool) + open position.
	if err := st.ApplyPostings("user_seed:"+id, 100, []store.Posting{
		{UserID: user, Asset: "USDT", DeltaAvailable: num.MustParse(principal), Reason: "seed", Ref: "test"},
	}); err != nil {
		t.Fatal(err)
	}
	pos := &models.EarnPosition{
		ID: id, UserID: user, ProductID: "USDT-FLEX", Asset: "USDT", Kind: models.EarnFlexible,
		Principal: num.MustParse(principal), APR: num.MustParse("0.05"), AccruedTotal: num.Zero,
		Status: models.EarnActive, StartAt: 100, LastAccrualAt: 100,
	}
	postings := []store.Posting{
		{UserID: user, Asset: "USDT", DeltaAvailable: pos.Principal.Neg(), Reason: "earn_subscribe", Ref: id},
		{UserID: store.EarnPoolID, Asset: "USDT", DeltaAvailable: pos.Principal, Reason: "earn_subscribe", Ref: id},
	}
	if err := st.SubscribeEarn("earn_sub:"+id, 100, postings, pos); err != nil {
		t.Fatal(err)
	}
	return pos
}

func redeemPostings(pos *models.EarnPosition, payout string) []store.Posting {
	return []store.Posting{
		{UserID: store.EarnPoolID, Asset: pos.Asset, DeltaAvailable: num.MustParse(payout).Neg(), Reason: "earn_redeem", Ref: pos.ID},
		{UserID: pos.UserID, Asset: pos.Asset, DeltaAvailable: num.MustParse(payout), Reason: "earn_redeem", Ref: pos.ID},
	}
}

// TestRedeemEarnIsIdempotent is the regression guard for the double-redeem
// (TOCTOU) vuln: a second redeem of an already-redeemed position must apply no
// payout and report ErrNotActive, so principal cannot be minted from the pool.
func TestRedeemEarnIsIdempotent(t *testing.T) {
	st := newStore(t)
	const user = int64(1)
	pos := seedActiveEarn(t, st, user, "pos-1", "1000")

	// After subscribing, spendable USDT is 0 (all 1000 moved into the pool).
	if b, _ := st.GetBalance(user, "USDT"); b.Available.Cmp(num.Zero) != 0 {
		t.Fatalf("post-subscribe balance = %s, want 0", b.Available)
	}

	// First redeem: succeeds, returns principal.
	pos.Status = models.EarnRedeemed
	pos.RedeemedAt = 200
	pos.LastAccrualAt = 200
	if err := st.RedeemEarn("earn_redeem:"+pos.ID, 200, redeemPostings(pos, "1000"), pos); err != nil {
		t.Fatalf("first redeem failed: %v", err)
	}

	// Second redeem of the same position: must be rejected with ErrNotActive and
	// apply no payout.
	err := st.RedeemEarn("earn_redeem2:"+pos.ID, 201, redeemPostings(pos, "1000"), pos)
	if !errors.Is(err, store.ErrNotActive) {
		t.Fatalf("second redeem err = %v, want ErrNotActive", err)
	}

	// Balance must reflect exactly one payout of 1000, not two.
	b, _ := st.GetBalance(user, "USDT")
	if b.Available.Cmp(num.MustParse("1000")) != 0 {
		t.Fatalf("balance after double-redeem attempt = %s, want 1000 (single payout)", b.Available)
	}
}

// TestConcurrentRedeemEarnPaysOnce fires many concurrent redeems at one
// position and asserts exactly one succeeds and the balance credits once.
func TestConcurrentRedeemEarnPaysOnce(t *testing.T) {
	st := newStore(t)
	const user = int64(2)
	pos := seedActiveEarn(t, st, user, "pos-2", "5000")

	const N = 16
	results := make(chan error, N)
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		go func() {
			<-start
			p := *pos
			p.Status = models.EarnRedeemed
			p.RedeemedAt = 300
			p.LastAccrualAt = 300
			results <- st.RedeemEarn("earn_redeem:"+p.ID, 300, redeemPostings(&p, "5000"), &p)
		}()
	}
	close(start)

	ok, notActive, other := 0, 0, 0
	for i := 0; i < N; i++ {
		switch err := <-results; {
		case err == nil:
			ok++
		case errors.Is(err, store.ErrNotActive):
			notActive++
		default:
			other++
			t.Errorf("unexpected redeem error: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("successful redeems = %d, want exactly 1", ok)
	}
	if notActive != N-1 {
		t.Errorf("ErrNotActive results = %d, want %d", notActive, N-1)
	}
	if b, _ := st.GetBalance(user, "USDT"); b.Available.Cmp(num.MustParse("5000")) != 0 {
		t.Errorf("balance after concurrent redeems = %s, want 5000 (single payout)", b.Available)
	}
}

// TestAccrueEarnSkipsRedeemedPosition confirms interest cannot be paid on — or
// resurrect — a position that has already been redeemed.
func TestAccrueEarnSkipsRedeemedPosition(t *testing.T) {
	st := newStore(t)
	const user = int64(3)
	pos := seedActiveEarn(t, st, user, "pos-3", "1000")

	// Redeem it.
	pos.Status = models.EarnRedeemed
	pos.RedeemedAt = 400
	pos.LastAccrualAt = 400
	if err := st.RedeemEarn("earn_redeem:"+pos.ID, 400, redeemPostings(pos, "1000"), pos); err != nil {
		t.Fatalf("redeem failed: %v", err)
	}

	// An accrual that raced (read the position as active) must now no-op.
	accrue := &models.EarnPosition{ID: pos.ID, Asset: "USDT", AccruedTotal: num.MustParse("1"), LastAccrualAt: 460}
	postings := []store.Posting{
		{UserID: store.EarnPoolID, Asset: "USDT", DeltaAvailable: num.MustParse("1").Neg(), Reason: "earn_interest", Ref: pos.ID},
		{UserID: user, Asset: "USDT", DeltaAvailable: num.MustParse("1"), Reason: "earn_interest", Ref: pos.ID},
	}
	if err := st.AccrueEarn("earn_accrue:"+pos.ID, 460, postings, accrue); !errors.Is(err, store.ErrNotActive) {
		t.Fatalf("accrue on redeemed position err = %v, want ErrNotActive", err)
	}

	// Balance stays at the single principal payout — no bonus interest minted.
	if b, _ := st.GetBalance(user, "USDT"); b.Available.Cmp(num.MustParse("1000")) != 0 {
		t.Errorf("balance = %s, want 1000 (no interest on redeemed position)", b.Available)
	}
	// Position remains redeemed (not resurrected to active).
	got, err := st.GetEarnPosition(pos.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.EarnRedeemed {
		t.Errorf("status = %s, want redeemed (must not be resurrected)", got.Status)
	}
}

// ---------- earn read methods ----------

func TestListEarnProducts(t *testing.T) {
	st := newStore(t)
	products, err := st.ListEarnProducts()
	if err != nil {
		t.Fatal(err)
	}
	if len(products) < 3 {
		t.Fatalf("earn products = %d, want the seeded set", len(products))
	}
	byID := map[string]models.EarnProduct{}
	for _, p := range products {
		byID[p.ID] = p
	}
	flex, ok := byID["USDT-FLEX"]
	if !ok {
		t.Fatal("USDT-FLEX not seeded")
	}
	if flex.Asset != "USDT" || flex.Kind != models.EarnFlexible {
		t.Errorf("USDT-FLEX = %+v, want a flexible USDT product", flex)
	}
	if flex.APR.String() != "0.08" || flex.MinAmount.String() != "10" {
		t.Errorf("USDT-FLEX apr/min = %s/%s, want 0.08/10", flex.APR, flex.MinAmount)
	}
	if flex.TermDays != 0 {
		t.Errorf("flexible term = %d, want 0", flex.TermDays)
	}
	// Fixed-term products carry a term.
	fixed, ok := byID["USDT-30D"]
	if !ok {
		t.Fatal("USDT-30D not seeded")
	}
	if fixed.Kind != models.EarnFixed || fixed.TermDays != 30 {
		t.Errorf("USDT-30D = %+v, want fixed/30d", fixed)
	}
}

func TestGetEarnProduct(t *testing.T) {
	st := newStore(t)
	p, err := st.GetEarnProduct("BTC-FLEX")
	if err != nil {
		t.Fatal(err)
	}
	if p.Asset != "BTC" || p.Kind != models.EarnFlexible || p.MaxAmount.Sign() != 0 {
		t.Errorf("BTC-FLEX = %+v, want flexible BTC, uncapped", p)
	}

	if _, err := st.GetEarnProduct("NO-SUCH-PRODUCT"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown product err = %v, want ErrNotFound", err)
	}
}

func TestListEarnPositionsActiveOnly(t *testing.T) {
	st := newStore(t)
	const user = int64(7)
	active := seedActiveEarn(t, st, user, "pos-active", "1000")
	redeemed := seedActiveEarn(t, st, user, "pos-redeemed", "500")

	// Close the second one.
	redeemed.Status = models.EarnRedeemed
	redeemed.RedeemedAt = 500
	redeemed.LastAccrualAt = 500
	if err := st.RedeemEarn("earn_redeem:"+redeemed.ID, 500, redeemPostings(redeemed, "500"), redeemed); err != nil {
		t.Fatalf("redeem: %v", err)
	}

	all, err := st.ListEarnPositions(user, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("all positions = %d, want 2", len(all))
	}

	onlyActive, err := st.ListEarnPositions(user, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyActive) != 1 || onlyActive[0].ID != active.ID {
		t.Errorf("active positions = %+v, want only %s", onlyActive, active.ID)
	}

	// Another user sees nothing.
	other, err := st.ListEarnPositions(999, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Errorf("other user's positions = %d, want 0", len(other))
	}
}

func TestListActiveEarnPositionsAcrossUsers(t *testing.T) {
	st := newStore(t)
	seedActiveEarn(t, st, 1, "u1-pos", "1000")
	seedActiveEarn(t, st, 2, "u2-pos", "2000")
	closed := seedActiveEarn(t, st, 3, "u3-pos", "3000")

	closed.Status = models.EarnRedeemed
	closed.RedeemedAt = 600
	closed.LastAccrualAt = 600
	if err := st.RedeemEarn("earn_redeem:"+closed.ID, 600, redeemPostings(closed, "3000"), closed); err != nil {
		t.Fatalf("redeem: %v", err)
	}

	// The accrual scheduler only ever sees still-active positions.
	positions, err := st.ListActiveEarnPositions()
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 2 {
		t.Fatalf("active positions = %d, want 2 (redeemed excluded)", len(positions))
	}
	for _, p := range positions {
		if p.Status != models.EarnActive {
			t.Errorf("position %s status = %s, want active", p.ID, p.Status)
		}
		if p.ID == closed.ID {
			t.Errorf("redeemed position %s must not be listed", p.ID)
		}
	}
}

func TestGetEarnPositionNotFound(t *testing.T) {
	st := newStore(t)
	if _, err := st.GetEarnPosition("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown position err = %v, want ErrNotFound", err)
	}
}
