package server

import "testing"

func TestCleanRequestPathSandboxesFileManager(t *testing.T) {
	for _, tc := range []struct {
		name        string
		path        string
		destructive bool
		wantErr     bool
	}{
		{name: "root readable", path: "/", destructive: false},
		{name: "models child", path: "/models/checkpoints/model.safetensors", destructive: true},
		{name: "empty destructive", path: "", destructive: true, wantErr: true},
		{name: "root destructive", path: "/", destructive: true, wantErr: true},
		{name: "top-level destructive", path: "/models", destructive: true, wantErr: true},
		{name: "outside sandbox", path: "/etc/passwd", destructive: false, wantErr: true},
		{name: "traversal", path: "/models/../user/file", destructive: false, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cleanRequestPath(tc.path, tc.destructive)
			if (err != nil) != tc.wantErr {
				t.Fatalf("cleanRequestPath(%q, %v) error = %v, wantErr %v", tc.path, tc.destructive, err, tc.wantErr)
			}
		})
	}
}
