package main

// placement is where the objects of a scenario are: the referrer in rns, what
// it references in tns, and what that references in turn in zns.
type placement struct{ rns, tns, zns string }

// built is the objects of a scenario, in creation order.
type built struct {
	objs     []*object
	referrer *object
	// target is what the referrer references through the axis.
	target *object
	// hop2 is the reference of the target to an object of the third
	// namespace (scenario C), nil when the axis has none.
	hop2 *ref
}

// axis is a cross-namespace reference of a resource.
type axis struct {
	// name is short, used in the names of the objects.
	name string
	// what describes the target, e.g. "its virtual cluster".
	what  string
	build func(tag string, p placement, pt *ports) built
	// switchable tells the reference can be switched to another target of
	// the namespace of the referrer (scenario S): not a parent reference,
	// which Konnect binds the entity to, nor one of a policy to another one,
	// which must be attached to the same virtual cluster.
	switchable bool
}

type resource struct {
	dir, kind string
	axes      []axis
	// konnectSkip lists axes not tested against Konnect, as another test
	// covers them.
	konnectSkip map[string]string
	// tls tells the tests create the TLS Secrets.
	tls bool
	// held tells the on-prem test runs scenario H.
	held bool
	// konnectOnly tells the resource has no test against an
	// OnPremEventGateway.
	konnectOnly bool
}

// hop2 returns the reference of target to obj, for scenario C, when target is
// in another namespace than the one of the gateway.
func hop2Of(target *object, i int, p placement) *ref {
	if p.tns == nsGateway {
		return nil
	}
	r := target.Refs[i]
	return &r
}

func gatewayAxis(newObj func(tag, ns string) *object) axis {
	return axis{name: "gw", what: "its gateway", build: func(tag string, p placement, _ *ports) built {
		o := newObj(tag, p.rns)
		return built{objs: []*object{o}, referrer: o, target: gateway}
	}}
}

func secretAxis(newObj func(tag, ns, secretNS string) *object) axis {
	return secretAxisOf("the Secret of its value", newObj, 1)
}

// secretAxisOf is the reference of an object to a Secret, its reference at
// index i.
func secretAxisOf(what string, newObj func(tag, ns, secretNS string) *object, i int) axis {
	return axis{name: "sec", what: what, switchable: true, build: func(tag string, p placement, _ *ports) built {
		o := newObj(tag, p.rns, p.tns)
		return built{objs: []*object{o}, referrer: o, target: o.Refs[i].Target}
	}}
}

// gatewayChildResource is a resource attached to the gateway, reading a
// Secret.
func gatewayChildResource(dir, kind, prefix string, newObj func(name, ns, secretNS string) *object, tls bool) resource {
	return resource{dir: dir, kind: kind, tls: tls, axes: []axis{
		gatewayAxis(func(tag, ns string) *object { return newObj(prefix+"-"+tag, ns, ns) }),
		secretAxis(func(tag, ns, sns string) *object { return newObj(prefix+"-"+tag, ns, sns) }),
	}}
}

// vcParentAxis is the reference of a policy to its virtual cluster: the
// virtual cluster in the target namespace, its backend cluster in the
// namespace of the gateway.
func vcParentAxis(newObj func(tag, ns string, v *object) *object) axis {
	return axis{name: "vc", what: "its virtual cluster", build: func(tag string, p placement, _ *ports) built {
		b := backendCluster("backend-"+tag, nsGateway)
		v := virtualCluster("vc-"+tag, p.tns, b)
		o := newObj(tag, p.rns, v)
		return built{objs: []*object{b, v, o}, referrer: o, target: v, hop2: hop2Of(v, 0, p)}
	}}
}

