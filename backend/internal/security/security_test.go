package security

import (
	"bytes"
	"testing"
)

func TestCredentialEncryptionUsesAssociatedData(t *testing.T) {
	manager, err := New(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := manager.Encrypt("access-token", []byte("account-a:access"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := manager.Decrypt(ciphertext, []byte("account-a:access"))
	if err != nil || plaintext != "access-token" {
		t.Fatalf("decrypt = %q, %v", plaintext, err)
	}
	if _, err := manager.Decrypt(ciphertext, []byte("account-b:access")); err == nil {
		t.Fatal("decrypt with different account scope succeeded")
	}
}

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("long-password-value")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "long-password-value") || CheckPassword(hash, "different-password") {
		t.Fatal("password verification result is incorrect")
	}
}
