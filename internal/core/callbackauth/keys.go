package callbackauth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

const maxBody = 64 * 1024

type key struct {
	ID         string    `json:"key_id"`
	Restaurant uuid.UUID `json:"restaurant_id"`
	Secret     string    `json:"secret_base64"`
	Expires    time.Time `json:"expires_at"`
	secret     []byte
}
type Keys struct{ keys map[string]key }

func Load(path string) (*Keys, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open callback keys: %w", err)
	}
	defer func() { _ = file.Close() }()
	var entries []key
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode callback keys: %w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("trailing callback key data")
	}
	keys := &Keys{keys: make(map[string]key)}
	if len(entries) == 0 {
		return nil, fmt.Errorf("callback keys empty")
	}
	for _, entry := range entries {
		secret, err := base64.StdEncoding.DecodeString(entry.Secret)
		if err != nil || len(secret) < 32 || entry.ID == "" || len(entry.ID) > 128 || entry.Restaurant == uuid.Nil || entry.Expires.IsZero() {
			return nil, fmt.Errorf("invalid callback key")
		}
		if _, ok := keys.keys[entry.ID]; ok {
			return nil, fmt.Errorf("duplicate callback key ID")
		}
		entry.secret = secret
		keys.keys[entry.ID] = entry
	}
	return keys, nil
}
func signature(secret []byte, timestamp, method, path string, body []byte) string {
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "v1\n%s\n%s\n%s\n%s", timestamp, method, path, hex.EncodeToString(digest[:]))
	return hex.EncodeToString(mac.Sum(nil))
}
func (k *Keys) Signer(id string, restaurant uuid.UUID) (func(*http.Request, []byte) error, error) {
	entry, ok := k.keys[id]
	if !ok || entry.Restaurant != restaurant || !time.Now().Before(entry.Expires) {
		return nil, fmt.Errorf("invalid active callback key")
	}
	return func(r *http.Request, body []byte) error {
		if !time.Now().Before(entry.Expires) {
			return fmt.Errorf("callback key expired")
		}
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		r.Header.Set("X-Callback-Key-ID", id)
		r.Header.Set("X-Callback-Timestamp", timestamp)
		r.Header.Set("X-Callback-Signature", signature(entry.secret, timestamp, r.Method, r.URL.EscapedPath(), body))
		return nil
	}, nil
}
func (k *Keys) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deny := func(code int) { http.Error(w, http.StatusText(code), code) }
		for _, header := range []string{"X-Callback-Key-ID", "X-Callback-Timestamp", "X-Callback-Signature"} {
			if len(r.Header.Values(header)) != 1 {
				deny(401)
				return
			}
		}
		entry, ok := k.keys[r.Header.Get("X-Callback-Key-ID")]
		restaurant, err := uuid.Parse(r.PathValue("restaurant_id"))
		if !ok || err != nil || restaurant != entry.Restaurant || !time.Now().Before(entry.Expires) {
			deny(401)
			return
		}
		timestamp := r.Header.Get("X-Callback-Timestamp")
		seconds, err := strconv.ParseInt(timestamp, 10, 64)
		now := time.Now()
		sent := time.Unix(seconds, 0)
		if err != nil || sent.Before(now.Add(-5*time.Minute)) || sent.After(now.Add(time.Minute)) || r.Method != "POST" {
			deny(401)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			deny(400)
			return
		}
		if len(body) > maxBody {
			deny(413)
			return
		}
		originalPath := r.URL.EscapedPath()
		if r.RequestURI != "" {
			original, parseErr := url.ParseRequestURI(r.RequestURI)
			if parseErr != nil {
				deny(401)
				return
			}
			originalPath = original.EscapedPath()
		}
		provided, err := hex.DecodeString(r.Header.Get("X-Callback-Signature"))
		expected, _ := hex.DecodeString(signature(entry.secret, timestamp, r.Method, originalPath, body))
		if err != nil || !hmac.Equal(provided, expected) {
			deny(401)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}
