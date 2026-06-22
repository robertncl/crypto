// Package earn implements crypto "earn" savings products: yield-bearing
// subscriptions that pay interest on an asset balance. Subscribing moves the
// principal from the user's spendable balance into a reserved Earn pool account;
// a background scheduler accrues interest at each product's APR and credits it
// to the user's spendable balance; redeeming returns the principal (plus any
// final stub of interest) from the pool. Every balance change flows through the
// store's ledger-backed ApplyPostings, so funds and the audit trail never
// diverge.
package earn

import (
	"context"
	"errors"
	"strconv"
	"time"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"

	"github.com/google/uuid"
)

var (
	ErrUnknownProduct  = errors.New("unknown earn product")
	ErrProductInactive = errors.New("earn product is not active")
	ErrInvalidAmount   = errors.New("amount must be positive")
	ErrBelowMin        = errors.New("amount below product minimum")
	ErrAboveMax        = errors.New("amount above product maximum")
	ErrPositionClosed  = errors.New("position already redeemed")
	ErrNotMatured      = errors.New("fixed-term position has not matured")
	ErrNotOwner        = errors.New("not your position")
)

// secondsPerYear is the basis for annualized-rate accrual (a 365-day year).
const secondsPerYear int64 = 365 * 24 * 60 * 60

// poolSeedTarget is the per-asset balance the Earn pool is topped up to on boot
// so it can always cover interest payouts.
var poolSeedTarget = num.FromInt(1_000_000_000)

// Service owns earn subscriptions and the interest-accrual scheduler.
type Service struct {
	st         *store.Store
	hub        *ws.Hub
	accrueEach time.Duration
}

// NewService builds the service. accrueSec is the interest-accrual interval in
// seconds; values <= 0 fall back to 60s.
func NewService(st *store.Store, hub *ws.Hub, accrueSec int64) *Service {
	if accrueSec <= 0 {
		accrueSec = 60
	}
	return &Service{st: st, hub: hub, accrueEach: time.Duration(accrueSec) * time.Second}
}

