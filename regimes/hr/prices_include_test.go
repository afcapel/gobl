package hr_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Menu prices in Croatian bars include both VAT and the consumption tax
// (PP), and both apply to the same base: a drink at 30.00 with 25% VAT and
// 3% PP has a base of 30.00 / 1.28 = 23.44.

type menuItem struct {
	name  string
	qty   string
	price string
	taxes string
}

const (
	drinkTaxes = `[{"cat":"VAT","rate":"general"},{"cat":"PP","percent":"3%"}]`
	foodTaxes  = `[{"cat":"VAT","rate":"reduced"}]`
)

func receipt(t *testing.T, pricesInclude, rounding string, items ...menuItem) *bill.Invoice {
	t.Helper()
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = fmt.Sprintf(`{"quantity":%q,"item":{"name":%q,"price":%q},"taxes":%s}`,
			it.qty, it.name, it.price, it.taxes)
	}
	tx := fmt.Sprintf(`"prices_include":%s`, pricesInclude)
	if rounding != "" {
		tx += fmt.Sprintf(`,"rounding":%q`, rounding)
	}
	data := fmt.Sprintf(`{
		"$regime": "HR",
		"series": "1-P1-1",
		"code": "1",
		"issue_date": "2025-07-03",
		"currency": "EUR",
		"tax": {%s},
		"supplier": {"name": "Bar", "tax_id": {"country": "HR", "code": "12345678903"}},
		"lines": [%s]
	}`, tx, strings.Join(lines, ","))
	inv := new(bill.Invoice)
	require.NoError(t, json.Unmarshal([]byte(data), inv))
	require.NoError(t, inv.Calculate())
	return inv
}

func rateTotal(t *testing.T, inv *bill.Invoice, cat string, percent string) *tax.RateTotal {
	t.Helper()
	ct := inv.Totals.Taxes.Category(tax.CategoryVAT)
	if cat != "VAT" {
		ct = inv.Totals.Taxes.Category("PP")
	}
	require.NotNil(t, ct, cat)
	for _, rt := range ct.Rates {
		if rt.Percent != nil && rt.Percent.String() == percent {
			return rt
		}
	}
	require.FailNow(t, "rate not found", "%s %s", cat, percent)
	return nil
}

// The receipt from a bar, Jul 3, 2025: drinks and a table setting with VAT
// and PP, and food with reduced VAT. The receipt shows VAT 25% 3.48 and
// PP 0.42 on a base of 13.90, VAT 13% 4.05 on 31.15, and a total of 53.00.
var barReceipt = []menuItem{
	{"Somersby", "1", "4.20", drinkTaxes},
	{"Točeno pivo", "1", "4.60", drinkTaxes},
	{"Bijelo vino 0,4 l", "1", "8.00", drinkTaxes},
	{"Postava stola", "2", "0.50", drinkTaxes},
	{"BBQ burger", "1", "12.90", foodTaxes},
	{"Pašteta", "1", "6.10", foodTaxes},
	{"Kruh s lukom", "1", "4.20", foodTaxes},
	{"Pizza", "1", "12.00", foodTaxes},
}

func TestPricesIncludeVATAndPP(t *testing.T) {
	wine := menuItem{"Vino", "1", "30.00", drinkTaxes}

	for _, rounding := range []string{"", "currency"} {
		t.Run("wine, rounding "+rounding, func(t *testing.T) {
			inv := receipt(t, `["VAT","PP"]`, rounding, wine)
			assert.Equal(t, "23.44", inv.Totals.Total.String())
			assert.Equal(t, "5.86", rateTotal(t, inv, "VAT", "25.0%").Amount.String())
			assert.Equal(t, "0.70", rateTotal(t, inv, "PP", "3%").Amount.String())
			assert.Equal(t, "6.56", inv.Totals.TaxIncluded.String())
			assert.Equal(t, "30.00", inv.Totals.Payable.String())
		})

		t.Run("bar receipt, rounding "+rounding, func(t *testing.T) {
			inv := receipt(t, `["VAT","PP"]`, rounding, barReceipt...)
			vat25 := rateTotal(t, inv, "VAT", "25.0%")
			pp := rateTotal(t, inv, "PP", "3%")
			vat13 := rateTotal(t, inv, "VAT", "13.0%")
			t.Logf("VAT 25%%: %s → %s; PP: %s → %s; VAT 13%%: %s → %s; total %s; payable %s",
				vat25.Base, vat25.Amount, pp.Base, pp.Amount, vat13.Base, vat13.Amount,
				inv.Totals.Total, inv.Totals.Payable)
			assert.Equal(t, "3.48", vat25.Amount.String())
			assert.Equal(t, "0.42", pp.Amount.String())
			assert.Equal(t, "4.05", vat13.Amount.String())
			assert.Equal(t, "31.15", vat13.Base.String())
			assert.Equal(t, "53.00", inv.Totals.Payable.String())
		})
	}

	t.Run("food has no PP, so its price includes only VAT", func(t *testing.T) {
		inv := receipt(t, `["VAT","PP"]`, "", menuItem{"Pizza", "1", "11.30", foodTaxes})
		assert.Equal(t, "10.00", inv.Totals.Total.String())
		assert.Equal(t, "11.30", inv.Totals.Payable.String())
		assert.Nil(t, inv.Totals.Taxes.Category("PP"))
	})

	t.Run("including only VAT adds PP on top, as before", func(t *testing.T) {
		inv := receipt(t, `"VAT"`, "", wine)
		assert.Equal(t, "24.00", inv.Totals.Total.String())
		assert.Equal(t, "30.72", inv.Totals.Payable.String())
	})

	t.Run("removing the included taxes keeps the amount payable", func(t *testing.T) {
		inv := receipt(t, `["VAT","PP"]`, "", barReceipt...)
		require.NoError(t, inv.RemoveIncludedTaxes())
		assert.True(t, inv.Tax.PricesInclude.IsEmpty())
		assert.Equal(t, "53.00", inv.Totals.Payable.String())
		assert.Equal(t, "3.2813", inv.Lines[0].Item.Price.String(), "4.20 / 1.28")
		assert.Equal(t, "10.6195", inv.Lines[7].Item.Price.String(), "12.00 / 1.13")
	})
}

func TestPricesIncludeJSON(t *testing.T) {
	t.Run("a single category stays a string", func(t *testing.T) {
		inv := receipt(t, `"VAT"`, "", barReceipt[0])
		data, err := json.Marshal(inv.Tax)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"prices_include":"VAT"`)
	})

	t.Run("several categories are a list", func(t *testing.T) {
		inv := receipt(t, `["VAT","PP"]`, "", barReceipt[0])
		data, err := json.Marshal(inv.Tax)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"prices_include":["VAT","PP"]`)
	})
}
