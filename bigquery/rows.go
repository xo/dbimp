package bigquery

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"

	"github.com/xo/dbimp"
)

// jobRef names a job: its id, and its location, which every later call sends
// (D189).
type jobRef struct {
	id       string
	location string
}

// rows reads the result of one statement, one token at a time (D25 and D189).
// An answer is one JSON object. The service wrote its members in this order:
// kind, schema, jobReference, totalRows, pageToken when there is one, rows, and
// then the totals and the times (recorded: bigquery-163). The driver does not
// need that order for anything but one rule: the schema comes before the rows.
// A result of several pages is the answer of the first request and then the
// answer of one getQueryResults for each page token, in order and one at a
// time, which the driver reads when the caller has read the rows before it.
type rows struct {
	s   *dbimp.Stream
	dec *jsontext.Decoder
	// fetch sends the request for a page, or for a poll. It holds the context of
	// the statement, so the rows hold no context (rule 4 of AGENTS.md).
	fetch func(job jobRef, token string, wait int) (*http.Response, error)
	// cause returns the error of the context of the statement, or nil.
	cause func() error

	names []string
	cols  []column
	// cur is the current row.
	cur []driver.Value
	job jobRef

	// token is the page token of the page that the rows read, and empty for the
	// last page.
	token string
	// schemaSeen is true once a schema reached the driver, and inRows while the
	// decoder stands inside the array of the rows of a page.
	schemaSeen bool
	inRows     bool
	// completeSeen is true when the answer holds jobComplete, and complete is its
	// value.
	completeSeen bool
	complete     bool
	// total is totalRows, and totalKnown is true when the answer holds it.
	total      int64
	totalKnown bool
	// affected is numDmlAffectedRows, and affectedKnown is true when the answer
	// holds it.
	affected      int64
	affectedKnown bool
	// page is the count of the pages after the first that the rows opened, and
	// count the rows that the rows read.
	page  int
	count int64
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the result is read to its
	// end.
	read bool
	done bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
)

