package server

import (
	"errors"
	"fmt"
	"net/url"
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
		if !isDomain(domain) {
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

// isDomain reports whether domain is a bare host: parsed as an authority, it
// gives back the same host, with no scheme, port, path or userinfo. An entry
// that is a valid host but not a DNS name, e.g. a wildcard, matches no host.
func isDomain(domain string) bool {
	u, err := url.Parse("//" + domain)
	return err == nil && u.Hostname() == domain
}

// Allowed reports whether rawURL points at one of the domains. The URL is
// parsed, so only the host the request will be sent to decides: a port, a path
// and a userinfo part do not take part, and a URL that does not parse is
// refused.
func (d Domains) Allowed(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))

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
