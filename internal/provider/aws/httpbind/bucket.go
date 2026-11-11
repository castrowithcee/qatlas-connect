package httpbind

import (
	"strings"
)

// Bucket names. The rules are those of general purpose S3 buckets: 3 to 63 characters of letters, digits,
// dots, hyphens and underscores, beginning and ending with a letter or digit. Upper case letters and
// underscores are accepted because old buckets have them, but such a name, like one with dots, cannot be a
// host label of the virtual-hosted address and is sent path-style. Names that are no general purpose bucket
// (an IP address, an access point or Express One Zone alias, a reserved prefix or suffix) are refused:
// the tools address ordinary buckets only.
const (
	minBucketName = 3
	maxBucketName = 63
)

var (
	reservedBucketPrefixes = []string{"xn--", "sthree-", "amzn-s3-demo-"}
	reservedBucketSuffixes = []string{"-s3alias", "--ol-s3", ".mrap", "--x-s3", "--table-s3"}
)

// ValidBucketName reports whether name is the name of a general purpose bucket.
func ValidBucketName(name string) bool {
	if len(name) < minBucketName || len(name) > maxBucketName || !alnum(name[0]) || !alnum(name[len(name)-1]) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !alnum(c) && c != '.' && c != '-' && c != '_' {
			return false
		}
	}
	if strings.Contains(name, "..") || strings.Contains(name, ".-") || strings.Contains(name, "-.") || ipv4Form(name) {
		return false
	}
	for _, p := range reservedBucketPrefixes {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	for _, p := range reservedBucketSuffixes {
		if strings.HasSuffix(name, p) {
			return false
		}
	}
	return true
}

// VirtualHostable reports whether a valid bucket name is a single DNS label that a wildcard certificate of
// the S3 endpoint covers: lower case letters, digits and hyphens only.
func VirtualHostable(name string) bool {
	if !ValidBucketName(name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
			return false
		}
	}
	return true
}

func alnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// ipv4Form reports four dot-separated groups of digits, which no bucket name may look like.
func ipv4Form(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}