// newRows reads the head of the answer res of a statement with the status 200,
// up to the first row. It polls a job that still runs, and it cancels the job
// when the context ends. It reads the end of an answer that holds no rows, and
// returns its error, which comes before any row and so does not wrap
// dbimp.ErrIncomplete (D107). The rows hold ctx in the function that fetches a
// page.
func (c *Connector) newRows(ctx context.Context, res *http.Response, o options) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), cause: ctx.Err}
	r.fetch = func(job jobRef, token string, wait int) (*http.Response, error) {
		return c.results(ctx, job, token, o, wait)
	}
	r.job.location = o.location
	if err := r.start(ctx, c, o); err != nil {
		_ = r.s.Close()
		return nil, err
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the type of the column in upper case, such as INTEGER, or ARRAY for a
// column of the mode REPEATED.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D189).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.cols[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. It is the mode of
// the field: an ARRAY and a REQUIRED field are never NULL.
func (r *rows) ColumnTypeNullable(i int) (bool, bool) {
	return r.cols[i].nullable(), true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). A result that the caller closes before the end needs no cancel, because
// the job is done when the rows start.
func (r *rows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row of a page, it reads the end of the page
// and opens the next one. After the last row of the last page, it reads the end
// of the answer (D21 and D189).
func (r *rows) NextRow() error {
	for {
		if r.done {
			return io.EOF
		}
		if r.inRows {
			if r.dec.PeekKind() != ']' {
				if err := r.readRow(); err != nil {
					return r.fail(err)
				}
				r.count++
				r.read = true
				return nil
			}
			if err := r.endRows(); err != nil {
				return r.fail(err)
			}
		}
		if err := r.nextPage(); err != nil {
			return r.fail(err)
		}
	}
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	copy(dest, r.cur)
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.cur[i])
}

// start opens the first page. A job that is not done answers jobComplete false
// with no schema and no rows, so the driver polls it until it ends, with a wait
// that grows (recorded: bigquery-215 and bigquery-217). If the context ends
// while the job runs, the driver cancels the job (D189).
func (r *rows) start(ctx context.Context, c *Connector, o options) error {
	if err := r.openPage(); err != nil {
		return err
	}
	delay := c.pollMin
	for r.pending() {
		_ = r.s.Close()
		if r.job.id == "" {
			return fmt.Errorf("polling the job: the answer has no job id: %w", dbimp.ErrInvalidValue)
		}
		if err := c.wait(ctx, delay); err != nil {
			c.abandon(ctx, r.job)
			return fmt.Errorf("waiting for the job %s: %w", r.job.id, err)
		}
		res, err := c.results(ctx, r.job, "", o, pollWait)
		if err != nil {
			if ctx.Err() != nil {
				c.abandon(ctx, r.job)
			}
			return err
		}
		r.s = dbimp.NewStream(res.Body)
		r.dec = r.s.Decoder()
		if err := r.openPage(); err != nil {
			if ctx.Err() != nil {
				c.abandon(ctx, r.job)
			}
			return err
		}
		delay = min(delay*2, c.pollMax)
	}
	for !r.inRows && !r.done {
		if err := r.nextPage(); err != nil {
			return err
		}
	}
	return nil
}

// abandon cancels a job whose context ended, and ignores the answer, because
// the caller sees the error of the context.
func (c *Connector) abandon(ctx context.Context, job jobRef) {
	if job.id != "" {
		_ = c.stop(ctx, job)
	}
}

// pending reports whether the answer is that of a job that still runs: it holds
// jobComplete false and no schema. A statement that changes rows can answer
// jobComplete false with its count, when the service deduplicated it by its
// requestId (recorded: bigquery-381), and that answer is the end.
func (r *rows) pending() bool {
	return !r.inRows && r.completeSeen && !r.complete && !r.schemaSeen && !r.affectedKnown && r.token == ""
}

// result returns the result of a statement that ran with Exec.
func (r *rows) result() result {
	return result{affected: r.affected, known: r.affectedKnown}
}

// openPage reads an answer up to the opening of rows, and leaves the decoder
// inside the array of the rows. An answer with no rows member is read to its
// end, and it leaves inRows false.
func (r *rows) openPage() error {
	r.token = ""
	r.inRows = false
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		name := tok.String()
		if name != "rows" {
			if err := r.readMember(name); err != nil {
				return err
			}
			continue
		}
		if !r.schemaSeen {
			return fmt.Errorf("reading the answer: rows come before the schema: %w", dbimp.ErrInvalidValue)
		}
		if r.dec.PeekKind() == 'n' {
			if _, err := r.dec.ReadToken(); err != nil {
				return fmt.Errorf("reading the rows: %w", err)
			}
			continue
		}
		if err := expect(r.dec, '['); err != nil {
			return fmt.Errorf("reading the rows: %w", err)
		}
		r.inRows = true
		return nil
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	return r.s.End()
}

// endRows reads the end of the rows of a page, and the members that follow them.
func (r *rows) endRows() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the rows: %w", err)
	}
	r.inRows = false
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
		if err := r.readMember(tok.String()); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	return r.s.End()
}

// nextPage runs when a page ended. It closes the body, which is at its end, so
// that the connection goes back to the pool (D36). If the page had a token, it
// fetches the next page. Otherwise it makes sure that the result held the rows
// that totalRows named, and ends the result.
func (r *rows) nextPage() error {
	if err := r.s.Close(); err != nil {
		return fmt.Errorf("closing the page %d: %w", r.page, err)
	}
	if r.token == "" {
		switch {
		case r.totalKnown && r.count < r.total:
			return fmt.Errorf("reading the result: %d rows of %d: %w", r.count, r.total, ErrCut)
		case r.totalKnown && r.count > r.total:
			return fmt.Errorf("reading the result: %d rows of %d: %w", r.count, r.total, dbimp.ErrInvalidValue)
		}
		r.done = true
		return nil
	}
	if r.job.id == "" {
		return fmt.Errorf("fetching the page %d: the driver has no id of the job: %w", r.page+1, dbimp.ErrInvalidValue)
	}
	prev := r.token
	res, err := r.fetch(r.job, prev, 0)
	if err != nil {
		return fmt.Errorf("fetching the page %d: %w", r.page+1, err)
	}
	r.s = dbimp.NewStream(res.Body)
	r.dec = r.s.Decoder()
	r.page++
	if err := r.openPage(); err != nil {
		return fmt.Errorf("reading the page %d: %w", r.page, err)
	}
	if r.token == prev {
		return fmt.Errorf("reading the page %d: the service answered the same page token: %w", r.page, dbimp.ErrInvalidValue)
	}
	return nil
}

