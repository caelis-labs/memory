// Package backupfile owns the encrypted streaming container used by
// memoryctl. The SQLite snapshot inside remains a private appliance format.
package backupfile

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	KeySize   = 32
	ChunkSize = 1 << 20
	KeyFormat = "memory.backup.key.v1"
)

var magic = []byte("CAELIS-MEMORY-BACKUP-V1\n")

type keyDocument struct {
	Format string `json:"format"`
	Key    string `json:"key"`
}

func GenerateKey(random io.Reader) ([]byte, error) {
	if random == nil {
		random = rand.Reader
	}
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(random, key); err != nil {
		return nil, fmt.Errorf("generate backup key: %w", err)
	}
	return key, nil
}

func WriteKey(output io.Writer, key []byte) error {
	if len(key) != KeySize {
		return fmt.Errorf("backup key must be %d bytes", KeySize)
	}
	return json.NewEncoder(output).Encode(keyDocument{
		Format: KeyFormat,
		Key:    base64.RawURLEncoding.EncodeToString(key),
	})
}

func ReadKey(input io.Reader) ([]byte, error) {
	decoder := json.NewDecoder(io.LimitReader(input, 4<<10))
	var document keyDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode backup key: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("backup key contains trailing JSON")
	}
	if document.Format != KeyFormat {
		return nil, fmt.Errorf("unsupported backup key format %q", document.Format)
	}
	key, err := base64.RawURLEncoding.DecodeString(document.Key)
	if err != nil || len(key) != KeySize {
		return nil, fmt.Errorf("backup key material is invalid")
	}
	return key, nil
}

// Encrypt writes an authenticated chunk stream. No receipt or schema metadata
// is exposed outside the AEAD ciphertext.
func Encrypt(input io.Reader, output io.Writer, key []byte, random io.Reader) error {
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	if random == nil {
		random = rand.Reader
	}
	prefixSize := aead.NonceSize() - 4
	if prefixSize <= 0 {
		return fmt.Errorf("backup AEAD nonce is too small")
	}
	prefix := make([]byte, prefixSize)
	if _, err := io.ReadFull(random, prefix); err != nil {
		return fmt.Errorf("generate backup nonce: %w", err)
	}
	if err := writeAll(output, magic); err != nil {
		return err
	}
	if err := writeAll(output, prefix); err != nil {
		return err
	}
	buffer := make([]byte, ChunkSize)
	for sequence := uint64(0); ; sequence++ {
		if sequence > uint64(^uint32(0)) {
			return fmt.Errorf("backup exceeds chunk sequence capacity")
		}
		count, readErr := io.ReadFull(input, buffer)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return fmt.Errorf("read backup snapshot: %w", readErr)
		}
		nonce := nonceFor(prefix, uint32(sequence))
		sealed := aead.Seal(nil, nonce, buffer[:count], associatedData(uint32(sequence)))
		if err := binary.Write(output, binary.BigEndian, uint32(len(sealed))); err != nil {
			return fmt.Errorf("write backup chunk length: %w", err)
		}
		if err := writeAll(output, sealed); err != nil {
			return err
		}
		if errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
	}
	if err := binary.Write(output, binary.BigEndian, uint32(0)); err != nil {
		return fmt.Errorf("write backup terminator: %w", err)
	}
	return nil
}

// Decrypt verifies every chunk before writing its plaintext. A corrupt or
// truncated backup returns an error and must never be installed.
func Decrypt(input io.Reader, output io.Writer, key []byte) error {
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	header := make([]byte, len(magic))
	if _, err := io.ReadFull(input, header); err != nil {
		return fmt.Errorf("read backup header: %w", err)
	}
	if !bytes.Equal(header, magic) {
		return fmt.Errorf("unsupported backup format")
	}
	prefix := make([]byte, aead.NonceSize()-4)
	if _, err := io.ReadFull(input, prefix); err != nil {
		return fmt.Errorf("read backup nonce: %w", err)
	}
	for sequence := uint64(0); ; sequence++ {
		if sequence > uint64(^uint32(0)) {
			return fmt.Errorf("backup exceeds chunk sequence capacity")
		}
		var size uint32
		if err := binary.Read(input, binary.BigEndian, &size); err != nil {
			return fmt.Errorf("read backup chunk length: %w", err)
		}
		if size == 0 {
			var trailing [1]byte
			if count, err := input.Read(trailing[:]); count != 0 || !errors.Is(err, io.EOF) {
				return fmt.Errorf("backup contains trailing data")
			}
			return nil
		}
		if size < uint32(aead.Overhead()) || size > uint32(ChunkSize+aead.Overhead()) {
			return fmt.Errorf("backup chunk length is invalid")
		}
		sealed := make([]byte, size)
		if _, err := io.ReadFull(input, sealed); err != nil {
			return fmt.Errorf("read backup chunk: %w", err)
		}
		plaintext, err := aead.Open(nil, nonceFor(prefix, uint32(sequence)), sealed, associatedData(uint32(sequence)))
		if err != nil {
			return fmt.Errorf("authenticate backup chunk %d: %w", sequence, err)
		}
		if err := writeAll(output, plaintext); err != nil {
			return err
		}
	}
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("backup key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonceFor(prefix []byte, sequence uint32) []byte {
	nonce := make([]byte, len(prefix)+4)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[len(prefix):], sequence)
	return nonce
}

func associatedData(sequence uint32) []byte {
	value := make([]byte, len(magic)+4)
	copy(value, magic)
	binary.BigEndian.PutUint32(value[len(magic):], sequence)
	return value
}

func writeAll(output io.Writer, value []byte) error {
	for len(value) > 0 {
		count, err := output.Write(value)
		if err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		value = value[count:]
	}
	return nil
}
