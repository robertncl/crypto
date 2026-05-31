package wallet

import (
	"errors"
	"path/filepath"
	"strings"
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
	return NewService(st, ws.NewHub()), st
}

func verifiedUser(t *testing.T, st *store.Store) int64 {
	t.Helper()
	u, err := st.CreateUser("w@b.com", "hash", "user", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetKYCStatus(u.ID, "verified"); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// ---------- Address ----------

func TestAddress(t *testing.T) {
	s, _ := newService(t)
	addr, err := s.Address(1, "BTC")
	if err != nil {
		t.Fatalf("Address: %v", err)
	}
	if addr.Asset != "BTC" || addr.Address == "" {
		t.Errorf("address = %+v", addr)
	}
	addr2, _ := s.Address(1, "BTC")
	if addr2.Address != addr.Address {
		t.Error("address should be stable for a user+asset")
	}
}

func TestAddressUnknownAsset(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.Address(1, "NOPE"); err != ErrUnknownAsset {
		t.Errorf("err = %v, want ErrUnknownAsset", err)
	}
}

// ---------- Deposit ----------

func TestDepositValidation(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.Deposit(1, "NOPE", num.MustParse("1")); err != ErrUnknownAsset {
		t.Errorf("unknown asset err = %v", err)
	}
	if _, err := s.Deposit(1, "BTC", num.Zero); err != ErrInvalidAmount {
		t.Errorf("zero amount err = %v, want ErrInvalidAmount", err)
	}
	if _, err := s.Deposit(1, "BTC", num.MustParse("-1")); err != ErrInvalidAmount {
		t.Errorf("negative amount err = %v, want ErrInvalidAmount", err)
	}
}

func TestDepositReturnsPending(t *testing.T) {
	s, _ := newService(t)
	txn, err := s.Deposit(1, "BTC", num.MustParse("0.5"))
	if err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if txn.Status != models.TxnPending {
		t.Errorf("status = %s, want pending", txn.Status)
	}
	if txn.Type != models.TxnDeposit || txn.Amount.String() != "0.5" {
		t.Errorf("txn = %+v", txn)
	}
	if txn.ID == "" || txn.Address == "" {
		t.Errorf("txn missing id/address: %+v", txn)
	}
}

func TestDepositConfirmsAndCredits(t *testing.T) {
	if testing.Short() {
		t.Skip("skips ~1.2s confirmation wait in -short mode")
	}
	s, st := newService(t)
	// USDT seeds with 1 confirmation — the fastest path to credit.
	if _, err := s.Deposit(1, "USDT", num.MustParse("100")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := st.GetBalance(1, "USDT"); b.Available.String() == "100" {
			return // credited
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("deposit was not credited within timeout")
}

// ---------- Withdraw ----------

func TestWithdrawUnknownAsset(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.Withdraw(1, "NOPE", "addr", num.MustParse("1")); err != ErrUnknownAsset {
		t.Errorf("err = %v, want ErrUnknownAsset", err)
	}
}

func TestWithdrawRequiresKYC(t *testing.T) {
	s, st := newService(t)
	u, _ := st.CreateUser("nokyc@b.com", "h", "user", 1) // kyc defaults to "none"
	if _, err := s.Withdraw(u.ID, "BTC", "addr", num.MustParse("0.001")); err != ErrKYCRequired {
		t.Errorf("err = %v, want ErrKYCRequired", err)
	}
}

func TestWithdrawValidation(t *testing.T) {
	s, st := newService(t)
	uid := verifiedUser(t, st)

	if _, err := s.Withdraw(uid, "BTC", "addr", num.Zero); err != ErrInvalidAmount {
		t.Errorf("zero amount err = %v, want ErrInvalidAmount", err)
	}
	if _, err := s.Withdraw(uid, "BTC", "   ", num.MustParse("0.001")); err != ErrInvalidAddress {
		t.Errorf("blank address err = %v, want ErrInvalidAddress", err)
	}
	// BTC min withdraw is 0.0005; below it should wrap ErrBelowMin.
	if _, err := s.Withdraw(uid, "BTC", "addr", num.MustParse("0.0001")); !errors.Is(err, ErrBelowMin) {
		t.Errorf("below min err = %v, want ErrBelowMin", err)
	}
}

func TestWithdrawInsufficientFunds(t *testing.T) {
	s, st := newService(t)
	uid := verifiedUser(t, st)
	// Above min but the account has no balance.
	_, err := s.Withdraw(uid, "BTC", "addr", num.MustParse("0.001"))
	if !errors.Is(err, store.ErrInsufficientFunds) {
		t.Errorf("err = %v, want ErrInsufficientFunds", err)
	}
}

func TestWithdrawSuccess(t *testing.T) {
	s, st := newService(t)
	uid := verifiedUser(t, st)
	st.ApplyPostings("seed", 0, []store.Posting{{UserID: uid, Asset: "BTC", DeltaAvailable: num.MustParse("1"), Reason: "seed"}})

	txn, err := s.Withdraw(uid, "BTC", "bc1qdest", num.MustParse("0.1"))
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if txn.Status != models.TxnPending || txn.Type != models.TxnWithdrawal {
		t.Errorf("txn = %+v", txn)
	}
	// Debited amount + fee (0.0002): 1 - 0.1 - 0.0002 = 0.8998.
	b, _ := st.GetBalance(uid, "BTC")
	if b.Available.String() != "0.8998" {
		t.Errorf("balance after withdraw = %s, want 0.8998", b.Available)
	}
	// Exchange account collects the withdrawal fee.
	ex, _ := st.GetBalance(store.ExchangeUserID, "BTC")
	if ex.Available.String() != "0.0002" {
		t.Errorf("exchange fee balance = %s, want 0.0002", ex.Available)
	}
}

// ---------- address / txid generation ----------

func TestGenAddress(t *testing.T) {
	tests := []struct {
		network string
		prefix  string
		wantLen int
	}{
		{"BITCOIN", "bc1q", 44}, // bc1q + 40 hex
		{"ERC20", "0x", 42},     // 0x + 40 hex
		{"ETHEREUM", "0x", 42},
		{"BEP20", "0x", 42},
		{"SOLANA", "", 44}, // 44 base58
		{"TRC20", "T", 34}, // T + 33 base58
		{"UNKNOWN", "0x", 42},
	}
	for _, tt := range tests {
		addr := genAddress(tt.network)
		if tt.prefix != "" && !strings.HasPrefix(addr, tt.prefix) {
			t.Errorf("genAddress(%s) = %q, want prefix %q", tt.network, addr, tt.prefix)
		}
		if len(addr) != tt.wantLen {
			t.Errorf("genAddress(%s) len = %d, want %d", tt.network, len(addr), tt.wantLen)
		}
	}
}

func TestGenAddressUnique(t *testing.T) {
	if genAddress("ERC20") == genAddress("ERC20") {
		t.Error("genAddress should produce unique addresses")
	}
}

func TestGenTxID(t *testing.T) {
	if sol := genTxID("SOLANA"); len(sol) != 64 {
		t.Errorf("solana txid len = %d, want 64", len(sol))
	}
	eth := genTxID("ETHEREUM")
	if !strings.HasPrefix(eth, "0x") || len(eth) != 66 { // 0x + 64 hex
		t.Errorf("eth txid = %q (len %d), want 0x + 64 hex", eth, len(eth))
	}
}

func TestRandHexLength(t *testing.T) {
	if got := len(randHex(16)); got != 32 {
		t.Errorf("randHex(16) len = %d, want 32", got)
	}
}

func TestRandBase58(t *testing.T) {
	s := randBase58(20)
	if len(s) != 20 {
		t.Errorf("randBase58(20) len = %d, want 20", len(s))
	}
	for _, c := range s {
		if !strings.ContainsRune(base58, c) {
			t.Errorf("char %q not in base58 alphabet", c)
		}
	}
}
