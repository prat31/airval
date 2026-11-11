package targets

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// A minimal AWS Signature Version 4 signer (header-based, single-chunk
// payloads), enough for Bedrock Runtime. Kept in-tree to avoid the AWS SDK.

type awsCreds struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// awsURIEscape percent-encodes everything but unreserved characters (and '/'
// when keepSlash).
func awsURIEscape(s string, keepSlash bool) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && keepSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&15])
		}
	}
	return b.String()
}

// signV4 computes the Authorization header value. wirePath is the request path
// exactly as sent on the wire (already percent-encoded once); for non-S3
// services it is encoded a second time in the canonical request. headers
// are the headers to sign (names case-insensitive; must include host).
// canonicalQuery must already be sorted and encoded.
func signV4(method, wirePath, canonicalQuery string, headers map[string]string, payload []byte,
	c awsCreds, region, service string, t time.Time) (authorization, amzDate string) {

	t = t.UTC()
	amzDate = t.Format("20060102T150405Z")
	date := t.Format("20060102")

	names := make([]string, 0, len(headers)+1)
	lower := make(map[string]string, len(headers)+1)
	names = append(names, "x-amz-date")
	lower["x-amz-date"] = amzDate
	for k, v := range headers {
		lk := strings.ToLower(k)
		if lk == "x-amz-date" {
			continue
		}
		names = append(names, lk)
		lower[lk] = strings.Join(strings.Fields(v), " ")
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, n := range names {
		ch.WriteString(n)
		ch.WriteByte(':')
		ch.WriteString(lower[n])
		ch.WriteByte('\n')
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		method,
		awsURIEscape(wirePath, true),
		canonicalQuery,
		ch.String(),
		signed,
		sha256Hex(payload),
	}, "\n")

	scope := date + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonical))

	k := hmacSHA256([]byte("AWS4"+c.SecretAccessKey), date)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))

	authorization = "AWS4-HMAC-SHA256 Credential=" + c.AccessKeyID + "/" + scope +
		", SignedHeaders=" + signed + ", Signature=" + sig
	return authorization, amzDate
}