// policySiblingAxis is the reference of a policy to an entity of the gateway
// reading a Secret: the policy and its virtual cluster in the referrer
// namespace, the entity in the target one, its Secret in the third.
func policySiblingAxis(name, what string, makeTarget func(tag, ns, secretNS string) *object, makeReferrer func(tag, ns string, v, t *object) *object) axis {
	return axis{name: name, what: what, switchable: true, build: func(tag string, p placement, _ *ports) built {
		b := backendCluster("backend-"+tag, p.rns)
		v := virtualCluster("vc-"+tag, p.rns, b)
		sns := p.tns
		if p.tns != nsGateway {
			sns = p.zns
		}
		t := makeTarget(tag, p.tns, sns)
		o := makeReferrer(tag, p.rns, v, t)
		return built{objs: []*object{b, v, t, o}, referrer: o, target: t, hop2: hop2Of(t, 1, p)}
	}}
}

// parentPolicyAxis is the reference of a child policy to its parent: the
// child in the referrer namespace, its parent in the target one, both of a
// virtual cluster of the third.
func parentPolicyAxis(side string) axis {
	return axis{name: "parent", what: "its parent policy", build: func(tag string, p placement, _ *ports) built {
		vns := nsGateway
		if p.tns != nsGateway {
			vns = p.zns
		}
		b := backendCluster("backend-"+tag, vns)
		v := virtualCluster("vc-"+tag, vns, b)
		r := schemaRegistry("registry-"+tag, p.tns, p.tns)
		parent := schemaValidation(side, "validate-"+tag, p.tns, v, r)
		o := childPolicy(side, "validated-"+tag, p.rns, v, parent)
		return built{objs: []*object{b, v, r, parent, o}, referrer: o, target: parent, hop2: hop2Of(parent, 0, p)}
	}}
}

// childSiblingAxis is the reference of a child policy to an entity of the
// gateway reading a Secret: the policy, its schemaValidation parent and their
// virtual cluster in the referrer namespace, the entity in the target one,
// its Secret in the third.
func childSiblingAxis(side, name, what string, makeTarget func(tag, ns, secretNS string) *object, makeReferrer func(tag, ns string, v, parent, t *object) *object) axis {
	return axis{name: name, what: what, switchable: true, build: func(tag string, p placement, _ *ports) built {
		b := backendCluster("backend-"+tag, p.rns)
		v := virtualCluster("vc-"+tag, p.rns, b)
		pr := schemaRegistry("parent-registry-"+tag, p.rns, p.rns)
		parent := schemaValidation(side, "validate-"+tag, p.rns, v, pr)
		sns := p.tns
		if p.tns != nsGateway {
			sns = p.zns
		}
		t := makeTarget(tag, p.tns, sns)
		o := makeReferrer(tag, p.rns, v, parent, t)
		return built{objs: []*object{b, v, pr, parent, t, o}, referrer: o, target: t, hop2: hop2Of(t, 1, p)}
	}}
}

func listenerPolicyObjects(tag string, rns, lns, tbNS, tbSecretNS, secretNS string, pt *ports) (*object, *object, *object) {
	l := listener("listener-"+tag, lns, pt)
	tb := tlsTrustBundle("client-ca-"+tag, tbNS, tbSecretNS)
	return l, tb, listenerPolicy("mtls-"+tag, rns, l, secretNS, tb)
}

