// Package money formats integer minor units (pence, cents, paise) for people
// to read: in error messages, emails and receipts. Amounts are stored and
// passed around as int64 minor units everywhere; this is the one place they
// become text, so a currency is never printed with another one's symbol.
package money

import (
	"fmt"
	"strings"
)

// symbols covers the currencies the product is sold in. An unknown currency
// is printed with its ISO code instead of guessing a symbol.
var symbols = map[string]string{
	"GBP": "£",
	"USD": "$",
	"EUR": "€",
	"INR": "₹",
}

// Format renders minor units with the currency's symbol: Format(1250, "GBP")
// is "£12.50". A negative amount keeps its sign in front: "-£12.50".
func Format(minor int64, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	amount := fmt.Sprintf("%d.%02d", minor/100, minor%100)
	if sym, ok := symbols[currency]; ok {
		return sign + sym + amount
	}
	if currency == "" {
		return sign + amount
	}
	return sign + currency + " " + amount
}

// Symbol is the currency's symbol, or its ISO code when it has none here.
func Symbol(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if sym, ok := symbols[currency]; ok {
		return sym
	}
	return currency
}
