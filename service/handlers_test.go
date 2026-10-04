package service_test

import (
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/client"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/service"
)

func TestQuery(t *testing.T) {
	src := `
{_path:"b",ts:1970-01-01T00:00:01Z}
{_path:"a",ts:1970-01-01T00:00:01Z}
`
	expected := `{_path:"b",ts:1970-01-01T00:00:01Z}
`
	_, conn := newCore(t)
	poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	conn.TestLoad(poolID, "main", strings.NewReader(src))
	assert.Equal(t, expected, conn.TestQuery("from test | _path == 'b'"))
}

func TestQueryEmptyPool(t *testing.T) {
	_, conn := newCore(t)
	conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	assert.Equal(t, "", conn.TestQuery("from test"))
}

func TestPoolStats(t *testing.T) {
	src := `
{_path:"conn",ts:1970-01-01T00:00:01Z,uid:"CBrzd94qfowOqJwCHa"}
{_path:"conn",ts:1970-01-01T00:00:02Z,uid:"C8Tful1TvM3Zf5x8fl"}
`
	_, conn := newCore(t)
	poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	conn.TestLoad(poolID, "main", strings.NewReader(src))

	span := nano.Span{Ts: 1e9, Dur: 1e9 + 1}
	expected := exec.PoolStats{
		Span: &span,
		Size: 86,
	}
	require.Equal(t, expected, conn.TestPoolStats(poolID))
}

func TestPoolStatsNoData(t *testing.T) {
	_, conn := newCore(t)
	poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	info := conn.TestPoolStats(poolID)
	expected := exec.PoolStats{
		Size: 0,
	}
	require.Equal(t, expected, info)
}

func TestPoolPostNameOnly(t *testing.T) {
	_, conn := newCore(t)
	poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	assert.NotEqual(t, ksuid.Nil, poolID)
}

func TestPoolPostDuplicateName(t *testing.T) {
	_, conn := newCore(t)
	conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	_, err := conn.CreatePool(t.Context(), api.PoolPostRequest{Name: "test"})
	require.Equal(t, errors.Is(err, client.ErrPoolExists), true)
}

func TestPoolInvalidName(t *testing.T) {
	t.Skip("verify invalid characters for a pool name")
	_, conn := newCore(t)
	t.Run("Post", func(t *testing.T) {
		_, err := conn.CreatePool(t.Context(), api.PoolPostRequest{Name: "𝚭𝚴𝚪 is.good"})
		require.NoError(t, err)
		_, err = conn.CreatePool(t.Context(), api.PoolPostRequest{Name: "𝚭𝚴𝚪/bad"})
		require.EqualError(t, err, "status code 400: name may not contain '/' or non-printable characters")
	})
	t.Run("Put", func(t *testing.T) {
		poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "𝚭𝚴𝚪1"})
		err := conn.RenamePool(t.Context(), poolID, api.PoolPutRequest{Name: "𝚭𝚴𝚪/2"})
		require.EqualError(t, err, "status code 400: name may not contain '/' or non-printable characters")
	})
}

func TestPoolPutDuplicateName(t *testing.T) {
	_, conn := newCore(t)
	poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	conn.TestPoolPost(api.PoolPostRequest{Name: "test1"})
	err := conn.RenamePool(t.Context(), poolID, api.PoolPutRequest{Name: "test"})
	assert.EqualError(t, err, "status code 409: test: pool already exists")
}

func TestPoolPut(t *testing.T) {
	_, conn := newCore(t)
	poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	err := conn.RenamePool(t.Context(), poolID, api.PoolPutRequest{Name: "new_name"})
	require.NoError(t, err)
	info := conn.TestPoolGet(poolID)
	assert.Equal(t, "new_name", info.Name)
}

func TestPoolRemote(t *testing.T) {
	_, conn := newCore(t)
	poolID := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	err := conn.RemovePool(t.Context(), poolID)
	require.NoError(t, err)
	list := conn.TestPoolList()
	require.Len(t, list, 0)
}