func resources() []resource {
	res := []resource{
		{dir: "backendcluster", kind: "EventGatewayBackendCluster", tls: true, axes: []axis{
			gatewayAxis(func(tag, ns string) *object { return backendCluster("backend-"+tag, ns) }),
			secretAxisOf("the Secret of its client certificate", func(tag, ns, sns string) *object { return backendClusterTLS("backend-"+tag, ns, sns) }, 1),
		}},
		// The deprecated gatewayRef of a backend cluster, as of the other
		// entities of a gateway: only of a KonnectEventGateway. A test of its
		// own, so that the grant of the backend clusters of the namespace to
		// the gateway of the other test doesn't allow it.
		{dir: "backendcluster-gatewayref", kind: "EventGatewayBackendCluster", konnectOnly: true, axes: []axis{
			gatewayAxis(func(tag, ns string) *object { return backendClusterGatewayRef("backend-"+tag, ns) }),
		}},
		{dir: "virtualcluster", kind: "EventGatewayVirtualCluster", axes: []axis{
			{name: "bc", what: "its backend cluster", build: func(tag string, p placement, _ *ports) built {
				b := backendCluster("backend-"+tag, p.tns)
				v := virtualCluster("vc-"+tag, p.rns, b)
				return built{objs: []*object{b, v}, referrer: v, target: b, hop2: hop2Of(b, 0, p)}
			}},
		}},
		{dir: "listener", kind: "EventGatewayListener", axes: []axis{
			{name: "gw", what: "its gateway", build: func(tag string, p placement, pt *ports) built {
				o := listener("listener-"+tag, p.rns, pt)
				return built{objs: []*object{o}, referrer: o, target: gateway}
			}},
		}},
		{dir: "listenerpolicy", kind: "EventGatewayListenerPolicy", tls: true, axes: []axis{
			{name: "lst", what: "its listener", build: func(tag string, p placement, pt *ports) built {
				l, tb, o := listenerPolicyObjects(tag, p.rns, p.tns, p.rns, p.rns, p.rns, pt)
				return built{objs: []*object{l, tb, o}, referrer: o, target: l, hop2: hop2Of(l, 0, p)}
			}},
			{name: "sec", what: "the Secret of its server certificate", switchable: true, build: func(tag string, p placement, pt *ports) built {
				l, tb, o := listenerPolicyObjects(tag, p.rns, p.rns, p.rns, p.rns, p.tns, pt)
				return built{objs: []*object{l, tb, o}, referrer: o, target: o.Refs[1].Target}
			}},
			{name: "tb", what: "its TLS trust bundle", switchable: true, build: func(tag string, p placement, pt *ports) built {
				sns := p.tns
				if p.tns != nsGateway {
					sns = p.zns
				}
				l, tb, o := listenerPolicyObjects(tag, p.rns, p.rns, p.tns, sns, p.rns, pt)
				return built{objs: []*object{l, tb, o}, referrer: o, target: tb, hop2: hop2Of(tb, 1, p)}
			}},
		}},
		{dir: "virtualclusterpolicy", kind: "EventGatewayVirtualClusterPolicy", axes: []axis{
			vcParentAxis(func(tag, ns string, v *object) *object { return acls("acls-"+tag, ns, v) }),
		}},
	}
	for _, side := range []string{"produce", "consume"} {
		axes := []axis{vcParentAxis(func(tag, ns string, v *object) *object { return modifyHeaders(side, "headers-"+tag, ns, v) })}
		if side == "produce" {
			axes = append(axes, policySiblingAxis("key", "the static key it encrypts with",
				func(tag, ns, sns string) *object { return staticKey("key-"+tag, ns, sns) },
				func(tag, ns string, v, k *object) *object { return encrypt("encrypt-"+tag, ns, v, k) }),
				childSiblingAxis(side, "fkey", "the static key it encrypts fields with",
					func(tag, ns, sns string) *object { return staticKey("key-"+tag, ns, sns) },
					func(tag, ns string, v, parent, k *object) *object {
						return encryptFields("encrypt-fields-"+tag, ns, v, parent, k)
					}))
		}
		// The schema registry of a transcode policy, by its schema source:
		// Avro on the produce side, JSON on the consume one, the formats
		// resolving their schema registry alike.
		// Konnect requires the schema registry of a transcode policy to be of
		// its output format.
		schemaType := "json"
		if side == "produce" {
			schemaType = "avro"
		}
		axes = append(axes, childSiblingAxis(side, "trc", "the schema registry it transcodes with",
			func(tag, ns, sns string) *object { return schemaRegistryOf("registry-"+tag, ns, sns, schemaType) },
			func(tag, ns string, v, parent, r *object) *object {
				return transcode(side, "transcode-"+tag, ns, v, parent, r)
			}))
		axes = append(axes, policySiblingAxis("reg", "the schema registry it validates with",
			func(tag, ns, sns string) *object { return schemaRegistry("registry-"+tag, ns, sns) },
			func(tag, ns string, v, r *object) *object { return schemaValidation(side, "validate-"+tag, ns, v, r) }),
			parentPolicyAxis(side))
		res = append(res, resource{dir: side + "policy", kind: sideKind(side), axes: axes})
	}

	res = append(res, resource{dir: "policychain", kind: "EventGatewayVirtualClusterPolicyChain", axes: []axis{
		{name: "vc", what: "its virtual cluster", build: func(tag string, p placement, _ *ports) built {
			b := backendCluster("backend-"+tag, nsGateway)
			v := virtualCluster("vc-"+tag, p.tns, b)
			// The policy beside the chain: the policy axis checks the
			// reference of the chain to a policy of another namespace.
			pol := acls("acls-"+tag, p.rns, v)
			o := policyChain("chain-"+tag, p.rns, v, pol)
			return built{objs: []*object{b, v, pol, o}, referrer: o, target: v, hop2: hop2Of(v, 0, p)}
		}},
		{name: "pol", what: "the cluster policy it orders", build: func(tag string, p placement, _ *ports) built {
			vns := nsGateway
			if p.tns != nsGateway {
				vns = p.zns
			}
			b := backendCluster("backend-"+tag, vns)
			v := virtualCluster("vc-"+tag, vns, b)
			pol := acls("acls-"+tag, p.tns, v)
			o := policyChain("chain-"+tag, p.rns, v, pol)
			return built{objs: []*object{b, v, pol, o}, referrer: o, target: pol, hop2: hop2Of(pol, 0, p)}
		}},
	}})

	res = append(res, resource{dir: "listenerpolicychain", kind: "EventGatewayListenerPolicyChain", tls: true, axes: []axis{
		{name: "lst", what: "its listener", build: func(tag string, p placement, pt *ports) built {
			// The policy beside the chain: the policy axis checks the
			// reference of the chain to a policy of another namespace.
			l, tb, pol := listenerPolicyObjects(tag, p.rns, p.tns, p.rns, p.rns, p.rns, pt)
			o := listenerPolicyChain("chain-"+tag, p.rns, l, pol)
			return built{objs: []*object{l, tb, pol, o}, referrer: o, target: l, hop2: hop2Of(l, 0, p)}
		}},
		{name: "pol", what: "the listener policy it orders", build: func(tag string, p placement, pt *ports) built {
			lns := nsGateway
			if p.tns != nsGateway {
				lns = p.zns
			}
			l, tb, pol := listenerPolicyObjects(tag, p.tns, lns, p.tns, p.tns, p.tns, pt)
			o := listenerPolicyChain("chain-"+tag, p.rns, l, pol)
			return built{objs: []*object{l, tb, pol, o}, referrer: o, target: pol, hop2: hop2Of(pol, 0, p)}
		}},
	}})

	statickey := gatewayChildResource("statickey", "EventGatewayStaticKey", "key", staticKey, false)
	// eventgateway/statickey-secretref-cross-namespace covers the Secret of a
	// static key against Konnect.
	statickey.konnectSkip = map[string]string{"sec": "eventgateway/statickey-secretref-cross-namespace"}
	statickey.held = true
	res = append(res, statickey,
		gatewayChildResource("schemaregistry", "EventGatewaySchemaRegistry", "registry", schemaRegistry, false),
		gatewayChildResource("tlstrustbundle", "EventGatewayTLSTrustBundle", "client-ca", tlsTrustBundle, true),
		gatewayChildResource("dataplanecertificate", "EventGatewayDataPlaneCertificate", "certificate", dataPlaneCertificate, true),
		resource{dir: "kegdataplane", kind: "KegDataPlane", axes: []axis{
			gatewayAxis(func(tag, ns string) *object { return kegDataPlane("keg-"+tag, ns) }),
		}},
	)
	return res
}
