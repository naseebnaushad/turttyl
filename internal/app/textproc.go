package app

import (
	"bytes"
	"strconv"
	"strings"
)

// ring is a fixed-size byte buffer that remembers the absolute offset of its contents.
// Offsets act as cursors: a reader asks for "everything since offset N".
type ring struct {
	buf  []byte
	size int
	end  int64 // absolute offset one past the last byte written
}

func newRing(size int) *ring { return &ring{buf: make([]byte, 0, size), size: size} }

func (r *ring) write(p []byte) {
	r.end += int64(len(p))
	if len(p) >= r.size {
		r.buf = append(r.buf[:0], p[len(p)-r.size:]...)
		return
	}
	if over := len(r.buf) + len(p) - r.size; over > 0 {
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
	r.buf = append(r.buf, p...)
}

func (r *ring) start() int64 { return r.end - int64(len(r.buf)) }

// since returns a copy of the bytes from offset (clamped to what is kept) up to max bytes,
// the offset they start at, and whether older bytes were already dropped.
func (r *ring) since(offset int64, max int) (data []byte, from int64, truncated bool) {
	from = offset
	if from < r.start() {
		from, truncated = r.start(), true
	}
	if from > r.end {
		from = r.end
	}
	lo := int(from - r.start())
	hi := len(r.buf)
	if max > 0 && hi-lo > max {
		hi = lo + max
	}
	return append([]byte(nil), r.buf[lo:hi]...), from, truncated
}

// between returns the kept bytes in [a, b).
func (r *ring) between(a, b int64) []byte {
	d, _, _ := r.since(a, int(b-max64(a, r.start())))
	return d
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// marker is a shell-integration event (OSC 133) found in the output stream.
type marker struct {
	kind   byte  // 'A' prompt start, 'C' output start, 'D' command finished
	code   int   // exit code for 'D'
	offset int64 // absolute offset of the marker's first byte
}

// oscScanner finds OSC 133 markers in a byte stream that arrives in chunks.
type oscScanner struct {
	carry []byte
	base  int64 // absolute offset of carry[0]
}

var oscPrefix = []byte("\x1b]133;")

// feed scans a chunk that starts at absolute offset off and returns the markers found.
func (s *oscScanner) feed(off int64, p []byte) []marker {
	data := p
	base := off
	if len(s.carry) > 0 {
		data = append(s.carry, p...)
		base = s.base
		s.carry = nil
	}
	var out []marker
	i := 0
	for {
		j := bytes.Index(data[i:], oscPrefix)
		if j < 0 {
			// Keep a possible partial prefix at the end.
			for k := len(oscPrefix) - 1; k > 0; k-- {
				if len(data) >= k && bytes.Equal(data[len(data)-k:], oscPrefix[:k]) {
					s.carry = append([]byte(nil), data[len(data)-k:]...)
					s.base = base + int64(len(data)-k)
					break
				}
			}
			return out
		}
		start := i + j
		body := data[start+len(oscPrefix):]
		end := bytes.IndexAny(body, "\x07\x1b")
		if end < 0 {
			if len(body) < 32 {
				s.carry = append([]byte(nil), data[start:]...)
				s.base = base + int64(start)
			}
			return out
		}
		params := string(body[:end])
		if m, ok := parseMarker(params); ok {
			m.offset = base + int64(start)
			out = append(out, m)
		}
		i = start + len(oscPrefix) + end + 1
	}
}

func parseMarker(params string) (marker, bool) {
	parts := strings.Split(params, ";")
	switch parts[0] {
	case "A":
		return marker{kind: 'A'}, true
	case "C":
		return marker{kind: 'C'}, true
	case "D":
		code := 0
		if len(parts) > 1 {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				code = n
			}
		}
		return marker{kind: 'D', code: code}, true
	}
	return marker{}, false
}

// plainText turns raw terminal output into readable text for agents: it removes escape
// sequences, applies carriage-return overwrites and backspaces, and normalises newlines.
func plainText(p []byte) string {
	b := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == 0x1b && i+1 < len(p):
			switch p[i+1] {
			case '[': // CSI: ESC [ params final(0x40-0x7e)
				i += 2
				for i < len(p) && (p[i] < 0x40 || p[i] > 0x7e) {
					i++
				}
			case ']', 'P', '_', '^': // OSC / DCS / APC / PM: until BEL or ST
				i += 2
				for i < len(p) {
					if p[i] == 0x07 {
						break
					}
					if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '\\' {
						i++
						break
					}
					i++
				}
			case '(', ')', '*', '+', '#': // charset selection: ESC ( X
				i += 2
			default:
				i++
			}
		case c == 0x1b:
			// lone ESC at end
		case c == '\b':
			if n := len(b); n > 0 && b[n-1] != '\n' {
				b = b[:n-1]
			}
		case c == '\n' || c == '\r' || c == '\t' || c >= 0x20:
			b = append(b, c)
		}
	}
	// Apply carriage returns per line: "\r\n" is a newline; a lone "\r" overwrites.
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	for i, l := range lines {
		if k := strings.LastIndexByte(strings.TrimRight(l, "\r"), '\r'); k >= 0 {
			l = l[k+1:]
		}
		lines[i] = strings.TrimRight(l, "\r ")
	}
	return strings.Join(lines, "\n")
}
