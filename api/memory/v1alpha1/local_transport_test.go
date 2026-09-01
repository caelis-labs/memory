package v1alpha1

import (
	"runtime"
	"testing"
)

func TestDefaultLocalEndpointMatchesPlatform(t *testing.T) {
	endpoint := DefaultLocalEndpoint(t.TempDir())
	if err := endpoint.Validate(); err != nil {
		t.Fatal(err)
	}
	want := LocalNetworkUnix
	if runtime.GOOS == "windows" {
		want = LocalNetworkNamedPipe
	}
	if endpoint.Network != want {
		t.Fatalf("DefaultLocalEndpoint network = %q, want %q", endpoint.Network, want)
	}
}

func TestLocalEndpointRejectsUnknownNetwork(t *testing.T) {
	if err := (LocalEndpoint{Network: "tcp", Address: "127.0.0.1"}).Validate(); err == nil {
		t.Fatal("TCP endpoint passed local validation")
	}
}
