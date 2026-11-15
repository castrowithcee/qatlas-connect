package github

import (
	"net/url"
	"strconv"
	"strings"
)

// A registry host target approves one address host for the private registries of an organization. It names
// no GitHub object: no tool takes it as a repository, owner, or project, and no allowlist of an existing tool
// counts it. Only checkRegistryURL reads it.

const registryHostPrefix = "registry-hosts/"

// validRegistryHost accepts a DNS host name, optionally followed by :PORT. IP addresses, IPv6 literals,
// patterns, paths, and anything with whitespace are refused, since an approval must name one host exactly.
func validRegistryHost(value string) bool {
	host, port, hasPort := strings.Cut(value, ":")
	if hasPort && !validPort(port) {
		return false
	}
	return validHostname(host)
}

func validPort(value string) bool {
	if value == "" || len(value) > 5 || value[0] == '0' {
		return false
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	// A purely numeric last label would read as an IPv4 address.
	return !allDigits(labels[len(labels)-1])
}

// checkRegistryURL accepts a registry address only when it is an https URL without user information whose
// host, with its port when one is given, equals a registry host target of the list without regard to case.
// There is no pattern, no subdomain extension, and no default port: a URL without a port matches only a
// target without one. Everything unclear is refused. The error never quotes the value.
func checkRegistryURL(allowed allowlist, raw string) error {
	refused := invalidRequest("url must be an https address whose host the connection approves as " +
		"registry-hosts/HOST; nothing was sent")
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\\ \t\r\n") {
		return refused
	}
	for _, r := range raw {
		if r < 0x21 || r == 0x7f {
			return refused
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" ||
		strings.HasSuffix(u.Host, ":") || !validRegistryHost(u.Host) {
		return refused
	}
	for _, entry := range allowed.targets {
		if entry.kind == kindRegistryHost && strings.EqualFold(entry.host, u.Host) {
			return nil
		}
	}
	return refused
}
