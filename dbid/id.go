package dbid

import (
	"encoding/hex"
	"fmt"

	"uuid"
)

func ParseID(s string) (uuid.UUID, error) {
	// Check if this is a cut-and-paste from BSUP, which encodes
	// the 16-byte UUID as a 32 character hex string with 0x prefix.
	var id uuid.UUID
	var err error
	if len(s) == 34 && s[0:2] == "0x" {
		var b []byte
		b, err = hex.DecodeString(s[2:])
		if err == nil {
			id = uuid.UUID(b[:])
		}
	} else {
		id, err = uuid.Parse(s)
	}
	if err != nil {
		return uuid.Nil(), fmt.Errorf("invalid ID: %s", s)
	}
	return id, nil
}

func ParseIDs(ss []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		id, err := ParseID(s)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
