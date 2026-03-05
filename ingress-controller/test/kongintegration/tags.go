package kongintegration

import "strings"

func trimEnterpriseTagToSemver(tag string) string {
	// Enterprise tags can contain a fourth segment, trim it to adhere to semver.
	if strings.Count(tag, ".") > 2 {
		parts := strings.SplitN(tag, ".", 4)
		tag = strings.Join(parts[:3], ".")
	}
	return tag
}
