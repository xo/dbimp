package maxcompute

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"
)

// maxTag is the most that the driver reads for one tag of an answer. A tag of
// the answers that the driver reads is short (recorded).
const maxTag = 4 << 10

// maxText is the most that the driver reads for a text that is not the rows of
// a result: the message of a task, a result of the form text, and the
// properties of a project.
const maxText = 16 << 20

// cdataStart and cdataEnd open and close a section of character data.
const (
	cdataStart = "<![CDATA["
	cdataEnd   = "]]>"
)

// answer is the head of an XML answer that holds a task. The driver reads it
// up to the content of the element Result, so the rows stay in the body.
type answer struct {
	// status is the state of the task, such as Success. It is the last
	// element Status that the head holds, because an answer to a poll has the
	// state of the instance before the state of the task (recorded). It is
	// empty when the head has none.
	status string
	// format is the attribute Format of the element Result, which is csv or
	// text, and transform is its attribute Transform, which is Base64 for a
	// result that holds a special character. Both are empty when the answer
	// has no result.
	format    string
	transform string
	// content is the content of the element Result, as text. It is nil when
	// the answer has no result. It is not decoded: a Transform of Base64 is
	// still on it.
	content io.Reader
}

// scanner reads the tags of an XML answer one at a time. The answers of
// MaxCompute are small and flat, and a result is character data in one
// section, which the scanner hands over as a stream (D25), so the driver reads
// no whole answer into memory and needs no parser of its own for XML.
type scanner struct {
	br *bufio.Reader
}

// newScanner returns a scanner over r.
func newScanner(r io.Reader) *scanner {
	return &scanner{br: bufio.NewReaderSize(r, 64<<10)}
}

// readAnswer reads the head of the answer in r.
func readAnswer(r io.Reader) (*answer, error) {
	s := newScanner(r)
	a := &answer{}
	for {
		name, attrs, selfClosing, err := s.tag()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return a, nil
			}
			return nil, err
		}
		switch name {
		case "Status":
			if selfClosing {
				continue
			}
			text, err := s.text()
			if err != nil {
				return nil, err
			}
			a.status = strings.TrimSpace(text)
		case "Result":
			a.format, a.transform = attrValue(attrs, "Format"), attrValue(attrs, "Transform")
			if selfClosing {
				a.content = strings.NewReader("")
				return a, nil
			}
			a.content, err = s.content()
			if err != nil {
				return nil, err
			}
			return a, nil
		}
	}
}

// tag reads up to the next tag of an element, and returns its name, its
// attributes as text, and whether it ends with a slash. It skips the
// declaration, the comments, a closing tag and the character data between tags.
// It returns io.EOF at the end of the answer.
func (s *scanner) tag() (string, string, bool, error) {
	for {
		if _, err := s.br.ReadString('<'); err != nil {
			return "", "", false, fmt.Errorf("reading the answer: %w", err)
		}
		inner, err := s.br.ReadString('>')
		if err != nil {
			return "", "", false, fmt.Errorf("reading a tag: %w", unexpected(err))
		}
		if len(inner) > maxTag {
			return "", "", false, fmt.Errorf("reading a tag: it is longer than %d bytes: %w", maxTag, ErrCut)
		}
		inner = inner[:len(inner)-1]
		switch {
		case inner == "", inner[0] == '?', inner[0] == '/', strings.HasPrefix(inner, "!--"):
			continue
		case strings.HasPrefix(inner, "![CDATA["):
			// A section of character data outside the element Result is
			// not one that the driver reads.
			continue
		}
		selfClosing := strings.HasSuffix(inner, "/")
		inner = strings.TrimSuffix(inner, "/")
		name, attrs, _ := strings.Cut(inner, " ")
		return name, attrs, selfClosing, nil
	}
}

// text reads the character data up to the next tag, and returns it with its
// entities decoded.
func (s *scanner) text() (string, error) {
	t, err := s.br.ReadString('<')
	if err != nil {
		return "", fmt.Errorf("reading the text of an element: %w", unexpected(err))
	}
	if err := s.br.UnreadByte(); err != nil {
		return "", fmt.Errorf("reading the text of an element: %w", err)
	}
	return html.UnescapeString(strings.TrimSuffix(t, "<")), nil
}

