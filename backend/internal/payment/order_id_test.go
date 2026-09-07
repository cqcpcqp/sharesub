package payment

import (
	"regexp"
	"testing"
)

func TestOrderIDMatchesZPayContract(t *testing.T) {
	pattern := regexp.MustCompile(`^20[0-9]{30}$`)
	seen := make(map[string]bool)
	for index := 0; index < 1000; index++ {
		id, err := NewOrderID()
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(id) || seen[id] {
			t.Fatalf("invalid or repeated order ID %q", id)
		}
		seen[id] = true
	}
}
