package live25

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// DefaultKeychainService is the macOS Keychain service name used by the local
// 25Live reader. It contains no user identity or secret material.
const DefaultKeychainService = "edu.unc.loriss.live25.session"

type commandOutput func(context.Context, string, ...string) ([]byte, error)

// KeychainSession reads a complete Cookie request header from a macOS Generic
// Password item. The value is read only when an HTTP request is made and is
// never written to disk, logged, or returned by public diagnostics.
type KeychainSession struct {
	service string
	account string
	run     commandOutput
	once    sync.Once
	value   string
	err     error
}

// NewKeychainSession creates a source for a named macOS Keychain item. The
// caller must create the item locally; this package never imports browser
// cookies or changes Keychain contents.
func NewKeychainSession(service, account string) (*KeychainSession, error) {
	if service == "" {
		service = DefaultKeychainService
	}
	if account == "" {
		return nil, fmt.Errorf("%w: keychain account is required", ErrInvalidRequest)
	}
	return &KeychainSession{
		service: service,
		account: account,
		run: func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, arguments...).Output()
		},
	}, nil
}

func (s *KeychainSession) CookieHeader(ctx context.Context) (string, error) {
	if s == nil || s.run == nil {
		return "", ErrSessionSource
	}
	s.once.Do(func() {
		value, err := s.run(ctx, "security", "find-generic-password", "-s", s.service, "-a", s.account, "-w")
		if err != nil {
			s.err = fmt.Errorf("live25: read session from macOS Keychain: %w", err)
			return
		}
		s.value, s.err = StaticSession(strings.TrimRight(string(value), "\r\n")).CookieHeader(ctx)
	})
	return s.value, s.err
}
