package config

import (
	"errors"
	"fmt"
)

// File modes for the configuration. The file names environment variables rather than secrets, but it
// describes how to reach internal systems, so it stays private to the user.
const (
	fileMode = 0o600
	dirMode  = 0o700
)

// InUseError reports an entry that cannot be deleted because something still references it.
type InUseError struct {
	Kind string
	Name string
	By   []string
}

func (e *InUseError) Error() string {
	return fmt.Sprintf("%s %q is still used by %v", e.Kind, e.Name, e.By)
}

// NotThereError reports an entry that does not exist.
type NotThereError struct {
	Kind string
	Name string
}

func (e *NotThereError) Error() string {
	return fmt.Sprintf("%s %q does not exist", e.Kind, e.Name)
}

// Clone returns a deep copy, so a caller can try a change and discard it without touching the original.
func (c *Config) Clone() *Config {
	out := New()
	out.providers = c.providers
	out.Version = c.Version
	for name, s := range c.Services {
		copied := s
		copied.Options = cloneStrings(s.Options)
		out.Services[name] = copied
	}
	for name, cred := range c.Credentials {
		copied := cred
		copied.Values = cloneStrings(cred.Values)
		copied.Fields = append([]string(nil), cred.Fields...)
		out.Credentials[name] = copied
	}
	for name, conn := range c.Connections {
		copied := conn
		copied.Targets = append([]string(nil), conn.Targets...)
		copied.Exclude = append([]string(nil), conn.Exclude...)
		copied.Effects = append([]Effect(nil), conn.Effects...)
		if conn.Effects != nil && copied.Effects == nil {
			copied.Effects = []Effect{}
		}
		copied.Tools = append([]string(nil), conn.Tools...)
		if conn.Tools != nil && copied.Tools == nil {
			copied.Tools = []string{}
		}
		copied.BlockedTools = append([]string(nil), conn.BlockedTools...)
		copied.Paths = append([]string(nil), conn.Paths...)
		copied.Files = conn.Files.Clone()
		copied.ForwardSecrets = append([]string(nil), conn.ForwardSecrets...)
		copied.TargetLabels = cloneStrings(conn.TargetLabels)
		out.Connections[name] = copied
	}
	out.ProviderNotes = cloneStrings(c.ProviderNotes)
	if c.Defaults.Connections != nil {
		out.Defaults.Connections = cloneStrings(c.Defaults.Connections)
	}
	out.Defaults.SecretStore = c.Defaults.SecretStore
	out.Vault = c.Vault
	out.Logs = c.Logs
	return out
}

func cloneStrings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// New returns an empty configuration of the current schema version.
func New(providers ...ProviderCatalog) *Config {
	return &Config{
		Version:     Version,
		Services:    map[string]Service{},
		Credentials: map[string]Credential{},
		Connections: map[string]Connection{},
		Defaults:    Defaults{Connections: map[string]string{}},
		providers:   providerCatalog(providers),
	}
}

// SetService creates or replaces a service.
func (c *Config) SetService(name string, s Service) error {
	if name == "" {
		return errors.New("a service name must not be empty")
	}
	c.ensure()
	if s.ID == "" {
		s.ID = c.Services[name].ID
	}
	c.Services[name] = s
	return nil
}

// DeleteService removes a service that no connection uses.
func (c *Config) DeleteService(name string) error {
	if _, ok := c.Services[name]; !ok {
		return &NotThereError{Kind: "service", Name: name}
	}
	var by []string
	for _, conn := range sortedKeys(c.Connections) {
		if c.Connections[conn].Service == name {
			by = append(by, conn)
		}
	}
	if len(by) > 0 {
		return &InUseError{Kind: "service", Name: name, By: by}
	}
	delete(c.Services, name)
	return nil
}

// SetCredential creates or replaces a credential. Values name environment variables; a caller that passes
// a secret value instead of a name is rejected by validation, not stored.
func (c *Config) SetCredential(name string, cred Credential) error {
	if name == "" {
		return errors.New("a credential name must not be empty")
	}
	c.ensure()
	if cred.ID == "" {
		cred.ID = c.Credentials[name].ID
	}
	c.Credentials[name] = cred
	return nil
}

