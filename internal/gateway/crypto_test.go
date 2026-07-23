package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialCipherRoundTripAndPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway", "credential.key")
	first, err := loadOrCreateCipher(path)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := first.encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "refresh-secret" || encrypted == "" {
		t.Fatalf("credential was not encrypted: %q", encrypted)
	}
	second, err := loadOrCreateCipher(path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := second.decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "refresh-secret" {
		t.Fatalf("decrypt = %q", plain)
	}
	key, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != credentialKeySize {
		t.Fatalf("key length = %d", len(key))
	}
}

func TestCredentialCipherRejectsMalformedValue(t *testing.T) {
	cipher, err := loadOrCreateCipher(filepath.Join(t.TempDir(), "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.decrypt("not-base64!"); err == nil {
		t.Fatal("expected malformed ciphertext error")
	}
}
