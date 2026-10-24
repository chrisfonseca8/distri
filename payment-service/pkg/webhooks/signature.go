// Package webhooks receives Razorpay webhooks: it authenticates the request,
// records the event durably and acknowledges it. Applying the event is a
// separate step (services.WebhookProcessor).
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the hex HMAC-SHA256 of body, the value Razorpay sends in
// X-Razorpay-Signature. It is exported for tests and the simulator's contract
// check; the production path only verifies.
func Sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether signature is the HMAC-SHA256 of the exact raw body under
// the webhook secret. The comparison is constant-time. The body must be the bytes
// as received: re-serialising parsed JSON would change them.
func Verify(secret, body []byte, signature string) bool {
	if len(secret) == 0 || signature == "" {
		return false
	}
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), provided)
}