// content returns the content of the element Result that has just opened. A
// section of character data is a stream that ends at its end marker, and any
// other content is a short text.
func (s *scanner) content() (io.Reader, error) {
	head, err := s.br.Peek(len(cdataStart))
	if err == nil && string(head) == cdataStart {
		_, _ = s.br.Discard(len(cdataStart))
		return &cdata{br: s.br}, nil
	}
	text, err := s.text()
	if err != nil {
		return nil, err
	}
	return strings.NewReader(text), nil
}

// attrValue returns the value of the attribute name in the text of the
// attributes of a tag, such as Format="csv", and "" if there is none.
func attrValue(attrs, name string) string {
	_, rest, ok := strings.Cut(" "+attrs, " "+name+"=\"")
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, "\"")
	return html.UnescapeString(v)
}

// unexpected returns io.ErrUnexpectedEOF for an io.EOF that comes inside an
// element.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// cdata reads the character data of a section up to its end marker, and returns
// io.EOF there. It holds back the closing brackets that can start the marker.
// A body that ends inside the section is io.ErrUnexpectedEOF, so a result that
// the server cut is never a result that ended.
type cdata struct {
	br   *bufio.Reader
	out  []byte
	held int
	done bool
	err  error
}

// Read satisfies io.Reader.
func (c *cdata) Read(p []byte) (int, error) {
	for len(c.out) == 0 {
		switch {
		case c.err != nil:
			return 0, c.err
		case c.done:
			return 0, io.EOF
		}
		c.fill()
	}
	n := copy(p, c.out)
	c.out = c.out[n:]
	return n, nil
}

// fill reads one buffer of bytes into out.
func (c *cdata) fill() {
	buf := make([]byte, 0, 4<<10)
	for len(buf) < cap(buf)-3 {
		// Return what the reader holds already, and never wait for more while
		// there is something to return: the server can pause between two rows.
		if len(buf) > 0 && c.br.Buffered() == 0 {
			break
		}
		b, err := c.br.ReadByte()
		if err != nil {
			c.err = unexpected(err)
			break
		}
		switch {
		case b == ']':
			if c.held == 2 {
				buf = append(buf, ']')
			} else {
				c.held++
			}
		case b == '>' && c.held == 2:
			c.held, c.done = 0, true
		default:
			for ; c.held > 0; c.held-- {
				buf = append(buf, ']')
			}
			buf = append(buf, b)
		}
		if c.done {
			break
		}
	}
	c.out = buf
}

// decoded returns the content of the answer as a stream of the text of the
// result: a Transform of Base64 is decoded, and the line breaks that the server
// puts into the base64 text are dropped.
func (a *answer) decoded() io.Reader {
	if a.content == nil {
		return strings.NewReader("")
	}
	if strings.EqualFold(a.transform, "Base64") {
		return base64.NewDecoder(base64.StdEncoding, &skipSpace{r: a.content})
	}
	return a.content
}

// skipSpace drops the spaces and the line breaks of a stream.
type skipSpace struct {
	r io.Reader
}

// Read satisfies io.Reader.
func (s *skipSpace) Read(p []byte) (int, error) {
	for {
		n, err := s.r.Read(p)
		kept := p[:0]
		for _, b := range p[:n] {
			if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
				kept = append(kept, b)
			}
		}
		if len(kept) > 0 || err != nil {
			return len(kept), err //nolint:wrapcheck // The error is the one of the stream.
		}
	}
}

// readText reads the whole text of the result of a task, up to maxText bytes.
// A result of the form text, and the message of a task that failed, is small.
func (a *answer) readText() (string, error) {
	var b strings.Builder
	n, err := io.Copy(&b, io.LimitReader(a.decoded(), maxText+1))
	if err != nil {
		return "", fmt.Errorf("reading the text of a result: %w", err)
	}
	if n > maxText {
		return "", fmt.Errorf("reading the text of a result: it is longer than %d bytes: %w", maxText, ErrCut)
	}
	return b.String(), nil
}

// property reads the value of the property name from the XML of a project, and
// returns "" if the project has none.
func property(r io.Reader, name string) (string, error) {
	dec := xml.NewDecoder(io.LimitReader(r, maxText))
	var current string
	var inName, inValue bool
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("reading the properties of the project: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			inName, inValue = t.Name.Local == "Name", t.Name.Local == "Value"
		case xml.EndElement:
			inName, inValue = false, false
		case xml.CharData:
			switch {
			case inName:
				current = string(bytes.TrimSpace(t))
			case inValue && current == name:
				return string(bytes.TrimSpace(t)), nil
			}
		}
	}
}
