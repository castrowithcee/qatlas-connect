package invokelog

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"filippo.io/age"
)

// storedLine is a line decoded with the members the log owns, in the order they are written. The tests
// read and forge lines with it from the documented format, independently of how Append writes them.
type storedLine struct {
	Entry
	PrevHash string `json:"prev_hash"`
	MACKey   int    `json:"mac_key,omitempty"`
	MAC      string `json:"mac"`
}

// genesisHash is the prev_hash of the first line: the SHA-256 of zero bytes.
var genesisHash = hashHex(nil)

func hashHex(line []byte) string {
	sum := sha256.Sum256(line)
	return hex.EncodeToString(sum[:])
}

func splitLines(data []byte) [][]byte {
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil
	}
	return bytes.Split(data, []byte("\n"))
}

func appendLines(path string, lines [][]byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			return err
		}
	}
	return nil
}

// dayFiles returns the day file names of dir in date order.
func dayFiles(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "????-??-??.jsonl"))
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	sort.Strings(names)
	return names, err
}

// unsignedOf splits a signed line into the bytes its check value covers and the check value.
func unsignedOf(line []byte) (unsigned, mac []byte, err error) {
	at := bytes.LastIndex(line, []byte(`,"mac":"`))
	if at < 0 || !bytes.HasSuffix(line, []byte(`"}`)) {
		return nil, nil, errors.New("no check value")
	}
	mac = line[at+len(`,"mac":"`) : len(line)-2]
	unsigned = append(append([]byte(nil), line[:at]...), `,"mac":""}`...)
	return unsigned, mac, nil
}

// labelKey is the key HKDF derives from id with label, computed from the definition rather than through
// DeriveKey.
func labelKey(t *testing.T, id *age.X25519Identity, label string) []byte {
	t.Helper()
	derived, err := hkdf.Key(sha256.New, []byte(id.String()), nil, label, sha256.Size)
	if err != nil {
		t.Fatal(err)
	}
	return derived
}
