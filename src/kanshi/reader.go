// Reads kanshi responses (../kanshi/docs/protocol.md, format 1.x): an
// export (header, samples, end) or a version line, plain or gzip, as
// printed on stdout by `kanshi ...` over SSH. Everything in a response is
// treated as hostile and checked before it is handed on.

package kanshi

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
)

const (
	// SupportedMajor is the protocol major version this reader knows.
	SupportedMajor = 1

	// MaxLineBytes caps one response line. A sample with 64 containers
	// and 32 errors is far below it.
	MaxLineBytes = 1 << 20
)

var (
	// ErrNotKanshiOutput means the output does not start with a kanshi
	// line: usually the login's shell startup files printed something.
	ErrNotKanshiOutput = errors.New("output does not start with a kanshi line")

	// ErrNoOutput means the command printed nothing on stdout (kanshi
	// missing or failing before its first line).
	ErrNoOutput = errors.New("no output")

	// ErrTruncated means the response ended before its end line; it must
	// be discarded.
	ErrTruncated = errors.New("response was cut off before its end line")

	// ErrUnsupportedFormat means the server speaks a protocol major
	// version this reader does not know.
	ErrUnsupportedFormat = errors.New("unsupported kanshi protocol version")

	// ErrProtocol means a line broke the protocol.
	ErrProtocol = errors.New("kanshi protocol error")
)

var (
	formatPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)
	storePattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Header is the first line of an export response.
type Header struct {
	Kanshi string `json:"kanshi"`
	Format string `json:"format"`
	// Store identifies the server's store; a new value means the store was
	// recreated and sequence numbers restarted.
	Store string `json:"store"`
}

// End is the last line of an export response.
type End struct {
	// Next is the value to pass as --since on the next request.
	Next uint64 `json:"next"`
	// More is true when --limit stopped the response early.
	More bool `json:"more"`
	// LostThrough, if not 0, is the highest seq above --since that was
	// deleted by the server's retention before it could be sent.
	LostThrough uint64 `json:"lost_through"`
}

// Version is the answer to `kanshi version`.
type Version struct {
	Kanshi string `json:"kanshi"`
	Format string `json:"format"`
}

// ServerError is an error line from kanshi.
type ServerError struct {
	// Code is bad_request, no_store or internal; an unknown code must be
	// treated like internal.
	Code string `json:"code"`
	// Message is for humans and may come from a hostile server.
	Message string `json:"message"`
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("kanshi error %s: %s", Printable(e.Code), Printable(e.Message))
}

// ReadExport reads the response to `kanshi export --since since [--limit
// limit]` (limit 0: none) from r and calls onSample for each sample, in
// order, with the decoded sample and its line (without the newline; the
// slice is only valid during the call). It returns the header and end
// lines. An error from onSample stops reading and is returned. Any other
// error means the whole response must be discarded, including samples
// already passed to onSample: ErrNoOutput, ErrNotKanshiOutput,
// ErrTruncated, ErrUnsupportedFormat or ErrProtocol (wrapped), a
// *ServerError, or a read error.
func ReadExport(r io.Reader, since, limit uint64, onSample func(Sample, []byte) error) (Header, End, error) {
	lines, err := newLineReader(r)
	if err != nil {
		return Header{}, End{}, err
	}

	line, err := lines.next()
	if err != nil {
		return Header{}, End{}, err
	}
	var header Header
	switch firstLineType(line) {
	case "header":
		if err := decode(line, &header); err != nil {
			return Header{}, End{}, err
		}
		if err := checkFormat(header.Format); err != nil {
			return Header{}, End{}, err
		}
		if !storePattern.MatchString(header.Store) {
			return Header{}, End{}, fmt.Errorf("%w: header store %q is not 32 hex digits", ErrProtocol, Printable(header.Store))
		}
		if err := checkText("header kanshi", header.Kanshi, maxText); err != nil {
			return Header{}, End{}, err
		}
	case "error":
		return Header{}, End{}, serverError(lines, line)
	default:
		return Header{}, End{}, ErrNotKanshiOutput
	}

	last := since
	var count uint64
	for {
		line, err := lines.next()
		if err != nil {
			return header, End{}, err
		}
		typ, err := lineType(line)
		if err != nil {
			return header, End{}, err
		}
		switch typ {
		case "sample":
			var s Sample
			if err := decode(line, &s); err != nil {
				return header, End{}, err
			}
			if s.Seq <= last {
				return header, End{}, fmt.Errorf("%w: sample seq %d after %d", ErrProtocol, s.Seq, last)
			}
			if err := checkSample(&s); err != nil {
				return header, End{}, err
			}
			count++
			if limit > 0 && count > limit {
				return header, End{}, fmt.Errorf("%w: more than --limit %d samples", ErrProtocol, limit)
			}
			last = s.Seq
			if err := onSample(s, line); err != nil {
				return header, End{}, err
			}
		case "end":
			var end End
			if err := decode(line, &end); err != nil {
				return header, End{}, err
			}
			if end.Next != last {
				return header, End{}, fmt.Errorf("%w: end next %d, want %d", ErrProtocol, end.Next, last)
			}
			if end.More && (limit == 0 || count != limit) {
				return header, End{}, fmt.Errorf("%w: end says more after %d samples with --limit %d", ErrProtocol, count, limit)
			}
			if err := lines.mustEnd(); err != nil {
				return header, End{}, err
			}
			return header, end, nil
		default:
			return header, End{}, fmt.Errorf("%w: unexpected %q line", ErrProtocol, Printable(typ))
		}
	}
}