// Init seeds the Earn pool with enough of each product's asset to cover future
// interest payouts. Safe to call repeatedly (it only tops up).
func (s *Service) Init() error {
	products, err := s.st.ListEarnProducts()
	if err != nil {
		return err
	}
	seeded := map[string]bool{}
	for _, p := range products {
		if seeded[p.Asset] {
			continue
		}
		seeded[p.Asset] = true
		bal, err := s.st.GetBalance(store.EarnPoolID, p.Asset)
		if err != nil {
			return err
		}
		if bal.Available.Lt(poolSeedTarget) {
			topUp := poolSeedTarget.Sub(bal.Available)
			if err := s.st.ApplyPostings("earn_pool_seed:"+p.Asset, time.Now().Unix(), []store.Posting{{
				UserID: store.EarnPoolID, Asset: p.Asset, DeltaAvailable: topUp,
				Reason: "earn_pool_seed", Ref: "boot",
			}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Products returns the active earn products.
func (s *Service) Products() ([]models.EarnProduct, error) {
	all, err := s.st.ListEarnProducts()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		if p.Status == "active" {
			out = append(out, p)
		}
	}
	return out, nil
}

// Positions returns a user's earn positions (active first, newest first).
func (s *Service) Positions(userID int64) ([]models.EarnPosition, error) {
	return s.st.ListEarnPositions(userID, false)
}

// Subscribe moves amount of the product's asset from the user's spendable
// balance into the Earn pool and opens a position. Returns the new position.
func (s *Service) Subscribe(userID int64, productID string, amount num.Dec) (*models.EarnPosition, error) {
	p, err := s.st.GetEarnProduct(productID)
	if err != nil {
		return nil, ErrUnknownProduct
	}
	if p.Status != "active" {
		return nil, ErrProductInactive
	}
	if amount.Sign() <= 0 {
		return nil, ErrInvalidAmount
	}
	if amount.Lt(p.MinAmount) {
		return nil, ErrBelowMin
	}
	if p.MaxAmount.Sign() > 0 && amount.Gt(p.MaxAmount) {
		return nil, ErrAboveMax
	}

	now := time.Now().Unix()
	var maturity int64
	if p.Kind == models.EarnFixed && p.TermDays > 0 {
		maturity = now + int64(p.TermDays)*24*60*60
	}
	pos := &models.EarnPosition{
		ID: uuid.NewString(), UserID: userID, ProductID: p.ID, Asset: p.Asset, Kind: p.Kind,
		Principal: amount, APR: p.APR, AccruedTotal: num.Zero, Status: models.EarnActive,
		StartAt: now, MaturityAt: maturity, LastAccrualAt: now, RedeemedAt: 0,
	}
	postings := []store.Posting{
		{UserID: userID, Asset: p.Asset, DeltaAvailable: amount.Neg(), Reason: "earn_subscribe", Ref: pos.ID},
		{UserID: store.EarnPoolID, Asset: p.Asset, DeltaAvailable: amount, Reason: "earn_subscribe", Ref: pos.ID},
	}
	if err := s.st.SubscribeEarn("earn_sub:"+pos.ID, now, postings, pos); err != nil {
		if errors.Is(err, store.ErrInsufficientFunds) {
			return nil, store.ErrInsufficientFunds
		}
		return nil, err
	}
	s.publishBalances(userID)
	s.publishPositions(userID)
	return pos, nil
}

// Redeem closes a position: it pays out any interest accrued since the last
// scheduler tick and returns the principal to the user's spendable balance.
// Flexible positions redeem anytime; fixed positions only after maturity.
func (s *Service) Redeem(userID int64, positionID string) (*models.EarnPosition, error) {
	pos, err := s.st.GetEarnPosition(positionID)
	if err != nil {
		return nil, err
	}
	if pos.UserID != userID {
		return nil, ErrNotOwner
	}
	if pos.Status != models.EarnActive {
		return nil, ErrPositionClosed
	}
	now := time.Now().Unix()
	if pos.Kind == models.EarnFixed && pos.MaturityAt > 0 && now < pos.MaturityAt {
		return nil, ErrNotMatured
	}

	// Pay the final stub of interest (capped at maturity for fixed) and return
	// principal — both from the pool — in one atomic, ledgered transaction.
	interest := interestFor(pos.Principal, pos.APR, pos.LastAccrualAt, effectiveTime(pos, now))
	payout := pos.Principal.Add(interest)
	postings := []store.Posting{
		{UserID: store.EarnPoolID, Asset: pos.Asset, DeltaAvailable: payout.Neg(), Reason: "earn_redeem", Ref: pos.ID},
		{UserID: userID, Asset: pos.Asset, DeltaAvailable: payout, Reason: "earn_redeem", Ref: pos.ID},
	}
	pos.AccruedTotal = pos.AccruedTotal.Add(interest)
	pos.Status = models.EarnRedeemed
	pos.LastAccrualAt = now
	pos.RedeemedAt = now
	if err := s.st.CommitEarnPosting("earn_redeem:"+pos.ID, now, postings, pos); err != nil {
		return nil, err
	}
	s.publishBalances(userID)
	s.publishPositions(userID)
	return pos, nil
}

// Start runs the interest-accrual scheduler until ctx is canceled.
func (s *Service) Start(ctx context.Context) {
	tick := time.NewTicker(s.accrueEach)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.accrueAll()
		}
	}
}

// accrueAll credits interest earned since each active position's last accrual to
// the owning user's spendable balance, paid from the Earn pool.
func (s *Service) accrueAll() {
	positions, err := s.st.ListActiveEarnPositions()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	touched := map[int64]bool{}
	for i := range positions {
		pos := &positions[i]
		end := effectiveTime(pos, now)
		interest := interestFor(pos.Principal, pos.APR, pos.LastAccrualAt, end)
		// Skip when truncation yields zero so the elapsed time keeps accumulating
		// (tiny principals still accrue once enough time passes).
		if interest.Sign() <= 0 {
			continue
		}
		postings := []store.Posting{
			{UserID: store.EarnPoolID, Asset: pos.Asset, DeltaAvailable: interest.Neg(), Reason: "earn_interest", Ref: pos.ID},
			{UserID: pos.UserID, Asset: pos.Asset, DeltaAvailable: interest, Reason: "earn_interest", Ref: pos.ID},
		}
		pos.AccruedTotal = pos.AccruedTotal.Add(interest)
		pos.LastAccrualAt = end
		if err := s.st.CommitEarnPosting("earn_accrue:"+pos.ID+":"+strconv.FormatInt(end, 10), now, postings, pos); err != nil {
			continue
		}
		touched[pos.UserID] = true
	}
	for userID := range touched {
		s.publishBalances(userID)
		s.publishPositions(userID)
	}
}

// effectiveTime caps the accrual end at maturity for fixed-term positions so
// interest stops once a fixed product has matured.
func effectiveTime(pos *models.EarnPosition, now int64) int64 {
	if pos.Kind == models.EarnFixed && pos.MaturityAt > 0 && now > pos.MaturityAt {
		return pos.MaturityAt
	}
	return now
}

// interestFor computes simple interest on principal at annual rate apr over
// [from, to] seconds: principal * apr * (to-from) / secondsPerYear, truncated to
// the fixed-point scale. Returns zero for non-positive intervals.
func interestFor(principal, apr num.Dec, from, to int64) num.Dec {
	elapsed := to - from
	if elapsed <= 0 {
		return num.Zero
	}
	return principal.Mul(apr).Mul(num.FromInt(elapsed)).Div(num.FromInt(secondsPerYear))
}

func (s *Service) publishBalances(userID int64) {
	bals, err := s.st.ListBalances(userID)
	if err != nil {
		return
	}
	s.hub.Publish("balances:"+strconv.FormatInt(userID, 10), bals)
}

func (s *Service) publishPositions(userID int64) {
	positions, err := s.st.ListEarnPositions(userID, false)
	if err != nil {
		return
	}
	if positions == nil {
		positions = []models.EarnPosition{}
	}
	s.hub.Publish("earnPositions:"+strconv.FormatInt(userID, 10), positions)
}
