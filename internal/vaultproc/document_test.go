package vaultproc

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// The handover of a release before the kit's channel, the JSON of vault.Snapshot, becomes the kit's state:
// the key, the secrets and bindings as the document, the lock time, the unlock time and the maximum. An
// unlock time in the future counts from the successor's start.
func TestDecodeEarlierReadsTheEarlierHandover(t *testing.T) {
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	locksAt := time.Now().Add(time.Hour).Truncate(time.Second)
	unlockedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	bindings := vault.Bindings{IDs: map[string]string{"wiki": "id-1"}, Approvals: map[string]string{"wiki": "fp"}}
	secrets := map[string]map[string]string{"wiki": {"token": "cnry-dec1"}}
	data, _ := json.Marshal(vault.Snapshot{Identity: key.String(), Secrets: secrets, Bindings: bindings,
		LocksAt: &locksAt, UnlockedAt: &unlockedAt, MaxUnlock: 2 * time.Hour})

	state, err := decodeEarlier(data)
	if err != nil {
		t.Fatalf("decodeEarlier() = %v", err)
	}
	if state.Identity.String() != key.String() || !state.LocksAt.Equal(locksAt) || !state.UnlockedAt.Equal(unlockedAt) ||
		state.MaxUnlock != 2*time.Hour {
		t.Fatalf("decodeEarlier() = %+v", state)
	}
	doc, err := decodeDocument(state.Document)
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Secrets["wiki"]["token"]) != "cnry-dec1" || !reflect.DeepEqual(doc.Bindings, bindings) {
		t.Fatalf("the document = %+v", doc)
	}

	future := time.Now().Add(time.Hour)
	data, _ = json.Marshal(vault.Snapshot{Identity: key.String(), LocksAt: &locksAt, UnlockedAt: &future})
	if state, err := decodeEarlier(data); err != nil || !state.UnlockedAt.IsZero() {
		t.Fatalf("decodeEarlier() with a future unlock = %+v, %v, want it counted from the start", state, err)
	}
}

// A handover that cannot be read is refused without quoting it.
func TestDecodeEarlierRefusesWithoutQuoting(t *testing.T) {
	for name, data := range map[string]string{
		"not JSON":    "cnry-nojson",
		"no key":      `{"secrets":{"wiki":{"token":"cnry-nokey"}}}`,
		"another key": `{"identity":"cnry-badkey"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeEarlier([]byte(data))
			if err == nil || strings.Contains(err.Error(), "cnry") {
				t.Fatalf("decodeEarlier() = %v, want a refusal that quotes nothing", err)
			}
		})
	}
}

// What 'vault unlock' hands a vault process is the document of the snapshot, read back as it was.
func TestDocumentRoundTrip(t *testing.T) {
	bindings := vault.Bindings{IDs: map[string]string{"wiki": "id-1"}, Approvals: map[string]string{}}
	d := documentOf(map[string]map[string]string{"wiki": {"token": "v", "id": "w"}}, bindings)
	data, err := d.encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(back.Secrets["wiki"]["token"]) != "v" || string(back.Secrets["wiki"]["id"]) != "w" ||
		!reflect.DeepEqual(back.Bindings, bindings) {
		t.Fatalf("decodeDocument() = %+v", back)
	}
	back.wipe()
	if back.Secrets != nil {
		t.Fatal("wipe() left the secrets")
	}
	if _, err := decodeDocument([]byte("cnry")); err == nil || strings.Contains(err.Error(), "cnry") {
		t.Fatalf("decodeDocument() = %v, want a refusal that quotes nothing", err)
	}
}
