package service_test

import (
	"bytes"
	"io"
	"testing"

	"uuid"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/client"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	dbapi "github.com/superdb/super/db/api"
	"github.com/superdb/super/db/branches"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sio/supio"
)

type testClient struct {
	*testing.T
	*client.Connection
}

func (c *testClient) TestPoolStats(id uuid.UUID) exec.PoolStats {
	r, err := c.Connection.PoolStats(c.Context(), id)
	require.NoError(c, err)
	return r
}

func (c *testClient) TestPoolGet(id uuid.UUID) (config pools.Config) {
	remote := dbapi.NewRemoteDB(c.Connection)
	pool, err := dbapi.LookupPoolByID(c.Context(), remote, id)
	require.NoError(c, err)
	return *pool
}

func (c *testClient) TestBranchGet(id uuid.UUID) (config db.BranchMeta) {
	remote := dbapi.NewRemoteDB(c.Connection)
	branch, err := dbapi.LookupBranchByID(c.Context(), remote, id)
	require.NoError(c, err)
	return *branch
}

func (c *testClient) TestPoolList() []pools.Config {
	r, err := c.Query(c.Context(), srcfiles.Plain("from :pools"))
	require.NoError(c, err)
	defer r.Body.Close()
	var confs []pools.Config
	puller, err := bsupio.NewReader(c.Context(), super.NewContext(), r.Body, nil, 1)
	require.NoError(c, err)
	reader := sbuf.PullerReader(sbuf.NewMaterializer(puller))
	for {
		rec, err := reader.Read()
		require.NoError(c, err)
		if rec == nil {
			return confs
		}
		var pool pools.Config
		err = super.Unmarshal(*rec, &pool)
		require.NoError(c, err)
		confs = append(confs, pool)
	}
}

func (c *testClient) TestPoolPost(payload api.PoolPostRequest) uuid.UUID {
	r, err := c.Connection.CreatePool(c.Context(), payload)
	require.NoError(c, err)
	return r.Pool.ID
}

func (c *testClient) TestBranchPost(poolID uuid.UUID, payload api.BranchPostRequest) branches.Config {
	r, err := c.Connection.CreateBranch(c.Context(), poolID, payload)
	require.NoError(c, err)
	return r
}

func (c *testClient) TestQuery(query string) string {
	r, err := c.Connection.Query(c.Context(), srcfiles.Plain(query))
	require.NoError(c, err)
	defer r.Body.Close()
	zr, err := bsupio.NewReader(c.Context(), super.NewContext(), r.Body, nil, 1)
	require.NoError(c, err)
	var buf bytes.Buffer
	zw := supio.NewWriter(sio.NopCloser(&buf), supio.WriterOpts{})
	require.NoError(c, sio.Copy(zw, sbuf.PullerReader(sbuf.NewMaterializer(zr))))
	return buf.String()
}

func (c *testClient) TestLoad(poolID uuid.UUID, branchName string, r io.Reader) uuid.UUID {
	commit, err := c.Connection.Load(c.Context(), poolID, branchName, "", r, api.CommitMessage{})
	require.NoError(c, err)
	return commit.Commit
}

func (c *testClient) TestAuthMethod() api.AuthMethodResponse {
	r, err := c.Connection.AuthMethod(c.Context())
	require.NoError(c, err)
	return r
}

func (c *testClient) TestAuthIdentity() api.AuthIdentityResponse {
	r, err := c.Connection.AuthIdentity(c.Context())
	require.NoError(c, err)
	return r
}
