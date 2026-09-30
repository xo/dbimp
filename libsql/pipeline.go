package libsql

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"

	"github.com/xo/dbimp"
)

// counts are the counts of one statement that a pipeline ran.
type counts struct {
	affected int64
	lastID   int64
}

// execute runs stmt on s through /v3/pipeline, and closes s after it when
// closing is true (D149). It reads the answer one token at a time, skips any
// rows, and returns the counts of the statement, or its error.
func (c *Connector) execute(ctx context.Context, s *stream, stmt map[string]any, closing bool) (counts, error) {
	reqs := []any{map[string]any{"type": "execute", "stmt": stmt}}
	if closing {
		reqs = append(reqs, map[string]any{"type": "close"})
	}
	results, err := c.pipeline(ctx, s, reqs)
	if err != nil {
		return counts{}, err
	}
	return results[0].counts, results[0].err
}

// closeStream sends close for s, with ctx without its end and the limit of
// closeTimeout, because a cursor leaves its stream open (D149). A stream
// that expired is closed already, as one does while a slow reader reads a
// large cursor (measured in CI), so STREAM_EXPIRED is no error here.
func (c *Connector) closeStream(ctx context.Context, s *stream) error {
	if s.baton == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()
	_, err := c.pipeline(ctx, s, []any{map[string]any{"type": "close"}})
	if e, ok := errors.AsType[*Error](err); ok && e.Code == CodeStreamExpired {
		return nil
	}
	return err
}

// reply is the result of one request of a pipeline.
type reply struct {
	counts counts
	err    error
}

// pipeline sends reqs on s, and returns the result of each. It takes the
// baton and the base_url of the answer into s.
func (c *Connector) pipeline(ctx context.Context, s *stream, reqs []any) ([]reply, error) {
	body, err := json.Marshal(map[string]any{"baton": s.batonJSON(), "requests": reqs})
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	res, err := c.post(ctx, s, pathPipeline, body)
	if err != nil {
		return nil, err
	}
	st := dbimp.NewStream(res.Body)
	defer st.Close()
	dec := st.Decoder()
	if err := expect(dec, '{'); err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	var (
		baton, base string
		results     []reply
	)
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading the answer: %w", err)
		}
		switch tok.String() {
		case "baton":
			if baton, err = optString(dec); err != nil {
				return nil, fmt.Errorf("reading the baton: %w", err)
			}
		case "base_url":
			if base, err = optString(dec); err != nil {
				return nil, fmt.Errorf("reading the base_url: %w", err)
			}
		case "results":
			if results, err = readResults(dec, res.StatusCode); err != nil {
				return nil, err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return nil, fmt.Errorf("reading the answer: %w", err)
			}
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := st.End(); err != nil {
		return nil, err
	}
	if len(results) != len(reqs) {
		return nil, fmt.Errorf("reading the answer: %d results for %d requests: %w", len(results), len(reqs), dbimp.ErrInvalidValue)
	}
	if err := s.answer(baton, base); err != nil {
		return nil, err
	}
	return results, nil
}

// readResults reads the results of a pipeline. Each is {"type": "ok",
// "response": ...} or {"type": "error", "error": ...} (measured).
func readResults(dec *jsontext.Decoder, status int) ([]reply, error) {
	if err := expect(dec, '['); err != nil {
		return nil, fmt.Errorf("reading the results: %w", err)
	}
	var results []reply
	for dec.PeekKind() != ']' {
		if err := expect(dec, '{'); err != nil {
			return nil, fmt.Errorf("reading a result: %w", err)
		}
		var r reply
		for dec.PeekKind() != '}' {
			tok, err := dec.ReadToken()
			if err != nil {
				return nil, fmt.Errorf("reading a result: %w", err)
			}
			switch tok.String() {
			case "error":
				r.err, err = readError(dec, status)
			case "response":
				r.counts, err = readResponse(dec)
			default:
				err = dec.SkipValue()
			}
			if err != nil {
				return nil, fmt.Errorf("reading a result: %w", err)
			}
		}
		if _, err := dec.ReadToken(); err != nil {
			return nil, fmt.Errorf("reading the end of a result: %w", err)
		}
		results = append(results, r)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading the end of the results: %w", err)
	}
	return results, nil
}

// readResponse reads the response of a request, and the counts of an
// execute, and skips its rows.
func readResponse(dec *jsontext.Decoder) (counts, error) {
	var n counts
	if err := expect(dec, '{'); err != nil {
		return n, err
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return n, err
		}
		if tok.String() != "result" || dec.PeekKind() != '{' {
			if err := dec.SkipValue(); err != nil {
				return n, err
			}
			continue
		}
		if n, err = readCounts(dec); err != nil {
			return n, err
		}
	}
	_, err := dec.ReadToken()
	return n, err
}

// readCounts reads the result of a statement, and returns its counts. It
// skips the rows one value at a time.
func readCounts(dec *jsontext.Decoder) (counts, error) {
	var n counts
	if err := expect(dec, '{'); err != nil {
		return n, err
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return n, err
		}
		switch tok.String() {
		case "affected_row_count":
			v, err := dec.ReadValue()
			if err != nil {
				return n, err
			}
			if n.affected, err = dbimp.Int64(v); err != nil {
				return n, fmt.Errorf("reading affected_row_count: %w", err)
			}
		case "last_insert_rowid":
			s, err := optString(dec)
			if err != nil {
				return n, fmt.Errorf("reading last_insert_rowid: %w", err)
			}
			if s != "" {
				if n.lastID, err = strconv.ParseInt(s, 10, 64); err != nil {
					return n, fmt.Errorf("reading last_insert_rowid %q: %w", s, dbimp.ErrInvalidValue)
				}
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return n, err
			}
		}
	}
	_, err := dec.ReadToken()
	return n, err
}

// readError reads {"message": ..., "code": ...} as an *Error.
func readError(dec *jsontext.Decoder, status int) (error, error) {
	v, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}
	var e struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(v, &e); err != nil {
		return nil, fmt.Errorf("reading an error: %w", err)
	}
	return &Error{HTTPStatus: status, Code: e.Code, Message: e.Message}, nil
}

// optString reads a string or null, and returns "" for null.
func optString(dec *jsontext.Decoder) (string, error) {
	v, err := dec.ReadValue()
	if err != nil {
		return "", err
	}
	if dbimp.IsNull(v) {
		return "", nil
	}
	return dbimp.String(v)
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
