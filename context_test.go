package super_test

import (
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
)

func TestContextLookupByValueAndLookupTypeValue(t *testing.T) {
	sctx := super.NewContext()
	recType := sctx.MustLookupTypeRecord(nil)

	tv := super.EncodeTypeValue(recType)
	typ, err := sctx.LookupByValue(tv)
	require.NoError(t, err)
	assert.Equal(t, recType, typ)

	// Overwriting tv should not affect sctx's cached type value for recType.
	super.AppendTypeValue(tv[:0], super.TypeNull)
	val := sctx.LookupTypeValue(recType)
	assert.Exactly(t, super.EncodeTypeValue(recType), []byte(val.Bytes()))
}

func TestContextLookupTypeNamedErrors(t *testing.T) {
	sctx := super.NewContext()

	_, err := sctx.LookupTypeNamed("\xff", super.TypeNull)
	assert.EqualError(t, err, `bad type name "\xff": invalid UTF-8`)

	_, err = sctx.LookupTypeNamed("null", super.TypeNull)
	assert.EqualError(t, err, `named type collides with primitive type: null`)
}

func TestContextLookupTypeNamedAndLookupTypeDef(t *testing.T) {
	sctx := super.NewContext()

	assert.Nil(t, sctx.LookupByName("x"))

	named1, err := sctx.LookupTypeNamed("x", super.TypeNull)
	require.NoError(t, err)
	assert.Same(t, named1, sctx.LookupByName("x"))
}

func TestBindTypeNamedWakesAllWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sctx := super.NewContext()
		named, patch, err := sctx.DeclareTypeNamed("foo")
		require.NoError(t, err)
		require.GreaterOrEqual(t, patch, 0)

		const n = 8
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() {
				// "foo" is declared but unbound, so this blocks in Wait.
				typ, p, err := sctx.DeclareTypeNamed("foo")
				assert.NoError(t, err)
				assert.Equal(t, -1, p)
				assert.Equal(t, super.TypeInt64, typ.Type)
			})
		}
		// Return only once every waiter is parked on the condition variable.
		synctest.Wait()

		sctx.BindTypeNamed(patch, named, super.TypeInt64)

		// With Signal (the old way) instead of Broadcast (the new way),
		// only one waiter wakes. The rest stay blocked, and synctest
		// reports a deadlock here instead of hanging.
		wg.Wait()
	})
}
