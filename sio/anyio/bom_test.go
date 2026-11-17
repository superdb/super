package anyio

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// noSeekReader fails the test if SkipBOM seeks.
type noSeekReader struct {
	*strings.Reader
	t *testing.T
}

func (n noSeekReader) Seek(int64, int) (int64, error) {
	n.t.Error("unexpected Seek")
	return 0, nil
}

func TestSkipBOM(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"a,b\n", "a,b\n"},
		{"\xef", "\xef"},
		{"\xef\xbb", "\xef\xbb"},
		{"\xef\xbb\xbe", "\xef\xbb\xbe"},
		{"\xefa,b\n", "\xefa,b\n"},
		{"\xef\xbba,b\n", "\xef\xbba,b\n"},
		{utf8BOM, ""},
		{utf8BOM + "a,b\n", "a,b\n"},
		{utf8BOM + utf8BOM + "a,b\n", utf8BOM + "a,b\n"},
	}
	for _, c := range cases {
		b, err := io.ReadAll(SkipBOM(noSeekReader{strings.NewReader(c.input), t}))
		require.NoError(t, err)
		require.Equal(t, c.expected, string(b), "input %q", c.input)
	}
}

// TestSkipBOMReadsOnlyUntilMismatch tests that SkipBOM doesn't read beyond
// the first byte that isn't part of a byte order mark.
func TestSkipBOMReadsOnlyUntilMismatch(t *testing.T) {
	for _, s := range []string{"1", "\xef\xbb1", utf8BOM} {
		pr, pw := io.Pipe()
		ch := make(chan struct{})
		var writeErr error
		go func() {
			// SkipBOM should return upon seeing this input.  It will
			// block (and this test will time out) if it tries to read
			// more.
			_, writeErr = pw.Write([]byte(s))
			close(ch)
		}()
		r := SkipBOM(pr)
		<-ch
		require.NoError(t, writeErr)
		require.NoError(t, pw.Close())
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		require.Equal(t, strings.TrimPrefix(s, utf8BOM), string(b))
	}
}
