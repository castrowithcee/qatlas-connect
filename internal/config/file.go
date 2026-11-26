package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"

	"github.com/castrowithcee/qatlas-kit/configfile"
	yaml "go.yaml.in/yaml/v3"
)

// Revision identifies the exact bytes of a configuration file as one read saw them. It is opaque: callers
// only keep it and hand it back to SaveIfUnchanged, Transact or RestoreBackup. RevisionAbsent stands for "the
// file did not exist".
type Revision = configfile.Revision

// RevisionAbsent is the revision of a configuration file that does not exist yet.
const RevisionAbsent = configfile.RevisionAbsent

// Observer receives measurement events of the store it is attached to with Store.Observed. It is passed per
// store, never global, so concurrent stores observe independently; a nil member is ignored.
type Observer = configfile.Observer

// Backup is one backup of the configuration file in the backups directory beside it. Name is its file name
// there and Time the moment its name records.
type Backup = configfile.Backup

// Restored is what RestoreBackup replaced, for UndoRestore.
type Restored = configfile.Restored

// ErrConflict is matched by errors.Is for a *ConflictError.
var ErrConflict = errors.New("the configuration changed since it was read")

// ConflictError reports that the configuration file no longer has the content a change was based on, so
// the change was not written. The caller reloads and decides again; nothing is merged automatically.
type ConflictError struct {
	Path string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s changed since it was read; reload it and repeat the change", e.Path)
}

// Is makes errors.Is(err, ErrConflict) true.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// The names of the backups, of the copies made by BackupCopy and of the temporary file are those every
// earlier release wrote, so their files stay listed and restorable without being renamed.
var backupNaming = configfile.BackupNaming{
	Prefix:          "config-",
	FirstHandPrefix: "config-first-hand-",
	HandMarker:      "-hand",
	Suffix:          ".yaml",
	SeqWidth:        3,
	CopyMarker:      "-",
	CopySuffix:      ".bak",
}

const (
	backupDirName = "backups"
	tempPrefix    = ".qatlas-config-"
)

// Store reads and writes one configuration file. A save either replaces the file completely or leaves it
// exactly as it was. Every write takes a cross-process lock beside the resolved file, checks the revision it
// is based on, backs up the bytes it replaces, and leaves the file private to the user.
type Store struct {
	path      string
	providers ProviderCatalog
	// marshal is a seam so a test can simulate an encoding failure.
	marshal func(any) ([]byte, error)
	file    *configfile.Store[*Config]
	fileErr error
}

// decodeError marks a failure of the codec's decode, which means an unreadable file when it read the file
// and a defect of the encoder when it read the bytes about to be written.
type decodeError struct{ err error }

func (e decodeError) Error() string { return e.err.Error() }
func (e decodeError) Unwrap() error { return e.err }

// NewStore returns a store for the configuration file at path.
func NewStore(path string, providers ...ProviderCatalog) *Store {
	s := &Store{path: path, providers: providerCatalog(providers), marshal: yaml.Marshal}
	s.file, s.fileErr = configfile.NewStore(path, configfile.Codec[*Config]{
		Validate: func(cfg *Config) error {
			if err := cfg.Validate(); err != nil && !onlyOutdatedTargets(err) {
				return &InvalidError{Path: s.path, Err: err}
			}
			return nil
		},
		Encode: func(cfg *Config) ([]byte, error) {
			data, err := s.encode(cfg)
			if err != nil {
				return nil, &InvalidError{Path: s.path, Err: fmt.Errorf("the configuration could not be encoded: %w", err)}
			}
			return data, nil
		},
		Decode: func(data []byte) (*Config, error) {
			cfg, err := decode(bytes.NewReader(data), s.providers)
			if err != nil {
				return nil, decodeError{err}
			}
			return cfg, nil
		},
		// Renamed provider or tool IDs do not make a hand-edited version: the canonical form is computed
		// from the file as written, before they are mapped.
		Canonicalize: func(data []byte) ([]byte, error) {
			raw, err := decodeRaw(bytes.NewReader(data), s.providers)
			if err != nil {
				return nil, err
			}
			raw.ensure()
			raw.normalize()
			return s.marshal(raw)
		},
	}, configfile.WithBackupNaming(backupNaming), configfile.WithTempPrefix(tempPrefix), configfile.WithPrivateReplace())
	return s
}

// Observed returns a store like s that reports its file retries and lock waits to obs. s itself is
// unchanged.
func (s *Store) Observed(obs Observer) *Store {
	out := *s
	if s.file != nil {
		out.file = s.file.Observed(obs)
	}
	return &out
}

// Path returns the file this store writes.
func (s *Store) Path() string { return s.path }

// New returns an empty configuration attached to the same provider catalog this store loads and saves.
func (s *Store) New() *Config { return New(s.providers) }

// encode returns the file form of cfg in the current schema. The ids missing from cfg are assigned first, in
// cfg itself, so a caller that keeps cfg holds what was written.
func (s *Store) encode(cfg *Config) ([]byte, error) {
	cfg.AssignIDs()
	cfg.Version = Version
	return s.marshal(cfg)
}

