package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

const encPrefix = "enc:"

var (
	errInvalidCiphertext = errors.New("crypto: invalid ciphertext")
	errNotEncrypted      = errors.New("crypto: value is not encrypted")
)

// DeriveKey returns a 32-byte AES-256 key from the given passphrase.
func DeriveKey(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}

// EncryptionKey returns the active encryption key from environment variables.
// Priority: OBS_DB_ENCRYPTION_KEY > JWT_SECRET.
// Returns empty slice if neither is set (encryption disabled).
func EncryptionKey() []byte {
	if v := os.Getenv("OBS_DB_ENCRYPTION_KEY"); v != "" {
		return DeriveKey(v)
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		return DeriveKey(v)
	}
	return nil
}

// Encrypt encrypts plaintext using AES-GCM and returns a base64 string
// prefixed with "enc:". Returns the plaintext unchanged if key is nil.
func Encrypt(key []byte, plaintext string) (string, error) {
	if key == nil || plaintext == "" {
		return plaintext, nil
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts an AES-GCM encrypted string. Returns an error if the value
// is not encrypted (missing "enc:" prefix).
func Decrypt(key []byte, encrypted string) (string, error) {
	if key == nil || encrypted == "" {
		return encrypted, nil
	}

	if !IsEncrypted(encrypted) {
		return "", errNotEncrypted
	}

	data, err := base64.StdEncoding.DecodeString(encrypted[len(encPrefix):])
	if err != nil {
		return "", errInvalidCiphertext
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", errInvalidCiphertext
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// IsEncrypted returns true if the value has the encryption prefix.
func IsEncrypted(s string) bool {
	return len(s) > len(encPrefix) && s[:len(encPrefix)] == encPrefix
}
