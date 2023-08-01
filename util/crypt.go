package util

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

func pad(data []byte) []byte {
	blockSize := aes.BlockSize
	padLen := blockSize - len(data)%blockSize
	padText := bytes.Repeat([]byte{byte(padLen)}, padLen)
	return append(data, padText...)
}

func Encrypt(data, key string) string {
	hashKey := sha256.Sum256([]byte(key))

	block, _ := aes.NewCipher(hashKey[:])
	iv := make([]byte, aes.BlockSize)
	io.ReadFull(rand.Reader, iv)

	mode := cipher.NewCBCEncrypter(block, iv)
	paddedData := pad([]byte(data))
	ciphertext := make([]byte, len(paddedData))
	mode.CryptBlocks(ciphertext, paddedData)

	return hex.EncodeToString(iv) + hex.EncodeToString(ciphertext)
}
