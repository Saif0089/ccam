package accounts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Manager is the CRUD API the HTTP layer drives. It owns slug/alias
// uniqueness and per-account directory provisioning; it knows nothing
// about PTYs, shells, or services.
type Manager struct {
	store       *Store
	accountsDir string
}

// NewManager builds a Manager whose account directories live under
// accountsDir (typically ~/.ccam/accounts).
func NewManager(store *Store, accountsDir string) *Manager {
	return &Manager{store: store, accountsDir: accountsDir}
}

// List returns every known account.
func (m *Manager) List() ([]Account, error) {
	return m.store.Load()
}

// Get returns a single account by ID, or an error if it doesn't exist.
func (m *Manager) Get(id string) (Account, error) {
	list, err := m.store.Load()
	if err != nil {
		return Account{}, err
	}
	for _, a := range list {
		if a.ID == id {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("no account with id %q", id)
}

// Add provisions a new account: a unique slug/ID/alias derived from name,
// a fresh (empty) config directory, and status "pending" until a login
// succeeds against it.
func (m *Manager) Add(name string) (Account, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Account{}, fmt.Errorf("account name must not be empty")
	}

	var created Account
	_, err := m.store.Mutate(func(list []Account) ([]Account, error) {
		slug := uniqueSlug(slugify(name), list)
		dir := filepath.Join(m.accountsDir, slug)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("creating account dir: %w", err)
		}
		created = Account{
			ID:        slug,
			Name:      name,
			Slug:      slug,
			ConfigDir: dir,
			Alias:     uniqueAlias(aliasFor(slug), list),
			Status:    StatusPending,
			CreatedAt: time.Now().UTC(),
		}
		return append(list, created), nil
	})
	if err != nil {
		return Account{}, err
	}
	return created, nil
}

// Rename changes an account's display name and, since the alias is
// derived from the name, regenerates its alias. The account's ID, slug,
// and config directory never change, so its credentials are untouched.
func (m *Manager) Rename(id, newName string) (Account, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return Account{}, fmt.Errorf("account name must not be empty")
	}

	var updated Account
	found := false
	_, err := m.store.Mutate(func(list []Account) ([]Account, error) {
		for i := range list {
			if list[i].ID == id {
				others := append(append([]Account{}, list[:i]...), list[i+1:]...)
				list[i].Name = newName
				list[i].Alias = uniqueAlias(aliasFor(slugify(newName)), others)
				updated = list[i]
				found = true
				return list, nil
			}
		}
		return list, nil
	})
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, fmt.Errorf("no account with id %q", id)
	}
	return updated, nil
}

// SetStatus updates an account's lifecycle status, e.g. once a login is
// observed to succeed.
func (m *Manager) SetStatus(id string, status Status) (Account, error) {
	var updated Account
	found := false
	_, err := m.store.Mutate(func(list []Account) ([]Account, error) {
		for i := range list {
			if list[i].ID == id {
				list[i].Status = status
				if status == StatusLinked {
					list[i].LastUsedAt = time.Now().UTC()
				}
				updated = list[i]
				found = true
				return list, nil
			}
		}
		return list, nil
	})
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, fmt.Errorf("no account with id %q", id)
	}
	return updated, nil
}

// Remove deletes an account's metadata and its on-disk config directory,
// and returns the removed record so the caller (the HTTP layer) can also
// strip its alias from shell rc files.
func (m *Manager) Remove(id string) (Account, error) {
	var removed Account
	found := false
	_, err := m.store.Mutate(func(list []Account) ([]Account, error) {
		out := make([]Account, 0, len(list))
		for _, a := range list {
			if a.ID == id {
				removed = a
				found = true
				continue
			}
			out = append(out, a)
		}
		return out, nil
	})
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, fmt.Errorf("no account with id %q", id)
	}
	if removed.ConfigDir != "" {
		// Best-effort: an account dir that's already gone isn't an error.
		_ = os.RemoveAll(removed.ConfigDir)
	}
	return removed, nil
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "account"
	}
	return s
}

func aliasFor(slug string) string {
	return "claude-" + slug
}

func uniqueSlug(base string, existing []Account) string {
	candidate := base
	for i := 2; slugTaken(candidate, existing); i++ {
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return candidate
}

func slugTaken(slug string, existing []Account) bool {
	for _, a := range existing {
		if a.Slug == slug || a.ID == slug {
			return true
		}
	}
	return false
}

func uniqueAlias(base string, existing []Account) string {
	candidate := base
	for i := 2; aliasTaken(candidate, existing); i++ {
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return candidate
}

func aliasTaken(alias string, existing []Account) bool {
	for _, a := range existing {
		if a.Alias == alias {
			return true
		}
	}
	return false
}
