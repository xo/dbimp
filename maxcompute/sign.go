package maxcompute

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // The signature of the service is an HMAC with SHA-1.
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// basePath is the path that the endpoint holds before the resource. The
// signature leaves it out of the resource.
const basePath = "/api"

// sign sets the headers Date and Authorization of req, with signature version
// 4 (D221 item 1). The signature is the base64 of the HMAC with SHA-1 of the
// text below, under a key that an HMAC with SHA-256 derives from the secret,
// the date, the region and the names of the service. The text has the method,
// Content-Md5, Content-Type, Date, each header that starts with x-odps- in
// the order of its name, and the resource with its sorted query.
func sign(req *http.Request, id, secret, region string, now time.Time) {
	now = now.UTC()
	req.Header.Set("Date", now.Format(http.TimeFormat))
	var b strings.Builder
	b.WriteString(req.Method + "\n")
	b.WriteString(req.Header.Get("Content-MD5") + "\n")
	b.WriteString(req.Header.Get("Content-Type") + "\n")
	b.WriteString(req.Header.Get("Date") + "\n")
	var names []string
	for name := range req.Header {
		if lower := strings.ToLower(name); strings.HasPrefix(lower, "x-odps-") {
			names = append(names, lower)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteString(name + ":" + strings.Join(req.Header.Values(name), ",") + "\n")
	}
	b.WriteString(strings.TrimPrefix(req.URL.Path, basePath))
	b.WriteString(sortedQuery(req.URL.Query()))
	date := now.Format("20060102")
	key := hmacSHA256([]byte("aliyun_v4"+secret), date)
	for _, part := range []string{region, "odps", "aliyun_v4_request"} {
		key = hmacSHA256(key, part)
	}
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(b.String()))
	req.Header.Set("Authorization", "ODPS "+id+"/"+date+"/"+region+"/odps/aliyun_v4_request:"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// sortedQuery returns the query as the signature writes it: a question mark,
// and each key in order, as key or key=value, joined by an ampersand. It is
// empty for a request with no query.
func sortedQuery(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('?')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(key)
		if v := query.Get(key); v != "" {
			b.WriteString("=" + v)
		}
	}
	return b.String()
}

// hmacSHA256 returns the HMAC with SHA-256 of text under key.
func hmacSHA256(key []byte, text string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(text))
	return mac.Sum(nil)
}
