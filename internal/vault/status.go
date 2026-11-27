package vault

// Status reports what qconnect vault status shows: no secret value ever appears in it.
type Status struct {
	State State
	Dir   string
	// Entries is the number of credentials the vault holds: 0 for a vault that does not exist yet, or -1
	// when it is encrypted and locked, so the count cannot be known without the passphrase.
	Entries int
	// Pending is the number of entries waiting in pending, added or changed while the vault was locked.
	Pending int
	// Warning explains a state worth a person's attention, such as an unencrypted vault, or "" otherwise.
	Warning string
}

// Status reports the vault's state without asking for a passphrase. A locked vault does not unlock to
// answer it: Entries is -1 and Pending still counts the entries that are waiting, since counting them needs
// no decryption.
func (v *Vault) Status() (Status, error) {
	kst, err := v.kv.Status()
	if err != nil {
		return Status{}, err
	}
	st := Status{State: kst.State, Dir: v.dir, Pending: kst.Pending}
	switch kst.State {
	case StateAbsent:
		st.Pending = 0
		return st, nil
	case StateLocked:
		st.Entries = -1
		return st, nil
	case StateUnencrypted:
		st.Warning = "the vault is unencrypted: no passphrase was set when its first secret was stored"
	}
	doc, err := v.load()
	if err != nil {
		return Status{}, err
	}
	st.Entries = len(doc.Entries)
	return st, nil
}
