package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheRuntimeAndStateInstance(t *testing.T) {
	d := Dirs{Cache: "/c", State: "/s"}
	if got, want := d.CacheRuntime("2026.06"), filepath.Join("/c", "runtime", "2026.06"); got != want {
		t.Errorf("CacheRuntime = %q, want %q", got, want)
	}
	if got, want := d.StateInstance("default"), filepath.Join("/s", "state", "default"); got != want {
		t.Errorf("StateInstance = %q, want %q", got, want)
	}
	if got := d.StateInstance(""); !strings.HasSuffix(got, filepath.Join("state", "default")) {
		t.Errorf("empty instance should default: %q", got)
	}
}

func TestDefaultReturnsDirs(t *testing.T) {
	d, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if d.Cache == "" || d.State == "" {
		t.Errorf("Default returned empty dirs: %+v", d)
	}
}

func TestGuestArchDefault(t *testing.T) {
	got := GuestArchDefault()
	if got != "arm64" && got != "x86_64" {
		t.Errorf("unexpected guest arch default: %q", got)
	}
}
