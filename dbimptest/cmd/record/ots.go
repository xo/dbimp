package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec // G501: the server asks for the MD5 of the body as a check of the body, and not as a secret.
	"crypto/sha1" //nolint:gosec // G505: the signature of Tablestore is an HMAC with SHA-1.
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
	"time"
)

// authOTS is the Auth of a script that signs each request as Alibaba Cloud
// Tablestore takes it. The user of each URL is the access key id, its password
// is the access key secret, and the first label of the host is the name of the
// instance.
const authOTS = "ots"

// otsPrefix starts the headers that the signature covers.
const otsPrefix = "x-ots-"

// signOTS sets the headers that Tablestore needs on req, and signs it with
// the first version of its signature, which is an HMAC with SHA-1 over the
// path, the method and the headers that start with x-ots-.
func signOTS(req *http.Request, payload []byte, id, secret string, now time.Time) {
	sum := md5.Sum(payload) //nolint:gosec // G401: the MD5 of the body is a check of the body, and not a secret.
	instance, _, _ := strings.Cut(req.URL.Hostname(), ".")
	req.Header.Set("X-Ots-Date", now.UTC().Format("2006-01-02T15:04:05.000Z"))
	req.Header.Set("X-Ots-Apiversion", "2015-12-31")
	req.Header.Set("X-Ots-Accesskeyid", id)
	req.Header.Set("X-Ots-Instancename", instance)
	req.Header.Set("X-Ots-Contentmd5", base64.StdEncoding.EncodeToString(sum[:]))
	var names []string
	for name := range req.Header {
		if lower := strings.ToLower(name); strings.HasPrefix(lower, otsPrefix) && lower != "x-ots-signature" {
			names = append(names, lower)
		}
	}
	sort.Strings(names)
	var b bytes.Buffer
	b.WriteString(req.URL.Path + "\n" + req.Method + "\n\n")
	for _, name := range names {
		b.WriteString(name + ":" + strings.TrimSpace(req.Header.Get(name)) + "\n")
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(b.Bytes())
	req.Header.Set("X-Ots-Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}