// DeleteCredential removes a credential that no connection uses.
func (c *Config) DeleteCredential(name string) error {
	if _, ok := c.Credentials[name]; !ok {
		return &NotThereError{Kind: "credential", Name: name}
	}
	var by []string
	for _, conn := range sortedKeys(c.Connections) {
		if c.Connections[conn].Credential == name {
			by = append(by, conn)
		}
	}
	if len(by) > 0 {
		return &InUseError{Kind: "credential", Name: name, By: by}
	}
	delete(c.Credentials, name)
	return nil
}

// SetConnection creates or replaces a connection. The description and the target labels are stored in their
// normalized form, so an editor and a hand-written file end up with the same text, and a label whose target
// the connection no longer has is dropped (see Connection.PruneTargetLabels). conn's own label map is not
// changed.
func (c *Config) SetConnection(name string, conn Connection) error {
	if name == "" {
		return errors.New("a connection name must not be empty")
	}
	c.ensure()
	old, has := c.Connections[name]
	if conn.ID == "" {
		conn.ID = old.ID
	}
	// Outdated targets stay marked until the targets or exclusions are chosen anew; any other change to the
	// connection keeps them as they are and never reads them in their current meaning.
	if has {
		conn.TargetsOutdated = old.TargetsOutdated && conn.sameReach(old)
	}
	conn.Description = normalizeDescription(conn.Description)
	conn.TargetLabels = conn.PruneTargetLabels()
	for target, label := range conn.TargetLabels {
		conn.TargetLabels[target] = normalizeDescription(label)
	}
	c.Connections[name] = conn
	return nil
}

// DeleteConnection removes a connection that no default points at.
func (c *Config) DeleteConnection(name string) error {
	if _, ok := c.Connections[name]; !ok {
		return &NotThereError{Kind: "connection", Name: name}
	}
	var by []string
	for _, domain := range sortedKeys(c.Defaults.Connections) {
		if c.Defaults.Connections[domain] == name {
			by = append(by, domain)
		}
	}
	if len(by) > 0 {
		return &InUseError{Kind: "connection", Name: name, By: by}
	}
	delete(c.Connections, name)
	return nil
}

// SetProviderNote sets the note of one provider in its normalized form, or removes it when the note is
// empty, so a provider without a note is never written as an empty entry.
func (c *Config) SetProviderNote(provider, note string) error {
	if provider == "" {
		return errors.New("a provider must not be empty")
	}
	note = normalizeDescription(note)
	if note == "" {
		delete(c.ProviderNotes, provider)
		if len(c.ProviderNotes) == 0 {
			c.ProviderNotes = nil
		}
		return nil
	}
	if c.ProviderNotes == nil {
		c.ProviderNotes = map[string]string{}
	}
	c.ProviderNotes[provider] = note
	return nil
}

// SetDefault points a domain at a connection.
func (c *Config) SetDefault(domain, connection string) error {
	if domain == "" {
		return errors.New("a domain must not be empty")
	}
	c.ensure()
	c.Defaults.Connections[domain] = connection
	return nil
}

// DeleteDefault removes the default of a domain.
func (c *Config) DeleteDefault(domain string) error {
	if _, ok := c.Defaults.Connections[domain]; !ok {
		return &NotThereError{Kind: "default", Name: domain}
	}
	delete(c.Defaults.Connections, domain)
	return nil
}

// normalize rewrites the free text a user maintains into the one form this build stores, compares and
// publishes. It changes no reference and no rule; a configuration is valid before and after it.
func (c *Config) normalize() {
	for name, conn := range c.Connections {
		conn.Description = normalizeDescription(conn.Description)
		if conn.TargetLabels != nil {
			labels := make(map[string]string, len(conn.TargetLabels))
			for target, label := range conn.TargetLabels {
				labels[target] = normalizeDescription(label)
			}
			conn.TargetLabels = labels
		}
		c.Connections[name] = conn
	}
	for provider, note := range c.ProviderNotes {
		_ = c.SetProviderNote(provider, note)
	}
	if len(c.ProviderNotes) == 0 {
		c.ProviderNotes = nil
	}
}

// ensure makes the maps usable on a configuration that was decoded without them.
func (c *Config) ensure() {
	if c.Services == nil {
		c.Services = map[string]Service{}
	}
	if c.Credentials == nil {
		c.Credentials = map[string]Credential{}
	}
	if c.Connections == nil {
		c.Connections = map[string]Connection{}
	}
	if c.Defaults.Connections == nil {
		c.Defaults.Connections = map[string]string{}
	}
}
