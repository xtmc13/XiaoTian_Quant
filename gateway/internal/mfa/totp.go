// Package mfa implements TOTP (RFC 6238) on top of HMAC-SHA1 with a 30-second
// time step, plus backup-code generation. It uses only the Go standard library
// so the gateway gains no new third-party dependency.
package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// Step is the TOTP time step (RFC 6238 default: 30s).
	Step = 30 * time.Second
	// Digits is the number of digits in generated codes.
	Digits = 6
	// secretBytes is the raw secret length (160 bits, RFC 4226 recommendation).
	secretBytes = 20
)

// GenerateSecret returns a new random base32-encoded TOTP secret (no padding).
func GenerateSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// CodeAt returns the TOTP code for the given secret at time t.
func CodeAt(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("invalid base32 secret")
	}
	counter := uint64(t.Unix()) / uint64(Step.Seconds())
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	otp := bin % 1000000
	return fmt.Sprintf("%0*d", Digits, otp), nil
}

// Validate checks the code against the secret at time t, accepting the
// previous, current and next 30s windows (clock skew tolerance).
func Validate(secret, code string, t time.Time) bool {
	code = strings.TrimSpace(code)
	if code == "" || len(code) != Digits {
		return false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return false
	}
	for _, offset := range []int{-1, 0, 1} {
		want, err := CodeAt(secret, t.Add(time.Duration(offset)*Step))
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(want), []byte(code)) {
			return true
		}
	}
	return false
}

// OTPAuthURI builds the otpauth:// URI shown/saved by authenticator apps.
func OTPAuthURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	v := url.Values{}
	v.Set("secret", strings.ToUpper(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", strconv.Itoa(Digits))
	v.Set("period", strconv.Itoa(int(Step.Seconds())))
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// GenerateBackupCodes returns n one-time recovery codes (8 chars, dash-grouped).
func GenerateBackupCodes(n int) []string {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	codes := make([]string, 0, n)
	for len(codes) < n {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			break
		}
		var sb strings.Builder
		for i, x := range b {
			if i == 4 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(x)%len(alphabet)])
		}
		codes = append(codes, sb.String())
	}
	return codes
}
