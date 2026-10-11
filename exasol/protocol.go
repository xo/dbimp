package exasol

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"github.com/xo/dbimp"
)

// These types are the frames of the WebSocket API of Exasol, as step 6
// recorded them (docs/EXASOL.md, Requests and Responses). A request is a JSON
// object with the member command. An answer is a JSON object with the member
// status, which is ok or error.

// The statuses of an answer.
const (
	statusOK    = "ok"
	statusError = "error"
)

// The commands that the driver sends.
const (
	cmdLogin        = "login"
	cmdExecute      = "execute"
	cmdCreate       = "createPreparedStatement"
	cmdExecutePrep  = "executePreparedStatement"
	cmdClosePrep    = "closePreparedStatement"
	cmdFetch        = "fetch"
	cmdCloseResult  = "closeResultSet"
	cmdSetAttribute = "setAttributes"
	cmdGetAttribute = "getAttributes"
	cmdAbort        = "abortQuery"
)

// envelope is an answer of the server.
type envelope struct {
	Status       string       `json:"status"`
	Exception    *exception   `json:"exception,omitzero"`
	Attributes   *attributes  `json:"attributes,omitzero"`
	ResponseData responseData `json:"responseData,omitzero"`
}

// exception is the error of an answer.
type exception struct {
	Text    string `json:"text"`
	SQLCode string `json:"sqlCode"`
}

// attributes are the attributes of the session that an answer reports. An
// answer holds the ones that a statement changed, such as timezone after
// ALTER SESSION SET TIME_ZONE (recorded: "set the time zone to Tokyo").
type attributes struct {
	Timezone      *string `json:"timezone,omitzero"`
	Autocommit    *bool   `json:"autocommit,omitzero"`
	CurrentSchema *string `json:"currentSchema,omitzero"`
}

// responseData holds the members of every kind of answer that the driver
// reads, so one type decodes the answer to each command.
type responseData struct {
	// The answer to login.
	PublicKeyModulus   string `json:"publicKeyModulus,omitzero"`
	PublicKeyExponent  string `json:"publicKeyExponent,omitzero"`
	ProtocolVersion    int    `json:"protocolVersion,omitzero"`
	TimeZone           string `json:"timeZone,omitzero"`
	MaxDataMessageSize int64  `json:"maxDataMessageSize,omitzero"`
	ReleaseVersion     string `json:"releaseVersion,omitzero"`

	// The answer to execute, createPreparedStatement and executePreparedStatement.
	Results         []result       `json:"results,omitzero"`
	StatementHandle int64          `json:"statementHandle,omitzero"`
	ParameterData   *parameterData `json:"parameterData,omitzero"`

	// The answer to fetch.
	NumRows int64      `json:"numRows,omitzero"`
	Data    columnData `json:"data,omitzero"`
}

// parameterData describes the parameters of a prepared statement. The driver
// sends Columns back as they came, in executePreparedStatement.
type parameterData struct {
	NumColumns int            `json:"numColumns"`
	Columns    jsontext.Value `json:"columns"`
}

// result is the result of one statement: a result set, or the number of rows
// that it changed.
type result struct {
	ResultType string     `json:"resultType"`
	RowCount   int64      `json:"rowCount,omitzero"`
	ResultSet  *resultSet `json:"resultSet,omitzero"`
}

// The types of a result.
const (
	resultSetType = "resultSet"
	rowCountType  = "rowCount"
)

// resultSet is a result set. A result of many rows has a handle, and the first
// message holds as many rows as fit in it, none for a result of 1000 rows or
// more that is small (recorded: "select 1000 rows").
type resultSet struct {
	Handle           int64        `json:"resultSetHandle,omitzero"`
	NumColumns       int          `json:"numColumns"`
	NumRows          int64        `json:"numRows"`
	NumRowsInMessage int64        `json:"numRowsInMessage"`
	Columns          []wireColumn `json:"columns"`
	Data             columnData   `json:"data,omitzero"`
}

// wireColumn is a column as the server names it.
type wireColumn struct {
	Name     string   `json:"name"`
	DataType dataType `json:"dataType"`
}

// dataType is the type of a column. The member type names it, and the members
// that it needs follow.
type dataType struct {
	Type              string `json:"type"`
	Precision         int    `json:"precision,omitzero"`
	Scale             int    `json:"scale,omitzero"`
	Size              int    `json:"size,omitzero"`
	CharacterSet      string `json:"characterSet,omitzero"`
	WithLocalTimeZone bool   `json:"withLocalTimeZone,omitzero"`
	Fraction          int    `json:"fraction,omitzero"`
	SRID              int    `json:"srid,omitzero"`
}

// columnData is the data of a result: a list of columns, and each column is the
// list of the values of that column (recorded: "select every row of types").
// Each entry is the raw JSON of one column, which a decoder reads one value at
// a time.
type columnData []jsontext.Value

// UnmarshalJSONFrom satisfies json.UnmarshalerFrom. It copies each column
// once, so the message can go.
func (cd *columnData) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() == 'n' {
		*cd = nil
		_, err := dec.ReadValue()
		return err
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '[' {
		return fmt.Errorf("reading the data: %v where a list of columns was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
	}
	var cols columnData
	for dec.PeekKind() != ']' {
		v, err := dec.ReadValue()
		if err != nil {
			return err
		}
		cols = append(cols, v.Clone())
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	*cd = cols
	return nil
}

// decodeEnvelope decodes an answer.
func decodeEnvelope(msg []byte) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(msg, &env); err != nil {
		return nil, fmt.Errorf("reading an answer: %w", err)
	}
	switch env.Status {
	case statusOK:
	case statusError:
		if env.Exception == nil {
			return &env, &Error{Message: "the answer has the status error and no exception"}
		}
		return &env, &Error{Code: env.Exception.SQLCode, Message: env.Exception.Text}
	default:
		return nil, fmt.Errorf("reading an answer: the status is %q: %w", env.Status, dbimp.ErrInvalidValue)
	}
	return &env, nil
}

// request is a frame that the driver sends. Its members keep their order.
type request struct {
	Command string `json:"command"`
}

type executeRequest struct {
	Command string `json:"command"`
	SQLText string `json:"sqlText"`
}

type createRequest executeRequest

type fetchRequest struct {
	Command         string `json:"command"`
	ResultSetHandle int64  `json:"resultSetHandle"`
	StartPosition   int64  `json:"startPosition"`
	NumBytes        int    `json:"numBytes"`
}

type closeResultRequest struct {
	Command string  `json:"command"`
	Handles []int64 `json:"resultSetHandles"`
}

type closePreparedRequest struct {
	Command string `json:"command"`
	Handle  int64  `json:"statementHandle"`
}

type attributeRequest struct {
	Command    string         `json:"command"`
	Attributes map[string]any `json:"attributes"`
}

type executePreparedRequest struct {
	Command    string           `json:"command"`
	Handle     int64            `json:"statementHandle"`
	NumColumns int              `json:"numColumns"`
	NumRows    int              `json:"numRows"`
	Columns    jsontext.Value   `json:"columns,omitzero"`
	Data       []jsontext.Value `json:"data"`
}

// unmarshalColumns reads the list of columns that describes the parameters of a
// prepared statement.
func unmarshalColumns(raw jsontext.Value, cols *[]wireColumn) error {
	if err := json.Unmarshal(raw, cols); err != nil {
		return fmt.Errorf("reading the columns of the parameters: %w", err)
	}
	return nil
}
