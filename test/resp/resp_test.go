package resp_test

import (
	"bytes"
	"testing"

	"xrockscache/internal/resp"
)

func TestReadCommandArray(t *testing.T) {
	r := resp.NewReader(bytes.NewBufferString("*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n"))
	args, err := r.ReadCommand()
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[0] != "SET" || args[1] != "k" || args[2] != "v" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestWriter(t *testing.T) {
	var buf bytes.Buffer
	w := resp.NewWriter(&buf)
	if err := w.SimpleString("OK"); err != nil {
		t.Fatal(err)
	}
	if err := w.Integer(3); err != nil {
		t.Fatal(err)
	}
	if err := w.Nil(); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	want := "+OK\r\n:3\r\n$-1\r\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}
