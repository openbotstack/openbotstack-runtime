package crypto

import (
	"strings"
	"testing"
)

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	key := DeriveKey("test-passphrase")
	plaintext := "sk-test-api-key-12345"

	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if encrypted == plaintext {
		t.Error("encrypted should differ from plaintext")
	}
	if !IsEncrypted(encrypted) {
		t.Error("encrypted should have enc: prefix")
	}

	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncrypt_NilKey(t *testing.T) {
	plaintext := "sk-test"
	result, err := Encrypt(nil, plaintext)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != plaintext {
		t.Errorf("nil key should return plaintext unchanged, got %q", result)
	}
}

func TestEncrypt_EmptyValue(t *testing.T) {
	key := DeriveKey("test")
	result, err := Encrypt(key, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("empty value should remain empty, got %q", result)
	}
}

func TestDecrypt_PlaintextRejected(t *testing.T) {
	key := DeriveKey("test")
	plaintext := "sk-plain-key"

	_, err := Decrypt(key, plaintext)
	if err == nil {
		t.Fatal("expected error for non-encrypted value, got nil")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := DeriveKey("correct-key")
	key2 := DeriveKey("wrong-key")

	encrypted, _ := Encrypt(key1, "secret")

	_, err := Decrypt(key2, encrypted)
	if err == nil {
		t.Error("decrypting with wrong key should fail")
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key := DeriveKey("test")
	_, err := Decrypt(key, "enc:not-valid-base64!!!")
	if err == nil {
		t.Error("invalid base64 should fail")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	key := DeriveKey("test")
	_, err := Decrypt(key, "enc:AA==")
	if err == nil {
		t.Error("too-short ciphertext should fail")
	}
}

func TestEncrypt_DifferentNonces(t *testing.T) {
	key := DeriveKey("test")
	plaintext := "same-key"

	enc1, _ := Encrypt(key, plaintext)
	enc2, _ := Encrypt(key, plaintext)

	if enc1 == enc2 {
		t.Error("same plaintext should produce different ciphertexts (random nonce)")
	}

	dec1, _ := Decrypt(key, enc1)
	dec2, _ := Decrypt(key, enc2)
	if dec1 != dec2 || dec1 != plaintext {
		t.Error("both should decrypt to the same plaintext")
	}
}

func TestIsEncrypted(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"enc:abc123", true},
		{"sk-plain-key", false},
		{"", false},
		{"enc:", false},
		{"enc:x", true},
	}
	for _, tt := range tests {
		if got := IsEncrypted(tt.input); got != tt.want {
			t.Errorf("IsEncrypted(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDeriveKey_Deterministic(t *testing.T) {
	k1 := DeriveKey("passphrase")
	k2 := DeriveKey("passphrase")
	if len(k1) != 32 {
		t.Errorf("key length = %d, want 32", len(k1))
	}
	if string(k1) != string(k2) {
		t.Error("same passphrase should produce same key")
	}
}

func TestEncryptDecrypt_LongValue(t *testing.T) {
	key := DeriveKey("test")
	plaintext := strings.Repeat("x", 10000)

	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Error("long value roundtrip failed")
	}
}