// fromKit returns an error of the file store in the types of this package. encoded says that a decode
// failure can only concern the bytes about to be written.
func (s *Store) fromKit(err error, encoded bool) error {
	var invalid *InvalidError
	var dec decodeError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, configfile.ErrConflict):
		return &ConflictError{Path: s.path}
	case errors.As(err, &invalid):
		return invalid
	case errors.As(err, &dec) && encoded:
		return &InvalidError{Path: s.path, Err: fmt.Errorf("the encoded configuration is not loadable: %w", dec.err)}
	case errors.As(err, &dec):
		return &InvalidError{Path: s.path, Err: dec.err}
	}
	return err
}

// Load reads the configuration through the same loader the CLI uses.
func (s *Store) Load() (*Config, error) {
	cfg, _, err := s.LoadVersioned()
	return cfg, err
}

// Snapshot returns the bytes of the configuration file as it is now and their revision. A missing file is a
// *NotFoundError.
func (s *Store) Snapshot() ([]byte, Revision, error) {
	if s.fileErr != nil {
		return nil, RevisionAbsent, s.fileErr
	}
	data, rev, err := s.file.Snapshot()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, RevisionAbsent, &NotFoundError{Path: s.path}
	}
	if err != nil {
		return nil, RevisionAbsent, fmt.Errorf("cannot read %s: %w", s.path, err)
	}
	return data, rev, nil
}

// LoadVersioned loads the configuration like Load and returns the revision of the bytes it decoded. It
// takes no lock: files are only ever replaced atomically, so a read sees a complete old or new file.
// A missing file returns a *NotFoundError together with RevisionAbsent, so a caller can create the file
// with SaveIfUnchanged(cfg, RevisionAbsent). An invalid file returns its revision with the *InvalidError.
//
// Transaction contract: a read-modify-write is only safe when it ends in Update, SaveIfUnchanged or the save
// of Transact. Save checks no revision of an earlier read and must not be used for a change based on one.
func (s *Store) LoadVersioned() (*Config, Revision, error) {
	data, rev, err := s.Snapshot()
	if err != nil {
		return nil, rev, err
	}
	cfg, err := decode(bytes.NewReader(data), s.providers)
	if err != nil {
		return nil, rev, &InvalidError{Path: s.path, Err: err}
	}
	return cfg, rev, nil
}

// HasCurrentSchema reports from the top-level version line alone, without decoding the file, whether the
// configuration is already in the current schema. It is the cheap check every start makes before it would
// migrate; false only means the file has to be loaded to know more.
func (s *Store) HasCurrentSchema() bool {
	return configfile.HasCurrentSchema(s.path, Version, func(data []byte) (int, error) {
		for _, line := range bytes.Split(data, []byte("\n")) {
			if value, ok := bytes.CutPrefix(line, []byte("version:")); ok {
				return strconv.Atoi(string(bytes.TrimSpace(value)))
			}
		}
		return 0, errors.New("no version line")
	})
}

// Save writes cfg over whatever the file holds when Save reads it: it takes that revision and saves like
// SaveIfUnchanged, so validation, the load-back check, the backup and the atomic replacement are the same,
// and a change between that read and the write is a *ConflictError. It checks no earlier read, so a change
// based on one can overwrite a concurrent change; use Update or LoadVersioned with SaveIfUnchanged for that.
// Save takes the lock, so it must not run inside Update or Transact.
func (s *Store) Save(cfg *Config) error {
	_, rev, err := s.Snapshot()
	if err != nil && !errors.As(err, new(*NotFoundError)) {
		return err
	}
	return s.SaveIfUnchanged(cfg, rev)
}

// SaveIfUnchanged writes cfg only if the file still has the revision rev, which LoadVersioned returned for
// the configuration the change was based on. Check and write happen under the cross-process lock. If the
// file changed in between, nothing is written and the error is a *ConflictError (errors.Is(err, ErrConflict)).
// An invalid configuration is an *InvalidError and is not written; the bytes that are written must load
// back, and the file they replace is saved as a backup first. Any failure leaves the file byte-identical.
func (s *Store) SaveIfUnchanged(cfg *Config, rev Revision) error {
	if s.fileErr != nil {
		return s.fileErr
	}
	_, err := s.file.SaveIfUnchanged(cfg, rev)
	return s.fromKit(err, true)
}

// RevisionOf returns the revision the file has right after cfg was saved to it: the revision of the bytes
// a save writes for cfg. A caller that saved cfg through SaveIfUnchanged, Update, or Transact and keeps cfg
// as its loaded state uses it as the base of its next change, without reading the file again; a re-read
// could pick up a change another writer made meanwhile and then pair it with a state that lacks it. It is
// only meaningful for a cfg the store just saved, and it reads nothing from disk.
func (s *Store) RevisionOf(cfg *Config) (Revision, error) {
	if s.fileErr != nil {
		return RevisionAbsent, s.fileErr
	}
	rev, err := s.file.RevisionOf(cfg)
	return rev, s.fromKit(err, true)
}

