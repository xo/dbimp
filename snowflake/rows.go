package snowflake

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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// metadata is resultSetMetaData, the first member of an answer (measured).
// It comes before data, so the driver knows the columns before the first
// row, and it holds the count of the rows of each partition.
type metadata struct {
	NumRows    int64  `json:"numRows"`
	Format     string `json:"format"`
	Partitions []struct {
		RowCount int64 `json:"rowCount"`
	} `json:"partitionInfo"`
	RowType []struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		Precision *int64 `json:"precision"`
		Scale     *int64 `json:"scale"`
		Length    *int64 `json:"length"`
		Nullable  bool   `json:"nullable"`
	} `json:"rowType"`
}

// stats is the member stats of the answer of a statement that changes rows
// (measured).
type stats struct {
	Inserted int64 `json:"numRowsInserted"`
	Updated  int64 `json:"numRowsUpdated"`
	Deleted  int64 `json:"numRowsDeleted"`
}

// wireFormat is the format of every result that the driver reads (measured).
const wireFormat = "jsonv2"

// rows reads the result of one statement, one token at a time (D25 and
// D183). The answer is one JSON object. Its first member is the metadata,
// and its member data holds the rows of the first partition, each row an
// array of strings or nulls. The members after data, such as stats, come
// last. Each later partition is the answer of another request, an object
// with data and nothing else, which the driver fetches when the caller has
// read the rows before it, one at a time (D183).
type rows struct {
	s   *dbimp.Stream
	dec *jsontext.Decoder
	w   *watch
	// fetch sends the request for a partition. It holds the context of the
	// statement, so the rows hold no context (rule 4 of AGENTS.md).
	fetch func(handle string, n int) (*http.Response, error)
	loc   *time.Location

	cols  []string
	types []column
	// counts is the count of the rows of each partition, from the metadata.
	counts []int64
	// cur is the current row.
	cur []driver.Value
	// handle is the handle of the statement, which names each later
	// partition.
	handle string
	stats  *stats
	// counted marks the columns that name a count of rows, such as "number of
	// rows updated", and total is the sum of their values in the answer. A
	// statement that changes rows answers with such a row (recorded: "an update
	// and its counts"), and a statement that changes no row has no stats.
	counted []bool
	total   int64
	// part is the partition that the rows read, and count the rows of it
	// that the caller has read.
	part  int
	count int64
	// skip is true when the rows decode no value, for Exec.
	skip bool
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the result is read to
	// its end.
	read bool
	done bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
)

