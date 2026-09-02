package main

import (
	"reflect"
	"testing"
)

func TestDeviceNameFromHealth(t *testing.T) {
	tests := []struct {
		name   string
		health map[string]any
		want   string
	}{
		{
			name:   "extracts device_name",
			health: map[string]any{"status": "running", "device_name": "aikewang-instance"},
			want:   "aikewang-instance",
		},
		{
			name:   "trims whitespace",
			health: map[string]any{"device_name": "  my-device  "},
			want:   "my-device",
		},
		{
			name:   "missing device_name",
			health: map[string]any{"status": "running"},
			want:   "",
		},
		{
			name:   "empty device_name",
			health: map[string]any{"device_name": ""},
			want:   "",
		},
		{
			name:   "non-string device_name",
			health: map[string]any{"device_name": 42},
			want:   "",
		},
		{
			name:   "nil health",
			health: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deviceNameFromHealth(tt.health)
			if got != tt.want {
				t.Fatalf("deviceNameFromHealth() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPostUpdateRestartArgs(t *testing.T) {
	tests := []struct {
		name       string
		deviceName string
		profile    string
		want       []string
	}{
		{
			name:       "device name only",
			deviceName: "aikewang-instance",
			want:       []string{"daemon", "restart", "--device-name", "aikewang-instance"},
		},
		{
			name:    "profile only",
			profile: "staging",
			want:    []string{"daemon", "restart", "--profile", "staging"},
		},
		{
			name:       "device name and profile",
			deviceName: "aikewang-instance",
			profile:    "dev",
			want:       []string{"daemon", "restart", "--device-name", "aikewang-instance", "--profile", "dev"},
		},
		{
			name: "neither",
			want: []string{"daemon", "restart"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPostUpdateRestartArgs(tt.deviceName, tt.profile)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildPostUpdateRestartArgs(%q, %q) = %#v, want %#v", tt.deviceName, tt.profile, got, tt.want)
			}
		})
	}
}
