package resp_test

import (
	"strings"
	"testing"
	"xrockscache/internal/resp"
)

func TestRejectOversizedDeclarations(t *testing.T) {
	for _, input := range []string{"*9223372036854775807\r\n", "*1025\r\n", "*1\r\n$9223372036854775807\r\n", "*1\r\n$1048577\r\n", strings.Repeat("a", 65537)} {
		r := resp.NewReader(strings.NewReader(input))
		if _, err := r.ReadCommand(); err == nil {
			t.Fatal("oversized input accepted")
		}
		r.Close()
	}
}

func TestTotalRequestBudget(t *testing.T) {
	bulk := "$1048576\r\n" + strings.Repeat("v", 1048576) + "\r\n"
	r := resp.NewReader(strings.NewReader("*9\r\n" + strings.Repeat(bulk, 9)))
	defer r.Close()
	if _, err := r.ReadCommand(); err == nil {
		t.Fatal("aggregate request accepted")
	}
}

func TestReaderReleasesBudgetBetweenCommands(t *testing.T) {
	bulk := "$1048576\r\n" + strings.Repeat("v", 1048576) + "\r\n"
	r := resp.NewReader(strings.NewReader("*1\r\n" + bulk + "*1\r\n$1\r\na\r\n"))
	if _, err := r.ReadCommand(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadCommand(); err != nil {
		t.Fatalf("budget was not released: %v", err)
	}
}

func TestReaderRejectsMalformedBulkTerminator(t *testing.T) {
	r := resp.NewReader(strings.NewReader("*1\r\n$1\r\naX"))
	if _, err := r.ReadCommand(); err == nil {
		t.Fatal("malformed bulk terminator accepted")
	}
}
