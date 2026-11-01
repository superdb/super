package auth0

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	pkgfs "github.com/superdb/super/pkg/fs"
	"github.com/superdb/super/pkg/storage"
)

const version = 1

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path}
}

func (s Store) Tokens(uri string) (*Tokens, error) {
	creds, err := s.load()
	if err != nil || creds == nil {
		return nil, err
	}
	return creds.Tokens(uri), nil
}

func (s Store) SetTokens(uri string, tokens Tokens) error {
	creds, err := s.load()
	if err != nil {
		return err
	}
	creds.AddTokens(uri, tokens)
	return s.save(creds)
}

func (s Store) RemoveTokens(uri string) error {
	creds, err := s.load()
	if err != nil {
		return err
	}
	creds.RemoveTokens(uri)
	return s.save(creds)
}

func (s Store) load() (*Credentials, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return newCredentials(), nil
		}
		return nil, err
	}
	// check version
	v := struct {
		Version int `json:"version"`
	}{}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Version != version {
		// On version change just start with new credentials file.
		return newCredentials(), nil
	}
	var creds Credentials
	if err := json.Unmarshal(b, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

func (s Store) save(creds *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	return pkgfs.MarshalJSONFile(creds, s.path, 0600)
}

type Credentials struct {
	Version  int               `json:"version"`
	Services map[string]Tokens `json:"services"`
}

func newCredentials() *Credentials {
	return &Credentials{
		Version:  version,
		Services: make(map[string]Tokens),
	}
}

func (c *Credentials) Tokens(uri string) *Tokens {
	if tokens, ok := c.Services[normalizeURI(uri)]; ok {
		return &tokens
	}
	return nil
}

func (c *Credentials) AddTokens(uri string, tokens Tokens) {
	c.Services[normalizeURI(uri)] = tokens
}

func (c *Credentials) RemoveTokens(uri string) {
	delete(c.Services, normalizeURI(uri))
}

func normalizeURI(uri string) string {
	u := storage.MustParseURI(uri)
	return u.String()
}
