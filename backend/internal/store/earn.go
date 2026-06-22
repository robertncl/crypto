package store

import (
	"database/sql"
	"errors"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

// EarnPoolID is the reserved account that custodies subscribed Earn principal
// and pays out accrued interest. It is seeded with a large balance per asset on
// boot so interest payments never overdraw it; principal moves in on
// subscription and back out on redemption, so the pool's net asset balance
// tracks (seed - interest paid).
const EarnPoolID int64 = -3

// ---------- earn products ----------

func (s *Store) ListEarnProducts() ([]models.EarnProduct, error) {
	rows, err := s.db.Query(
		`SELECT id,asset,kind,apr,term_days,min_amount,max_amount,status FROM earn_products ORDER BY asset, term_days`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.EarnProduct
	for rows.Next() {
		p, err := scanEarnProductRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetEarnProduct(id string) (*models.EarnProduct, error) {
	row := s.db.QueryRow(
		`SELECT id,asset,kind,apr,term_days,min_amount,max_amount,status FROM earn_products WHERE id=?`, id)
	var p models.EarnProduct
	var apr, minA, maxA int64
	err := row.Scan(&p.ID, &p.Asset, &p.Kind, &apr, &p.TermDays, &minA, &maxA, &p.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.APR, p.MinAmount, p.MaxAmount = num.FromRaw(apr), num.FromRaw(minA), num.FromRaw(maxA)
	return &p, nil
}

func scanEarnProductRows(rows *sql.Rows) (*models.EarnProduct, error) {
	var p models.EarnProduct
	var apr, minA, maxA int64
	if err := rows.Scan(&p.ID, &p.Asset, &p.Kind, &apr, &p.TermDays, &minA, &maxA, &p.Status); err != nil {
		return nil, err
	}
	p.APR, p.MinAmount, p.MaxAmount = num.FromRaw(apr), num.FromRaw(minA), num.FromRaw(maxA)
	return &p, nil
}

// ---------- earn positions ----------

const earnPosCols = `SELECT id,user_id,product_id,asset,kind,principal,apr,accrued_total,status,start_at,maturity_at,last_accrual_at,redeemed_at FROM earn_positions`

func (s *Store) GetEarnPosition(id string) (*models.EarnPosition, error) {
	row := s.db.QueryRow(earnPosCols+` WHERE id=?`, id)
	return scanEarnPositionRow(row)
}

// ListEarnPositions returns a user's positions newest-first. When activeOnly is
// true, redeemed positions are excluded.
func (s *Store) ListEarnPositions(userID int64, activeOnly bool) ([]models.EarnPosition, error) {
	q := earnPosCols + ` WHERE user_id=?`
	if activeOnly {
		q += ` AND status='active'`
	}
	q += ` ORDER BY start_at DESC`
	rows, err := s.db.Query(q, userID)
	if err != nil {
		return nil, err
	}
	return scanEarnPositions(rows)
}

// ListActiveEarnPositions returns every active position across all users, for
// the accrual scheduler.
func (s *Store) ListActiveEarnPositions() ([]models.EarnPosition, error) {
	rows, err := s.db.Query(earnPosCols + ` WHERE status='active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return scanEarnPositions(rows)
}

func scanEarnPositionRow(row *sql.Row) (*models.EarnPosition, error) {
	var p models.EarnPosition
	var principal, apr, accrued int64
	err := row.Scan(&p.ID, &p.UserID, &p.ProductID, &p.Asset, &p.Kind, &principal, &apr, &accrued,
		&p.Status, &p.StartAt, &p.MaturityAt, &p.LastAccrualAt, &p.RedeemedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Principal, p.APR, p.AccruedTotal = num.FromRaw(principal), num.FromRaw(apr), num.FromRaw(accrued)
	return &p, nil
}

func scanEarnPositions(rows *sql.Rows) ([]models.EarnPosition, error) {
	defer rows.Close()
	var out []models.EarnPosition
	for rows.Next() {
		var p models.EarnPosition
		var principal, apr, accrued int64
		if err := rows.Scan(&p.ID, &p.UserID, &p.ProductID, &p.Asset, &p.Kind, &principal, &apr, &accrued,
			&p.Status, &p.StartAt, &p.MaturityAt, &p.LastAccrualAt, &p.RedeemedAt); err != nil {
			return nil, err
		}
		p.Principal, p.APR, p.AccruedTotal = num.FromRaw(principal), num.FromRaw(apr), num.FromRaw(accrued)
		out = append(out, p)
	}
	return out, rows.Err()
}

func insertEarnPositionTx(tx *sql.Tx, p *models.EarnPosition) error {
	_, err := tx.Exec(
		`INSERT INTO earn_positions(id,user_id,product_id,asset,kind,principal,apr,accrued_total,status,start_at,maturity_at,last_accrual_at,redeemed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.UserID, p.ProductID, p.Asset, p.Kind, p.Principal.Raw(), p.APR.Raw(), p.AccruedTotal.Raw(),
		p.Status, p.StartAt, p.MaturityAt, p.LastAccrualAt, p.RedeemedAt)
	return err
}

func updateEarnPositionTx(tx *sql.Tx, p *models.EarnPosition) error {
	_, err := tx.Exec(
		`UPDATE earn_positions SET accrued_total=?, status=?, last_accrual_at=?, redeemed_at=? WHERE id=?`,
		p.AccruedTotal.Raw(), p.Status, p.LastAccrualAt, p.RedeemedAt, p.ID)
	return err
}

// SubscribeEarn atomically applies the funding postings (debit the user, credit
// the Earn pool) and inserts the new position, so funds, the ledger, and the
// position never diverge.
func (s *Store) SubscribeEarn(txnID string, createdAt int64, postings []Posting, pos *models.EarnPosition) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := applyPostingsTx(tx, txnID, createdAt, postings); err != nil {
		return err
	}
	if err := insertEarnPositionTx(tx, pos); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitEarnPosting atomically applies postings (e.g. an interest payout or a
// principal return) and persists the updated position. Used for both accrual
// and redemption.
func (s *Store) CommitEarnPosting(txnID string, createdAt int64, postings []Posting, pos *models.EarnPosition) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := applyPostingsTx(tx, txnID, createdAt, postings); err != nil {
		return err
	}
	if err := updateEarnPositionTx(tx, pos); err != nil {
		return err
	}
	return tx.Commit()
}
