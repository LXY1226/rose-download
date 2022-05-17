package main

import (
	"crypto/aes"
	"crypto/cipher"
	"io"
)

const blockSize = 16

// AES-128 CBC
type aesCBCReader[md cipher.BlockMode] struct {
	rd    *wrappedBuffer
	rem   int // remain byte in obuf
	block md
	oBuf  [blockSize]byte
}

func (a *aesCBCReader[md]) Read(p []byte) (n int, err error) {
	n = len(p)
	// first drain remain
	if a.rem != 0 {
		if len(p) <= a.rem {
			copy(p, a.oBuf[blockSize-a.rem:])
			a.rem -= len(p)
			return len(p), nil
		}
		copy(p, a.oBuf[blockSize-a.rem:])
		p = p[a.rem:]
		a.rem = 0
	}

	for len(p) > 0 {
		var b []byte
		b, err = a.rd.Get(blockSize)
		if err != nil {
			break
		}
		if len(p) > blockSize {
			a.block.CryptBlocks(p, b)
			p = p[16:]
			continue
		}
		a.block.CryptBlocks(a.oBuf[:], b)
		i := copy(p, a.oBuf[:])
		p = p[i:]
		a.rem = blockSize - i
	}
	n -= len(p)
	return n, err
}

// newCipherBlockReader returns a cipherBlockReader that decrypts the given io.Reader using
// the provided block mode cipher.
func newCipherBlockReader[md cipher.BlockMode](r *wrappedBuffer, mode md) *aesCBCReader[md] {
	return &aesCBCReader[md]{rd: r, block: mode}
}

// newAesDecryptReader returns a cipherBlockReader that decrypts input from a given io.Reader using AES.
// It will panic if the provided key is invalid.
func newAesDecryptReader(rd *wrappedBuffer, key, iv []byte) io.Reader {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	mode := cipher.NewCBCDecrypter(block, iv)

	return newCipherBlockReader(rd, mode)
}
