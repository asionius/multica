// Package auth: Tencent SmartGate SSO support.
//
// SmartGate is Tencent's internal identity gateway. When this server is
// deployed behind SmartGate, every request carries an encrypted identity
// header (x-tai-identity) plus a signature and timestamp. We verify the
// signature, decrypt the JWE identity payload, and return the enclosed
// staff id / login name to the caller so a normal application session
// (JWT cookie) can be minted from it.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v3"
)

// smartGateKeySize is the required raw key byte length for A256GCM/dir.
// SmartGate delivers the key as an ASCII string whose bytes are used directly.
const smartGateKeySize = 32

// timestampSkew is the maximum allowed difference between the request
// timestamp header and the server clock when safeMode is true.
const smartGateTimestampSkew = 180 * time.Second

// expirationBuffer is the grace period added to the payload Expiration field
// when safeMode is true (mirrors the reference implementation).
const smartGateExpirationBuffer = 3 * time.Minute

type smartGateConfig struct {
	enabled  bool
	key      []byte
	safeMode bool
}

var (
	smartGateCfg     smartGateConfig
	smartGateCfgOnce sync.Once
)

// resetSmartGateConfigForTests clears the memoized config so tests can
// change the underlying env vars between cases. Package-private and
// intended only for use by smartgate_test.go / export_test.go.
func resetSmartGateConfigForTests() {
	smartGateCfgOnce = sync.Once{}
	smartGateCfg = smartGateConfig{}
}

func loadSmartGateConfig() smartGateConfig {
	smartGateCfgOnce.Do(func() {
		enabled, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("SMARTGATE_ENABLED")))
		key := []byte(os.Getenv("SMARTGATE_KEY"))

		safeMode := true
		if v := strings.TrimSpace(os.Getenv("SMARTGATE_SAFE_MODE")); v != "" {
			if parsed, err := strconv.ParseBool(v); err == nil {
				safeMode = parsed
			}
		}

		smartGateCfg = smartGateConfig{
			enabled:  enabled,
			key:      key,
			safeMode: safeMode,
		}
	})
	return smartGateCfg
}

// SmartGateEnabled reports whether SmartGate SSO is configured.
// It requires both SMARTGATE_ENABLED=true and a 32-byte SMARTGATE_KEY.
func SmartGateEnabled() bool {
	cfg := loadSmartGateConfig()
	return cfg.enabled && len(cfg.key) == smartGateKeySize
}

// SmartGateIdentity is the decrypted x-tai-identity payload.
// Field names match the upstream JWE body produced by SmartGate.
type SmartGateIdentity struct {
	StaffID    string    `json:"StaffId"`
	LoginName  string    `json:"LoginName"`
	Expiration time.Time `json:"-"`

	// rawExpiration keeps the original string for debugging / tests.
	rawExpiration string
}

// identityPayload is the raw shape used for JSON decoding before we
// normalize the Expiration field.
type identityPayload struct {
	StaffID    string `json:"StaffId"`
	LoginName  string `json:"LoginName"`
	Expiration string `json:"Expiration"`
}

