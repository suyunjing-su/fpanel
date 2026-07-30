package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

type Cipher struct {
	key [32]byte
}

func New(secret string) (*Cipher, error) {
	if secret == "" {
		return nil, errors.New("secret is empty")
	}
	return &Cipher{key: sha256.Sum256([]byte(secret))}, nil
}

func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...)), nil
}

func (c *Cipher) Decrypt(encoded string) ([]byte, error) {
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted payload: %w", err)
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	if len(payload) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("encrypted payload is too short")
	}
	return gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], nil)
}
