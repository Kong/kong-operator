package konnect

import "github.com/kong/go-database-reconciler/pkg/file"

// countEntities returns the number of Kong entities in the given deck content, including the nested ones
// (e.g. routes of services, plugins of routes, credentials of consumers).
// Memberships of consumers in consumer groups are not counted as they are references, not entities.
func countEntities(content *file.Content) int {
	if content == nil {
		return 0
	}

	count := len(content.Services)
	for _, service := range content.Services {
		count += len(service.Plugins)
		count += countRoutes(service.Routes)
	}
	count += len(content.Routes)
	for _, route := range content.Routes {
		count += len(route.Plugins)
	}
	count += len(content.Plugins)

	count += len(content.Consumers)
	for _, consumer := range content.Consumers {
		count += len(consumer.Plugins) +
			len(consumer.KeyAuths) +
			len(consumer.HMACAuths) +
			len(consumer.JWTAuths) +
			len(consumer.BasicAuths) +
			len(consumer.Oauth2Creds) +
			len(consumer.ACLGroups) +
			len(consumer.MTLSAuths)
	}
	count += len(content.ConsumerGroups)
	for _, consumerGroup := range content.ConsumerGroups {
		count += len(consumerGroup.Plugins)
	}

	count += len(content.Upstreams)
	for _, upstream := range content.Upstreams {
		count += len(upstream.Targets)
	}
	count += len(content.Certificates)
	for _, certificate := range content.Certificates {
		count += len(certificate.SNIs)
	}
	count += len(content.CACertificates) +
		len(content.Vaults) +
		len(content.Licenses) +
		len(content.Keys) +
		len(content.KeySets) +
		len(content.Partials)

	return count
}

func countRoutes(routes []*file.FRoute) int {
	count := len(routes)
	for _, route := range routes {
		count += len(route.Plugins)
	}
	return count
}
