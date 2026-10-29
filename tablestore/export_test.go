package tablestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall"
)

// Raw sends the action of the API of Tablestore, such as CreateTable or
// DeleteTable, with the protobuf message body, as the connector of cfg sends a
// statement. It returns the status and the body of the answer, whatever the
// status is. The integration tests use it for what the driver does not send: the
// calls of the API that are no statement.
func Raw(ctx context.Context, cfg Config, action string, body []byte) (int, []byte, error) {
	c := NewConnector(cfg)
	defer c.transport.CloseIdleConnections()
	var res *http.Response
	// The service resets a connection now and then (recorded: step 6), and the
	// calls that the tests send here can go again.
	for try := 1; ; try++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/"+action, bytes.NewReader(body))
		if err != nil {
			return 0, nil, fmt.Errorf("making the request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		sign(req, body, cfg.User, cfg.Password, cfg.Instance, c.now())
		res, err = c.client.Do(req)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.ECONNRESET) || try == 5 {
			return 0, nil, fmt.Errorf("sending the request: %w", err)
		}
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("reading the answer: %w", err)
	}
	return res.StatusCode, b, nil
}

// The types of a primary key column in CreateTable.
const (
	KeyInteger = 1
	KeyString  = 2
	KeyBinary  = 3
)

// NoKeepAlives makes the connector open a new connection for each request. A
// service that resets an idle connection then costs no request.
func (c *Connector) NoKeepAlives() {
	c.transport.DisableKeepAlives = true
}

// CreateTableBody returns the body of CreateTable for a table named name with
// one primary key column named id, of the type keyType, and no other column,
// because a table of Tablestore has no schema for the other columns.
func CreateTableBody(name string, keyType int64) []byte {
	pk := appendString(nil, 1, "id")
	pk = appendInt(pk, 2, keyType)
	meta := appendString(nil, 1, name)
	meta = appendBytes(meta, 2, pk)
	body := appendBytes(nil, 1, meta)
	capacity := appendInt(nil, 1, 0)
	capacity = appendInt(capacity, 2, 0)
	body = appendBytes(body, 2, appendBytes(nil, 1, capacity))
	opts := appendInt(nil, 1, -1)
	opts = appendInt(opts, 2, 1)
	return appendBytes(body, 3, opts)
}

// DeleteTableBody returns the body of DeleteTable for the table name.
func DeleteTableBody(name string) []byte {
	return appendString(nil, 1, name)
}

// appendBytes appends the field number field, holding the bytes b.
func appendBytes(b []byte, field int, v []byte) []byte {
	return appendString(b, field, string(v))
}

// TableNames returns the names of the tables that the answer to ListTable holds,
// which are the strings of the field 1 of the message.
func TableNames(body []byte) []string {
	p := newPBReader(bytes.NewReader(body))
	var names []string
	for {
		field, wire, err := p.tag()
		if err != nil {
			return names
		}
		if field != 1 || wire != wireBytes {
			if p.skip(wire) != nil {
				return names
			}
			continue
		}
		n, err := p.length()
		if err != nil {
			return names
		}
		b, err := p.readBytes(n)
		if err != nil {
			return names
		}
		names = append(names, string(b))
	}
}
