package gh

import "testing"

func TestParseAuthStatus(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "modern gh format (account)",
			in:   "github.com\n  ✓ Logged in to github.com account MatthewKennedy (keyring)\n  - Active account: true\n",
			want: "MatthewKennedy@github.com",
		},
		{
			name: "older gh format (as)",
			in:   "✓ Logged in to github.com as matt (oauth_token)\n",
			want: "matt@github.com",
		},
		{
			name: "enterprise host",
			in:   "✓ Logged in to ghe.example.com account some-user (token)\n",
			want: "some-user@ghe.example.com",
		},
		{
			name: "no match",
			in:   "You are not logged into any GitHub hosts. Run gh auth login to authenticate.",
			want: "",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseAuthStatus([]byte(c.in))
			if got != c.want {
				t.Errorf("parseAuthStatus = %q, want %q", got, c.want)
			}
		})
	}
}