// ParseSmartGateHeaders performs the full SmartGate handshake: signature
// verification, JWE decryption of x-tai-identity, and (when safeMode is
// enabled) timestamp / Expiration checks. Returns the decrypted identity
// on success, or a wrapped error describing what went wrong.
func ParseSmartGateHeaders(headers http.Header) (*SmartGateIdentity, error) {
	cfg := loadSmartGateConfig()
	if !cfg.enabled {
		return nil, fmt.Errorf("smartgate: disabled")
	}
	if len(cfg.key) != smartGateKeySize {
		return nil, fmt.Errorf("smartgate: key must be exactly %d bytes, got %d", smartGateKeySize, len(cfg.key))
	}

	identity := strings.TrimSpace(headers.Get("x-tai-identity"))
	timestamp := strings.TrimSpace(headers.Get("timestamp"))
	signature := strings.TrimSpace(headers.Get("signature"))
	rioSeq := headers.Get("x-rio-seq")
	staffID := headers.Get("staffid")
	staffName := headers.Get("staffname")
	extData := headers.Get("x-ext-data")

	if identity == "" {
		return nil, fmt.Errorf("smartgate: missing x-tai-identity header")
	}
	if timestamp == "" {
		return nil, fmt.Errorf("smartgate: missing timestamp header")
	}
	if signature == "" {
		return nil, fmt.Errorf("smartgate: missing signature header")
	}

	// 1. Verify signature.
	if err := verifySmartGateSignature(timestamp, signature, cfg.key, rioSeq, staffID, staffName, extData, cfg.safeMode); err != nil {
		return nil, err
	}

	// 2. Verify timestamp window (strict only under safeMode).
	if cfg.safeMode {
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("smartgate: invalid timestamp: %w", err)
		}
		delta := time.Since(time.Unix(ts, 0))
		if delta < 0 {
			delta = -delta
		}
		if delta > smartGateTimestampSkew {
			return nil, fmt.Errorf("smartgate: timestamp outside allowed window (%s)", delta)
		}
	}

	// 3. Decrypt JWE.
	payload, err := decryptSmartGateIdentity(identity, cfg.key)
	if err != nil {
		return nil, err
	}

	// 4. Verify Expiration (strict only under safeMode).
	var exp time.Time
	if payload.Expiration != "" {
		parsed, perr := time.Parse(time.RFC3339, payload.Expiration)
		if perr != nil {
			if cfg.safeMode {
				return nil, fmt.Errorf("smartgate: invalid Expiration %q: %w", payload.Expiration, perr)
			}
		} else {
			exp = parsed
			if cfg.safeMode && time.Now().After(exp.Add(smartGateExpirationBuffer)) {
				return nil, fmt.Errorf("smartgate: identity expired at %s", exp.Format(time.RFC3339))
			}
		}
	} else if cfg.safeMode {
		return nil, fmt.Errorf("smartgate: missing Expiration in payload")
	}

	return &SmartGateIdentity{
		StaffID:       payload.StaffID,
		LoginName:     payload.LoginName,
		Expiration:    exp,
		rawExpiration: payload.Expiration,
	}, nil
}

// verifySmartGateSignature rebuilds the SHA-256 digest SmartGate uses and
// compares it against the supplied signature in constant time.
//
// safeMode=true:   extHeaders = [rioSeq, "", "", ""]
// safeMode=false:  extHeaders = [rioSeq, staffID, staffName, extData]
func verifySmartGateSignature(timestamp, signature string, key []byte, rioSeq, staffID, staffName, extData string, safeMode bool) error {
	var extHeaders []string
	if safeMode {
		extHeaders = []string{rioSeq, "", "", ""}
	} else {
		extHeaders = []string{rioSeq, staffID, staffName, extData}
	}

	var sb strings.Builder
	sb.WriteString(timestamp)
	sb.Write(key)
	sb.WriteString(strings.Join(extHeaders, ","))
	sb.WriteString(timestamp)

	sum := sha256.Sum256([]byte(sb.String()))
	expectedBytes := sum[:]

	sigBytes, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(sigBytes) != sha256.Size {
		return fmt.Errorf("smartgate: signature format invalid")
	}
	if subtle.ConstantTimeCompare(expectedBytes, sigBytes) != 1 {
		return fmt.Errorf("smartgate: signature mismatch")
	}
	return nil
}

// decryptSmartGateIdentity decrypts a JWE compact serialization produced
// with alg=dir, enc=A256GCM and the shared symmetric key.
func decryptSmartGateIdentity(compact string, key []byte) (*identityPayload, error) {
	obj, err := jose.ParseEncrypted(compact)
	if err != nil {
		return nil, fmt.Errorf("smartgate: parse JWE: %w", err)
	}
	raw, err := obj.Decrypt(key)
	if err != nil {
		return nil, fmt.Errorf("smartgate: decrypt JWE: %w", err)
	}
	payload := &identityPayload{}
	if err := json.Unmarshal(raw, payload); err != nil {
		return nil, fmt.Errorf("smartgate: parse identity payload: %w", err)
	}
	if payload.LoginName == "" || payload.StaffID == "" {
		return nil, fmt.Errorf("smartgate: identity payload missing StaffId/LoginName")
	}
	return payload, nil
}
