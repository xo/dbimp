package maxcompute_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/maxcompute"
)

// The id and the secret are no real key.
const (
	keyID   = "LTAItestid"
	secret  = "test+secret/0123456789"
	host    = "service.ap-southeast-5.maxcompute.aliyun.com"
	project = "dbimp"
)

// cfg returns the configuration of a DSN with the host, the key and the project.
func cfg() maxcompute.Config {
	return maxcompute.Config{Host: host, Port: 443, ID: keyID, Secret: secret, Project: project, Region: "ap-southeast-5"}
}

// base is the DSN of cfg, with the secret escaped as a URL holds it.
const base = "maxcompute://" + keyID + ":test%2Bsecret%2F0123456789@" + host + "/" + project

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(f func(*maxcompute.Config)) maxcompute.Config {
		c := cfg()
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want maxcompute.Config
	}{
		{base, cfg()},
		{base + "/", cfg()},
		{base + "?tls=true", cfg()},
		{base + "?tls=false", with(func(c *maxcompute.Config) { c.Insecure = true })},
		{base + "?region=ap-southeast-5", cfg()},
		{base + "?odps.sql.submit.mode=script", with(func(c *maxcompute.Config) { c.Settings = map[string]string{"odps.sql.submit.mode": "script"} })},
		{base + "?odps.sql.timezone=UTC&odps.sql.submit.mode=script", with(func(c *maxcompute.Config) {
			c.Settings = map[string]string{"odps.sql.timezone": "UTC", "odps.sql.submit.mode": "script"}
		})},
		// A host of another form needs the key region.
		{"maxcompute://" + keyID + ":x@mc.example.test:8443/p?region=cn-hangzhou&tls=false", maxcompute.Config{Host: "mc.example.test", Port: 8443, ID: keyID, Secret: "x", Project: "p", Region: "cn-hangzhou", Insecure: true}},
		{"maxcompute://" + keyID + ":x@127.0.0.1:8080/p?region=r", maxcompute.Config{Host: "127.0.0.1", Port: 8080, ID: keyID, Secret: "x", Project: "p", Region: "r"}},
		{"maxcompute://" + keyID + ":x@[::1]:8080/p?region=r", maxcompute.Config{Host: "::1", Port: 8080, ID: keyID, Secret: "x", Project: "p", Region: "r"}},
		{"maxcompute://" + keyID + ":x@[::1]/p?region=r", maxcompute.Config{Host: "::1", Port: 443, ID: keyID, Secret: "x", Project: "p", Region: "r"}},
		// A key that holds characters that a URL escapes.
		{"maxcompute://a%40b:p%40ss@" + host + "/p%20q", with(func(c *maxcompute.Config) { c.ID, c.Secret, c.Project = "a@b", "p@ss", "p q" })},
	} {
		got, err := maxcompute.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%s): %v", redacted(tt.dsn), err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%s) = %+v, want %+v", redacted(tt.dsn), *got, tt.want)
			continue
		}
		again, err := maxcompute.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN) of %s: %v", redacted(tt.dsn), err)
			continue
		}
		if !reflect.DeepEqual(*again, *got) {
			t.Errorf("the round trip of %s is %+v, want %+v", redacted(tt.dsn), *again, *got)
		}
	}
}

// redacted returns a DSN with its password hidden, for a message of a test.
func redacted(dsn string) string {
	i, j := strings.Index(dsn, "://"), strings.LastIndex(dsn, "@")
	if i < 0 || j < i {
		return dsn
	}
	return dsn[:i+3] + "***" + dsn[j:]
}

func TestDSNErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, dsn string
		want      error
	}{
		{"another scheme", "postgres://a:b@" + host + "/p", dbimp.ErrScheme},
		{"no host", "maxcompute://a:b@/p?region=r", dbimp.ErrInvalidValue},
		{"no user", "maxcompute://:b@" + host + "/p", dbimp.ErrInvalidValue},
		{"no password", "maxcompute://a@" + host + "/p", dbimp.ErrInvalidValue},
		{"no user and no password", "maxcompute://" + host + "/p", dbimp.ErrInvalidValue},
		{"no project", "maxcompute://a:b@" + host, dbimp.ErrInvalidValue},
		{"two names in the path", "maxcompute://a:b@" + host + "/p/q", dbimp.ErrInvalidValue},
		{"a host of another form with no region", "maxcompute://a:b@mc.example.test/p", dbimp.ErrInvalidValue},
		{"an empty region", "maxcompute://a:b@" + host + "/p?region=", dbimp.ErrInvalidValue},
		{"an unknown key", base + "?timeout=5s", dbimp.ErrUnknownKey},
		{"a key of a setting without the prefix", base + "?sql.timezone=UTC", dbimp.ErrUnknownKey},
		{"a repeated key", base + "?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"a repeated setting", base + "?odps.a=1&odps.a=2", dbimp.ErrRepeatedKey},
		{"an empty setting", base + "?odps.a=", dbimp.ErrInvalidValue},
		{"a bad bool", base + "?tls=maybe", dbimp.ErrInvalidValue},
		{"a bad port", "maxcompute://a:b@" + host + ":99999/p", dbimp.ErrInvalidValue},
	} {
		_, err := maxcompute.ParseDSN(tt.dsn)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: ParseDSN gave %v, want %v", tt.name, err, tt.want)
			continue
		}
		if strings.Contains(err.Error(), "test+secret") || strings.Contains(err.Error(), "test%2Bsecret") {
			t.Errorf("%s: the error holds the secret: %v", tt.name, err)
		}
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		base, base + "?tls=false", base + "?odps.sql.submit.mode=script", "maxcompute://a:b@[::1]:1/p?region=r",
		"maxcompute://", "maxcompute://a:b@/", "maxcompute://a:b@h/p?region=r&region=s", "maxcompute://a:b@service..maxcompute.aliyun.com/p",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := maxcompute.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := maxcompute.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(FormatDSN) of a DSN that parsed: %v", err)
		}
		if !reflect.DeepEqual(*again, *got) {
			t.Fatalf("the round trip changed the configuration: %+v, then %+v", *got, *again)
		}
	})
}