// readAnswer reads the head of an answer with the status 200, up to the
// first row. It reads the end of an answer that holds no rows, and returns its
// error, which comes before any row and so does not wrap dbimp.ErrIncomplete
// (D107). w is the watch of the statement, which the rows end. The rows hold
// ctx in the function that fetches a partition.
func (c *Connector) readAnswer(ctx context.Context, res *http.Response, w *watch, handle string, loc *time.Location, skip bool) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), w: w, handle: handle, loc: loc, skip: skip}
	r.fetch = func(handle string, n int) (*http.Response, error) {
		return c.partition(ctx, handle, n)
	}
	if err := r.readHead(true); err != nil {
		_ = s.Close()
		return nil, err
	}
	if r.dec.PeekKind() == ']' && len(r.counts) <= 1 {
		if err := r.NextRow(); !errors.Is(err, io.EOF) {
			_ = s.Close()
			if err == nil {
				err = fmt.Errorf("reading the answer: a row after the end of the rows: %w", dbimp.ErrInvalidValue)
			}
			return nil, err
		}
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the type of the column in upper case, such as FIXED. The server names
// a geography and a geometry object, so they report OBJECT (measured).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.types[i].databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and
// D183).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.types[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. It is the
// member nullable of the column.
func (r *rows) ColumnTypeNullable(i int) (bool, bool) {
	return r.types[i].nullable, true
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. It is the length of
// a text and of a binary column, in characters and in bytes, when the column
// names one.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	if c := r.types[i]; (c.wire == wireText || c.wire == wireBinary) && c.length >= 0 {
		return c.length, true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. It
// is the precision and the scale of a fixed column.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if c := r.types[i]; c.wire == wireFixed && c.precision >= 0 && c.scale >= 0 {
		return c.precision, c.scale, true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). It ends the watch of the statement. A result that the caller read to
// its last row needs no cancel, and one whose context ended was canceled by the
// watch. Rows that the caller closes before the last row cancel the statement
// on the server too (D183).
func (r *rows) Close() error {
	finished := r.atEnd()
	err := r.s.Close()
	if finished {
		r.w.end()
	} else {
		r.w.abort()
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row of a partition, it reads the end of
// the partition and fetches the next one. After the last row of the last
// partition, it reads the end of the answer (D21 and D183).
func (r *rows) NextRow() error {
	for {
		if r.done {
			return io.EOF
		}
		if r.dec.PeekKind() == ']' {
			if err := r.endPartition(); err != nil {
				return r.fail(err)
			}
			continue
		}
		if err := r.readRow(); err != nil {
			return r.fail(err)
		}
		r.count++
		r.read = true
		return nil
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

// atEnd reports whether the caller read every row: the result is done, or the
// last row of the last partition is read and only the end of the array
// follows. A caller that reads one row of a result of one row, as QueryRow
// does, has read the whole result, and the statement needs no cancel.
func (r *rows) atEnd() bool {
	if r.done {
		return true
	}
	last := r.part+1 >= max(1, len(r.counts))
	return last && r.dec.PeekKind() == ']'
}

// result returns the result of a statement that ran with Exec, whose rows
// the driver read to the end.
func (r *rows) result() result {
	// The row of the answer names the count of the statement. INSERT OVERWRITE
	// has stats of 1 inserted and 3 deleted, and its row says 1 (measured,
	// 2026-10-10).
	if slices.Contains(r.counted, true) {
		return result{affected: r.total, known: true}
	}
	if r.stats == nil {
		return result{}
	}
	return result{affected: r.stats.Inserted + r.stats.Updated + r.stats.Deleted, known: true}
}

// isCountColumn reports whether a column holds a count of the rows that a
// statement changed. The column of the rows that were updated more than once
// ("number of multi-joined rows updated") is not one.
func isCountColumn(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "number of rows ")
}

// readHead reads the answer up to the opening of data, and leaves the
// decoder inside the array of the rows. The first answer holds the metadata.
// A later partition holds data only.
func (r *rows) readHead(first bool) error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		switch name := tok.String(); name {
		case "resultSetMetaData":
			var m metadata
			if err := json.UnmarshalDecode(r.dec, &m); err != nil {
				return fmt.Errorf("reading the metadata: %w", err)
			}
			if first {
				if err := r.setMetadata(&m); err != nil {
					return err
				}
			}
		case "data":
			if r.cols == nil {
				return fmt.Errorf("reading the answer: data comes before the metadata: %w", dbimp.ErrInvalidValue)
			}
			if err := expect(r.dec, '['); err != nil {
				return fmt.Errorf("reading the rows: %w", err)
			}
			r.count = 0
			return nil
		default:
			if err := r.readOther(name); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("reading the answer: it has no data: %w", dbimp.ErrInvalidValue)
}

// setMetadata keeps the columns and the counts of the metadata.
func (r *rows) setMetadata(m *metadata) error {
	if m.Format != "" && m.Format != wireFormat {
		return fmt.Errorf("reading the metadata: the format is %q, and the driver reads %q: %w", m.Format, wireFormat, dbimp.ErrNotSupported)
	}
	r.cols = make([]string, len(m.RowType))
	r.types = make([]column, len(m.RowType))
	r.counted = make([]bool, len(m.RowType))
	for i, rt := range m.RowType {
		r.cols[i] = rt.Name
		r.counted[i] = isCountColumn(rt.Name)
		r.types[i] = column{
			name:      rt.Name,
			wire:      strings.ToLower(rt.Type),
			precision: orNone(rt.Precision),
			scale:     orNone(rt.Scale),
			length:    orNone(rt.Length),
			nullable:  rt.Nullable,
		}
	}
	for _, p := range m.Partitions {
		r.counts = append(r.counts, p.RowCount)
	}
	r.cur = make([]driver.Value, len(r.cols))
	return nil
}

// orNone returns the number that n points at, and -1 for a member that the
// entry leaves null.
func orNone(n *int64) int64 {
	if n == nil {
		return -1
	}
	return *n
}

// readOther reads a member of the answer that is not the metadata or the
// rows: the handle, the count of the rows that a statement changed, and the
// members that the driver does not use.
func (r *rows) readOther(name string) error {
	switch name {
	case "statementHandle":
		var h *string
		if err := json.UnmarshalDecode(r.dec, &h); err != nil {
			return fmt.Errorf("reading the handle: %w", err)
		}
		if h != nil && *h != "" {
			r.handle = *h
			r.w.setHandle(*h)
		}
	case "stats":
		var st *stats
		if err := json.UnmarshalDecode(r.dec, &st); err != nil {
			return fmt.Errorf("reading the stats: %w", err)
		}
		r.stats = st
	default:
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the member %q: %w", name, err)
		}
	}
	return nil
}

// readRow reads and decodes one row.
func (r *rows) readRow() error {
	if err := expect(r.dec, '['); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	i := 0
	for ; r.dec.PeekKind() != ']'; i++ {
		if r.dec.PeekKind() == 0 {
			// The decoder holds an error, such as the end of the body, and
			// ReadToken returns it.
			_, err := r.dec.ReadToken()
			return fmt.Errorf("reading a row: %w", err)
		}
		if i >= len(r.cols) {
			return fmt.Errorf("reading a row: more than %d values: %w", len(r.cols), dbimp.ErrColumnCount)
		}
		if r.skip {
			if r.counted[i] {
				v, err := r.dec.ReadValue()
				if err != nil {
					return fmt.Errorf("reading a row: %w", err)
				}
				var s string
				if json.Unmarshal(v, &s) == nil {
					if n, err := strconv.ParseInt(s, 10, 64); err == nil {
						r.total += n
					}
				}
				continue
			}
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading a row: %w", err)
			}
			continue
		}
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		// The value is decoded before the next call to the decoder, which
		// reuses its buffer.
		if r.cur[i], err = decode(r.types[i], v, r.loc); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i], err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a row: %w", err)
	}
	if i < len(r.cols) {
		return fmt.Errorf("reading a row: %d values for %d columns: %w", i, len(r.cols), dbimp.ErrColumnCount)
	}
	return nil
}

// endPartition reads the end of the rows of the partition and the members
// that follow them, makes sure that the partition held the rows that the
// metadata named, and opens the next partition. After the last one, it ends
// the result and the watch.
func (r *rows) endPartition() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the rows: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
		if err := r.readOther(tok.String()); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	if r.part < len(r.counts) {
		switch want := r.counts[r.part]; {
		case r.count < want:
			return fmt.Errorf("reading the partition %d: %d rows of %d: %w", r.part, r.count, want, ErrCut)
		case r.count > want:
			return fmt.Errorf("reading the partition %d: %d rows of %d: %w", r.part, r.count, want, dbimp.ErrInvalidValue)
		}
	}
	// The body is at its end, so closing it returns the connection to the
	// pool (D36).
	if err := r.s.Close(); err != nil {
		return fmt.Errorf("closing the partition %d: %w", r.part, err)
	}
	if r.part+1 < len(r.counts) {
		return r.open(r.part + 1)
	}
	r.done = true
	r.w.end()
	return nil
}

// open fetches the partition n and reads it up to its rows.
func (r *rows) open(n int) error {
	if r.handle == "" {
		return fmt.Errorf("fetching the partition %d: the driver has no handle of the statement: %w", n, dbimp.ErrInvalidValue)
	}
	res, err := r.fetch(r.handle, n)
	if err != nil {
		return err
	}
	s := dbimp.NewStream(res.Body)
	r.s, r.dec, r.part = s, s.Decoder(), n
	if err := r.readHead(false); err != nil {
		return fmt.Errorf("reading the partition %d: %w", n, err)
	}
	return nil
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row
// reached the caller before err (D107), which holds for every error in a
// partition after the first.
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before
	// the read sees the end, and the read then fails with "use of closed
	// network connection". The caller must see that its context ended (D36).
	if cerr := r.w.cause(); cerr != nil && !errors.Is(err, cerr) {
		err = fmt.Errorf("%w: %w", cerr, err)
	}
	if r.read || r.part > 0 {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// expect reads one token, and returns an error if it is not the delimiter
// kind.
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
