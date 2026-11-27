package app

import (
	"testing"
)

func TestRingKeepsTailAndReportsTruncation(t *testing.T) {
	r := newRing(8)
	r.write([]byte("abcdef"))
	r.write([]byte("ghij"))
	if got := string(r.buf); got != "cdefghij" {
		t.Fatalf("buf = %q", got)
	}
	d, from, trunc := r.since(0, 0)
	if string(d) != "cdefghij" || from != 2 || !trunc {
		t.Fatalf("since(0) = %q %d %v", d, from, trunc)
	}
	d, from, trunc = r.since(8, 0)
	if string(d) != "ij" || from != 8 || trunc {
		t.Fatalf("since(8) = %q %d %v", d, from, trunc)
	}
	d, _, _ = r.since(4, 3)
	if string(d) != "efg" {
		t.Fatalf("since(4,3) = %q", d)
	}
	if got := string(r.between(5, 9)); got != "fghi" {
		t.Fatalf("between = %q", got)
	}
	r.write([]byte("0123456789"))
	if got := string(r.buf); got != "23456789" || r.end != 20 {
		t.Fatalf("big write buf=%q end=%d", got, r.end)
	}
}

func TestOSCScannerAcrossChunks(t *testing.T) {
	var s oscScanner
	stream := "out\x1b]133;D;7\x07\x1b]133;A\x07prompt$ "
	var got []marker
	off := int64(0)
	for i := 0; i < len(stream); i += 3 { // feed in 3-byte chunks to split markers
		end := i + 3
		if end > len(stream) {
			end = len(stream)
		}
		got = append(got, s.feed(off, []byte(stream[i:end]))...)
		off += int64(end - i)
	}
	if len(got) != 2 {
		t.Fatalf("got %d markers: %+v", len(got), got)
	}
	if got[0].kind != 'D' || got[0].code != 7 || got[0].offset != 3 {
		t.Fatalf("D marker wrong: %+v", got[0])
	}
	if got[1].kind != 'A' || got[1].offset != 13 {
		t.Fatalf("A marker wrong: %+v", got[1])
	}
}

func TestOSCScannerST(t *testing.T) {
	var s oscScanner
	got := s.feed(10, []byte("x\x1b]133;D;0\x1b\\"))
	if len(got) != 1 || got[0].kind != 'D' || got[0].offset != 11 {
		t.Fatalf("%+v", got)
	}
}

func TestPlainText(t *testing.T) {
	cases := map[string]string{
		"\x1b[1;32mgreen\x1b[0m\r\n":          "green\n",
		"\x1b]0;title\x07hello":               "hello",
		"progress 10%\rprogress 100%\r\ndone": "progress 100%\ndone",
		"abc\b\bX":                            "aX",
		"\x1b]133;A\x07$ ls\r\nfile\r\n":      "$ ls\nfile\n",
		"\x1b(Bplain\x1b=":                    "plain",
	}
	for in, want := range cases {
		if got := plainText([]byte(in)); got != want {
			t.Errorf("plainText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDropEcho(t *testing.T) {
	if got := dropEcho("$ echo hi\nhi\n"); got != "hi" {
		t.Fatalf("%q", got)
	}
	if got := dropEcho("only echo"); got != "" {
		t.Fatalf("%q", got)
	}
}
