package repository

import (
	"reflect"
	"testing"
)

func TestAutoSeedStatusFilterValues(t *testing.T) {
	testCases := []struct {
		name   string
		status string
		want   []string
	}{
		{
			name:   "not pushed includes pending and rejected",
			status: AutoSeedItemStatusNotPushed,
			want:   []string{AutoSeedItemStatusPending, AutoSeedItemStatusRejected},
		},
		{
			name:   "specific status remains exact",
			status: AutoSeedItemStatusPushed,
			want:   []string{AutoSeedItemStatusPushed},
		},
		{
			name:   "empty status does not filter",
			status: "",
			want:   nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := autoSeedStatusFilterValues(testCase.status); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("autoSeedStatusFilterValues(%q) = %v, want %v", testCase.status, got, testCase.want)
			}
		})
	}
}
