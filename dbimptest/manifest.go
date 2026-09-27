package dbimptest

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
)

// ManifestName is the name of the manifest in testdata/<driver>/.
const ManifestName = "manifest.json"

// The principals of step 6 of docs/DRIVER.md.
const (
	// Administrator is the principal that administers the server.
	Administrator = "administrator"
	// Ordinary is the ordinary user that the Init step of dbrun creates.
	Ordinary = "ordinary"
)

// Manifest lists the recorded exchanges of one driver, as step 6 of
// docs/DRIVER.md requires.
type Manifest struct {
	// Driver is the name of the driver.
	Driver string `json:"driver"`
	// NoOrdinaryUser is the reason why the product has no ordinary user, or
	// "" if it has one.
	NoOrdinaryUser string `json:"noOrdinaryUser,omitzero"`
	// Entries has one entry for each recorded exchange.
	Entries []Entry `json:"entries"`
}

// Entry is one recorded exchange, or one item of step 6 that does not apply
// to the product.
type Entry struct {
	// Item is the number of the item in step 6 of docs/DRIVER.md.
	Item int `json:"item"`
	// Principal is Administrator or Ordinary.
	Principal string `json:"principal"`
	// Release is the release of the server, as dbrun names it.
	Release string `json:"release"`
	// Date is the date of the recording, as YYYY-MM-DD.
	Date string `json:"date"`
	// File is the name of the exchange in testdata/<driver>/, or "" if Absent
	// is set.
	File string `json:"file,omitzero"`
	// Absent is the reason why the item does not apply to the product, such as
	// "no transactions", or "" if File is set.
	Absent string `json:"absent,omitzero"`
}

// ReadManifest reads the manifest in the file at path.
func ReadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading a manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("reading the manifest %s: %w", path, err)
	}
	return &m, nil
}

// WriteManifest writes m to the file at path.
func WriteManifest(path string, m *Manifest) error {
	b, err := json.Marshal(m, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("writing a manifest: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing the manifest %s: %w", path, err)
	}
	return nil
}
