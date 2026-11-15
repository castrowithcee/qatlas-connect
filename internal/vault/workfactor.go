package vault

import (
	"testing"

	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// scryptLogN is the scrypt work factor of a new vault key and of an encrypted export. It is a variable only
// so a test can lower it; the kit refuses a lower one outside a test binary.
var scryptLogN = kitvault.DefaultWorkFactor

// LowerWorkFactorForTests drops the scrypt work factor of vault keys and encrypted exports to a value that
// keeps a test binary fast. It panics outside a test binary, so a mistaken call in a real program fails loudly
// instead of silently weakening a key; testing.Testing is false there and nothing else can change the factor.
func LowerWorkFactorForTests() {
	if !testing.Testing() {
		panic("vault: LowerWorkFactorForTests called outside a test binary")
	}
	scryptLogN = 4
}
