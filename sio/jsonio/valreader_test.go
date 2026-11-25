package jsonio

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A number that straddles or ends at the end of the read buffer is read
// whole, whether more input or EOF follows.
func TestValReaderNumberAtBufferEnd(t *testing.T) {
	size := len(NewValReader(nil).buf)
	for _, num := range []string{"12345", "-42", "12.5", "1.5e10"} {
		for k := 1; k <= len(num); k++ {
			// A string fills the buffer up to the number's first k bytes.
			pad := `"` + strings.Repeat("x", size-k-3) + "\"\n"
			for _, rest := range []string{"\n2\n", ""} {
				r := NewValReader(strings.NewReader(pad + num + rest))
				var vals []string
				for {
					b, err := r.Next()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					vals = append(vals, strings.TrimSpace(string(b)))
				}
				want := []string{num}
				if rest != "" {
					want = append(want, "2")
				}
				require.Equal(t, want, vals[1:])
			}
		}
	}
}

// Only a number can be cut short at the end of the read buffer and still
// parse, so a string, record, or array that exactly fills the buffer is
// returned without growing the buffer.
func TestValReaderFullBufferValue(t *testing.T) {
	size := len(NewValReader(nil).buf)
	for _, wrap := range [][2]string{{`"`, `"`}, {`{"a":"`, `"}`}, {`["`, `"]`}} {
		val := wrap[0] + strings.Repeat("x", size-len(wrap[0])-len(wrap[1])) + wrap[1]
		r := NewValReader(strings.NewReader(val + "\n1\n"))
		b, err := r.Next()
		require.NoError(t, err)
		require.Equal(t, val, string(b))
		require.Equal(t, size, len(r.buf))
		b, err = r.Next()
		require.NoError(t, err)
		require.Equal(t, "1", strings.TrimSpace(string(b)))
	}
}
