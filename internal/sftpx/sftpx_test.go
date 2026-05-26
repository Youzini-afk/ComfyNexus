package sftpx

import "testing"

func TestCleanPathNormalizesSafePaths(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "/"},
		{name: "relative", in: "models/loras", want: "/models/loras"},
		{name: "absolute", in: "/models/loras", want: "/models/loras"},
		{name: "duplicate slashes", in: "//models///checkpoints//", want: "/models/checkpoints"},
		{name: "current directory elements", in: "/models/./loras", want: "/models/loras"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CleanPath(tt.in)
			if err != nil {
				t.Fatalf("CleanPath(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("CleanPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCleanPathRejectsUnsafePaths(t *testing.T) {
	for _, p := range []string{
		"..",
		"../models",
		"models/../secrets",
		"/models/..",
		"/models/../../etc/passwd",
		"/models\x00/loras",
		"/models\n/loras",
		"/models\t/loras",
	} {
		t.Run(p, func(t *testing.T) {
			if got, err := CleanPath(p); err == nil {
				t.Fatalf("CleanPath(%q) = %q, want error", p, got)
			}
		})
	}
}
