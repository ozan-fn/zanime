package main

import "testing"

func TestListenAddr(t *testing.T) {
	cases := []struct {
		name, flag, port, want string
	}{
		{"default", "", "", ":8080"},
		{"port env", "", "3000", ":3000"},
		{"port env with colon", "", ":3000", ":3000"},
		{"flag wins over env", ":9000", "3000", ":9000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PORT", c.port)
			if got := listenAddr(c.flag); got != c.want {
				t.Fatalf("listenAddr(%q) = %q, want %q", c.flag, got, c.want)
			}
		})
	}
}
