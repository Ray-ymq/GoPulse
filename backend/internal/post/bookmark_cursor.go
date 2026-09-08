package post

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// Bookmark continuation tokens are bounded, signed and bound to the session
// owner. The key is domain-separated from the authentication secret.
func (h *Handler) WithBookmarkCursorSecret(secret string) *Handler {
	h.bookmarkKey = sha256.Sum256([]byte("gopulse/bookmarks/v1\x00" + secret))
	return h
}
func newBookmarkKey() [32]byte {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic("initialize bookmark cursor key")
	}
	return key
}
func (h *Handler) signBookmarkCursor(viewer uint64, token string) string {
	mac := hmac.New(sha256.New, h.bookmarkKey[:])
	fmt.Fprintf(mac, "%d\x00%s", viewer, token)
	return token + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (h *Handler) verifyBookmarkCursor(viewer uint64, token string) (string, error) {
	if len(token) > 600 {
		return "", validationError("cursor is invalid")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(h.signBookmarkCursor(viewer, parts[0])), []byte(token)) {
		return "", validationError("cursor is invalid")
	}
	return parts[0], nil
}
