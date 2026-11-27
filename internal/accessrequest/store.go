package accessrequest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/castrowithcee/qatlas-kit/configfile"
	yaml "go.yaml.in/yaml/v3"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// PathFor returns the queue file that belongs to the configuration file at configPath.
func PathFor(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), FileName)
}

func newStore(path string) (*configfile.Store[document], error) {
	return configfile.NewStore(path, configfile.Codec[document]{
		Validate: validate,
		Encode: func(d document) ([]byte, error) {
			d.Version = fileVersion
			// The default stays out of the file; see KindAddTools.
			d.Requests = slices.Clone(d.Requests)
			for i := range d.Requests {
				if d.Requests[i].Kind == KindAddTools {
					d.Requests[i].Kind = ""
				}
			}
			return yaml.Marshal(d)
		},
		Decode: func(data []byte) (document, error) {
			var d document
			dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true)
			if err := dec.Decode(&d); err != nil && !errors.Is(err, io.EOF) {
				return document{}, err
			}
			for i := range d.Requests {
				if d.Requests[i].Kind == "" {
					d.Requests[i].Kind = KindAddTools
				}
			}
			return d, nil
		},
	})
}

func validate(d document) error {
	// The zero document of an absent file has no version and is valid; anything read from a file has one.
	if d.Version != 0 && d.Version != fileVersion {
		return fmt.Errorf("unsupported version %d", d.Version)
	}
	seen := map[string]bool{}
	for i, r := range d.Requests {
		switch {
		case !idPattern.MatchString(r.ID) || seen[r.ID]:
			return fmt.Errorf("request %d has no unique id", i+1)
		case r.Kind != KindAddTools && r.Kind != KindRemoveTools && r.Kind != KindCreateConnection:
			return fmt.Errorf("request %d has an unknown kind", i+1)
		case r.Kind == KindCreateConnection && (r.ConnectionID != "" || r.Connection == "" || r.Provider == "" || len(r.Tools) == 0):
			return fmt.Errorf("request %d asks to create a connection without a provider, a name, or a tool, or names an existing one", i+1)
		case r.Kind != KindCreateConnection && (r.ConnectionID == "" || len(r.Tools) == 0 || len(r.Targets) > 0):
			return fmt.Errorf("request %d names no connection or no tool, or has targets", i+1)
		case r.RequestedAt.IsZero() || r.ExpiresAt.IsZero():
			return fmt.Errorf("request %d has no time", i+1)
		}
		seen[r.ID] = true
	}
	return nil
}
