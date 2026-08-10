package cmd

import "testing"

// The hole this closes: a daemon bound to every interface with no credential configured.
// Refusing to start is the only failure that cannot be missed — an open API does not
// announce itself.
func TestAPublicAddressWithoutATokenRefusesToStart(t *testing.T) {
	cases := map[string]string{
		"every interface":    "0.0.0.0:7070",
		"a named interface":  "192.168.1.20:7070",
		"every interface v6": "[::]:7070",
		"bare port":          ":7070",
	}
	for name, addr := range cases {
		t.Run(name, func(t *testing.T) {
			if err := checkServeExposure(addr, ""); err == nil {
				t.Fatal("the daemon agreed to listen unprotected on a public address")
			}
		})
	}
}

func TestLoopbackNeedsNoToken(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7070", "localhost:7070", "[::1]:7070"} {
		if err := checkServeExposure(addr, ""); err != nil {
			t.Errorf("checkServeExposure(%q) = %v, want it allowed", addr, err)
		}
	}
}

func TestAPublicAddressWithATokenIsAllowed(t *testing.T) {
	if err := checkServeExposure("0.0.0.0:7070", "un-secret"); err != nil {
		t.Errorf("checkServeExposure = %v, want it allowed once configured", err)
	}
}
