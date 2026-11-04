package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPrefix = "httpdf/1.2.3"

// The fixtures in testdata were rendered by chromium-headless-shell (Skia/PDF
// m156) through the rodRenderer, without a creator prefix.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// buildPDF builds a minimal PDF the way Skia does, with the given document
// information dictionary as the first object
func buildPDF(info string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xD3\xEB\xE9\xE1\n")
	objects := []string{info, "<</Type /Pages\n/Count 0\n/Kids []>>", "<</Type /Catalog\n/Pages 2 0 R>>"}
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d\n/Root 3 0 R\n/Info 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

// stamp writes pdf through a creatorStamper in chunks of the given size
func stamp(t *testing.T, pdf []byte, prefix string, chunkSize int) ([]byte, error) {
	t.Helper()
	var out bytes.Buffer
	s := newCreatorStamper(&out, prefix)
	for chunk := range slices.Chunk(pdf, chunkSize) {
		_, err := s.Write(chunk)
		require.NoError(t, err)
	}
	err := s.Close()
	return out.Bytes(), err
}

// resolveInfo resolves the document information dictionary the way a PDF
// reader does: through the last trailer and the cross-reference sections
func resolveInfo(t *testing.T, pdf []byte) (trailer, info []entry) {
	t.Helper()
	trailer, startxref, err := parseTrailer(pdf)
	require.NoError(t, err)
	ref, ok := lookup(trailer, "/Info")
	require.True(t, ok, "trailer has no /Info")
	num, gen, err := parseRef(ref)
	require.NoError(t, err)

	s := &scanner{b: pdf, pos: int(xrefOffset(t, pdf, startxref, num, gen))}
	n, err := s.integer()
	require.NoError(t, err)
	g, err := s.integer()
	require.NoError(t, err)
	require.Equal(t, []int64{num, gen}, []int64{n, g}, "the xref offset points to another object")
	require.NoError(t, s.keyword("obj"))
	info, err = s.dict(0)
	require.NoError(t, err)
	require.NoError(t, s.keyword("endobj"))
	return trailer, info
}

// xrefEntry matches a 20 byte cross-reference entry, see PDF 32000-1:2008,
// section 7.5.4
var xrefEntry = regexp.MustCompile(`^(\d{10}) (\d{5}) ([nf])(?: \n| \r|\r\n)$`)

// xrefOffset looks up the object's offset in the cross-reference section at
// startxref, following /Prev to earlier sections
func xrefOffset(t *testing.T, pdf []byte, startxref, num, gen int64) int64 {
	t.Helper()
	for {
		s := &scanner{b: pdf, pos: int(startxref)}
		require.NoError(t, s.keyword("xref"))
		for {
			beforeToken := s.pos
			if string(s.regular()) == "trailer" {
				break
			}
			s.pos = beforeToken
			first, err := s.integer()
			require.NoError(t, err)
			count, err := s.integer()
			require.NoError(t, err)
			s.skipSpace()
			for i := range count {
				require.LessOrEqual(t, s.pos+20, len(pdf))
				m := xrefEntry.FindSubmatch(pdf[s.pos : s.pos+20])
				require.NotNil(t, m, "invalid xref entry %q", pdf[s.pos:s.pos+20])
				s.pos += 20
				if first+i == num {
					require.Equal(t, fmt.Sprintf("%05d n", gen), string(m[2])+" "+string(m[3]))
					offset, err := strconv.ParseInt(string(m[1]), 10, 64)
					require.NoError(t, err)
					return offset
				}
			}
		}

		trailer, err := s.dict(0)
		require.NoError(t, err)
		prev, ok := lookup(trailer, "/Prev")
		require.True(t, ok, "object %d %d not found in xref", num, gen)
		startxref, err = strconv.ParseInt(string(prev), 10, 64)
		require.NoError(t, err)
	}
}

// text decodes a raw text string object
func text(t *testing.T, raw []byte) string {
	t.Helper()
	b, err := stringBytes(raw)
	require.NoError(t, err)
	if !bytes.HasPrefix(b, utf16BOM) {
		return string(bytes.TrimPrefix(b, utf8BOM))
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 2; i+1 < len(b); i += 2 {
		units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
	}
	return string(utf16.Decode(units))
}

// utf16Hex encodes s as a hexadecimal UTF-16 text string object
func utf16Hex(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, unit := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", unit)
	}
	b.WriteString(">")
	return b.String()
}

// pairs formats dictionary entries for readable comparisons
func pairs(dict []entry) []string {
	out := make([]string, len(dict))
	for i, e := range dict {
		out[i] = string(e.key) + " " + string(e.value)
	}
	return out
}

func without(dict []entry, key string) []entry {
	return slices.DeleteFunc(slices.Clone(dict), func(e entry) bool { return string(e.key) == key })
}

// limitWriter accepts up to limit bytes, then fails
type limitWriter struct {
	limit int
	bytes.Buffer
}

var errWriteFailed = errors.New("write failed")

func (w *limitWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.Len(); len(p) > room {
		n, _ := w.Buffer.Write(p[:room])
		return n, errWriteFailed
	}
	return w.Buffer.Write(p)
}

func TestCreatorStamper(t *testing.T) {
	t.Run("it_prefixes_the_creator_of_chromium_pdfs", func(t *testing.T) {
		for _, fixture := range []string{"plain.pdf", "tagged.pdf"} {
			t.Run(fixture, func(t *testing.T) {
				orig := readFixture(t, fixture)
				out, err := stamp(t, orig, testPrefix, 32<<10)
				require.NoError(t, err)
				assert.True(t, bytes.HasPrefix(out, orig), "the original PDF is not a prefix of the output")

				origTrailer, origInfo := resolveInfo(t, orig)
				trailer, info := resolveInfo(t, out)

				creator, ok := lookup(info, "/Creator")
				require.True(t, ok)
				assert.Equal(t, "httpdf/1.2.3 Chromium", text(t, creator))
				assert.Equal(t, pairs(without(origInfo, "/Creator")), pairs(without(info, "/Creator")))

				_, origStartxref, err := parseTrailer(orig)
				require.NoError(t, err)
				assert.Equal(t, append(pairs(origTrailer), fmt.Sprintf("/Prev %d", origStartxref)), pairs(trailer))
			})
		}
	})

	t.Run("it_appends_an_incremental_update", func(t *testing.T) {
		orig := buildPDF("<</Title (Test)\n/Creator (Chromium)>>")
		_, origStartxref, err := parseTrailer(orig)
		require.NoError(t, err)

		out, err := stamp(t, orig, testPrefix, len(orig))
		require.NoError(t, err)

		object := "1 0 obj\n<</Title (Test)\n/Creator (httpdf/1.2.3 Chromium)>>\nendobj\n"
		expected := object + fmt.Sprintf(
			"xref\n1 1\n%010d 00000 n \ntrailer\n<</Size 4\n/Root 3 0 R\n/Info 1 0 R\n/Prev %d>>\nstartxref\n%d\n%%%%EOF\n",
			len(orig), origStartxref, len(orig)+len(object),
		)
		assert.Equal(t, expected, string(out[len(orig):]))
	})

	t.Run("it_streams_regardless_of_write_sizes", func(t *testing.T) {
		orig := readFixture(t, "tagged.pdf")
		expected, err := stamp(t, orig, testPrefix, len(orig))
		require.NoError(t, err)

		for _, size := range []int{1, 2, 7, tailSize - 1, tailSize, tailSize + 1, 2*tailSize + 1, 4096} {
			out, err := stamp(t, orig, testPrefix, size)
			require.NoError(t, err, "chunk size %d", size)
			assert.Equal(t, expected, out, "chunk size %d", size)
		}
	})

	t.Run("it_keeps_the_encoding_of_the_creator", func(t *testing.T) {
		tests := []struct {
			name    string
			info    string
			creator string
		}{
			{"escapes", `<</Creator (a\(b\)\\c)>>`, `(httpdf/1.2.3 a\(b\)\\c)`},
			{"pdfdocencoding", `<</Creator (caf\351)>>`, `(httpdf/1.2.3 caf\351)`},
			{"utf16", "<</Creator " + utf16Hex("Größe ✓") + ">>", utf16Hex("httpdf/1.2.3 Größe ✓")},
			{"utf8", `<</Creator (\357\273\277Hi)>>`, `(\357\273\277httpdf/1.2.3 Hi)`},
			{"missing", `<</Producer (Skia/PDF m156)>>`, `(httpdf/1.2.3)`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				out, err := stamp(t, buildPDF(tt.info), testPrefix, 4096)
				require.NoError(t, err)
				_, info := resolveInfo(t, out)
				creator, ok := lookup(info, "/Creator")
				require.True(t, ok)
				assert.Equal(t, tt.creator, string(creator))
			})
		}
	})

	t.Run("it_passes_unexpected_pdfs_through_unmodified", func(t *testing.T) {
		plain := readFixture(t, "plain.pdf")
		stamped, err := stamp(t, plain, testPrefix, len(plain))
		require.NoError(t, err)

		tests := []struct {
			name   string
			pdf    []byte
			prefix string
			reason string
		}{
			{"empty", nil, testPrefix, "missing PDF header"},
			{"not_a_pdf", []byte("hello world"), testPrefix, "missing PDF header"},
			{"truncated", plain[:len(plain)-100], testPrefix, "no trailer found"},
			{"missing_eof", plain[:len(plain)-len("%%EOF\n")], testPrefix, "missing %%EOF"},
			{"already_updated", stamped, testPrefix, "updated before"},
			{
				"info_is_not_the_first_object",
				bytes.Replace(buildPDF("<</Creator (Chromium)>>"), []byte("/Info 1 0 R"), []byte("/Info 3 0 R"), 1),
				testPrefix, "trailer /Info is 3 0 R, but the first object is 1 0",
			},
			{
				"no_info",
				bytes.Replace(buildPDF("<</Creator (Chromium)>>"), []byte("\n/Info 1 0 R"), nil, 1),
				testPrefix, "trailer has no /Info",
			},
			{
				"no_trailer",
				[]byte("%PDF-1.5\n1 0 obj\n<</Creator (Chromium)>>\nendobj\nstartxref\n9\n%%EOF\n"),
				testPrefix, "no trailer found",
			},
			{
				"first_object_is_a_stream",
				buildPDF("<</Length 3>> stream\nabc\nendstream"),
				testPrefix, `expected "endobj", got "stream"`,
			},
			{
				"first_object_too_large",
				buildPDF("<</Title (" + strings.Repeat("x", maxHeadSize) + ")>>"),
				testPrefix, "first object exceeds",
			},
			{"creator_is_not_a_string", buildPDF("<</Creator 42>>"), testPrefix, "/Creator: not a string"},
			{"non_ascii_prefix", plain, "httpdf/ü", "not printable ASCII"},
			{"empty_prefix", plain, "", "not printable ASCII"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				out, err := stamp(t, tt.pdf, tt.prefix, 4096)
				assert.ErrorIs(t, err, errUnstamped)
				assert.ErrorContains(t, err, tt.reason)
				assert.Equal(t, tt.pdf, out)
			})
		}
	})

	t.Run("it_returns_write_errors", func(t *testing.T) {
		orig := readFixture(t, "plain.pdf")

		w := &limitWriter{limit: 100}
		s := newCreatorStamper(w, testPrefix)
		n, err := s.Write(orig)
		assert.Equal(t, 100, n)
		assert.ErrorIs(t, err, errWriteFailed)
		_, err = s.Write(orig[n:])
		assert.ErrorIs(t, err, errWriteFailed)
		err = s.Close()
		assert.ErrorIs(t, err, errWriteFailed)
		assert.NotErrorIs(t, err, errUnstamped)
		assert.Equal(t, orig[:100], w.Bytes())

		w = &limitWriter{limit: len(orig)}
		s = newCreatorStamper(w, testPrefix)
		_, err = s.Write(orig)
		require.NoError(t, err)
		err = s.Close()
		assert.ErrorIs(t, err, errWriteFailed)
		assert.NotErrorIs(t, err, errUnstamped)
	})
}

