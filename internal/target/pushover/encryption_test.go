package pushover

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// decryptTestField independently checks the wire format, authentication, padding,
// and compression rather than using any production encryption helpers.
func decryptTestField(encoded string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < 2*aes.BlockSize+sha256.Size {
		return "", fmt.Errorf("invalid encrypted field")
	}
	authenticated, tag := data[:len(data)-sha256.Size], data[len(data)-sha256.Size:]
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(authenticated)
	if !hmac.Equal(tag, mac.Sum(nil)) {
		return "", fmt.Errorf("invalid HMAC")
	}
	iv, ciphertext := authenticated[:aes.BlockSize], authenticated[aes.BlockSize:]
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	padded := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(padded, ciphertext)
	padding := int(padded[len(padded)-1])
	if padding == 0 || padding > aes.BlockSize ||
		!bytes.Equal(padded[len(padded)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return "", fmt.Errorf("invalid PKCS7 padding")
	}
	reader, err := gzip.NewReader(bytes.NewReader(padded[:len(padded)-padding]))
	if err != nil {
		return "", err
	}
	defer reader.Close()
	plain, err := io.ReadAll(reader)
	return string(plain), err
}

func TestNewEncryptionKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "disabled", valid: true},
		{name: "lowercase", value: testEncryptionKey, valid: true},
		{name: "uppercase", value: strings.ToUpper(testEncryptionKey), valid: true},
		{name: "short", value: testEncryptionKey[:32]},
		{name: "long", value: testEncryptionKey + "ab"},
		{name: "odd length", value: testEncryptionKey[:63]},
		{name: "non hex", value: strings.Repeat("z", 64)},
		{name: "whitespace is not a key", value: " "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, err := New("token", "user", "", "", test.value, 0)
			if !test.valid {
				if err == nil || client != nil {
					t.Fatalf("New() = (%v, %v), want nil client and an error", client, err)
				}
				if strings.TrimSpace(test.value) != "" && strings.Contains(err.Error(), test.value) {
					t.Error("error exposes supplied key")
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if test.value == "" {
				if client.encryptionKey != nil {
					t.Error("empty key does not disable encryption")
				}
			} else if want, _ := hex.DecodeString(testEncryptionKey); !bytes.Equal(client.encryptionKey, want) {
				t.Error("decoded key differs from configured key")
			}
		})
	}
}

func TestEncryptFieldRoundTrip(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString(testEncryptionKey)
	for _, plaintext := range []string{"", "Secret message", "Grüße 🚨\nFrom: root@host", strings.Repeat("long line ", 100)} {
		t.Run(fmt.Sprintf("length %d", len(plaintext)), func(t *testing.T) {
			t.Parallel()
			first, err := encryptField(plaintext, key, MaxMessageRunes)
			if err != nil {
				t.Fatalf("encryptField() error = %v", err)
			}
			got, err := decryptTestField(first, key)
			if err != nil || got != plaintext {
				t.Fatalf("decrypted field = %q, error = %v, want %q", got, err, plaintext)
			}
			second, err := encryptField(plaintext, key, MaxMessageRunes)
			if err != nil {
				t.Fatalf("encryptField() error = %v", err)
			}
			firstBytes, _ := base64.StdEncoding.DecodeString(first)
			secondBytes, _ := base64.StdEncoding.DecodeString(second)
			if bytes.Equal(firstBytes[:aes.BlockSize], secondBytes[:aes.BlockSize]) {
				t.Error("repeated encryption reused an IV")
			}
			for _, offset := range []int{0, aes.BlockSize, len(firstBytes) - 1} {
				tampered := bytes.Clone(firstBytes)
				tampered[offset] ^= 1
				if _, err := decryptTestField(base64.StdEncoding.EncodeToString(tampered), key); err == nil {
					t.Errorf("tampering at offset %d was not detected", offset)
				}
			}
		})
	}
}

func TestEncryptFieldFitsWireLimits(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString(testEncryptionKey)
	for _, limit := range []int{MaxTitleRunes, MaxMessageRunes} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			t.Parallel()
			var text strings.Builder
			for i := range limit {
				text.WriteRune(rune(0x10000 + i))
			}
			plaintext := text.String()
			encrypted, err := encryptField(plaintext, key, limit)
			if err != nil {
				t.Fatalf("encryptField() error = %v", err)
			}
			if len(encrypted) > limit {
				t.Errorf("encrypted length = %d, want <= %d", len(encrypted), limit)
			}
			got, err := decryptTestField(encrypted, key)
			if err != nil {
				t.Fatalf("decryptTestField() error = %v", err)
			}
			if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") ||
				!strings.HasPrefix(plaintext, strings.TrimSuffix(got, "…")) {
				t.Errorf("decrypted field is not a Unicode-safe truncated prefix: %q", got)
			}
			if utf8.RuneCountInString(got) >= limit {
				t.Error("test input did not exercise additional truncation for encryption overhead")
			}
		})
	}
}

func TestEncryptFieldFailsClosed(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString(testEncryptionKey)
	for _, invalidKey := range [][]byte{nil, make([]byte, 16), make([]byte, 24), make([]byte, 33)} {
		if got, err := encryptField("secret", invalidKey, MaxMessageRunes); err == nil || got != "" {
			t.Errorf("encryptField() with %d-byte key = (%q, %v), want empty result and error", len(invalidKey), got, err)
		}
	}
	if got, err := encryptField("secret", key, 1); err == nil || got != "" {
		t.Errorf("encryptField() with impossible limit = (%q, %v), want empty result and error", got, err)
	}
}

func TestEncryptFieldOpenSSLCompatibility(t *testing.T) {
	t.Parallel()
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("OpenSSL is not installed; Go wire-format tests still run")
	}
	key, _ := hex.DecodeString(testEncryptionKey)
	const plaintext = "Pushover interoperability: Grüße 🚨"
	encoded, err := encryptField(plaintext, key, MaxMessageRunes)
	if err != nil {
		t.Fatalf("encryptField() error = %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	authenticated, tag := data[:len(data)-sha256.Size], data[len(data)-sha256.Size:]
	mac := exec.Command(openssl, "dgst", "-sha256", "-mac", "HMAC", "-macopt", "hexkey:"+testEncryptionKey, "-binary")
	mac.Stdin = bytes.NewReader(authenticated)
	expectedTag, err := mac.Output()
	if err != nil {
		t.Fatalf("OpenSSL HMAC: %v", err)
	}
	if !bytes.Equal(tag, expectedTag) {
		t.Fatal("HMAC differs from OpenSSL")
	}
	decrypt := exec.Command(openssl, "enc", "-d", "-aes-256-cbc", "-K", testEncryptionKey,
		"-iv", hex.EncodeToString(authenticated[:aes.BlockSize]))
	decrypt.Stdin = bytes.NewReader(authenticated[aes.BlockSize:])
	compressed, err := decrypt.Output()
	if err != nil {
		t.Fatalf("OpenSSL AES-256-CBC decryption: %v", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != plaintext {
		t.Fatalf("OpenSSL-decrypted plaintext = %q, error = %v, want %q", got, err, plaintext)
	}
}
