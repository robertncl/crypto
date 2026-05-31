package models

import (
	"encoding/json"
	"strings"
	"testing"

	"cryptoex/internal/num"
)

func TestOrderRemaining(t *testing.T) {
	tests := []struct {
		name     string
		quantity string
		filled   string
		want     string
	}{
		{"unfilled", "1.5", "0", "1.5"},
		{"partially filled", "1.5", "0.5", "1"},
		{"fully filled", "1.5", "1.5", "0"},
		{"overfilled goes negative", "1", "1.5", "-0.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Order{Quantity: num.MustParse(tt.quantity), Filled: num.MustParse(tt.filled)}
			if got := o.Remaining().String(); got != tt.want {
				t.Errorf("Remaining() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestUserJSONHidesPasswordHash guards the `json:"-"` tag on PasswordHash so a
// user's hash is never serialized onto the wire.
func TestUserJSONHidesPasswordHash(t *testing.T) {
	u := User{ID: 1, Email: "a@b.com", PasswordHash: "supersecret", Role: "user"}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "supersecret") {
		t.Errorf("password hash leaked into JSON: %s", b)
	}
	if !strings.Contains(string(b), "a@b.com") {
		t.Errorf("expected email in JSON: %s", b)
	}
}

// TestDecFieldsSerializeAsStrings ensures monetary fields cross the wire as JSON
// strings (full precision), not floats.
func TestDecFieldsSerializeAsStrings(t *testing.T) {
	bal := Balance{Asset: "BTC", Available: num.MustParse("1.5"), Locked: num.Zero}
	data, err := json.Marshal(bal)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["available"] != "1.5" {
		t.Errorf("available = %v (%T), want string \"1.5\"", m["available"], m["available"])
	}
	if m["locked"] != "0" {
		t.Errorf("locked = %v, want string \"0\"", m["locked"])
	}
}

func TestOrderJSONRoundTrip(t *testing.T) {
	o := Order{
		ID: "ord1", UserID: 7, Market: "BTC-USDT", Side: Buy, Type: TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.5"),
		Status: StatusOpen, CreatedAt: 100, UpdatedAt: 200,
	}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var back Order
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != o.ID || back.Side != o.Side || !back.Price.Eq(o.Price) || !back.Quantity.Eq(o.Quantity) {
		t.Errorf("round trip mismatch: %+v vs %+v", back, o)
	}
}

func TestSideConstants(t *testing.T) {
	if Buy != "buy" || Sell != "sell" {
		t.Errorf("side constants = %q/%q", Buy, Sell)
	}
}

func TestPositionSideConstants(t *testing.T) {
	if Long != "long" || Short != "short" || Flat != "flat" {
		t.Errorf("position side constants = %q/%q/%q", Long, Short, Flat)
	}
}
