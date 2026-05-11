package cli

import "testing"

func TestSafeSlug(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"architect-2", "architect-2"},
		{"Architect 2", "architect-2"},
		{"FSE_Corporate", "fse-corporate"},
		{"  weird   slug  ", "weird-slug"},
		{"!!!---!!!", ""},
		{"123-abc-456", "123-abc-456"},
		{"UPPER", "upper"},
		{"with/slashes", "with-slashes"},
		{"emoji-🎉-stripped", "emoji-stripped"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := safeSlug(tc.in)
			if got != tc.want {
				t.Fatalf("safeSlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
