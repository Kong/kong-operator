package testenv

import internal "github.com/kong/kong-operator/ingress-controller/test/internal/testenv"

func GetDependencyVersion(path string) (string, error) {
	return internal.GetDependencyVersion(path)
}

func KongEnterpriseEnabled() bool {
	return internal.KongEnterpriseEnabled()
}
