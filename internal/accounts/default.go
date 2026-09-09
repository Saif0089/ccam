package accounts

import (
	"context"
	"time"
)

// DefaultAccountID is the fixed id of the account plain `claude` uses.
const DefaultAccountID = "default"

// DefaultAccountAlias is what the UI shows as the way to run it. There
// is no generated alias: typing `claude` already uses this account, and
// writing `alias claude=...` into someone's shell would be both
// redundant and a good way to break their `claude`.
const DefaultAccountAlias = "claude"

// EnsureDefault adopts the account plain `claude` is signed in as, so
// it shows up alongside the ones ccam created.
//
// It is deliberately not automatic-forever: once the user removes it,
// the store remembers that and this stops re-adding it. Removing it
// only makes ccam forget the account — the directory is the user's main
// Claude Code login and is never touched.
//
// Detection asks `claude auth status` with no CLAUDE_CONFIG_DIR at all,
// which is the only way to see that account (see EnvForConfigDir).
func (m *Manager) EnsureDefault(ctx context.Context, prober *Prober) (bool, error) {
	dismissed, err := m.store.DefaultDismissed()
	if err != nil {
		return false, err
	}
	if dismissed {
		return false, nil
	}

	existing, err := m.Get(DefaultAccountID)
	alreadyKnown := err == nil

	status, err := prober.Status(ctx, "")
	if err != nil || !status.LoggedIn {
		// Not signed in (or no claude at all): nothing to adopt. An
		// account already adopted is left alone rather than deleted —
		// a transient failure to run claude shouldn't make a row
		// disappear from under the user.
		return false, nil
	}

	if alreadyKnown {
		if existing.Status == StatusLinked {
			return false, nil
		}
		_, err := m.SetStatus(DefaultAccountID, StatusLinked)
		return err == nil, err
	}

	name := "Default"
	if status.Email != "" {
		name = status.Email
	}

	_, err = m.store.Mutate(func(list []Account) ([]Account, error) {
		for _, a := range list {
			if a.ID == DefaultAccountID {
				return list, nil
			}
		}
		return append(list, Account{
			ID:        DefaultAccountID,
			Name:      name,
			Slug:      DefaultAccountID,
			Kind:      KindDefault,
			ConfigDir: "", // deliberately empty: no override
			// The default account already *is* ~/.claude, so nothing is
			// swapped out from under it and its transcripts are found
			// where they have always been.
			Isolation: IsolationConfigDir,
			Alias:     DefaultAccountAlias,
			Status:    StatusLinked,
			CreatedAt: time.Now().UTC(),
		}), nil
	})
	return err == nil, err
}
