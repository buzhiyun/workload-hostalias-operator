package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMergeHostAliases(t *testing.T) {
	tests := []struct {
		name        string
		current     []corev1.HostAlias
		lastApplied []corev1.HostAlias
		desired     []corev1.HostAlias
		want        []corev1.HostAlias
	}{
		{
			name:        "fresh workload - no existing hostAliases",
			current:     nil,
			lastApplied: nil,
			desired: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"foo.bar.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"foo.bar.com"}},
			},
		},
		{
			name: "existing hostAliases with no last-applied - treat all as original",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"existing.com"}},
			},
			lastApplied: nil,
			desired: []corev1.HostAlias{
				{IP: "10.0.0.2", Hostnames: []string{"desired.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"existing.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"desired.com"}},
			},
		},
		{
			name: "replace managed entries with new desired",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"old-managed.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"old-managed.com"}},
			},
			desired: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"new-managed.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"new-managed.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
		},
		{
			name: "drift correction - someone modified a managed entry",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"tampered.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"original-managed.com"}},
			},
			desired: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"desired.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"desired.com"}},
			},
		},
		{
			name: "someone added a new entry alongside managed entries",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
				{IP: "10.0.0.3", Hostnames: []string{"added.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
			},
			desired: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"new.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"new.com"}},
				{IP: "10.0.0.3", Hostnames: []string{"added.com"}},
			},
		},
		{
			name: "IP conflict between original and desired - desired wins",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"original.com"}},
			},
			lastApplied: nil,
			desired: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"desired.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"desired.com"}},
			},
		},
		{
			name: "empty desired list removes managed entries",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
			},
			desired: nil,
			want: []corev1.HostAlias{
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
		},
		{
			name: "update desired hostAliases - managed entry replaced",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"old.com", "alias.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"old.com", "alias.com"}},
			},
			desired: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"new.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"new.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeHostAliases(tt.current, tt.lastApplied, tt.desired)
			if len(got) != len(tt.want) {
				t.Errorf("MergeHostAliases() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i].IP != tt.want[i].IP {
					t.Errorf("MergeHostAliases()[%d].IP = %v, want %v", i, got[i].IP, tt.want[i].IP)
				}
				if !hostAliasesEqual(got[i], tt.want[i]) {
					t.Errorf("MergeHostAliases()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRemoveManagedHostAliases(t *testing.T) {
	tests := []struct {
		name        string
		current     []corev1.HostAlias
		lastApplied []corev1.HostAlias
		want        []corev1.HostAlias
	}{
		{
			name: "remove managed entries, keep original",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.2", Hostnames: []string{"original.com"}},
			},
		},
		{
			name: "all entries are managed - result is empty",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
			},
			want: nil,
		},
		{
			name: "no managed entries - keep all",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"original.com"}},
			},
			lastApplied: nil,
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"original.com"}},
			},
		},
		{
			name:    "empty current - result is empty",
			current: nil,
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"managed.com"}},
			},
			want: nil,
		},
		{
			name: "managed entry was tampered - keep it as original",
			current: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"tampered.com"}},
			},
			lastApplied: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"original.com"}},
			},
			want: []corev1.HostAlias{
				{IP: "10.0.0.1", Hostnames: []string{"tampered.com"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveManagedHostAliases(tt.current, tt.lastApplied)
			if len(got) != len(tt.want) {
				t.Errorf("RemoveManagedHostAliases() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if !hostAliasesEqual(got[i], tt.want[i]) {
					t.Errorf("RemoveManagedHostAliases()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
