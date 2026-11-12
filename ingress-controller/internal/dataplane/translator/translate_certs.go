package translator

import (
	"bytes"
	"cmp"
	"crypto/tls"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kong/go-kong/kong"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/logging"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
)

func getCertFromSecret(secret *corev1.Secret) (string, string, error) {
	certData, okcert := secret.Data[corev1.TLSCertKey]
	keyData, okkey := secret.Data[corev1.TLSPrivateKeyKey]

	if !okcert || !okkey {
		return "", "", fmt.Errorf("no keypair could be found in"+
			" secret '%v/%v'", secret.Namespace, secret.Name)
	}

	cert := bytes.TrimSpace(certData)
	key := bytes.TrimSpace(keyData)

	if _, err := tls.X509KeyPair(cert, key); err != nil {
		return "", "", fmt.Errorf("parsing TLS key-pair in secret '%v/%v': %w",
			secret.Namespace, secret.Name, err)
	}

	return string(cert), string(key), nil
}

type certWrapper struct {
	identifier        string
	cert              kong.Certificate
	snis              []string
	CreationTimestamp metav1.Time
	// inheritedTags are the tags inherited from the Gateway (and its GatewayClass) whose listener
	// requested the certificate. They are appended to the certificate's own tags and set as the
	// tags of the SNIs the listener requested.
	inheritedTags []string
}

func (t *Translator) getGatewayCerts(resolver *inheritedTagResolver) []certWrapper {
	logger := t.logger
	s := t.storer
	certs := []certWrapper{}
	gateways, err := s.ListGateways()
	if err != nil {
		logger.Error(err, "Failed to list Gateways")
		return certs
	}
	// Sort Gateways to make the result, and thereby the merged certificates' inherited tags and the
	// SNI assignment, independent of the store's ordering.
	slices.SortFunc(gateways, func(a, b *gatewayapi.Gateway) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})
	for _, gateway := range gateways {
		inheritedTags := resolver.forGateway(gateway).all()
		statuses := make(map[gatewayapi.SectionName]gatewayapi.ListenerStatus, len(gateway.Status.Listeners))
		for _, status := range gateway.Status.Listeners {
			statuses[status.Name] = status
		}

		for _, listener := range gateway.Spec.Listeners {
			_, ok := statuses[listener.Name]
			if !ok {
				logger.V(logging.DebugLevel).Info("Listener missing status information",
					"gateway", gateway.Name,
					"listener", listener.Name,
					"listener_protocol", listener.Protocol,
					"listener_port", listener.Port,
				)
				continue
			}

			// We were checking if listener is marked as programmed when KIC reconciles "Unmanaged" gateways.
			// But here we do not need to do the check because the gateways are always managed by Kong operator.
			// Ref: https://github.com/Kong/kong-operator/issues/1769

			if listener.TLS != nil {
				if len(listener.TLS.CertificateRefs) > 0 {
					if len(listener.TLS.CertificateRefs) > 1 {
						// TODO support cert_alt and key_alt if there are 2 SecretObjectReferences
						// https://github.com/Kong/kubernetes-ingress-controller/issues/2604
						t.registerTranslationFailure("listener '%s' has more than one certificateRef, it's not supported", gateway)
						continue
					}

					// determine the Secret Namespace
					ref := listener.TLS.CertificateRefs[0]
					namespace := gateway.Namespace
					if ref.Namespace != nil {
						namespace = string(*ref.Namespace)
					}

					// retrieve the Secret and extract the PEM strings
					secret, err := s.GetSecret(namespace, string(ref.Name))
					if err != nil {
						logger.Error(err, "Failed to fetch secret",
							"gateway", gateway.Name,
							"listener", listener.Name,
							"secret_name", string(ref.Name),
							"secret_namespace", namespace,
						)
						continue
					}
					cert, key, err := getCertFromSecret(secret)
					if err != nil {
						t.registerTranslationFailure("failed to construct certificate from secret", secret, gateway)
						continue
					}

					// determine the SNI
					hostname := "*"
					if listener.Hostname != nil {
						hostname = string(*listener.Hostname)
					}

					// create a Kong certificate, wrap it in metadata, and add it to the certs slice
					certs = append(certs, certWrapper{
						identifier: cert + key,
						cert: kong.Certificate{
							ID:   new(string(secret.UID)),
							Cert: new(cert),
							Key:  new(key),
							Tags: util.GenerateTagsForObject(secret),
						},
						CreationTimestamp: secret.CreationTimestamp,
						snis:              []string{hostname},
						inheritedTags:     inheritedTags,
					})
				}
			}
		}
	}
	return certs
}

