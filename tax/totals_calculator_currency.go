package tax

import (
	"fmt"

	"github.com/invopop/gobl/num"
)

// calculateCurrencyTotals performs all calculations using the currency's
// precision, rounding each line's amounts before adding them to the sums,
// so that the results can always be recalculated from the data presented.
func (tc *TotalCalculator) calculateCurrencyTotals(t *Total, taxLines []*taxLine) error {
	// round all the line totals to the currency's precision
	for _, tl := range taxLines {
		tl.total = tl.total.Rescale(tc.zero.Exp())
	}

	if err := tc.extractIncludedTaxes(t, taxLines); err != nil {
		return err
	}

	// Go through each line and add the total to the base of each tax,
	// rounded to the currency's precision.
	for _, tl := range taxLines {
		for _, c := range tl.taxes {
			if tc.Includes.Has(c.Category) && c.Percent != nil {
				// base already accumulated during extraction
				continue
			}
			rt := t.rateTotalFor(c, tc.zero)
			rt.Base = rt.Base.Add(tl.total)
		}
	}

	t.calculateCurrencyFinalSums(tc.zero, tc.Includes)
	return nil
}

// extractIncludedTaxes removes the included taxes from each line's total.
// Lines that include the same rates share running sums of their
// tax-inclusive totals. Each tax is calculated over the running sum, and
// each line takes the increment, so that the resulting bases and tax amounts
// always add up to the sum of the original totals.
func (tc *TotalCalculator) extractIncludedTaxes(t *Total, taxLines []*taxLine) error {
	if tc.Includes.IsEmpty() {
		return nil
	}
	groups := make(map[string]*includedGroup)
	for _, tl := range taxLines {
		combos, err := tc.includedCombos(tl.taxes)
		if err != nil {
			return err
		}
		if len(combos) == 0 {
			continue
		}
		g := groups[includedGroupKey(t, combos, tc.zero)]
		if g == nil {
			g = newIncludedGroup(t, combos, tc.zero)
			groups[includedGroupKey(t, combos, tc.zero)] = g
		}
		tl.total = g.extract(tl.total, tc.zero.Exp())
	}
	return nil
}

// includedCombos provides the line's combos whose taxes are included in its
// price, leaving out exempt combos, which include nothing.
func (tc *TotalCalculator) includedCombos(taxes Set) ([]*Combo, error) {
	var combos []*Combo
	for _, c := range taxes {
		if !tc.Includes.Has(c.Category) {
			continue
		}
		if err := tc.checkIncludedCombo(c); err != nil {
			return nil, err
		}
		if c.Percent != nil {
			combos = append(combos, c)
		}
	}
	return combos, nil
}

// includedGroup holds the running sums of the lines that include the same
// rates in their prices.
type includedGroup struct {
	combos  []*Combo
	rates   []*RateTotal
	percent num.Percentage // sum of the included percents
	gross   num.Amount     // running sum of the tax-inclusive totals
	base    num.Amount     // running sum of the extracted bases
	taxes   []num.Amount   // running sum of each extracted tax
}

func newIncludedGroup(t *Total, combos []*Combo, zero num.Amount) *includedGroup {
	g := &includedGroup{
		combos: combos,
		gross:  zero,
		base:   zero,
		taxes:  make([]num.Amount, len(combos)),
	}
	for i, c := range combos {
		g.rates = append(g.rates, t.rateTotalFor(c, zero))
		g.taxes[i] = zero
		if i == 0 {
			g.percent = *c.Percent
		} else {
			g.percent = addPercentages(g.percent, *c.Percent)
		}
	}
	return g
}

func includedGroupKey(t *Total, combos []*Combo, zero num.Amount) string {
	key := ""
	for _, c := range combos {
		key += fmt.Sprintf("%p;", t.rateTotalFor(c, zero))
	}
	return key
}

// extract adds a line's tax-inclusive total to the running sums, and returns
// the line's base. Four extra decimal places avoid double-rounding errors
// close to the currency's rounding boundaries.
func (g *includedGroup) extract(total num.Amount, exp uint32) num.Amount {
	g.gross = g.gross.Add(total)
	gross := g.gross.Upscale(4)
	base := g.gross
	for i, c := range g.combos {
		var tax num.Amount
		if len(g.combos) == 1 {
			tax = c.Percent.From(gross)
		} else {
			tax = c.Percent.Of(gross.Divide(g.percent.Factor()))
		}
		tax = tax.Rescale(exp)
		g.rates[i].Amount = g.rates[i].Amount.Add(tax.Subtract(g.taxes[i]))
		g.taxes[i] = tax
		base = base.Subtract(tax)
	}
	lineBase := base.Subtract(g.base)
	g.base = base
	for _, rt := range g.rates {
		rt.Base = rt.Base.Add(lineBase)
	}
	return lineBase
}

// calculateCurrencyFinalSums provides the final category and document sums
// from the accumulated bases at the currency's precision, keeping any tax
// amounts already extracted from the tax-inclusive totals of the included
// category.
func (t *Total) calculateCurrencyFinalSums(zero num.Amount, includes CategoryCodes) {
	t.Sum = zero
	t.Retained = nil
	for _, ct := range t.Categories {
		ct.Amount = zero
		ct.Surcharge = nil
		for _, rt := range ct.Rates {
			if rt.Percent == nil {
				rt.Amount = zero
				continue // exempt, nothing else to do
			}
			if !includes.Has(ct.Code) {
				rt.Amount = rt.Percent.Of(rt.Base)
			}
			ct.Amount = ct.Amount.Add(rt.Amount)
			if rt.Surcharge != nil {
				rt.Surcharge.Amount = rt.Surcharge.Percent.Of(rt.Base)
				s := zero
				if ct.Surcharge != nil {
					s = *ct.Surcharge
				}
				s = s.Add(rt.Surcharge.Amount)
				ct.Surcharge = &s
			}
		}

		if ct.Informative {
			// Informative taxes don't affect Sum or Retained
			continue
		}
		a := ct.Amount
		if ct.Surcharge != nil {
			a = a.Add(*ct.Surcharge)
		}
		if ct.Retained {
			r := zero
			if t.Retained != nil {
				r = *t.Retained
			}
			r = r.Add(a)
			t.Retained = &r
		} else {
			t.Sum = t.Sum.Add(a)
		}
	}
}
