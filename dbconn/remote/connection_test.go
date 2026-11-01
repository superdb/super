package remote

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsupbytes"
	"github.com/superdb/super/dbconn"
	"github.com/superdb/super/dbconn/remote/auth0"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/service/api"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func TestClientRedirectReplay(t *testing.T) {
	sctx := super.NewContext()
	expected, err := sup.ParseValue(sctx, `"hello world"`)
	require.NoError(t, err)
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	mux.HandleFunc("/auth/method", func(w http.ResponseWriter, r *http.Request) {
		writer := bsupbytes.NewWriter()
		writer.Write(api.AuthMethodResponse{
			Kind: api.AuthMethodAuth0,
			Auth0: &api.AuthMethodAuth0Details{
				Domain: ts.URL,
			},
		})
		writer.Close()
		w.Write(writer.Bytes())
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(struct {
			AccessToken string `json:"access_token"`
		}{"12345"})
	})
	var requests int
	var result super.Value
	mux.HandleFunc("/table/", func(w http.ResponseWriter, r *http.Request) {
		if requests == 0 {
			requests++
			w.WriteHeader(401)
			io.WriteString(w, "invalid token")
			return
		}
		b, err := bsupio.NewReader(t.Context(), sctx, r.Body, nil, nil, 1)
		require.NoError(t, err)
		vec, err := b.Pull(false)
		require.NoError(t, err)
		require.NoError(t, r.Body.Close())
		assert.Equal(t, uint32(1), vec.Len())
		result = vector.ValueAt(nil, vec, 0)
	})
	conn := NewConn(ts.URL)
	store := auth0.NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	store.SetTokens(ts.URL, auth0.Tokens{
		Access:  "012345",
		Refresh: "98765",
	})
	conn.SetAuthStore(store)
	_, err = conn.Load(t.Context(), nil, uuid.NewV7(), "main", sbuf.ValToPuller(sctx, expected), dbconn.CommitMessage{})
	require.NoError(t, err)
	assert.Equal(t, expected, result)
}