// inheritedTagsForClientCertSecret returns the tags the Kong certificate translated from the Secret
// inherits from the Services referencing it through konghq.com/client-cert: the Services' inherited
// tags concatenated in service-name order, so that the result does not depend on the order in which
// populateServices visited them.
func inheritedTagsForClientCertSecret(clientCertSecretToServices map[string][]clientCertServiceTags, secretKey string) []string {
	services, ok := clientCertSecretToServices[secretKey]
	if !ok {
		return nil
	}
	// Clone before sorting to leave the caller's slice untouched.
	services = slices.Clone(services)
	slices.SortFunc(services, func(a, b clientCertServiceTags) int {
		return strings.Compare(a.ServiceName, b.ServiceName)
	})
	var tags []string
	for _, service := range services {
		tags = slices.Concat(tags, service.InheritedTags)
	}
	return tags
}

func (t *Translator) getCerts(
	secretsToSNIs SecretNameToSNIs,
	clientCertSecretToServices map[string][]clientCertServiceTags,
) []certWrapper {
	certs := []certWrapper{}

	for secretKey, SNIs := range secretsToSNIs.secretToSNIs {
		namespaceName := strings.Split(secretKey, "/")
		secret, err := t.storer.GetSecret(namespaceName[0], namespaceName[1])
		if err != nil {
			t.registerTranslationFailure(fmt.Sprintf("Failed to fetch the secret (%s)", secretKey), SNIs.Parents()...)
			continue
		}
		cert, key, err := getCertFromSecret(secret)
		if err != nil {
			causingObjects := append(SNIs.Parents(), secret)
			t.registerTranslationFailure("failed to construct certificate from secret", causingObjects...)
			continue
		}
		certs = append(certs, certWrapper{
			identifier: cert + key,
			cert: kong.Certificate{
				ID:   new(string(secret.UID)),
				Cert: new(cert),
				Key:  new(key),
				Tags: util.GenerateTagsForObject(secret),
			},
			CreationTimestamp: secret.CreationTimestamp,
			snis:              SNIs.Hosts(),
			// A Secret referenced through a backend Service's konghq.com/client-cert annotation makes
			// its certificate inherit the tags of the parent Gateways (and their GatewayClasses) of
			// the Gateway API routes using that Service.
			inheritedTags: inheritedTagsForClientCertSecret(clientCertSecretToServices, secretKey),
		})
	}

	return certs
}

type certIDToMergedCertID map[string]string

type identicalCertIDSet struct {
	mergedCertID string
	certIDs      []string
}