func TestNoEndSlashSupport(t *testing.T) {
	_, conn := newCore(t)
	_, err := conn.Do(conn.NewRequest(t.Context(), "GET", "/pool/", nil))
	require.Error(t, err)
	require.Equal(t, 404, err.(*client.ErrorResponse).StatusCode)
}

func TestRequestID(t *testing.T) {
	pools := api.QueryRequest{Query: "from :pools"}
	t.Run("GeneratesUniqueID", func(t *testing.T) {
		_, conn := newCore(t)
		res1, err := conn.Do(conn.NewRequest(t.Context(), "POST", "/query", pools))
		require.NoError(t, err)
		res2, err := conn.Do(conn.NewRequest(t.Context(), "POST", "/query", pools))
		require.NoError(t, err)
		assert.NotEqual(t, "", res1.Header.Get("X-Request-ID"))
		assert.NotEqual(t, "", res2.Header.Get("X-Request-ID"))
	})
	t.Run("PropagatesID", func(t *testing.T) {
		_, conn := newCore(t)
		requestID := "random-request-ID"
		req := conn.NewRequest(t.Context(), "POST", "/query", pools)
		req.Header.Set("X-Request-ID", requestID)
		res, err := conn.Do(req)
		require.NoError(t, err)
		require.Equal(t, requestID, res.Header.Get("X-Request-ID"))
	})
}

func TestEventsHandler(t *testing.T) {
	t.Skip("events protocol broken by multi-line SUP with type decls... this should be reworked.  let's not use fscanf for protocol parsing.")
	_, conn := newCore(t)
	ev, err := conn.SubscribeEvents(t.Context())
	require.NoError(t, err)
	id := conn.TestPoolPost(api.PoolPostRequest{Name: "test"})
	kind, v, err := ev.Recv()
	require.NoError(t, err)
	assert.Equal(t, "pool-new", kind)
	assert.Equal(t, &api.EventPool{PoolID: id}, v)
	commit := conn.TestLoad(id, "main", strings.NewReader("{ts:0}"))
	kind, v, err = ev.Recv()
	require.NoError(t, err)
	assert.Equal(t, "branch-commit", kind)
	assert.Equal(t, &api.EventBranchCommit{
		PoolID:   id,
		Branch:   "main",
		CommitID: commit,
		Parent:   "",
	}, v)
	require.NoError(t, conn.RemovePool(t.Context(), id))
	kind, v, err = ev.Recv()
	require.NoError(t, err)
	assert.Equal(t, "pool-delete", kind)
	assert.Equal(t, &api.EventPool{PoolID: id}, v)
	require.NoError(t, ev.Close())
}

func newCore(t *testing.T) (*service.Core, *testClient) {
	root := t.TempDir()
	return newCoreAtDir(t, root)
}

func newCoreAtDir(t *testing.T, dir string) (*service.Core, *testClient) {
	t.Cleanup(func() { os.RemoveAll(dir) })
	return newCoreWithConfig(t, service.Config{Root: storage.MustParseURI(dir)})
}

func newCoreWithConfig(t *testing.T, conf service.Config) (*service.Core, *testClient) {
	if conf.Root == nil {
		conf.Root = storage.MustParseURI(t.TempDir())
	}
	core, err := service.NewCore(t.Context(), conf)
	require.NoError(t, err)
	srv := httptest.NewServer(core)
	t.Cleanup(srv.Close)
	return core, &testClient{
		Connection: client.NewConnectionTo(srv.URL),
		T:          t,
	}
}

func promCounterValue(g prometheus.Gatherer, name string) any {
	metricFamilies, err := g.Gather()
	if err != nil {
		return err
	}
	for _, mf := range metricFamilies {
		if mf.GetName() == name {
			return mf.GetMetric()[0].GetCounter().GetValue()
		}
	}
	return errors.New("metric not found")
}
