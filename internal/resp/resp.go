package resp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Reader struct {
	r *bufio.Reader
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, 64*1024)}
}

func (r *Reader) ReadCommand() ([]string, error) {
	b, err := r.r.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != '*' {
		line, err := r.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		return strings.Fields(strings.TrimSpace(string(append([]byte{b}, line...)))), nil
	}

	line, err := readLine(r.r)
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("invalid array length")
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		prefix, err := r.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if prefix != '$' {
			return nil, fmt.Errorf("expected bulk string")
		}
		line, err := readLine(r.r)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid bulk string length")
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r.r, buf); err != nil {
			return nil, err
		}
		if buf[size] != '\r' || buf[size+1] != '\n' {
			return nil, fmt.Errorf("invalid bulk string terminator")
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return "", err
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return string(line), nil
}

type Writer struct {
	w *bufio.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: bufio.NewWriterSize(w, 64*1024)}
}

func (w *Writer) Flush() error {
	return w.w.Flush()
}

func (w *Writer) SimpleString(s string) error {
	_, err := fmt.Fprintf(w.w, "+%s\r\n", s)
	return err
}

func (w *Writer) Error(s string) error {
	_, err := fmt.Fprintf(w.w, "-%s\r\n", s)
	return err
}

func (w *Writer) Integer(v int64) error {
	_, err := fmt.Fprintf(w.w, ":%d\r\n", v)
	return err
}

func (w *Writer) BulkString(s string) error {
	_, err := fmt.Fprintf(w.w, "$%d\r\n%s\r\n", len(s), s)
	return err
}

func (w *Writer) BulkBytes(b []byte) error {
	if _, err := fmt.Fprintf(w.w, "$%d\r\n", len(b)); err != nil {
		return err
	}
	if _, err := w.w.Write(b); err != nil {
		return err
	}
	_, err := w.w.WriteString("\r\n")
	return err
}

func (w *Writer) Nil() error {
	_, err := w.w.WriteString("$-1\r\n")
	return err
}

func (w *Writer) ArrayLen(n int) error {
	_, err := fmt.Fprintf(w.w, "*%d\r\n", n)
	return err
}
