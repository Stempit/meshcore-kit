package companion

import (
	"strings"
	"testing"
)

// Connect's successful paths hand off to meshcore-go constructors that panic
// when the transport itself cannot be established, so without hardware only
// the config-error paths are testable here.
func TestConnectValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		opts ConnectOptions
		want string // expected error substring
	}{
		{
			name: "serial without port",
			opts: ConnectOptions{Type: "serial"},
			want: `serial_port must be set`,
		},
		{
			name: "tcp without address",
			opts: ConnectOptions{Type: "tcp"},
			want: `tcp_address must be set`,
		},
		{
			name: "wifi alias without address",
			opts: ConnectOptions{Type: "wifi"},
			want: `tcp_address must be set`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Connect(tt.opts)
			if err == nil {
				t.Fatal("Connect succeeded, want a config error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestTcpDialAddr(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"192.168.1.50", "192.168.1.50:5000"},
		{"meshcore.local", "meshcore.local:5000"},
		{"192.168.1.50:5001", "192.168.1.50:5001"},
		{"192.168.1.50:80", "192.168.1.50:80"},
	}
	for _, tt := range tests {
		if got := tcpDialAddr(tt.in); got != tt.want {
			t.Errorf("tcpDialAddr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
