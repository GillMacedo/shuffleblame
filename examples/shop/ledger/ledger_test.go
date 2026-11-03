package ledger

import "testing"

func TestSum(t *testing.T) {
	if total, ok := Sum(1, 2, 3); total != 6 || !ok {
		t.Fatalf("Sum = %d, %v", total, ok)
	}
}

func TestDefaultLimit(t *testing.T) {
	if _, ok := Sum(999_999); !ok {
		t.Fatal("999999 should be within the default limit")
	}
}

func TestLowLimit(t *testing.T) {
	old := SetLimit(10)
	t.Cleanup(func() { SetLimit(old) })
	if _, ok := Sum(11); ok {
		t.Fatal("11 should be over a limit of 10")
	}
}

func TestEmpty(t *testing.T) {
	if total, ok := Sum(); total != 0 || !ok {
		t.Fatalf("Sum() = %d, %v", total, ok)
	}
}
