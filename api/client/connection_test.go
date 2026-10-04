package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/client/auth0"
	"github.com/superdb/super/bsupbytes"
)

func TestClientRedirectReplay(t *testing.T) {
	const expected = "hello world"
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	mux.HandleFunc("/auth/method", func(w http.ResponseWriter, r *http.Request) {
		writer := bsupbytes.NewBytesWriter()
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
	var body string
	mux.HandleFunc("/pool/", func(w http.ResponseWriter, r *http.Request) {
		if requests == 0 {
			requests++
			w.WriteHeader(401)
			io.WriteString(w, "invalid token")
			return
		}
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		body = string(b)
	})
	conn := NewConnectionTo(ts.URL)
	store := auth0.NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	store.SetTokens(ts.URL, auth0.Tokens{
		Access:  "012345",
		Refresh: "98765",
	})
	conn.SetAuthStore(store)
	_, err := conn.Load(t.Context(), ksuid.New(), "main", "", strings.NewReader(expected), api.CommitMessage{})
	require.NoError(t, err)
	assert.Equal(t, expected, body)
}
