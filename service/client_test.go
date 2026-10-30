package service_test

import (
	"bytes"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/client"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	dbapi "github.com/superdb/super/db/api"
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/supio"
)

type testClient struct {
	*testing.T
	*client.Connection
}

func (c *testClient) TestTableStats(id uuid.UUID) exec.TableStats {
	r, err := c.Connection.TableStats(c.Context(), id)
	require.NoError(c, err)
	return r
}

func (c *testClient) TestTableGet(id uuid.UUID) (config tables.Config) {
	table, err := dbapi.LookupTableByID(c.Context(), c.Connection, id)
	require.NoError(c, err)
	return *table
}

func (c *testClient) TestBranchGet(id uuid.UUID) (config db.BranchMeta) {
	branch, err := dbapi.LookupBranchByID(c.Context(), c.Connection, id)
	require.NoError(c, err)
	return *branch
}

func (c *testClient) TestTableList() []tables.Config {
	r, err := c.Query(c.Context(), srcfiles.Plain("from :tables"))
	require.NoError(c, err)
	defer r.Close()
	var configs []tables.Config
	reader := sbuf.PullerReader(sbuf.NewMaterializer(r))
	for {
		rec, err := reader.Read()
		require.NoError(c, err)
		if rec == nil {
			return configs
		}
		var config tables.Config
		err = super.Unmarshal(*rec, &config)
		require.NoError(c, err)
		configs = append(configs, config)
	}
}

func (c *testClient) TestTablePost(payload api.TablePostRequest) uuid.UUID {
	id, err := c.Connection.CreateTable(c.Context(), payload.Name, payload.SortKeys, payload.ObjectCap, payload.FrameCap, nil, false)
	require.NoError(c, err)
	return id
}

func (c *testClient) TestQuery(query string) string {
	scanner, err := c.Connection.Query(c.Context(), srcfiles.Plain(query))
	require.NoError(c, err)
	defer scanner.Close()
	var buf bytes.Buffer
	zw := supio.NewWriter(sio.NopCloser(&buf), supio.WriterOpts{})
	require.NoError(c, sio.Copy(zw, sbuf.PullerReader(sbuf.NewMaterializer(scanner))))
	return buf.String()
}

func (c *testClient) TestLoad(tableID uuid.UUID, branchName string, sup string) uuid.UUID {
	sctx := super.NewContext()
	reader := sbuf.NewDematerializer(sctx, sbuf.NewPuller(supio.NewReader(sctx, strings.NewReader(sup))))
	commit, err := c.Connection.Load(c.Context(), nil, tableID, branchName, reader, api.CommitMessage{})
	require.NoError(c, err)
	return commit
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
