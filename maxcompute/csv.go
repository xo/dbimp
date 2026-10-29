package maxcompute

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/xo/dbimp"
)

// nullText is the text of the cell of a NULL, which the server writes in quotes
// (recorded: "type tinyint: low, high and NULL").
const nullText = `\N`

// stringNull is the cell that the server writes for a string of a backslash and
// an N, so that it differs from a NULL (recorded: "lead: a string of a backslash
// and N, which the models say is quoted as a string").
const stringNull = `""\N""`

// cell is one cell of the CSV.
type cell struct {
	// text is the text of the cell, with its quotes removed and a doubled
	// quote written once.
	text string
	// null is true for the cell of a NULL.
	null bool
	// quoted is true for a cell that the server wrote in quotes.
	quoted bool
}

// csvReader reads the cells of the CSV of a result. The server writes a record
// as cells that are separated by commas and end with a carriage return and a line
// feed. A cell of a string has quotes, with a quote written twice inside it, and
// a number, a boolean and a DATETIME has none (recorded). The standard reader of
// Go does not tell a quoted cell from another, and it cannot read the cell of a
// string of a backslash and an N, so the driver has a reader of its own.
type csvReader struct {
	br *bufio.Reader
}

// newCSVReader returns a reader of the CSV in r.
func newCSVReader(r io.Reader) *csvReader {
	return &csvReader{br: bufio.NewReaderSize(r, 64<<10)}
}

// record reads one record. It returns io.EOF when the CSV has no more
// records. A CSV that is cut inside a record is io.ErrUnexpectedEOF.
func (r *csvReader) record() ([]cell, error) {
	if _, err := r.br.Peek(1); err != nil {
		return nil, err //nolint:wrapcheck // The error is io.EOF, or the error of the stream.
	}
	var cells []cell
	for {
		c, end, err := r.cell()
		if err != nil {
			return nil, err
		}
		cells = append(cells, c)
		if end {
			return cells, nil
		}
	}
}

// cell reads one cell, and reports whether it ended its record.
func (r *csvReader) cell() (cell, bool, error) {
	first, err := r.br.Peek(1)
	if err != nil {
		return cell{}, false, fmt.Errorf("reading a cell: %w", unexpected(err))
	}
	if first[0] != '"' {
		return r.bare()
	}
	if head, _ := r.br.Peek(len(stringNull) + 1); len(head) >= len(stringNull) && string(head[:len(stringNull)]) == stringNull &&
		(len(head) == len(stringNull) || head[len(stringNull)] == ',' || head[len(stringNull)] == '\r' || head[len(stringNull)] == '\n') {
		_, _ = r.br.Discard(len(stringNull))
		end, err := r.delimiter()
		return cell{text: nullText, quoted: true}, end, err
	}
	_, _ = r.br.Discard(1)
	var text []byte
	for {
		b, err := r.br.ReadByte()
		if err != nil {
			return cell{}, false, fmt.Errorf("reading a quoted cell: %w", unexpected(err))
		}
		if b != '"' {
			text = append(text, b)
			continue
		}
		next, err := r.br.Peek(1)
		if err == nil && next[0] == '"' {
			_, _ = r.br.Discard(1)
			text = append(text, '"')
			continue
		}
		break
	}
	end, err := r.delimiter()
	return cell{text: string(text), null: string(text) == nullText, quoted: true}, end, err
}

// bare reads a cell that has no quotes, up to a comma or the end of the record.
func (r *csvReader) bare() (cell, bool, error) {
	var text []byte
	for {
		b, err := r.br.ReadByte()
		switch {
		case errors.Is(err, io.EOF):
			return cell{text: string(text)}, true, nil
		case err != nil:
			return cell{}, false, fmt.Errorf("reading a cell: %w", err)
		case b == ',':
			return cell{text: string(text)}, false, nil
		case b == '\r':
			if next, err := r.br.Peek(1); err == nil && next[0] == '\n' {
				_, _ = r.br.Discard(1)
			}
			return cell{text: string(text)}, true, nil
		case b == '\n':
			return cell{text: string(text)}, true, nil
		}
		text = append(text, b)
	}
}

// delimiter reads the byte that follows a quoted cell, and reports whether it
// ended its record.
func (r *csvReader) delimiter() (bool, error) {
	b, err := r.br.ReadByte()
	switch {
	case errors.Is(err, io.EOF):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("reading the end of a cell: %w", err)
	case b == ',':
		return false, nil
	case b == '\r':
		if next, err := r.br.Peek(1); err == nil && next[0] == '\n' {
			_, _ = r.br.Discard(1)
		}
		return true, nil
	case b == '\n':
		return true, nil
	}
	return false, fmt.Errorf("reading the end of a cell: the byte %q follows the quote: %w", b, dbimp.ErrInvalidValue)
}
