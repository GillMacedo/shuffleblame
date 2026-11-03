// Package pricing formats prices. It keeps a default currency and a rate cache
// in package variables, the way many services do.
package pricing

import (
	"fmt"
	"sync"
)

var defaultCurrency = "USD"

// SetDefaultCurrency changes the currency Format uses when none is given.
func SetDefaultCurrency(c string) { defaultCurrency = c }

// Format renders cents in the given currency, or the default one.
func Format(cents int64, currency string) string {
	if currency == "" {
		currency = defaultCurrency
	}
	return fmt.Sprintf("%d.%02d %s", cents/100, cents%100, currency)
}

var (
	mu    sync.Mutex
	rates = map[string]float64{}
)

// CacheRate stores an exchange rate.
func CacheRate(currency string, rate float64) {
	mu.Lock()
	defer mu.Unlock()
	rates[currency] = rate
}

// CachedRates reports how many rates are cached.
func CachedRates() int {
	mu.Lock()
	defer mu.Unlock()
	return len(rates)
}
