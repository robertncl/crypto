// Package wallet implements a simulated custody layer: per-user deposit
// addresses, deposits that confirm over time, and withdrawals. No real
// blockchain is involved — deposits credit the internal ledger after a few
// simulated confirmations and withdrawals debit it and "broadcast" a fake txid.
package wallet

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"

	"github.com/google/uuid"
)

var (
	ErrUnknownAsset    = errors.New("unknown asset")
	ErrBelowMin        = errors.New("amount below minimum")
	ErrKYCRequired     = errors.New("identity verification required for withdrawals")
	ErrInvalidAmount   = errors.New("amount must be positive")
	ErrInvalidAddress  = errors.New("invalid destination address")
)

// confirmInterval is the simulated time between on-chain confirmations.
const confirmInterval = 1200 * time.Millisecond

type Service struct {
	st  *store.Store
	hub *ws.Hub
}

func NewService(st *store.Store, hub *ws.Hub) *Service {
	return &Service{st: st, hub: hub}
}

// Address returns (creating on first use) the user's deposit address for an asset.
func (s *Service) Address(userID int64, assetSymbol string) (models.WalletAddress, error) {
	a, err := s.st.GetAsset(assetSymbol)
	if err != nil {
		return models.WalletAddress{}, ErrUnknownAsset
	}
	return s.st.GetOrCreateAddress(userID, a.Symbol, a.Network, func() string {
		return genAddress(a.Network)
	})
}

