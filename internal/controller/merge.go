package controller

import (
	corev1 "k8s.io/api/core/v1"
	"sort"
)

// MergeHostAliases computes the final hostAliases by merging the desired
// (from CRD) with the original (non-managed) entries from the workload.
// current: the current hostAliases on the workload
// lastApplied: the hostAliases last applied by this operator (from annotation)
// desired: the hostAliases the CRD specifies
func MergeHostAliases(current, lastApplied, desired []corev1.HostAlias) []corev1.HostAlias {
	lastAppliedByIP := make(map[string]corev1.HostAlias)
	for _, ha := range lastApplied {
		lastAppliedByIP[ha.IP] = ha
	}

	// Extract "original" entries: current entries that were NOT managed by us.
	var original []corev1.HostAlias
	for _, ha := range current {
		if managed, exists := lastAppliedByIP[ha.IP]; exists && hostAliasesEqual(ha, managed) {
			continue
		}
		original = append(original, ha)
	}

	// Merge: desired entries first, then original entries that don't conflict.
	desiredByIP := make(map[string]corev1.HostAlias)
	for _, ha := range desired {
		desiredByIP[ha.IP] = ha
	}

	var result []corev1.HostAlias
	seenIPs := make(map[string]bool)

	for _, ha := range desired {
		result = append(result, ha)
		seenIPs[ha.IP] = true
	}

	for _, ha := range original {
		if !seenIPs[ha.IP] {
			result = append(result, ha)
			seenIPs[ha.IP] = true
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].IP < result[j].IP
	})

	return result
}

// RemoveManagedHostAliases removes entries that were managed by the operator,
// preserving any original entries the workload had before management.
func RemoveManagedHostAliases(current, lastApplied []corev1.HostAlias) []corev1.HostAlias {
	lastAppliedByIP := make(map[string]corev1.HostAlias)
	for _, ha := range lastApplied {
		lastAppliedByIP[ha.IP] = ha
	}

	var result []corev1.HostAlias
	for _, ha := range current {
		if managed, exists := lastAppliedByIP[ha.IP]; exists && hostAliasesEqual(ha, managed) {
			continue
		}
		result = append(result, ha)
	}

	return result
}

// hostAliasesEqual compares two HostAlias entries by IP and hostname set equality.
func hostAliasesEqual(a, b corev1.HostAlias) bool {
	if a.IP != b.IP {
		return false
	}
	if len(a.Hostnames) != len(b.Hostnames) {
		return false
	}
	aSet := make(map[string]struct{}, len(a.Hostnames))
	for _, h := range a.Hostnames {
		aSet[h] = struct{}{}
	}
	for _, h := range b.Hostnames {
		if _, ok := aSet[h]; !ok {
			return false
		}
	}
	return true
}
