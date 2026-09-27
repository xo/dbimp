package dbimptest

import (
	"encoding/json/v2"
	"fmt"
	"os"
)

// FeaturesName is the name of the survey of step 5a of docs/DRIVER.md in
// testdata/<driver>/.
const FeaturesName = "features.json"

// The kinds of an entry of the survey.
const (
	// KindCRUD is a statement of CRUD, such as update.
	KindCRUD = "crud"
	// KindSchema is an operation on a schema, such as a foreign key.
	KindSchema = "schema"
	// KindFeature is a feature of the database that is its own.
	KindFeature = "feature"
	// KindType is a native type.
	KindType = "type"
)

// The kinds of a source of the survey.
const (
	// SourceModel is an AI model.
	SourceModel = "model"
	// SourceDriver is another driver, in Go or in another language.
	SourceDriver = "driver"
)

// The verdicts of an entry of the survey.
const (
	// NotMeasured is the verdict of an entry that no server settled yet.
	NotMeasured = "not measured"
	// Yes is the verdict of an entry that the server supports.
	Yes = "yes"
	// No is the verdict of an entry that the server refused.
	No = "no"
)

// Features is the survey of step 5a of docs/DRIVER.md: each operation,
// feature and type of one database, where each came from, what the server
// said about it, and the test that exercises it.
type Features struct {
	// Driver is the name of the driver.
	Driver string `json:"driver"`
	// Consulted lists each model and each driver that the survey asked.
	Consulted []Source `json:"consulted"`
	// Entries has one entry for each operation, feature and type.
	Entries []Feature `json:"entries"`
}

// Source is one model or one driver that the survey asked.
type Source struct {
	// Source names the model, or the import path of the driver.
	Source string `json:"source"`
	// Kind is SourceModel or SourceDriver.
	Kind string `json:"kind"`
	// Date is the date of the question, as YYYY-MM-DD.
	Date string `json:"date"`
}

// Feature is one operation, feature or type of the survey.
type Feature struct {
	// Kind is KindCRUD, KindSchema, KindFeature or KindType.
	Kind string `json:"kind"`
	// Name names it, such as "update", "foreign key" or "number". A type is
	// named as the type table names it.
	Name string `json:"name"`
	// Sources names each source that named it.
	Sources []string `json:"sources"`
	// Verdict is NotMeasured, Yes or No.
	Verdict string `json:"verdict"`
	// Evidence is the recorded file under testdata/<driver>/ that shows what
	// the server answered.
	Evidence string `json:"evidence,omitzero"`
	// Test is the test that exercises it, as a function and a subtest, such
	// as "TestIntegrationCRUD/update". For the verdict No, the test sends
	// the operation and expects the refusal of the server.
	Test string `json:"test,omitzero"`
}

// ReadFeatures reads the survey in the file at path.
func ReadFeatures(path string) (*Features, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading a survey: %w", err)
	}
	var f Features
	if err := json.Unmarshal(b, &f, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("reading the survey %s: %w", path, err)
	}
	return &f, nil
}
