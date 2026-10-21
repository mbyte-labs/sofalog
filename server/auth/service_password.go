package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters, the OWASP-recommended minimum — sized for the small
// self-hosted boxes this runs on. Verification reads the parameters back out
// of the stored hash, so bumping these later keeps old hashes verifiable.
const (
	argonMemoryKiB = 19456
	argonTime      = 2
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// hashPassword returns a PHC-format argon2id string:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<b64 salt>$<b64 key>
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword checks password against a PHC argon2id string, recomputing
// with the parameters stored in the hash itself.
func verifyPassword(phc, password string) (bool, error) {
	parts := strings.Split(phc, "$")
	// ["", "argon2id", "v=19", "m=…,t=…,p=…", salt, key]
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, fmt.Errorf("malformed password hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("malformed password hash version: %w", err)
	}
	if version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2 version %d", version)
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false, fmt.Errorf("malformed password hash parameters: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("malformed password hash salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("malformed password hash key: %w", err)
	}
	computed := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(computed, key) == 1, nil
}

// dummyHash is compared against when the username is unknown or the user has
// no local password (OIDC-only), so every login failure costs one argon2
// evaluation — otherwise response timing would reveal which usernames exist.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("auth: generating dummy hash: %v", err))
	}
	phc, err := hashPassword(base64.RawStdEncoding.EncodeToString(buf))
	if err != nil {
		panic(fmt.Sprintf("auth: hashing dummy password: %v", err))
	}
	return phc
}

// newSessionToken returns the opaque session cookie value: 32 random bytes,
// base64url. Stored verbatim in sessions.token.
func newSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
