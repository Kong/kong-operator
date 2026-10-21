package server

import (
	"errors"
	"fmt"
	"strings"
)

// Domains is the list of domains a Konnect server URL host must belong to.
type Domains []string

// DefaultDomains are the domains Kong serves Konnect from. The list is the
// switch: a deployment pointed at another domain adds it, there is nothing to
// turn off.
var DefaultDomains = Domains{"konghq.com", "konghq.tech"}

// NewDomains parses the comma-separated list given to
// --konnect-server-url-domains. An empty list is an error.
func NewDomains(flagValue string) (Domains, error) {
	if strings.TrimSpace(flagValue) == "" {
		return nil, errors.New("the list of server URL domains cannot be empty")
	}

	domains := make(Domains, 0)
	for domain := range strings.SplitSeq(flagValue, ",") {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			return nil, errors.New("the list of server URL domains cannot contain an empty entry")
		}
		if strings.ContainsAny(domain, "/:*@") {
			return nil, fmt.Errorf("invalid server URL domain %q: expected a bare domain, e.g. konghq.com", domain)
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

// String returns the list as it is given on the command line.
func (d Domains) String() string {
	return strings.Join(d, ",")
}

// Allowed reports whether host belongs to one of the domains. The match is on a
// DNS label boundary, so "notkonghq.com" is not under "konghq.com". Port and
// path are dropped: NewServer keeps a path attached to the last label.
func (d Domains) Allowed(host string) bool {
	host = strings.ToLower(host)
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSuffix(host, ".")

	for _, domain := range d {
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}
