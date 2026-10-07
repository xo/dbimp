package dbimp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
)

// SignV4 signs req, whose body is payload, with AWS Signature Version 4, by
// the access key key and the secret key secret, in the scope of region and
// service, at the time now. It signs the host and every header that req
// holds, so a caller that sends temporary credentials sets
// X-Amz-Security-Token before it calls SignV4. It sets X-Amz-Date and
// Authorization. The standard library is enough for it (D13 and D169).
func SignV4(req *http.Request, payload []byte, key, secret, region, service string, now time.Time) {
	now = now.UTC()
	day, stamp := now.Format("20060102"), now.Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", stamp)
	headers := map[string]string{"host": req.URL.Host}
	for name, vals := range req.Header {
		trimmed := make([]string, len(vals))
		for i, v := range vals {
			trimmed[i] = strings.Join(strings.Fields(v), " ")
		}
		headers[strings.ToLower(name)] = strings.Join(trimmed, ",")
	}
	names := slices.Sorted(maps.Keys(headers))
	var canonical strings.Builder
	for _, name := range names {
		canonical.WriteString(name + ":" + headers[name] + "\n")
	}
	signed := strings.Join(names, ";")
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	query := strings.ReplaceAll(req.URL.Query().Encode(), "+", "%20")
	request := strings.Join([]string{req.Method, path, query, canonical.String(), signed, hexSHA256(payload)}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hexSHA256([]byte(request))
	k := []byte("AWS4" + secret)
	for _, part := range []string{day, region, service, "aws4_request"} {
		k = hmacSHA256(k, part)
	}
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+key+"/"+scope+
		", SignedHeaders="+signed+", Signature="+hex.EncodeToString(hmacSHA256(k, toSign)))
}

// hexSHA256 returns the SHA-256 of b in lower case hex.
func hexSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// hmacSHA256 returns the HMAC-SHA256 of s by the key key.
func hmacSHA256(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}