// readMember reads the value of the member name of an answer, and keeps what the
// driver needs: the schema, the job, the count of rows, the page token, whether
// the job is complete, and the count of rows that a statement changed. It skips
// every other member.
func (r *rows) readMember(name string) error {
	switch name {
	case "schema":
		var sc schema
		if err := json.UnmarshalDecode(r.dec, &sc); err != nil {
			return fmt.Errorf("reading the schema: %w", err)
		}
		return r.setSchema(sc)
	case "jobReference":
		var ref struct {
			ID       string `json:"jobId"`
			Location string `json:"location"`
		}
		if err := json.UnmarshalDecode(r.dec, &ref); err != nil {
			return fmt.Errorf("reading the job reference: %w", err)
		}
		if r.job.id == "" {
			r.job.id = ref.ID
		}
		if ref.Location != "" {
			r.job.location = ref.Location
		}
	case "totalRows":
		n, err := r.readCount(name)
		if err != nil {
			return err
		}
		r.total, r.totalKnown = n, true
	case "numDmlAffectedRows":
		n, err := r.readCount(name)
		if err != nil {
			return err
		}
		r.affected, r.affectedKnown = n, true
	case "pageToken":
		var tok string
		if err := json.UnmarshalDecode(r.dec, &tok); err != nil {
			return fmt.Errorf("reading the page token: %w", err)
		}
		r.token = tok
	case "jobComplete":
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading jobComplete: %w", err)
		}
		b, err := dbimp.Bool(v)
		if err != nil {
			return fmt.Errorf("reading jobComplete: %w", err)
		}
		r.complete, r.completeSeen = b, true
	default:
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the member %q: %w", name, err)
		}
	}
	return nil
}

// readCount reads a count that the service writes as a string, such as
// "3000".
func (r *rows) readCount(name string) (int64, error) {
	v, err := r.dec.ReadValue()
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", name, err)
	}
	s, err := text(v)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", name, err)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("reading %s: %q is not a count: %w", name, s, dbimp.ErrInvalidValue)
	}
	return n, nil
}

// setSchema keeps the columns of the first schema. A later page repeats the
// schema, and the driver reads it again and keeps the first.
func (r *rows) setSchema(sc schema) error {
	if r.schemaSeen {
		return nil
	}
	cols, err := newColumns(sc.Fields, "")
	if err != nil {
		return err
	}
	r.cols = cols
	r.names = make([]string, len(cols))
	for i, c := range cols {
		r.names[i] = c.name
	}
	r.cur = make([]driver.Value, len(cols))
	r.schemaSeen = true
	return nil
}

// readRow reads and decodes one row, which is {"f":[{"v":...},...]} in the order
// of the columns (recorded: bigquery-017).
func (r *rows) readRow() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	// The row is decoded before the next call to the decoder, which reuses its
	// buffer.
	var row struct {
		F []cell `json:"f"`
	}
	if err := json.Unmarshal(v, &row); err != nil {
		return fmt.Errorf("reading a row: %w", dbimp.ErrInvalidValue)
	}
	if len(row.F) != len(r.cols) {
		return fmt.Errorf("reading a row: %d values for %d columns: %w", len(row.F), len(r.cols), dbimp.ErrColumnCount)
	}
	for i, c := range row.F {
		if r.cur[i], err = decode(r.cols[i], c.V); err != nil {
			return err
		}
	}
	return nil
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107), which holds for every error in a page after the
// first.
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before the
	// read sees the end, and the read then fails with "use of closed network
	// connection". The caller must see that its context ended (D36).
	if cerr := r.cause(); cerr != nil && !errors.Is(err, cerr) {
		err = fmt.Errorf("%w: %w", cerr, err)
	}
	if r.read || r.page > 0 {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// expect reads one token, and returns an error if it is not the delimiter kind.
func expect(dec *jsontext.Decoder, kind jsontext.Kind) error {
	tok, err := dec.ReadToken()
	switch {
	case err != nil:
		return err
	case tok.Kind() != kind:
		return fmt.Errorf("%v where %v was expected: %w", tok.Kind(), kind, dbimp.ErrInvalidValue)
	}
	return nil
}
