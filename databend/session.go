package databend

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"strings"
	"time"
)

// session is the session of a connection, which each response returns and
// the next request sends back (D121 and D122). The driver keeps every member
// as the server wrote it, and reads only the database, the settings and the
// state of the transaction.
type session map[string]jsontext.Value

// The settings that the session of the DSN sends, so that the values arrive
// in the form that the driver reads (D118).
var baseSettings = map[string]string{
	"format_null_as_str":     "0",
	"geometry_output_format": "WKT",
	"http_json_result_mode":  "display",
}

// newSession returns the session of the DSN of cfg.
func newSession(cfg *Config) session {
	s := session{}
	s.set("database", cfg.Database)
	settings := maps.Clone(baseSettings)
	if cfg.Timezone != "" {
		settings["timezone"] = cfg.Timezone
	}
	s.setSettings(settings)
	return s
}

// parseSession reads the session of a response.
func parseSession(v jsontext.Value) (session, error) {
	s := session{}
	if err := json.Unmarshal(v, &s); err != nil {
		return nil, fmt.Errorf("reading the session of the response: %w", err)
	}
	return s, nil
}

// str returns the member key as a string, or "".
func (s session) str(key string) string {
	var v string
	_ = json.Unmarshal(s[key], &v)
	return v
}

// set sets the member key to the string v.
func (s session) set(key, v string) {
	b, _ := json.Marshal(v)
	s[key] = b
}

// settings returns the settings of the session.
func (s session) settings() map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(s["settings"], &m)
	return m
}

// setSettings sets the settings of the session to m.
func (s session) setSettings(m map[string]string) {
	b, _ := json.Marshal(m)
	s["settings"] = b
}

// with returns a copy of s with the database and the settings of one
// statement, which the next statement does not keep.
func (s session) with(database string, settings map[string]string) session {
	out := maps.Clone(s)
	if database != "" {
		out.set("database", database)
	}
	if len(settings) > 0 {
		m := out.settings()
		maps.Copy(m, settings)
		out.setSettings(m)
	}
	return out
}

// restore sets back, in s, the database and the settings that one
// statement set, to their values in prev, so that the connection keeps only
// what the statement itself changed, such as by USE or SET (D122).
func (s session) restore(prev session, database string, settings map[string]string) {
	if database != "" {
		if v, ok := prev["database"]; ok {
			s["database"] = v
		}
	}
	if len(settings) == 0 {
		return
	}
	m, old := s.settings(), prev.settings()
	for k := range settings {
		if v, ok := old[k]; ok {
			m[k] = v
		} else {
			delete(m, k)
		}
	}
	s.setSettings(m)
}

// format returns how the server writes the values of the rows, by the
// settings of s. zone is the timezone of the server, for a session that names
// none.
func (s session) format(zone *time.Location) (*format, error) {
	m := s.settings()
	f := &format{
		driver:   m["http_json_result_mode"] == "driver",
		binary:   strings.ToLower(m["binary_output_format"]),
		geometry: strings.ToLower(m["geometry_output_format"]),
		loc:      zone,
	}
	if tz := m["timezone"]; tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("reading the timezone %q of the session: %w", tz, err)
		}
		f.loc = loc
	}
	return f, nil
}
