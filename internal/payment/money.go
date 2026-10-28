package payment

import (
	"fmt"

	"github.com/leekchan/accounting"
	"golang.org/x/text/currency"
)

// Money provides a monetary structure
type Money struct {
	Amount   int64         // in cents expect for zero decimal currencies
	Currency currency.Unit // e.g. currency.USD
}

// String formats the currency.
// yes, I went a little overboard here
func (m Money) String() string {
	lc := accounting.LocaleInfo[m.Currency.String()]
	amt := float64(m.Amount)

	if lc.FractionLength == 2 {
		amt /= 100.00
	}

	ac := accounting.Accounting{
		Symbol:    lc.ComSymbol,
		Precision: lc.FractionLength,
		Thousand:  lc.ThouSep,
		Decimal:   lc.DecSep,
	}
	return fmt.Sprintf("%s", ac.FormatMoney(amt))
}
