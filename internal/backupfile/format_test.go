package backupfile

import (
	"bytes"
	"testing"
)

func TestEncryptedBackupRoundTripAndCorruption(t *testing.T) {
	plaintext := bytes.Repeat([]byte("private receipt sentinel\n"), ChunkSize/8)
	key := bytes.Repeat([]byte{0x41}, KeySize)
	nonceSource := bytes.NewReader(bytes.Repeat([]byte{0x19}, 64))
	var encrypted bytes.Buffer
	if err := Encrypt(bytes.NewReader(plaintext), &encrypted, key, nonceSource); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("private receipt sentinel")) {
		t.Fatal("encrypted backup exposed receipt plaintext")
	}
	var restored bytes.Buffer
	if err := Decrypt(bytes.NewReader(encrypted.Bytes()), &restored, key); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Bytes(), plaintext) {
		t.Fatal("backup round trip changed plaintext")
	}

	corrupt := append([]byte(nil), encrypted.Bytes()...)
	corrupt[len(corrupt)/2] ^= 0x80
	if err := Decrypt(bytes.NewReader(corrupt), &bytes.Buffer{}, key); err == nil {
		t.Fatal("corrupted backup authenticated")
	}
	wrongKey := bytes.Repeat([]byte{0x42}, KeySize)
	if err := Decrypt(bytes.NewReader(encrypted.Bytes()), &bytes.Buffer{}, wrongKey); err == nil {
		t.Fatal("wrong backup key authenticated")
	}
	if err := Decrypt(bytes.NewReader(encrypted.Bytes()[:len(encrypted.Bytes())-2]), &bytes.Buffer{}, key); err == nil {
		t.Fatal("truncated backup authenticated")
	}
}

func TestBackupKeyDocumentRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x51}, KeySize)
	var encoded bytes.Buffer
	if err := WriteKey(&encoded, key); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadKey(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, key) {
		t.Fatal("backup key document changed key material")
	}
}
