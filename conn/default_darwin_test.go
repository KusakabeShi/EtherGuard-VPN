package conn

import "testing"

func TestNewDefaultBindFallsBackForMismatchedListenAddressFamilies(t *testing.T) {
	tests := []struct {
		name     string
		af       EnabledAf
		wantIPv4 [4]byte
		wantIPv6 [16]byte
	}{
		{
			name:     "IPv6 address configured for IPv4 listener",
			af:       EnabledAf{IPv4: true, ListenIPv4: "::1"},
			wantIPv4: [4]byte{},
		},
		{
			name:     "IPv4 address configured for IPv6 listener",
			af:       EnabledAf{IPv6: true, ListenIPv6: "127.0.0.1"},
			wantIPv6: [16]byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bind := NewDefaultBind(tt.af, "", 0).(*StdNetBind)
			if bind.listen_ip4 != tt.wantIPv4 {
				t.Fatalf("IPv4 listen address = %v, want %v", bind.listen_ip4, tt.wantIPv4)
			}
			if bind.listen_ip6 != tt.wantIPv6 {
				t.Fatalf("IPv6 listen address = %v, want %v", bind.listen_ip6, tt.wantIPv6)
			}
		})
	}
}