// ReadVersion reads the response to `kanshi version` from r. Errors are
// as for ReadExport.
func ReadVersion(r io.Reader) (Version, error) {
	lines, err := newLineReader(r)
	if err != nil {
		return Version{}, err
	}
	line, err := lines.next()
	if err != nil {
		return Version{}, err
	}
	switch firstLineType(line) {
	case "version":
		var v Version
		if err := decode(line, &v); err != nil {
			return Version{}, err
		}
		if err := checkFormat(v.Format); err != nil {
			return Version{}, err
		}
		if err := checkText("version kanshi", v.Kanshi, maxText); err != nil {
			return Version{}, err
		}
		if err := lines.mustEnd(); err != nil {
			return Version{}, err
		}
		return v, nil
	case "error":
		return Version{}, serverError(lines, line)
	default:
		return Version{}, ErrNotKanshiOutput
	}
}

// checkFormat accepts "<major>.<minor>" with the supported major.
func checkFormat(format string) error {
	m := formatPattern.FindStringSubmatch(format)
	if m == nil {
		return fmt.Errorf("%w: format %q", ErrProtocol, Printable(format))
	}
	major, err := strconv.Atoi(m[1])
	if err != nil || major != SupportedMajor {
		return fmt.Errorf("%w: format %s, this mitosu reads %d.x", ErrUnsupportedFormat, format, SupportedMajor)
	}
	return nil
}

// serverError decodes an error line, which must be the only line.
func serverError(lines *lineReader, line []byte) error {
	var e ServerError
	if err := decode(line, &e); err != nil {
		return err
	}
	if err := checkText("error message", e.Message, maxMessage); err != nil {
		return err
	}
	if err := lines.mustEnd(); err != nil {
		return err
	}
	return &e
}

// lineType returns the "type" field of a line, after the strict checks.
func lineType(line []byte) (string, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := decode(line, &envelope); err != nil {
		return "", err
	}
	return envelope.Type, nil
}

// firstLineType is lineType for the first line, where anything that is
// not a kanshi line gives "" (the caller reports ErrNotKanshiOutput).
func firstLineType(line []byte) string {
	typ, err := lineType(line)
	if err != nil {
		return ""
	}
	return typ
}

// decode checks line strictly (checkLineBytes, scanJSON, every required
// field of v present) and decodes it into v, a pointer to a struct.
func decode(line []byte, v any) error {
	if err := checkLineBytes(line); err != nil {
		return err
	}
	node, err := scanJSON(line)
	if err != nil {
		return err
	}
	if err := checkRequired(node, reflect.TypeOf(v).Elem(), ""); err != nil {
		return err
	}
	if err := json.Unmarshal(line, v); err != nil {
		return fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	return nil
}

// lineReader splits a response into lines of at most MaxLineBytes.
type lineReader struct {
	br *bufio.Reader
	// raw is the compressed stream under a gzip response, nil otherwise.
	raw *bufio.Reader
	buf []byte
}

// newLineReader looks at the first bytes of r: a gzip stream is
// decompressed, a '{' starts plain JSON lines, anything else (or nothing)
// is ErrNotKanshiOutput.
func newLineReader(r io.Reader) (*lineReader, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	head, err := br.Peek(2)
	if len(head) == 0 {
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		return nil, ErrNoOutput
	}
	if head[0] == '{' {
		return &lineReader{br: br}, nil
	}
	if len(head) == 2 && head[0] == 0x1f && head[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("%w: gzip: %v", ErrProtocol, err)
		}
		gz.Multistream(false)
		return &lineReader{br: bufio.NewReaderSize(gz, 64*1024), raw: br}, nil
	}
	return nil, ErrNotKanshiOutput
}

// next returns the next line without its newline. A last line without a
// newline, or no line at all, is ErrTruncated.
func (l *lineReader) next() ([]byte, error) {
	l.buf = l.buf[:0]
	for {
		chunk, err := l.br.ReadSlice('\n')
		if len(l.buf)+len(chunk) > MaxLineBytes+1 {
			return nil, fmt.Errorf("%w: line longer than %d bytes", ErrProtocol, MaxLineBytes)
		}
		l.buf = append(l.buf, chunk...)
		if err == nil {
			return l.buf[:len(l.buf)-1], nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrTruncated
		}
		return nil, err
	}
}

// mustEnd checks that nothing at all follows the last line: not in the
// lines, and for gzip not after the compressed stream either. Reading to
// the end of a gzip stream also verifies its checksum.
func (l *lineReader) mustEnd() error {
	if err := atEOF(l.br); err != nil {
		return err
	}
	if l.raw != nil {
		return atEOF(l.raw)
	}
	return nil
}

// atEOF returns nil if r has no more bytes.
func atEOF(r *bufio.Reader) error {
	_, err := r.ReadByte()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	return fmt.Errorf("%w: data after the last line", ErrProtocol)
}

// Printable returns s with every byte outside printable ASCII replaced by
// '?', cut to 200 bytes, for showing server text in a terminal or log
// without letting it send control sequences.
func Printable(s string) string {
	b := []byte(s)
	if len(b) > 200 {
		b = append(b[:197], "..."...)
	}
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '?'
		}
	}
	return string(b)
}
