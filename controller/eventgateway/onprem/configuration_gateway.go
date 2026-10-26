/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onprem

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// addGatewayEntities adds the entities attached to the gateway itself. It
// returns its backend clusters, the parents of its virtual clusters.
func (c *configuration) addGatewayEntities(ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) (parents, error) {
	var backendClusters configurationv1alpha1.EventGatewayBackendClusterList
	bcs, err := addAttached(ctx, c, cl, gateway, &backendClusters, &backendClusters.Items, index.IndexFieldEventGatewayBackendClusterOnOnPremEventGatewayRef,
		entityKind[*configurationv1alpha1.EventGatewayBackendCluster, gwadminapi.EventGatewayBackendClusterOnPrem]{
			kind: "EventGatewayBackendCluster", what: "backend cluster",
			request: func(ctx context.Context, cl client.Client, bc *configurationv1alpha1.EventGatewayBackendCluster) (any, error) {
				return bc.ToCreateBackendClusterRequest(ctx, cl)
			},
			name: func(e gwadminapi.EventGatewayBackendClusterOnPrem) string { return e.Name },
			list: in[*configurationv1alpha1.EventGatewayBackendCluster]("backend_clusters", &c.entities.BackendClusters),
		})
	if err != nil {
		return parents{}, err
	}

	var bundles configurationv1alpha1.EventGatewayTLSTrustBundleList
	if _, err := addAttached(ctx, c, cl, gateway, &bundles, &bundles.Items, index.IndexFieldEventGatewayTLSTrustBundleOnOnPremEventGatewayRef,
		entityKind[*configurationv1alpha1.EventGatewayTLSTrustBundle, gwadminapi.EventGatewayTLSTrustBundleOnPrem]{
			kind: "EventGatewayTLSTrustBundle", what: "TLS trust bundle",
			request: func(ctx context.Context, cl client.Client, b *configurationv1alpha1.EventGatewayTLSTrustBundle) (any, error) {
				return b.ToCreateTLSTrustBundleRequest(ctx, cl)
			},
			name: func(e gwadminapi.EventGatewayTLSTrustBundleOnPrem) string { return e.Name },
			list: in[*configurationv1alpha1.EventGatewayTLSTrustBundle]("tls_trust_bundles", &c.entities.TlsTrustBundles),
		}); err != nil {
		return parents{}, err
	}

	var registries configurationv1alpha1.EventGatewaySchemaRegistryList
	if _, err := addAttached(ctx, c, cl, gateway, &registries, &registries.Items, index.IndexFieldEventGatewaySchemaRegistryOnOnPremEventGatewayRef,
		entityKind[*configurationv1alpha1.EventGatewaySchemaRegistry, gwadminapi.EventGatewaySchemaRegistryOnPrem]{
			kind: "EventGatewaySchemaRegistry", what: "schema registry",
			request: func(ctx context.Context, cl client.Client, r *configurationv1alpha1.EventGatewaySchemaRegistry) (any, error) {
				return r.ToSchemaRegistryCreate(ctx, cl)
			},
			name: schemaRegistryName,
			list: in[*configurationv1alpha1.EventGatewaySchemaRegistry]("schema_registries", &c.entities.SchemaRegistries),
		}); err != nil {
		return parents{}, err
	}

	var keys configurationv1alpha1.EventGatewayStaticKeyList
	if _, err := addAttached(ctx, c, cl, gateway, &keys, &keys.Items, index.IndexFieldEventGatewayStaticKeyOnOnPremEventGatewayRef,
		entityKind[*configurationv1alpha1.EventGatewayStaticKey, gwadminapi.EventGatewayStaticKeyOnPrem]{
			kind: "EventGatewayStaticKey", what: "static key",
			request: func(ctx context.Context, cl client.Client, k *configurationv1alpha1.EventGatewayStaticKey) (any, error) {
				return k.ToEventGatewayStaticKeyCreate(ctx, cl)
			},
			name: func(e gwadminapi.EventGatewayStaticKeyOnPrem) string { return e.Name },
			list: in[*configurationv1alpha1.EventGatewayStaticKey]("static_keys", &c.entities.StaticKeys),
		}); err != nil {
		return parents{}, err
	}
	return bcs, nil
}

// schemaRegistryName returns the name of a schema registry: Confluent is its
// only type.
func schemaRegistryName(e gwadminapi.EventGatewaySchemaRegistryOnPrem) string {
	registry, err := e.AsSchemaRegistryConfluent()
	if err != nil {
		return ""
	}
	return registry.Name
}
