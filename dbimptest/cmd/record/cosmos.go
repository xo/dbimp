package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// authCosmos is the Auth of a script that signs each request with the master key
// of Azure Cosmos DB. The password of each URL is the key, as base64 text, and
// the user is not read.
const authCosmos = "cosmos"

// cosmosVersion is the value of the header X-Ms-Version of each request. The
// service reads header names with no regard to case.
const cosmosVersion = "2018-12-31"

// cosmosTarget returns the type and the link of the resource that a path names,
// as the text that the master key signs holds them. A path of an odd number of
// segments names a list, such as /dbs/db/colls, and its link is the parent. A
// path of an even number names one resource, and its link is the path.
func cosmosTarget(path string) (string, string) {
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

// signCosmos sets the headers X-Ms-Date, X-Ms-Version and Authorization of req,
// as the REST API of Cosmos DB takes them with the master key. The key is the
// base64 text of the key of the account.
func signCosmos(req *http.Request, key string, now time.Time) error {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("reading the master key: %w", err)
	}
	date := strings.ToLower(now.UTC().Format(http.TimeFormat))
	rtype, link := cosmosTarget(req.URL.Path)
	text := strings.ToLower(req.Method) + "\n" + strings.ToLower(rtype) + "\n" + link + "\n" + date + "\n\n"
	mac := hmac.New(sha256.New, raw)
	mac.Write([]byte(text))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-Ms-Date", date)
	req.Header.Set("X-Ms-Version", cosmosVersion)
	req.Header.Set("Authorization", url.QueryEscape("type=master&ver=1.0&sig="+sig))
	return nil
}