func mergeCerts(logger logr.Logger, certLists ...[]certWrapper) ([]kongstate.Certificate, certIDToMergedCertID) {
	// snisSeen maps every SNI to the identifier (cert+key) of the certificate serving it. The identifier, unlike
	// the ID, does not change when certificates with identical content are merged.
	snisSeen := make(map[string]string)
	certsSeen := make(map[string]certWrapper)
	certIDSets := make(map[string]identicalCertIDSet)
	// sniTags holds the inherited tags of every SNI, keyed by certificate identifier and SNI name.
	sniTags := make(map[string]map[string][]string)

	for _, cl := range certLists {
		for _, cw := range cl {
			current, ok := certsSeen[cw.identifier]
			if !ok {
				current = cw
			} else {
				// The Kong certificate is shared by every requester, so it inherits the tags of all of them.
				current.inheritedTags = slices.Concat(current.inheritedTags, cw.inheritedTags)

				// multiple Secrets that contain identical certificates are collapsed, because we only create one
				// Kong resource for a given cert+key pair. however, because we reuse the Secret ID, creation time,
				// and tags for the Kong resource equivalents, the selection of those needs to be deterministic to
				// avoid pointless configuration updates
				if current.CreationTimestamp.After(cw.CreationTimestamp.Time) {
					current.cert.ID = cw.cert.ID
					current.cert.Tags = cw.cert.Tags
					current.CreationTimestamp = cw.CreationTimestamp
				} else if current.CreationTimestamp.Time.Equal(cw.CreationTimestamp.Time) && (current.cert.ID == nil || *current.cert.ID > *cw.cert.ID) {
					current.cert.ID = cw.cert.ID
					current.cert.Tags = cw.cert.Tags
					current.CreationTimestamp = cw.CreationTimestamp
				}
			}

			// although we use current in the end, we only warn/exclude on new ones here. SNIs already in the slice
			// have already been vetted by some previous iteration and /are/ in the seen list, but they're in the seen
			// list because the current we retrieved from certsSeen added them
			for _, sni := range cw.snis {
				seen, ok := snisSeen[sni]
				switch {
				case !ok:
					snisSeen[sni] = current.identifier
					current.cert.SNIs = append(current.cert.SNIs, new(sni))
				case seen != current.identifier:
					// TODO this should really log information about the requesting Listener or Ingress-like, which is
					// what binds the SNI to a given Secret. Knowing the Secret ID isn't of great use beyond knowing
					// what cert will be served. however, the secretToSNIs input to getCerts does not provide this info
					// https://github.com/Kong/kubernetes-ingress-controller/issues/2605
					logger.Error(nil, "Same SNI requested for multiple certs, can only serve one cert",
						"served_secret_cert", *certsSeen[seen].cert.ID,
						"requested_secret_cert", *current.cert.ID,
						"sni", sni)
					continue
				}
				// The SNI is served by this certificate, either newly assigned above or already requested by
				// another requester of the same certificate (e.g. an Ingress and a Gateway listener sharing a
				// hostname). It inherits the tags of every requester (e.g. Gateway listener) asking for it.
				if len(cw.inheritedTags) > 0 {
					if sniTags[current.identifier] == nil {
						sniTags[current.identifier] = make(map[string][]string)
					}
					sniTags[current.identifier][sni] = slices.Concat(sniTags[current.identifier][sni], cw.inheritedTags)
				}
			}
			certsSeen[current.identifier] = current

			idSet := certIDSets[current.identifier]
			idSet.mergedCertID = *current.cert.ID
			idSet.certIDs = append(idSet.certIDs, *cw.cert.ID)
			certIDSets[current.identifier] = idSet

		}
	}
	res := make([]kongstate.Certificate, 0, len(certsSeen))
	for _, cw := range certsSeen {
		sort.SliceStable(cw.cert.SNIs, func(i, j int) bool {
			return strings.Compare(*cw.cert.SNIs[i], *cw.cert.SNIs[j]) < 0
		})
		if len(cw.inheritedTags) > 0 {
			cw.cert.Tags = util.MergeTags(cw.cert.Tags, cw.inheritedTags)
		}
		var snisTags map[string][]*string
		for sni, tags := range sniTags[cw.identifier] {
			if snisTags == nil {
				snisTags = make(map[string][]*string)
			}
			snisTags[sni] = util.MergeTags(nil, tags)
		}
		res = append(res, kongstate.Certificate{
			Certificate: cw.cert,
			SNITags:     snisTags,
		})
	}

	idToMergedID := certIDToMergedCertID{}
	for _, idSet := range certIDSets {
		for _, certID := range idSet.certIDs {
			idToMergedID[certID] = idSet.mergedCertID
		}
	}
	return res, idToMergedID
}