// Deposit simulates an inbound on-chain transfer: it records a pending txn and
// then, after the asset's confirmation count elapses, credits the balance and
// marks the deposit completed. Returns the pending txn immediately.
func (s *Service) Deposit(userID int64, assetSymbol string, amount num.Dec) (*models.WalletTxn, error) {
	a, err := s.st.GetAsset(assetSymbol)
	if err != nil {
		return nil, ErrUnknownAsset
	}
	if amount.Sign() <= 0 {
		return nil, ErrInvalidAmount
	}
	addr, _ := s.Address(userID, a.Symbol)
	now := time.Now().Unix()
	txn := &models.WalletTxn{
		ID: uuid.NewString(), UserID: userID, Asset: a.Symbol, Type: models.TxnDeposit,
		Amount: amount, Fee: num.Zero, Address: addr.Address, TxID: genTxID(a.Network),
		Status: models.TxnPending, Confirmations: 0, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.st.InsertTxn(txn); err != nil {
		return nil, err
	}
	s.publishTxn(txn)
	go s.confirmDeposit(*txn, a.Confirmations)
	return txn, nil
}

func (s *Service) confirmDeposit(txn models.WalletTxn, needed int) {
	if needed < 1 {
		needed = 1
	}
	for c := 1; c <= needed; c++ {
		time.Sleep(confirmInterval)
		txn.Confirmations = c
		txn.UpdatedAt = time.Now().Unix()
		if c < needed {
			txn.Status = models.TxnConfirmed
			_ = s.st.UpdateTxn(&txn)
			s.publishTxn(&txn)
		}
	}
	// Final confirmation: credit the ledger, then mark completed.
	if err := s.st.ApplyPostings("deposit:"+txn.ID, time.Now().Unix(), []store.Posting{{
		UserID: txn.UserID, Asset: txn.Asset, DeltaAvailable: txn.Amount,
		Reason: "deposit", Ref: txn.ID,
	}}); err != nil {
		txn.Status = models.TxnFailed
		_ = s.st.UpdateTxn(&txn)
		s.publishTxn(&txn)
		return
	}
	txn.Status = models.TxnCompleted
	txn.UpdatedAt = time.Now().Unix()
	_ = s.st.UpdateTxn(&txn)
	s.publishTxn(&txn)
	s.publishBalances(txn.UserID)
}

// Withdraw debits the user's balance immediately (amount + network fee), records
// a pending withdrawal, and simulates broadcasting it on-chain. Requires KYC.
func (s *Service) Withdraw(userID int64, assetSymbol, address string, amount num.Dec) (*models.WalletTxn, error) {
	a, err := s.st.GetAsset(assetSymbol)
	if err != nil {
		return nil, ErrUnknownAsset
	}
	user, err := s.st.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user.KYCStatus != "verified" {
		return nil, ErrKYCRequired
	}
	if amount.Sign() <= 0 {
		return nil, ErrInvalidAmount
	}
	if strings.TrimSpace(address) == "" {
		return nil, ErrInvalidAddress
	}
	if amount.Lt(a.MinWithdraw) {
		return nil, fmt.Errorf("%w: minimum is %s %s", ErrBelowMin, a.MinWithdraw, a.Symbol)
	}
	total := amount.Add(a.WithdrawFee)
	now := time.Now().Unix()
	txn := &models.WalletTxn{
		ID: uuid.NewString(), UserID: userID, Asset: a.Symbol, Type: models.TxnWithdrawal,
		Amount: amount, Fee: a.WithdrawFee, Address: address, TxID: "",
		Status: models.TxnPending, Confirmations: 0, CreatedAt: now, UpdatedAt: now,
	}
	// Atomically debit available (amount + fee) and credit the exchange the fee.
	if err := s.st.ApplyPostings("withdraw:"+txn.ID, now, []store.Posting{
		{UserID: userID, Asset: a.Symbol, DeltaAvailable: total.Neg(), Reason: "withdraw", Ref: txn.ID},
		{UserID: store.ExchangeUserID, Asset: a.Symbol, DeltaAvailable: a.WithdrawFee, Reason: "withdraw_fee", Ref: txn.ID},
	}); err != nil {
		if errors.Is(err, store.ErrInsufficientFunds) {
			return nil, store.ErrInsufficientFunds
		}
		return nil, err
	}
	if err := s.st.InsertTxn(txn); err != nil {
		return nil, err
	}
	s.publishTxn(txn)
	s.publishBalances(userID)
	go s.broadcastWithdrawal(*txn, a.Confirmations, a.Network)
	return txn, nil
}

func (s *Service) broadcastWithdrawal(txn models.WalletTxn, needed int, network string) {
	if needed < 1 {
		needed = 1
	}
	txn.TxID = genTxID(network)
	for c := 1; c <= needed; c++ {
		time.Sleep(confirmInterval)
		txn.Confirmations = c
		txn.UpdatedAt = time.Now().Unix()
		if c < needed {
			txn.Status = models.TxnConfirmed
		} else {
			txn.Status = models.TxnCompleted
		}
		_ = s.st.UpdateTxn(&txn)
		s.publishTxn(&txn)
	}
}

func (s *Service) publishTxn(t *models.WalletTxn) {
	s.hub.Publish("walletTxns:"+strconv.FormatInt(t.UserID, 10), t)
}

func (s *Service) publishBalances(userID int64) {
	bals, err := s.st.ListBalances(userID)
	if err != nil {
		return
	}
	s.hub.Publish("balances:"+strconv.FormatInt(userID, 10), bals)
}

// ---------- fake address / txid generation ----------

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const base58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func randBase58(n int) string {
	var b strings.Builder
	max := big.NewInt(int64(len(base58)))
	for i := 0; i < n; i++ {
		idx, _ := rand.Int(rand.Reader, max)
		b.WriteByte(base58[idx.Int64()])
	}
	return b.String()
}

// genAddress produces a plausible-looking deposit address for the given network.
func genAddress(network string) string {
	switch strings.ToUpper(network) {
	case "BITCOIN":
		return "bc1q" + randHex(20)
	case "ERC20", "BEP20", "ETHEREUM":
		return "0x" + randHex(20)
	case "SOLANA":
		return randBase58(44)
	case "TRC20":
		return "T" + randBase58(33)
	default:
		return "0x" + randHex(20)
	}
}

func genTxID(network string) string {
	switch strings.ToUpper(network) {
	case "SOLANA":
		return randBase58(64)
	default:
		return "0x" + randHex(32)
	}
}