// Update runs one transaction: it takes the cross-process lock, loads the current configuration (an empty
// one from Store.New when the file does not exist), calls change with it, and saves the result before it
// releases the lock. Concurrent Update and SaveIfUnchanged calls therefore never lose each other's change.
//
// If change returns an error, or the result is invalid, nothing is written and the error is returned
// unchanged (an invalid result as *InvalidError). change must only edit the configuration it gets and
// must not call back into this store, because the lock is not reentrant. A file that exists but does not
// load is an error; Update never replaces a configuration it could not read. If a writer that bypasses
// the lock replaced the file after the read, Update returns a *ConflictError instead of overwriting it.
func (s *Store) Update(change func(*Config) error) error {
	if s.fileErr != nil {
		return s.fileErr
	}
	var changeErr error
	changed := false
	_, err := s.file.Update(func(cfg *Config) (*Config, error) {
		if cfg == nil {
			cfg = s.New()
		}
		changed = true
		changeErr = change(cfg)
		return cfg, changeErr
	})
	if changeErr != nil {
		return changeErr
	}
	return s.fromKit(err, changed)
}

// Transact runs work under the cross-process lock if the file still has the revision rev, which
// LoadVersioned returned for the configuration the change was based on. It exists for a change that must do
// something else before it saves, such as writing the secrets a new credential names, and must not do it
// unless the configuration it is based on is still current.
//
// The revision is checked before work runs, so a conflict is reported as a *ConflictError without work
// having done anything. work gets a save function that saves like SaveIfUnchanged against rev, so a writer
// that bypassed the lock meanwhile, or an earlier save of the same work, makes it fail with a
// *ConflictError; if it fails, nothing was written and work can undo what it did, still under the lock,
// before it returns. The lock is held while work runs, so work must be short and must not call back into
// this store's writes (the lock is not reentrant); BackupCopy is allowed. Whatever work returns is returned
// unchanged.
func (s *Store) Transact(rev Revision, work func(save func(*Config) error) error) error {
	if s.fileErr != nil {
		return s.fileErr
	}
	ran := false
	err := s.file.Transact(rev, func(save func(*Config) (configfile.Revision, error)) error {
		ran = true
		return work(func(cfg *Config) error {
			_, err := save(cfg)
			return s.fromKit(err, true)
		})
	})
	if ran {
		return err
	}
	return s.fromKit(err, false)
}

// backupDir is the directory of the configured path, not of a symlink target: a configuration linked into
// a dotfiles repository does not drag its backups there.
func (s *Store) backupDir() string { return filepath.Join(filepath.Dir(s.path), backupDirName) }

// BackupPath returns the full path of the backup or copy called name.
func (s *Store) BackupPath(name string) string { return filepath.Join(s.backupDir(), name) }

// Backups lists the backups of the configuration file, newest first: the plain ones, the hand-edited ones
// and the first hand-edited one. Copies of other files and anything else in the directory are not listed.
// A missing backups directory has none.
func (s *Store) Backups() ([]Backup, error) {
	if s.fileErr != nil {
		return nil, s.fileErr
	}
	return s.file.Backups()
}

// ReadBackup returns the bytes of the backup called name, which Backups listed.
func (s *Store) ReadBackup(name string) ([]byte, error) {
	if s.fileErr != nil {
		return nil, s.fileErr
	}
	return s.file.ReadBackup(name)
}

// RestoreBackup replaces the configuration file by the bytes of the backup called name if the file still
// has the revision base. Check and replacement happen under the cross-process lock. The file it replaces is
// saved as a new backup first; if that fails, nothing is changed. The backup is not decoded: a backup the
// running program cannot read is the very reason to restore one.
func (s *Store) RestoreBackup(name string, base Revision) (Restored, error) {
	if s.fileErr != nil {
		return Restored{}, s.fileErr
	}
	r, err := s.file.RestoreBackup(name, base)
	return r, s.fromKit(err, false)
}

// UndoRestore puts back the file RestoreBackup replaced, from the backup it made of it, and takes no further
// backup. A restore that replaced no file is undone by removing the file. It writes only while the file
// still holds the restored bytes; a later change makes it a *ConflictError.
func (s *Store) UndoRestore(r Restored) error {
	if s.fileErr != nil {
		return s.fileErr
	}
	return s.fromKit(s.file.UndoRestore(r), false)
}

// BackupCopy stores a byte-for-byte copy of src in the backups directory next to the configuration, with a
// unique name and the private permissions of a configuration backup, and returns its path. It is meant for
// files such as the vault that a change must be able to roll back. Copies never take part in the rotation
// of configuration backups, so a rotation cannot evict the only copy of a vault file. It takes no lock and
// may run inside Transact.
func (s *Store) BackupCopy(src string) (string, error) {
	if s.fileErr != nil {
		return "", s.fileErr
	}
	return s.file.BackupCopy(src)
}
