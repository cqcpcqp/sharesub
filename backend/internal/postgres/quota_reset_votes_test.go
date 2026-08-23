package postgres

import "testing"

func TestQuotaResetVotePassedUsesStrictMajority(t *testing.T) {
	tests := []struct {
		name                        string
		mode                        string
		supportCount, supportWeight int
		eligibleCount               int
		want                        bool
	}{
		{name: "fixed exactly half", mode: "fixed", supportWeight: 5000, eligibleCount: 3, want: false},
		{name: "fixed above half", mode: "fixed", supportWeight: 5001, eligibleCount: 3, want: true},
		{name: "shared two of four", mode: "shared", supportCount: 2, eligibleCount: 4, want: false},
		{name: "shared three of four", mode: "shared", supportCount: 3, eligibleCount: 4, want: true},
		{name: "shared three of five", mode: "shared", supportCount: 3, eligibleCount: 5, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := quotaResetVotePassed(test.mode, test.supportCount, test.supportWeight, test.eligibleCount); got != test.want {
				t.Fatalf("quotaResetVotePassed() = %v, want %v", got, test.want)
			}
		})
	}
}
