package tail

import (
	"fmt"
	"strings"
	"testing"
)

func TestKeepsTheLastLines(t *testing.T) {
	b := New(3, 1<<10)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(b, "line %d\n", i)
	}
	if got, want := b.String(), "line 3\nline 4\nline 5\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestKeepsAnUnfinishedLastLine(t *testing.T) {
	b := New(2, 1<<10)
	b.Write([]byte("one\ntw"))
	b.Write([]byte("o\nthree"))
	if got, want := b.String(), "one\ntwo\nthree"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCapsTheSizeAtAWholeLine(t *testing.T) {
	b := New(100, 20)
	for i := 0; i < 10; i++ {
		b.Write([]byte("0123456789\n"))
	}
	got := b.String()
	if len(got) > 20 || !strings.HasPrefix(got, "0123456789") {
		t.Fatalf("got %q", got)
	}
}

func TestZeroLinesKeepsNothing(t *testing.T) {
	b := New(0, 100)
	b.Write([]byte("secret output\n"))
	if b.String() != "" {
		t.Fatal("kept output with maxLines 0")
	}
}
