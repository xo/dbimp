package cosmos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// apiVersion is the value of the header X-Ms-Version of each request. It is
// the version that step 6 measured from end to end (D190).
const apiVersion = "2018-12-31"

// target returns the type and the link of the resource that a path names, as
// the text that the master key signs holds them. A path of an odd number of
// segments names a list, such as /dbs/db/colls, and its link is the parent.
// A path of an even number names one resource, and its link is the path. The
// path of the account, /, has neither.
func target(path string) (string, string) {
	path, _, _ = strings.Cut(path, "?")
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) == 1 && segs[0] == "" {
		return "", ""
	}
	if len(segs)%2 == 1 {
		return segs[len(segs)-1], strings.Join(segs[:len(segs)-1], "/")
	}
	return segs[len(segs)-2], strings.Join(segs, "/")
}

// sign sets the headers X-Ms-Date, X-Ms-Version and Authorization of req, as
// the REST API of Cosmos DB takes them with the master key. The key is the
// base64 text of the key of the account. The signature is the base64 text of
// HMAC-SHA256, with the key as its bytes, over the lower case verb, the lower
// case type of the resource, the link of the resource, the lower case date
// and an empty line, each ended by a new line, and the header is the URL
// encoding of type=master&ver=1.0&sig=<signature> (recorded, and Microsoft,
// "Access Control on Azure Cosmos DB Resources"). The error never holds the
// key (D94).
func sign(req *http.Request, key string, now time.Time) error {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("reading the master key: it is not base64 text: %w", dbimp.ErrInvalidValue)
	}
	date := strings.ToLower(now.UTC().Format(http.TimeFormat))
	rtype, link := target(req.URL.Path)
	text := strings.ToLower(req.Method) + "\n" + strings.ToLower(rtype) + "\n" + link + "\n" + date + "\n\n"
	mac := hmac.New(sha256.New, raw)
	mac.Write([]byte(text))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-Ms-Date", date)
	req.Header.Set("X-Ms-Version", apiVersion)
	req.Header.Set("Authorization", url.QueryEscape("type=master&ver=1.0&sig="+sig))
	return nil
}
