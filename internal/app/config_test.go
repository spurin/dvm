package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadConfigFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dvm.yaml" {
			w.Write([]byte("name: webcfg\nguest:\n  memory_mb: 3072\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg, path, err := LoadConfig(srv.URL + "/dvm.yaml")
	if err != nil {
		t.Fatalf("load from URL: %v", err)
	}
	if cfg.Name != "webcfg" || cfg.Guest.MemoryMB != 3072 {
		t.Errorf("config not parsed from URL: name=%q mem=%d", cfg.Name, cfg.Guest.MemoryMB)
	}
	if path != srv.URL+"/dvm.yaml" {
		t.Errorf("path = %q", path)
	}

	if _, _, err := LoadConfig(srv.URL + "/missing.yaml"); err == nil {
		t.Error("expected error for 404 config URL")
	}
}

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		in              string
		wantHost, wantG int
		wantProto       string
		wantErr         bool
	}{
		{"8080:80", 8080, 80, "tcp", false},
		{"3000:3000", 3000, 3000, "tcp", false},
		{"5353:5353/udp", 5353, 5353, "udp", false},
		{"  2222 : 22 ", 2222, 22, "tcp", false},
		{"80", 0, 0, "", true},
		{"0:80", 0, 0, "", true},
		{"8080:70000", 0, 0, "", true},
		{"8080:80/sctp", 0, 0, "", true},
	}
	for _, tt := range tests {
		p, err := ParsePortSpec(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParsePortSpec(%q): expected error, got %+v", tt.in, p)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePortSpec(%q): unexpected error %v", tt.in, err)
			continue
		}
		if p.Host != tt.wantHost || p.Guest != tt.wantG || p.Protocol != tt.wantProto {
			t.Errorf("ParsePortSpec(%q) = %+v, want host=%d guest=%d proto=%s",
				tt.in, p, tt.wantHost, tt.wantG, tt.wantProto)
		}
	}
}

func TestIPConfigModeValid(t *testing.T) {
	for _, m := range []IPConfigMode{IPCloudInit, IPKernelDHCP, IPKernelStatic, IPNone} {
		if !m.Valid() {
			t.Errorf("%q should be valid", m)
		}
	}
	if IPConfigMode("bogus").Valid() {
		t.Error("bogus mode should be invalid")
	}
}

func TestValidate(t *testing.T) {
	base := Default()
	if err := base.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}

	c := Default()
	c.Network.IPConfig = "nope"
	if err := c.Validate(); err == nil {
		t.Error("invalid ip_config should fail validation")
	}

	c = Default()
	c.Network.BindAddress = "0.0.0.0"
	if err := c.Validate(); err == nil {
		t.Error("0.0.0.0 without allow_public_bind should fail")
	}
	c.Network.AllowPublicBind = true
	if err := c.Validate(); err != nil {
		t.Errorf("0.0.0.0 with allow_public_bind should pass: %v", err)
	}

	c = Default()
	c.Guest.MemoryMB = 0
	if err := c.Validate(); err == nil {
		t.Error("zero memory should fail")
	}
}

func TestMergeFlags(t *testing.T) {
	c := Default()
	mergeFlags(&c, Flags{
		Kernel:   "oci://r/k:1",
		MemoryMB: 4096,
		IPConfig: "kernel-dhcp",
		Ports:    []string{"8080:80", "3000:3000"},
	})
	if c.Components.Kernel != "oci://r/k:1" {
		t.Errorf("kernel not merged: %q", c.Components.Kernel)
	}
	if c.Guest.MemoryMB != 4096 {
		t.Errorf("memory not merged: %d", c.Guest.MemoryMB)
	}
	if c.Network.IPConfig != IPKernelDHCP {
		t.Errorf("ip-config not merged: %q", c.Network.IPConfig)
	}
	if len(c.Ports) != 2 || c.Ports[0].Host != 8080 || c.Ports[1].Guest != 3000 {
		t.Errorf("ports not appended correctly: %+v", c.Ports)
	}
}
