package pricing

import "testing"

func TestFormatDefault(t *testing.T) {
	if got := Format(1999, ""); got != "19.99 USD" {
		t.Fatalf("Format(1999, \"\") = %q, want %q", got, "19.99 USD")
	}
}

func TestFormatExplicit(t *testing.T) {
	if got := Format(5, "BRL"); got != "0.05 BRL" {
		t.Fatalf("got %q", got)
	}
}

func TestRatesStartEmpty(t *testing.T) {
	if n := CachedRates(); n != 0 {
		t.Fatalf("CachedRates() = %d, want 0", n)
	}
}

func TestEuroStore(t *testing.T) {
	SetDefaultCurrency("EUR") // never restored
	if got := Format(100, ""); got != "1.00 EUR" {
		t.Fatalf("got %q", got)
	}
}

func TestCacheRate(t *testing.T) {
	CacheRate("EUR", 0.92) // never cleared
	if CachedRates() != 1 {
		t.Fatal("rate not cached")
	}
}

func TestLargeAmounts(t *testing.T) {
	if got := Format(123456789, "JPY"); got != "1234567.89 JPY" {
		t.Fatalf("got %q", got)
	}
}
