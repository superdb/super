package dbid

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseID(t *testing.T) {
	id, err := ParseID("0x01234567890123456789012345678901")
	assert.NoError(t, err)
	assert.Equal(t, "01234567-8901-2345-6789-012345678901", id.String())

	// _ isn't a valid hexadecimal digit.
	_, err = ParseID("0x012345678901234567890123456789012345678_")
	assert.EqualError(t, err, "invalid ID: 0x012345678901234567890123456789012345678_")

	id, err = ParseID("01a11990-01e2-7cb4-a33f-d3f97191b61a")
	assert.NoError(t, err)
	assert.Equal(t, "01a11990-01e2-7cb4-a33f-d3f97191b61a", id.String())

	// Too long.
	_, err = ParseID("0A42ooXOWFkGit78ZjPVLpDkRgnn")
	assert.EqualError(t, err, "invalid ID: 0A42ooXOWFkGit78ZjPVLpDkRgnn")

	// Too short.
	_, err = ParseID("0A42ooXOWFkGit78ZjPVLpDkRg")
	assert.EqualError(t, err, "invalid ID: 0A42ooXOWFkGit78ZjPVLpDkRg")
}
