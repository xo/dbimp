package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: the signature of MaxCompute is an HMAC with SHA-1.
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
	"time"
)

// authODPS is the Auth of a script that signs each request as MaxCompute takes
// it, with signature version 4. The user of each URL is the access key id, its
// password is the access key secret, and the region of the script names the
// region of the key.
const authODPS = "odps"

// odpsPrefix starts the headers that the signature covers.
const odpsPrefix = "x-odps-"

// signODPS sets the headers Date and Authorization of req. The base is the
// path of the endpoint, such as /api, which the signature leaves out of the
// resource.
func signODPS(req *http.Request, base, id, secret, region string, now time.Time) {
	req.Header.Set("Date", now.UTC().Format(http.TimeFormat))
	var b bytes.Buffer
	b.WriteString(req.Method + "\n")
	b.WriteString(req.Header.Get("Content-MD5") + "\n")
	b.WriteString(req.Header.Get("Content-Type") + "\n")
	b.WriteString(req.Header.Get("Date") + "\n")
	var names []string
	for name := range req.Header {
		if lower := strings.ToLower(name); strings.HasPrefix(lower, odpsPrefix) {
			names = append(names, lower)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteString(name + ":" + strings.Join(req.Header.Values(name), ",") + "\n")
	}
	b.WriteString(strings.TrimPrefix(req.URL.Path, base))
	if query := req.URL.Query(); len(query) > 0 {
		keys := make([]string, 0, len(query))
		for key := range query {
			keys = append(keys, key)
		}
		sort.Strings(keys)
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
	}
	date := now.UTC().Format("20060102")
	key := hmacSHA256([]byte("aliyun_v4"+secret), date)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "odps")
	key = hmacSHA256(key, "aliyun_v4_request")
	mac := hmac.New(sha1.New, key)
	mac.Write(b.Bytes())
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	req.Header.Set("Authorization", "ODPS "+id+"/"+date+"/"+region+"/odps/aliyun_v4_request:"+sig)
}

// hmacSHA256 returns the HMAC of text under key.
func hmacSHA256(key []byte, text string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(text))
	return mac.Sum(nil)
}
