package cloudsync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const envelopeMagic = "RACOMI-SYNC-1\x00"

type codec struct{ aead cipher.AEAD }

func namespace(c Config) string {
	c = normalized(c)
	return c.Endpoint + "\n" + c.Bucket + "\n" + c.Prefix
}
func newCodec(c Config) (*codec, error) {
	// The namespace provides a stable, public salt shared by every device using
	// this sync space. AES-GCM gets a fresh random nonce for every envelope.
	salt := sha256.Sum256([]byte("racomi-cloudsync-key-v1\n" + namespace(c)))
	key, err := pbkdf2.Key(sha256.New, c.Passphrase, salt[:], 210000, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &codec{aead}, nil
}
func (c *codec) seal(key string, plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	output := append([]byte(envelopeMagic), nonce...)
	return c.aead.Seal(output, nonce, plain, []byte(key)), nil
}
func (c *codec) open(key string, data []byte) ([]byte, error) {
	start := len(envelopeMagic)
	end := start + c.aead.NonceSize()
	if len(data) < end+c.aead.Overhead() || string(data[:start]) != envelopeMagic {
		return nil, errors.New("invalid encrypted sync object")
	}
	plain, err := c.aead.Open(nil, data[start:end], data[end:], []byte(key))
	if err != nil {
		return nil, errors.New("cannot decrypt sync data: incorrect encryption passphrase or damaged remote data")
	}
	return plain, nil
}
func randomID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
