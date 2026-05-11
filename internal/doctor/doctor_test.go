package doctor

import "testing"

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{StatusOK, "ok"},
		{StatusWarn, "warn"},
		{StatusFail, "fail"},
		{Status(99), "?"},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Fatalf("Status(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestHasFailure(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		want    bool
	}{
		{"empty", nil, false},
		{"all ok", []Result{{Status: StatusOK}, {Status: StatusOK}}, false},
		{"warn only", []Result{{Status: StatusOK}, {Status: StatusWarn}}, false},
		{"one fail", []Result{{Status: StatusOK}, {Status: StatusFail}, {Status: StatusOK}}, true},
		{"first fail", []Result{{Status: StatusFail}, {Status: StatusOK}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasFailure(tc.results); got != tc.want {
				t.Fatalf("HasFailure(%v) = %v, want %v", tc.results, got, tc.want)
			}
		})
	}
}
