package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type Manager struct {
	pepper []byte
	aead   cipher.AEAD
}

func New(pepper, credentialKey []byte) (*Manager, error) {
	if len(pepper) != 32 || len(credentialKey) != 32 {
		return nil, errors.New("security keys must be 32 bytes")
	}
	block, err := aes.NewCipher(credentialKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Manager{pepper: append([]byte(nil), pepper...), aead: aead}, nil
}

func NewID() (string, error) {
	raw, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func NewOpaqueToken(prefix string) (string, error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (m *Manager) HashToken(token string) []byte {
	h := sha256.New()
	_, _ = h.Write(m.pepper)
	_, _ = h.Write([]byte(token))
	return h.Sum(nil)
}

func (m *Manager) EqualTokenHash(token string, expected []byte) bool {
	actual := m.HashToken(token)
	return len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
}

func HashPassword(password string) (string, error) {
	if len(password) < 10 || len(password) > 128 {
		return "", errors.New("password must contain 10 to 128 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (m *Manager) Encrypt(plaintext string, associatedData []byte) ([]byte, error) {
	if strings.TrimSpace(plaintext) == "" {
		return nil, errors.New("cannot encrypt an empty credential")
	}
	nonce, err := randomBytes(m.aead.NonceSize())
	if err != nil {
		return nil, err
	}
	return m.aead.Seal(nonce, nonce, []byte(plaintext), associatedData), nil
}

func (m *Manager) Decrypt(ciphertext, associatedData []byte) (string, error) {
	nonceSize := m.aead.NonceSize()
	if len(ciphertext) <= nonceSize {
		return "", errors.New("invalid credential ciphertext")
	}
	plaintext, err := m.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], associatedData)
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plaintext), nil
}

func randomBytes(size int) ([]byte, error) {
	out := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, out); err != nil {
		return nil, err
	}
	return out, nil
}
