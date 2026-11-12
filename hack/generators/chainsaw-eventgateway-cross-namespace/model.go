package main

import (
	"fmt"
	"strings"
)

// Namespaces of the objects of a scenario, as chainsaw expressions.
const (
	nsGateway  = "($namespace)"
	nsReferrer = "($referrer_namespace)"
	nsTarget   = "($target_namespace)"
	nsThird    = "($third_namespace)"

	// null leaves a field out of the object applied: the API server prunes a
	// null field of a CRD, so a reference without its namespace defaults to
	// the namespace of the object.
	null = "(`null`)"

	groupConfiguration = "configuration.konghq.com"
	groupEventGateway  = "eventgateway.konghq.com"
)

// refType is how an object references another, which tells the condition
// reporting a missing KongReferenceGrant, and whether it holds back the
// configuration of an OnPremEventGateway.
type refType string

const (
	// refParent is the reference to the parent of an object.
	refParent refType = "parent"
	// refSibling is a reference to another entity than the parent.
	refSibling refType = "sibling"
	// refSecret is a reference to a Secret.
	refSecret refType = "secret"
)

// binding is a binding of a step: a name and a value, a chainsaw expression or
// a plain value written as is, or a list of entries of a KongReferenceGrant.
type binding struct {
	Name  string
	Value string
	List  []map[string]string
}

// ref is a reference of an object to another.
type ref struct {
	Type   refType
	Target *object
	// NameKey is the binding of the template holding the name of the target,
	// if any.
	NameKey string
	// NSKey is the binding holding the namespace of the target: null when the
	// target is in the namespace of the object, so that the reference
	// defaults to it.
	NSKey string
	// ExplicitNSKey is a binding always holding the namespace of the target,
	// e.g. where a template creates the Secret it references.
	ExplicitNSKey string
}

// object is an object of a scenario: an entity of the gateway, or the gateway
// or a Secret, which the tests don't create but reference.
type object struct {
	Kind, Name, NS string
	// Template creates the object, a file of the step templates of the suite.
	Template string
	Group    string
	// Own are the bindings of the template other than the references.
	Own  []binding
	Refs []ref
	// Named tells the object has a name in its API entity, which the gateway
	// keeps unique.
	Named bool
	// ExplicitNS tells its references name the namespace of their target
	// even when it is the one of the object, rather than leaving it out.
	ExplicitNS bool
}

func (o *object) apiVersion() string {
	if o.Kind == "Secret" {
		return "v1"
	}
	return o.Group + "/v1alpha1"
}

func (o *object) isGateway() bool { return o.Kind == "gateway" }

// grantTo is the "to" entry of a KongReferenceGrant to the object.
func (o *object) grantTo() map[string]string {
	switch {
	case o.isGateway():
		return map[string]string{"group": "($gateway_group)", "kind": "($gateway_kind)"}
	case o.Kind == "Secret":
		return map[string]string{"group": "core", "kind": "Secret"}
	default:
		return map[string]string{"group": o.Group, "kind": o.Kind}
	}
}

// what describes the object in a step description.
func (o *object) what() string {
	switch {
	case o.isGateway():
		return "its gateway"
	case o.Kind == "Secret":
		return "the Secret"
	default:
		return "the " + o.Kind + " " + o.Name
	}
}

// bindings returns the bindings of the template creating the object.
func (o *object) bindings() []binding {
	b := append([]binding(nil), o.Own...)
	for _, r := range o.Refs {
		if r.Target.isGateway() {
			if o.Kind == "KegDataPlane" {
				v := "($control_plane_ref)"
				if o.NS == nsGateway && !o.ExplicitNS {
					v = "($control_plane_ref_local)"
				}
				b = append(b, binding{Name: "control_plane_ref", Value: v})
				continue
			}
			b = append(b, binding{Name: "gateway_ref_namespace", Value: nsOrNull(o, r.Target)})
			continue
		}
		if r.NameKey != "" {
			b = append(b, binding{Name: r.NameKey, Value: r.Target.Name})
		}
		if r.ExplicitNSKey != "" {
			b = append(b, binding{Name: r.ExplicitNSKey, Value: r.Target.NS})
		}
		if r.NSKey != "" {
			b = append(b, binding{Name: r.NSKey, Value: nsOrNull(o, r.Target)})
		}
	}
	return b
}

// nsOrNull is the namespace of the target of a reference of o, or null when it
// is the namespace of o and o doesn't name it explicitly.
func nsOrNull(o, target *object) string {
	if target.NS == o.NS && !o.ExplicitNS {
		return null
	}
	return target.NS
}

// withTarget returns a copy of o, its reference at index i to another target.
func (o *object) withTarget(i int, target *object) *object {
	c := *o
	c.Refs = append([]ref(nil), o.Refs...)
	c.Refs[i].Target = target
	return &c
}

// withAPIName returns a copy of o, named apiName in the API of the gateway.
func (o *object) withAPIName(apiName string) *object {
	c := *o
	c.Own = append([]binding(nil), o.Own...)
	for i := range c.Own {
		if c.Own[i].Name == "api_name" {
			c.Own[i].Value = apiName
			return &c
		}
	}
	panic(fmt.Sprintf("%s %s has no name in the API", o.Kind, o.Name))
}

