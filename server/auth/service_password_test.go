package auth

import (
	"strings"
	"testing"
)

func TestHashVerifyRoundtrip(t *testing.T) {
	t.Parallel()
	phc, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected PHC prefix: %q", phc)
	}
	ok, err := verifyPassword(phc, "correct horse battery staple")
	if err != nil {
		t.Fatalf("verifyPassword: %v", err)
	}
	if !ok {
		t.Error("correct password did not verify")
	}
	ok, err = verifyPassword(phc, "wrong password")
	if err != nil {
		t.Fatalf("verifyPassword(wrong): %v", err)
	}
	if ok {
		t.Error("wrong password verified")
	}
}

// Verification must read its parameters out of the stored hash, so old hashes
// survive future parameter bumps. This hash was produced with weaker params
// than the current constants.
func TestVerifyForeignParams(t *testing.T) {
	t.Parallel()
	// argon2id of "sofalog" with m=8192, t=1, p=1, fixed salt.
	const phc = "$argon2id$v=19$m=8192,t=1,p=1$c29mYWxvZ3NhbHQwMDE$JpJQ6zb9anNochOYco16310h8L39xZoOG8gOnDfniVU"
	ok, err := verifyPassword(phc, "sofalog")
	if err != nil {
		t.Fatalf("verifyPassword: %v", err)
	}
	if !ok {
		t.Error("hash with non-current params did not verify — params must come from the hash, not the constants")
	}
}

func TestVerifyMalformed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		phc  string
	}{
		{"empty", ""},
		{"not a hash", "hunter2"},
		{"wrong algorithm", "$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$a2V5"},
		{"wrong version", "$argon2id$v=18$m=19456,t=2,p=1$c2FsdA$a2V5"},
		{"missing key", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA"},
		{"bad base64 salt", "$argon2id$v=19$m=19456,t=2,p=1$!!!$a2V5"},
		{"bad base64 key", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$!!!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok, err := verifyPassword(tt.phc, "whatever")
			if err == nil {
				t.Error("expected an error")
			}
			if ok {
				t.Error("malformed hash verified")
			}
		})
	}
}

func TestDummyHashNeverVerifies(t *testing.T) {
	t.Parallel()
	for _, pw := range []string{"", "password", "default", dummyHash} {
		ok, err := verifyPassword(dummyHash, pw)
		if err != nil {
			t.Fatalf("verifyPassword(dummy, %q): %v", pw, err)
		}
		if ok {
			t.Errorf("dummy hash verified %q", pw)
		}
	}
}

func TestNewSessionToken(t *testing.T) {
	t.Parallel()
	a, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken: %v", err)
	}
	b, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken: %v", err)
	}
	if len(a) != 43 { // 32 bytes, base64url, unpadded
		t.Errorf("token length = %d, want 43", len(a))
	}
	if a == b {
		t.Error("two tokens are identical")
	}
}
