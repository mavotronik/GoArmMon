package config

import (
	"path/filepath"
	"strings"
)

// DiskThreshold extends Threshold with optional ignore lists for filesystem alerts.
type DiskThreshold struct {
	Threshold     `yaml:",inline"`
	IgnoreDevices []string `yaml:"ignore_devices"`
	IgnoreMounts  []string `yaml:"ignore_mounts"`
}

// IsDiskIgnored reports whether a filesystem should be excluded from disk alerts.
func IsDiskIgnored(device, mount string, disk *DiskThreshold) bool {
	if disk == nil {
		return false
	}
	for _, pattern := range disk.IgnoreDevices {
		for _, candidate := range deviceCandidates(device) {
			if matchDiskPattern(candidate, pattern) {
				return true
			}
		}
	}
	for _, pattern := range disk.IgnoreMounts {
		if matchDiskPattern(mount, pattern) {
			return true
		}
	}
	return false
}

func deviceCandidates(device string) []string {
	device = strings.TrimSpace(device)
	if device == "" {
		return nil
	}
	seen := map[string]struct{}{device: {}}
	out := []string{device}

	add := func(v string) {
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	add(normalizeDevice(device))
	if base := filepath.Base(device); base != device {
		add(base)
	}
	return out
}

func normalizeDevice(device string) string {
	device = strings.TrimSpace(device)
	if device == "" {
		return device
	}
	if strings.HasPrefix(device, "/dev/") {
		return device
	}
	return "/dev/" + device
}

func matchDiskPattern(value, pattern string) bool {
	value = strings.TrimSpace(value)
	pattern = strings.TrimSpace(pattern)
	if value == "" || pattern == "" {
		return false
	}
	if pattern == value {
		return true
	}
	if strings.ContainsAny(pattern, "*?[") {
		ok, err := filepath.Match(pattern, value)
		return err == nil && ok
	}
	return false
}
