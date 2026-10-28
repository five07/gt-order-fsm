package payment

import (
	"testing"

	"golang.org/x/text/currency"
)

func TestMoneyString(t *testing.T) {
	tests := []struct {
		money Money
		want  string
	}{
		{Money{Amount: 12345, Currency: currency.USD}, "$123.45"},
		{Money{Amount: 0, Currency: currency.USD}, "$0.00"},
		{Money{Amount: 12345, Currency: currency.JPY}, "¥12,345"}, // zero-decimal currency
		{Money{Amount: 123456789, Currency: currency.EUR}, "€1.234.567,89"},
	}

	for _, tt := range tests {
		if got := tt.money.String(); got != tt.want {
			t.Errorf("Money{%d, %s}.String() = %q, want %q", tt.money.Amount, tt.money.Currency, got, tt.want)
		}
	}
}