func FuzzCreatorStamper(f *testing.F) {
	for _, name := range []string{"plain.pdf", "tagged.pdf"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(f, err)
		f.Add(b, uint16(4096))
	}
	f.Add(buildPDF(`<</Title [(a) <0A> 1 0 R <</N /x>>]
/Creator (\(nested \(parens\)\))>>`), uint16(1))

	f.Fuzz(func(t *testing.T, pdf []byte, chunkSize uint16) {
		out, err := stamp(t, pdf, testPrefix, max(int(chunkSize), 1))
		if err != nil {
			require.ErrorIs(t, err, errUnstamped)
			require.True(t, bytes.Equal(pdf, out), "the output differs from the input")
			return
		}
		require.True(t, bytes.HasPrefix(out, pdf))
		_, info := resolveInfo(t, out)
		creator, ok := lookup(info, "/Creator")
		require.True(t, ok)
		require.True(t, strings.HasPrefix(text(t, creator), testPrefix))
	})
}

func TestScanner(t *testing.T) {
	t.Run("it_scans_objects", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"12 0 R>>", "12 0 R"},
			{"18\n/Root 1 0 R", "18"},
			{"1 2 3", "1"},
			{"/Name/Next", "/Name"},
			{"[1 2 3]", "[1 2 3]"},
			{"[<AB> (x) /N 1 0 R] 4", "[<AB> (x) /N 1 0 R]"},
			{"<</A <</B (c)>>>>/D", "<</A <</B (c)>>>>"},
			{`(a (b) \) c) d`, `(a (b) \) c)`},
			{"<FE FF>>>", "<FE FF>"},
		}
		for _, tt := range tests {
			s := &scanner{b: []byte(tt.input)}
			object, err := s.object(0)
			require.NoError(t, err, tt.input)
			assert.Equal(t, tt.expected, string(object), tt.input)
		}
	})

	t.Run("it_rejects_invalid_objects", func(t *testing.T) {
		tests := []struct {
			input  string
			reason string
		}{
			{"", "unexpected end"},
			{"(abc", "unexpected end"},
			{"<ABC", "unexpected end"},
			{"<XY>", "hexadecimal string"},
			{"[1 2", "unexpected end"},
			{"<</A 1", "unexpected end"},
			{"<<1 2>>", "expected a name"},
			{")", "unexpected"},
			{strings.Repeat("[", maxNesting+2), "nested too deeply"},
		}
		for _, tt := range tests {
			s := &scanner{b: []byte(tt.input)}
			_, err := s.object(0)
			assert.ErrorContains(t, err, tt.reason, tt.input)
		}
	})
}