// refIndex returns the index of the reference of o to target.
func (o *object) refIndex(target *object) int {
	for i, r := range o.Refs {
		if r.Target == target {
			return i
		}
	}
	panic(fmt.Sprintf("%s %s doesn't reference %s", o.Kind, o.Name, target.Name))
}

var gateway = &object{Kind: "gateway", Name: "($gateway_name)", NS: nsGateway, Group: "($gateway_group)"}

func secret(name, ns string) *object {
	return &object{Kind: "Secret", Name: name, NS: ns, Group: "core"}
}

// own returns bindings from names and values, alternated.
func own(kv ...string) []binding {
	b := make([]binding, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		b = append(b, binding{Name: kv[i], Value: kv[i+1]})
	}
	return b
}

func gatewayRef() ref { return ref{Type: refParent, Target: gateway} }

func backendCluster(name, ns string) *object {
	return &object{
		Kind: "EventGatewayBackendCluster", Name: name, NS: ns, Group: groupConfiguration, Named: true,
		Template: "apply-eventGatewayBackendCluster.yaml",
		Own:      own("backend_cluster_name", name, "backend_cluster_namespace", ns),
		Refs:     []ref{gatewayRef()},
	}
}

func virtualCluster(name, ns string, b *object) *object {
	return &object{
		Kind: "EventGatewayVirtualCluster", Name: name, NS: ns, Group: groupConfiguration, Named: true,
		Template: "apply-eventGatewayVirtualCluster.yaml",
		Own:      own("virtual_cluster_name", name, "virtual_cluster_namespace", ns),
		Refs:     []ref{{Type: refParent, Target: b, NameKey: "backend_cluster_name", NSKey: "backend_cluster_namespace"}},
	}
}

// backendClusterTLS is a backend cluster authenticating to Kafka with the
// client certificate of the Secret server-tls of secretNS.
func backendClusterTLS(name, ns, secretNS string) *object {
	o := backendCluster(name, ns)
	o.Template = "apply-eventGatewayBackendCluster-clientIdentity.yaml"
	o.Refs = append(o.Refs, ref{Type: refSecret, Target: secret("server-tls", secretNS), NSKey: "client_identity_secret_namespace"})
	return o
}

// backendClusterGatewayRef is a backend cluster of a KonnectEventGateway
// referencing it through the deprecated spec.gatewayRef.
func backendClusterGatewayRef(name, ns string) *object {
	o := backendCluster(name, ns)
	o.Template = "apply-eventGatewayBackendCluster-gatewayRef.yaml"
	return o
}

// ports gives each listener of a test a port of its own.
type ports struct{ next int }

func (p *ports) take() string {
	p.next++
	return fmt.Sprintf(`"%d"`, 19100+p.next)
}

func listener(name, ns string, p *ports) *object {
	return &object{
		Kind: "EventGatewayListener", Name: name, NS: ns, Group: groupConfiguration, Named: true,
		Template: "apply-eventGatewayListener.yaml",
		Own:      own("listener_name", name, "listener_namespace", ns, "listener_port", p.take()),
		Refs:     []ref{gatewayRef()},
	}
}

func gatewayChild(kind, template, prefix, name, ns string, s *object, secretNSKey string) *object {
	o := &object{
		Kind: kind, Name: name, NS: ns, Group: groupConfiguration, Template: template,
		Named: kind != "EventGatewayDataPlaneCertificate",
		// The name in the API, a binding of its own: an object of another
		// namespace with the name of the object can then have another one.
		Own:  own(prefix+"_name", name, prefix+"_namespace", ns, "api_name", name),
		Refs: []ref{gatewayRef(), {Type: refSecret, Target: s, NSKey: "secret_ref_namespace", ExplicitNSKey: secretNSKey}},
	}
	return o
}

func staticKey(name, ns, secretNS string) *object {
	// The template creates the Secret, named after the key.
	return gatewayChild("EventGatewayStaticKey", "apply-eventGatewayStaticKey.yaml", "static_key", name, ns, secret(name, secretNS), "secret_namespace")
}

func schemaRegistry(name, ns, secretNS string) *object {
	return schemaRegistryOf(name, ns, secretNS, "json")
}

// schemaRegistryOf is a schema registry of schemas of the type, json or avro:
// a transcode policy needs one of its output format.
func schemaRegistryOf(name, ns, secretNS, schemaType string) *object {
	o := gatewayChild("EventGatewaySchemaRegistry", "apply-eventGatewaySchemaRegistry.yaml", "schema_registry", name, ns, secret(name, secretNS), "secret_namespace")
	o.Own = append(o.Own, binding{Name: "schema_type", Value: schemaType})
	return o
}

func tlsTrustBundle(name, ns, secretNS string) *object {
	return gatewayChild("EventGatewayTLSTrustBundle", "apply-eventGatewayTLSTrustBundle.yaml", "tls_trust_bundle", name, ns, secret("client-ca", secretNS), "")
}

