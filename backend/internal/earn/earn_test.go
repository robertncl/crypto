package earn

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cryptoex/internal/db"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"
)

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	svc := NewService(st, ws.NewHub(), 60)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	return svc, st
}

// fundedUser creates a user and credits it `amount` USDT.
func fundedUser(t *testing.T, st *store.Store, amount string) int64 {
	t.Helper()
	u, err := st.CreateUser("u@x.com", "h", "user", time.Now().Unix())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.ApplyPostings("seed", time.Now().Unix(), []store.Posting{{
		UserID: u.ID, Asset: "USDT", DeltaAvailable: num.MustParse(amount), Reason: "seed", Ref: "t",
	}}); err != nil {
		t.Fatalf("seed funds: %v", err)
	}
	return u.ID
}

func TestInterestFor(t *testing.T) {
	p := num.FromInt(1000)
	apr := num.MustParse("0.10") // 10% annual
	cases := []struct {
		name    string
		elapsed int64
		want    string
	}{
		{"full year", secondsPerYear, "100"},
		{"half year", secondsPerYear / 2, "50"},
		{"zero", 0, "0"},
		{"negative", -100, "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := interestFor(p, apr, 0, c.elapsed)
			if !got.Eq(num.MustParse(c.want)) {
				t.Errorf("interestFor elapsed=%d = %s, want %s", c.elapsed, got, c.want)
			}
		})
	}
}

func TestSubscribeMovesFundsToPool(t *testing.T) {
	svc, st := newService(t)
	uid := fundedUser(t, st, "5000")

	poolBefore, _ := st.GetBalance(store.EarnPoolID, "USDT")
	pos, err := svc.Subscribe(uid, "USDT-FLEX", num.FromInt(1000))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if pos.Status != models.EarnActive || !pos.Principal.Eq(num.FromInt(1000)) {
		t.Fatalf("unexpected position: %+v", pos)
	}

	bal, _ := st.GetBalance(uid, "USDT")
	if !bal.Available.Eq(num.FromInt(4000)) {
		t.Errorf("user available = %s, want 4000", bal.Available)
	}
	poolAfter, _ := st.GetBalance(store.EarnPoolID, "USDT")
	if !poolAfter.Available.Sub(poolBefore.Available).Eq(num.FromInt(1000)) {
		t.Errorf("pool delta = %s, want 1000", poolAfter.Available.Sub(poolBefore.Available))
	}
}

func TestSubscribeValidation(t *testing.T) {
	svc, st := newService(t)
	uid := fundedUser(t, st, "5000")

	if _, err := svc.Subscribe(uid, "NOPE", num.FromInt(100)); !errors.Is(err, ErrUnknownProduct) {
		t.Errorf("unknown product err = %v, want ErrUnknownProduct", err)
	}
	if _, err := svc.Subscribe(uid, "USDT-FLEX", num.FromInt(5)); !errors.Is(err, ErrBelowMin) {
		t.Errorf("below-min err = %v, want ErrBelowMin", err)
	}
	if _, err := svc.Subscribe(uid, "USDT-FLEX", num.Zero); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("zero-amount err = %v, want ErrInvalidAmount", err)
	}
	if _, err := svc.Subscribe(uid, "USDT-FLEX", num.FromInt(999999)); !errors.Is(err, store.ErrInsufficientFunds) {
		t.Errorf("overdraw err = %v, want ErrInsufficientFunds", err)
	}
}

func TestRedeemFlexibleReturnsPrincipal(t *testing.T) {
	svc, st := newService(t)
	uid := fundedUser(t, st, "5000")

	pos, err := svc.Subscribe(uid, "USDT-FLEX", num.FromInt(1000))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	redeemed, err := svc.Redeem(uid, pos.ID)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if redeemed.Status != models.EarnRedeemed {
		t.Errorf("status = %s, want redeemed", redeemed.Status)
	}
	bal, _ := st.GetBalance(uid, "USDT")
	if bal.Available.Lt(num.FromInt(5000)) {
		t.Errorf("available = %s, want >= 5000 (principal returned)", bal.Available)
	}
}

func TestRedeemFixedBeforeMaturityRejected(t *testing.T) {
	svc, st := newService(t)
	uid := fundedUser(t, st, "5000")

	pos, err := svc.Subscribe(uid, "USDT-30D", num.FromInt(1000))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := svc.Redeem(uid, pos.ID); !errors.Is(err, ErrNotMatured) {
		t.Errorf("redeem err = %v, want ErrNotMatured", err)
	}
}

func TestRedeemNotOwner(t *testing.T) {
	svc, st := newService(t)
	uid := fundedUser(t, st, "5000")
	pos, _ := svc.Subscribe(uid, "USDT-FLEX", num.FromInt(1000))

	if _, err := svc.Redeem(uid+999, pos.ID); !errors.Is(err, ErrNotOwner) {
		t.Errorf("redeem err = %v, want ErrNotOwner", err)
	}
}

func TestAccrualCreditsInterest(t *testing.T) {
	svc, st := newService(t)
	uid := fundedUser(t, st, "5000")
	pos, err := svc.Subscribe(uid, "USDT-FLEX", num.FromInt(1000)) // 8% APR
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Backdate the last accrual by one full year so a single tick credits a full
	// year of interest: 1000 * 0.08 = 80 USDT.
	yearAgo := time.Now().Unix() - secondsPerYear
	if _, err := st.DB().Exec(`UPDATE earn_positions SET last_accrual_at=? WHERE id=?`, yearAgo, pos.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	svc.accrueAll()

	bal, _ := st.GetBalance(uid, "USDT")
	// 4000 remaining after subscribing 1000, plus ~80 interest.
	if !bal.Available.Eq(num.FromInt(4080)) {
		t.Errorf("available = %s, want 4080", bal.Available)
	}
	updated, _ := st.GetEarnPosition(pos.ID)
	if !updated.AccruedTotal.Eq(num.FromInt(80)) {
		t.Errorf("accruedTotal = %s, want 80", updated.AccruedTotal)
	}
}
