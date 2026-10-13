package secretbox

import "testing"

func TestEncryptDecrypt(t *testing.T) {
	box, err := New("test-key")
	if err != nil {
		t.Fatal(err)
	}

	encrypted, err := box.Encrypt("postgres://secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "postgres://secret" {
		t.Fatal("encrypted value matches plaintext")
	}

	decrypted, err := box.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "postgres://secret" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}
}

func TestDecryptRejectsInvalidValue(t *testing.T) {
	box, err := New("test-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.Decrypt("not-valid"); err == nil {
		t.Fatal("expected invalid ciphertext to fail")
	}
}
