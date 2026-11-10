package live25

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestKeychainSessionReadsGenericPasswordWithoutExposingIt(t *testing.T) {
	session, err := NewKeychainSession("test.service", "test-account")
	if err != nil {
		t.Fatalf("NewKeychainSession() error = %v", err)
	}
	var gotName string
	var gotArguments []string
	var calls int
	session.run = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls++
		gotName = name
		gotArguments = arguments
		return []byte("session_name=opaque-value\n"), nil
	}

	header, err := session.CookieHeader(context.Background())
	if err != nil {
		t.Fatalf("CookieHeader() error = %v", err)
	}
	if header != "session_name=opaque-value" {
		t.Fatal("CookieHeader() did not trim the Keychain command newline")
	}
	if gotName != "security" {
		t.Errorf("command = %q, want security", gotName)
	}
	if want := []string{"find-generic-password", "-s", "test.service", "-a", "test-account", "-w"}; !reflect.DeepEqual(gotArguments, want) {
		t.Errorf("arguments = %#v, want %#v", gotArguments, want)
	}
	secondHeader, err := session.CookieHeader(context.Background())
	if err != nil || secondHeader != header || calls != 1 {
		t.Fatalf("second CookieHeader() = %q, %v after %d command calls; want cached value and one call", secondHeader, err, calls)
	}
}

func TestKeychainSessionReturnsCommandErrorsWithoutSecretValue(t *testing.T) {
	session, err := NewKeychainSession("test.service", "test-account")
	if err != nil {
		t.Fatal(err)
	}
	session.run = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("item not found")
	}
	_, err = session.CookieHeader(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read session from macOS Keychain") || !strings.Contains(err.Error(), "item not found") {
		t.Fatalf("CookieHeader() error = %v, want wrapped command error", err)
	}
}

func TestNewKeychainSessionRequiresAccount(t *testing.T) {
	_, err := NewKeychainSession("test.service", "")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewKeychainSession() error = %v, want ErrInvalidRequest", err)
	}
}