func TestStrings(t *testing.T) {
	t.Run("it_unescapes_literal_strings", func(t *testing.T) {
		tests := []struct {
			raw      string
			expected string
		}{
			{`a\(b\)\\c`, `a(b)\c`},
			{`nested (parens)`, `nested (parens)`},
			{`\n\r\t\b\f`, "\n\r\t\b\f"},
			{`caf\351`, "caf\xe9"},
			{`\5`, "\x05"},
			{`\53x`, "+x"},
			{`\0053`, "\x053"},
			{`\q`, "q"},
			{"line\\\ncontinued", "linecontinued"},
			{"line\\\r\ncontinued", "linecontinued"},
			{"a\r\nb\rc", "a\nb\nc"},
		}
		for _, tt := range tests {
			assert.Equal(t, tt.expected, string(unescapeLiteral([]byte(tt.raw))), tt.raw)
		}
	})

	t.Run("it_decodes_hex_strings", func(t *testing.T) {
		b, err := decodeHex([]byte("48 69\n"))
		require.NoError(t, err)
		assert.Equal(t, "Hi", string(b))

		b, err = decodeHex([]byte("486"))
		require.NoError(t, err)
		assert.Equal(t, "H`", string(b))

		_, err = decodeHex([]byte("zz"))
		assert.Error(t, err)
	})

	t.Run("it_encodes_strings", func(t *testing.T) {
		tests := []struct {
			value    string
			expected string
		}{
			{"Chromium", "(Chromium)"},
			{`a(b)\c`, `(a\(b\)\\c)`},
			{"caf\xe9\n", `(caf\351\012)`},
			{"\xfe\xff\x00H", "<FEFF0048>"},
		}
		for _, tt := range tests {
			encoded := encodeString([]byte(tt.value))
			assert.Equal(t, tt.expected, string(encoded), tt.value)
			decoded, err := stringBytes(encoded)
			require.NoError(t, err)
			assert.Equal(t, tt.value, string(decoded), tt.value)
		}
	})
}
