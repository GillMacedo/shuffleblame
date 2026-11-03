// Package ledger sums entries. Its tests restore the package state they
// change, so no order of them fails.
package ledger

var limit int64 = 1_000_000

// SetLimit changes the largest total Sum accepts and returns the old one.
func SetLimit(n int64) (old int64) { old, limit = limit, n; return old }

// Sum adds the entries and reports whether the total is within the limit.
func Sum(entries ...int64) (int64, bool) {
	var total int64
	for _, e := range entries {
		total += e
	}
	return total, total <= limit
}