func dataPlaneCertificate(name, ns, secretNS string) *object {
	return gatewayChild("EventGatewayDataPlaneCertificate", "apply-eventGatewayDataPlaneCertificate.yaml", "data_plane_certificate", name, ns, secret("data-plane-ca", secretNS), "")
}

func listenerPolicy(name, ns string, l *object, secretNS string, tb *object) *object {
	return &object{
		Kind: "EventGatewayListenerPolicy", Name: name, NS: ns, Group: groupConfiguration, Named: true,
		Template: "apply-eventGatewayListenerPolicy-tlsServer.yaml",
		Own:      own("policy_name", name, "policy_namespace", ns),
		Refs: []ref{
			{Type: refParent, Target: l, NameKey: "listener_name", NSKey: "listener_namespace"},
			{Type: refSecret, Target: secret("server-tls", secretNS), NSKey: "server_tls_secret_namespace"},
			{Type: refSibling, Target: tb, NameKey: "tls_trust_bundle_name", NSKey: "tls_trust_bundle_namespace"},
		},
	}
}

func vcRef(v *object) ref {
	return ref{Type: refParent, Target: v, NameKey: "virtual_cluster_name", NSKey: "virtual_cluster_namespace"}
}

func vcPolicy(kind, template, name, ns string, v *object, refs ...ref) *object {
	return &object{
		Kind: kind, Name: name, NS: ns, Group: groupConfiguration, Template: template, Named: true,
		Own:  own("policy_name", name, "policy_namespace", ns),
		Refs: append([]ref{vcRef(v)}, refs...),
	}
}

func acls(name, ns string, v *object) *object {
	return vcPolicy("EventGatewayVirtualClusterPolicy", "apply-eventGatewayVirtualClusterPolicy-acls.yaml", name, ns, v)
}

func sideKind(side string) string {
	return "EventGatewayVirtualCluster" + strings.ToUpper(side[:1]) + side[1:] + "Policy"
}

func sideTemplate(side, variant string) string {
	k := sideKind(side)
	return "apply-" + strings.ToLower(k[:1]) + k[1:] + "-" + variant + ".yaml"
}

func modifyHeaders(side, name, ns string, v *object) *object {
	return vcPolicy(sideKind(side), sideTemplate(side, "modifyHeaders"), name, ns, v)
}

func childPolicy(side, name, ns string, v, parent *object) *object {
	return vcPolicy(sideKind(side), sideTemplate(side, "modifyHeaders-child"), name, ns, v, parentRef(parent))
}

func schemaValidation(side, name, ns string, v, r *object) *object {
	return vcPolicy(sideKind(side), sideTemplate(side, "schemaValidation"), name, ns, v,
		ref{Type: refSibling, Target: r, NameKey: "schema_registry_name", NSKey: "schema_registry_namespace"})
}

func encrypt(name, ns string, v, k *object) *object {
	return vcPolicy(sideKind("produce"), "apply-eventGatewayVirtualClusterProducePolicy-encrypt.yaml", name, ns, v,
		ref{Type: refSibling, Target: k, NameKey: "static_key_name", NSKey: "static_key_namespace"})
}

func parentRef(parent *object) ref {
	return ref{Type: refSibling, Target: parent, NameKey: "parent_policy_name", NSKey: "parent_policy_namespace"}
}

// encryptFields is an encryptFields produce policy, child of a schemaValidation
// one, encrypting a field with a static key.
func encryptFields(name, ns string, v, parent, k *object) *object {
	return vcPolicy(sideKind("produce"), "apply-eventGatewayVirtualClusterProducePolicy-encryptFields.yaml", name, ns, v, parentRef(parent),
		ref{Type: refSibling, Target: k, NameKey: "static_key_name", NSKey: "static_key_namespace"})
}

// transcode is a transcode policy, child of a schemaValidation one, with the
// schema of a schema registry.
func transcode(side, name, ns string, v, parent, r *object) *object {
	return vcPolicy(sideKind(side), sideTemplate(side, "transcode"), name, ns, v, parentRef(parent),
		ref{Type: refSibling, Target: r, NameKey: "schema_registry_name", NSKey: "schema_registry_namespace"})
}

func policyChain(name, ns string, v, p *object) *object {
	return &object{
		Kind: "EventGatewayVirtualClusterPolicyChain", Name: name, NS: ns, Group: groupConfiguration,
		Template: "apply-eventGatewayVirtualClusterPolicyChain.yaml",
		Own:      own("chain_name", name, "chain_namespace", ns),
		Refs: []ref{
			vcRef(v),
			{Type: refSibling, Target: p, NameKey: "policy_name", NSKey: "policy_namespace"},
		},
	}
}

func kegDataPlane(name, ns string) *object {
	return &object{
		Kind: "KegDataPlane", Name: name, NS: ns, Group: groupEventGateway,
		Template: "apply-kegDataPlane.yaml",
		Own:      own("keg_data_plane_name", name, "keg_data_plane_namespace", ns),
		Refs:     []ref{gatewayRef()},
	}
}
