package envtest

import "time"

const (
	// konnectInfiniteSyncTime is used for tests that want to verify behavior of the reconcilers not relying on the fixed
	// sync period. It's set to 60m that is virtually infinite for the tests.
	konnectInfiniteSyncTime = time.Minute * 60

	// konnectSyncTime is used for tests that want to verify behavior of the reconcilers relying on the fixed sync.
	konnectSyncTime = 100 * time.Millisecond

	// waitTime is a generic wait time for the tests' eventual conditions.
	waitTime = 10 * time.Second

	// tickTime is a generic tick time for the tests' eventual conditions.
	tickTime = 500 * time.Millisecond

	// dummyValidCACertPEM is a dummy valid CA certificate PEM to be used in tests.
	dummyValidCACertPEM = `-----BEGIN CERTIFICATE-----
MIIDPTCCAiWgAwIBAgIUcNKAk2icWRJGwZ5QDpdSkkeF5kUwDQYJKoZIhvcNAQEL
BQAwLjELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkNBMRIwEAYDVQQKDAlLb25nIElu
Yy4wHhcNMjQwOTE5MDkwODEzWhcNMjkwOTE4MDkwODEzWjAuMQswCQYDVQQGEwJV
UzELMAkGA1UECAwCQ0ExEjAQBgNVBAoMCUtvbmcgSW5jLjCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAMvDhLM0vTw0QXmgE+sB6gvKx2PUWzvd2tRZoamH
h4RAxYRjgJsJe6WEeAk0tjWQqwAq0Y2MQioMCC4X+L13kpdtomI+4PKjBozg+iTd
ThyV0oQSVHHWzayUzcSODnGR524H9YxmkXV5ImrXwbEqXwiUESPVtjnf/ZzWS01v
gtbu4x3YW+z8kRoXOTpJHKcEoI90SU9F4yeuQsCtbJHeJZRqPr6Kz84ZuHsZ2MeU
os4j1GdMaH3dSysqFv6o1hJ2+6bsrE/ONiGtBb4+tyhivgf+u+ixQwqIERlEJzhI
z/csoAAnfMBY401j2NNUgPpwx5sTQdCz5aFDmanol5152M8CAwEAAaNTMFEwHQYD
VR0OBBYEFK2qd3oRF37acVvgfDeLakx66ioTMB8GA1UdIwQYMBaAFK2qd3oRF37a
cVvgfDeLakx66ioTMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AAuul+rAztaueTpPIM63nrS4bSZsIatCgAQ5Pihm0+rZ+13BJk4K2GxkS+T0qkB5
34+F3eVhUB4cC+kVkWZrlEzD9BsJwWjnoJK+848znTg+ufTeaOQWslYNqFKjmy2k
K6NE7E6r+JLdNvafJzeDybSTXI1tCzDRWUdj5m+bgruX07B13KIJKrAweCTD1927
WvvfJYxsg8P7dYD9DPlcuOm22ggAaPPu4P/MsnApiq3kJEI/nSGSsboKyjBO2hcz
VF1CYr6Epfyw/47kwuJLCVHjlTgT4haOChW1S8rZILCLXfb8ukM/g3XVYIeEwzsr
KU74cm8lTFCdxlcXePbMdHc=
-----END CERTIFICATE-----
`
)
