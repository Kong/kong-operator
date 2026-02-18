package deploy

import "github.com/kong/kong-operator/v2/test/helpers/certificate"

var testValidCertPEM, testValidCertKeyPEM = certificate.MustGenerateSelfSignedCertPEMFormat()

// TestValidCertPEM is a valid certificate PEM to be used in tests.
var TestValidCertPEM = string(testValidCertPEM)

// TestValidCertKeyPEM is a valid certificate key PEM to be used in tests.
var TestValidCertKeyPEM = string(testValidCertKeyPEM)

// TestValidCACertPEM is a valid CA certificate PEM to be used in tests.
var TestValidCACertPEM = string(testValidCertPEM)
