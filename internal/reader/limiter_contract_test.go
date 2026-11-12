package reader

import (
	"fmt"
	"testing"
)

func TestLimiterPreservesKnownClientAtCapacity(t *testing.T) {
	var limit limiter
	for i := range 4096 {
		if !limit.allow(fmt.Sprint(i)) {
			t.Fatal("capacity reached too early", i)
		}
	}
	if !limit.allow("0") {
		t.Fatal("known client lost its unspent attempts at capacity")
	}
	if limit.allow("new") {
		t.Fatal("new client exceeded configured capacity")
	}
}
