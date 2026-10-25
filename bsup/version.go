package bsup

import "fmt"

// Versioning is done with major/minor versions.  Any major version can read
// files of the same version number forward and backwqard, but older minor versions
// might not be able to take advantage of or understand new types supported by
// later minor versions under the same major number.  No compatibility (except
// possibly backward compat depending on the implementaton) is availble for versions
// with different major numbers.  Major version 0 is for development and there is
// no compatibility between minor versions under major 0.  No releases should be made
// with a zero major version.
// Last dev minor version: 28
var CurrentVersion = Version{0, 28}

type Version struct {
	Major uint8
	Minor uint8
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
