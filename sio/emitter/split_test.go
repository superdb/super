package emitter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/storage"
	storagemock "github.com/superdb/super/pkg/storage/mock"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/mock/gomock"
)

func TestDirS3Source(t *testing.T) {
	t.Skip("split by _path no longer supported")
	path := "s3://testbucket/dir"
	const input = `
{_path:"conn",foo:"1"}
{_path:"http",bar:"2"}
`
	uri, err := storage.ParseURI(path)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	engine := storagemock.NewMockEngine(ctrl)

	engine.EXPECT().Put(t.Context(), uri.JoinPath("conn.sup")).
		Return(sio.NopCloser(bytes.NewBuffer(nil)), nil)
	engine.EXPECT().Put(t.Context(), uri.JoinPath("http.sup")).
		Return(sio.NopCloser(bytes.NewBuffer(nil)), nil)

	sctx := super.NewContext()
	r := supio.NewReader(sctx, strings.NewReader(input))
	require.NoError(t, err)
	w, err := NewSplit(t.Context(), sctx, engine, uri, "", false, anyio.WriterOpts{Format: "sup"})
	require.NoError(t, err)
	require.NoError(t, vio.Copy(w, sbuf.NewDematerializer(sctx, sbuf.NewPuller(r))))
}
