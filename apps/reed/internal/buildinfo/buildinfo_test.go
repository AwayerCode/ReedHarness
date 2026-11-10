package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionSources(t *testing.T) {
	originalVersion, originalReader := Version, readBuildInfo
	t.Cleanup(func() { Version, readBuildInfo = originalVersion, originalReader })
	for _, tt := range []struct {
		name, injected, module, want string
		available                    bool
		read                         bool
	}{
		{"injected takes precedence", "v9.0.0", "v1.2.3", "v9.0.0", true, false},
		{"dev falls back to module", "dev", "v1.2.3", "v1.2.3", true, true},
		{"empty falls back to module", "", "v1.2.3", "v1.2.3", true, true},
		{"missing build info", "dev", "v1.2.3", "dev", false, true},
		{"empty module version", "dev", "", "dev", true, true},
		{"development build", "", "(devel)", "dev", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.injected
			called := false
			readBuildInfo = func() (*debug.BuildInfo, bool) {
				called = true
				if !tt.available {
					return nil, false
				}
				return &debug.BuildInfo{Main: debug.Module{Version: tt.module}}, true
			}
			if got := Resolve(); got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
			if called != tt.read {
				t.Fatalf("read build info = %v, want %v", called, tt.read)
			}
		})
	}
}
