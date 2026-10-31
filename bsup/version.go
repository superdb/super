package bsup

import (
	"encoding/binary"
	"fmt"
)

// Versioning is done with major/minor versions.  Any implementation can read
// files with the same major version, forward and backward, but an older minor
// version might not be able to take advantage of or understand new types
// supported by a later minor version under the same major version.  No
// compatibility (except possibly backward compatibility depending on the
// implementation) is available between different major versions.  Major
// version 0 is for development and there is no compatibility between minor
// versions under major version 0.  No releases should be made with a zero
// major version.
//
// A version is encoded in two bytes as a little-endian uint16 with the major
// version in the upper 6 bits and the minor version in the lower 10 bits, so
// a development version encoded before major and minor versions were
// introduced (when the version was a plain uint16) decodes as major version 0.
//
// Last dev minor version: 28
var CurrentVersion = Version{1, 0}

const (
	VersionSize = 2

	majorBits = 6
	minorBits = 10
	MaxMajor  = 1<<majorBits - 1
	MaxMinor  = 1<<minorBits - 1
)

type Version struct {
	Major uint8
	Minor uint16
}

// Serialize encodes v into the first VersionSize bytes of b.
func (v Version) Serialize(b []byte) {
	if v.Major > MaxMajor || v.Minor > MaxMinor {
		panic(fmt.Sprintf("BSUP version %s out of range", v))
	}
	binary.LittleEndian.PutUint16(b, uint16(v.Major)<<minorBits|v.Minor)
}

// Deserialize decodes v from the first VersionSize bytes of b.
func (v *Version) Deserialize(b []byte) {
	u := binary.LittleEndian.Uint16(b)
	v.Major = uint8(u >> minorBits)
	v.Minor = u & MaxMinor
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

func checkVersion(version Version) error {
	if version.Major != CurrentVersion.Major || (CurrentVersion.Major == 0 && version.Minor != CurrentVersion.Minor) {
		return fmt.Errorf("version mismatch: expected %s, found %s", CurrentVersion, version)
	}
	return nil
}
