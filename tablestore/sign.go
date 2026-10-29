package tablestore

import (
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec // G501: the server asks for the MD5 of the body in x-ots-contentmd5, as a check of the body and not as a secret.
	"crypto/sha1" //nolint:gosec // G505: the signature of Tablestore is an HMAC with SHA-1.
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The values of the headers of a request (recorded: every request). The date
// is an ISO 8601 time in UTC, which the server accepts with a fraction of three
// digits.
const (
	apiVersion   = "2015-12-31"
	dateLayout   = "2006-01-02T15:04:05.000Z"
	headerPrefix = "x-ots-"
)

// sign sets the headers that Tablestore needs on req, and signs it with the
// first version of the signature (D222). The signature is the base64 text of the
// HMAC with SHA-1, under the secret, of the path, the method and each header that
// starts with x-ots- (except the signature), one for each line, in the order of
// their names.
func sign(req *http.Request, body []byte, id, secret, instance string, now time.Time) {
	sum := md5.Sum(body) //nolint:gosec // G401: the server asks for the MD5 of the body as a check of the body, and not as a secret.
	req.Header.Set("X-Ots-Date", now.UTC().Format(dateLayout))
	req.Header.Set("X-Ots-Apiversion", apiVersion)
	req.Header.Set("X-Ots-Accesskeyid", id)
	req.Header.Set("X-Ots-Instancename", instance)
	req.Header.Set("X-Ots-Contentmd5", base64.StdEncoding.EncodeToString(sum[:]))
	var names []string
	for name := range req.Header {
		if lower := strings.ToLower(name); strings.HasPrefix(lower, headerPrefix) && lower != "x-ots-signature" {
			names = append(names, lower)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(req.URL.Path + "\n" + req.Method + "\n\n")
	for _, name := range names {
		b.WriteString(name + ":" + strings.TrimSpace(req.Header.Get(name)) + "\n")
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(b.String()))
	req.Header.Set("X-Ots-Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}
