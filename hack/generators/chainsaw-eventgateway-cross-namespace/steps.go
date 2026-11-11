package main

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
)

const (
	suiteTemplates  = "../../_step_templates/"
	commonTemplates = "../../../common/_step_templates/"
)

type gatewayType string

const (
	konnect gatewayType = "konnect"
	onprem  gatewayType = "onprem"
)

// grant is a KongReferenceGrant.
type grant struct {
	name, namespace, description string
	from, to                     []map[string]string
}

// grantKey is what a KongReferenceGrant allows: objects of a kind of a
// namespace to reference objects of a kind of the namespace of the grant, the
// one named toName, or all of them if empty.
type grantKey struct{ fromKind, fromNS, toKind, toNS, toName string }

// keyOf is the key of the grants letting src reference every object of the
// kind of dst.
func keyOf(src, dst *object) grantKey {
	return grantKey{src.Kind, src.NS, dst.grantTo()["kind"], dst.NS, ""}
}

type steps struct {
	gw    gatewayType
	steps []string
	// granted counts the live grants allowing each grantKey: a grant allows
	// a kind of a namespace, not an object, so a scenario can't check a
	// reference an earlier grant allows.
	granted map[grantKey]int
	// grantNames are the names of the grants applied so far, by namespace:
	// applying a grant of the same name replaces the first one.
	grantNames map[[2]string]bool
	// heldBack tells, on-prem, the last assertion on the gateway was that it
	// doesn't push its configuration: only then does asserting it pushes it
	// again tell anything.
	heldBack bool
	// witnesses counts the backend clusters created to tell the gateway
	// pushes its configuration (see assertGatewayNotHeldBack).
	witnesses int
	// prefix starts the description of the steps: the scenario and the
	// reference they check.
	prefix string
}

func newSteps(gw gatewayType) *steps {
	return &steps{gw: gw, granted: map[grantKey]int{}, grantNames: map[[2]string]bool{}}
}

func (s *steps) use(name, description, template string, b []binding) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "    - name: %d-%s\n      description: %s\n      use:\n        template: %s\n",
		len(s.steps)+1, name, strconv.Quote(s.prefix+description), template)
	if len(b) > 0 {
		sb.WriteString("      bindings:\n")
		for _, kv := range b {
			fmt.Fprintf(&sb, "        - name: %s\n", kv.Name)
			if kv.List == nil {
				fmt.Fprintf(&sb, "          value: %s\n", kv.Value)
				continue
			}
			sb.WriteString("          value:\n")
			for _, entry := range kv.List {
				keys := make([]string, 0, len(entry))
				for k := range entry {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(i, j int) bool { return keyOrder(keys[i]) < keyOrder(keys[j]) })
				for i, k := range keys {
					prefix := "  "
					if i == 0 {
						prefix = "- "
					}
					fmt.Fprintf(&sb, "            %s%s: %s\n", prefix, k, entry[k])
				}
			}
		}
	}
	s.steps = append(s.steps, sb.String())
}

func keyOrder(k string) int {
	return strings.Index("group kind namespace name", k)
}

func objBindings(o *object) []binding {
	return own("object_api_version", o.apiVersion(), "object_kind", o.Kind, "object_name", o.Name, "object_namespace", o.NS)
}

func (s *steps) create(o *object) {
	s.use("create-"+slug(o.Kind)+"-"+o.Name, "Create the "+o.Kind+" "+o.Name+".", suiteTemplates+o.Template, o.bindings())
}

// update applies o again, e.g. with another target of a reference.
func (s *steps) update(o *object, name, description string) {
	s.use(name, description, suiteTemplates+o.Template, o.bindings())
}

func (s *steps) assertCondition(name, description string, o *object, condType, status, reason string) {
	b := append(objBindings(o), own("condition_type", condType, "condition_status", strconv.Quote(status))...)
	if reason == "" {
		s.use(name, description, suiteTemplates+"assert-condition-status.yaml", b)
		return
	}
	s.use(name, description, suiteTemplates+"assert-condition.yaml", append(b, binding{Name: "condition_reason", Value: reason}))
}

// condition is a condition an object reports.
type condition struct{ typ, status, reason string }

func (s *steps) assertCond(name, description string, o *object, c condition) {
	s.assertCondition(name, description, o, c.typ, c.status, c.reason)
}

// assertNotProgrammed asserts the object reports c and isn't programmed, the
// condition absent or False: c is the outcome of the reconcile that decided
// not to program it, so both hold once c does.
func (s *steps) assertNotProgrammed(name, description string, o *object, c condition) {
	s.use(name, description, suiteTemplates+"assert-condition-not-programmed.yaml",
		append(objBindings(o), own("condition_type", c.typ, "condition_status", strconv.Quote(c.status), "condition_reason", c.reason)...))
}

func (s *steps) assertNotReason(name, description string, o *object, condType, status, notReason string) {
	s.use(name, description, suiteTemplates+"assert-condition-not-reason.yaml",
		append(objBindings(o), own("condition_type", condType, "condition_status", strconv.Quote(status), "condition_not_reason", notReason)...))
}

func (s *steps) assertAbsent(name, description string, o *object, condType string) {
	s.use(name, description, suiteTemplates+"assert-condition-absent.yaml", append(objBindings(o), own("condition_type", condType)...))
}

func (s *steps) assertMessage(name, description string, o *object, c condition, messagePart string) {
	s.use(name, description, suiteTemplates+"assert-condition-message.yaml",
		append(objBindings(o), own("condition_type", c.typ, "condition_status", strconv.Quote(c.status), "condition_reason", c.reason, "message_part", messagePart)...))
}

// ready asserts the object is programmed, or, for a data plane, that it
// resolves its gateway.
func (s *steps) ready(name, description string, o *object) {
	switch {
	case o.Kind == "KegDataPlane":
		s.assertCondition(name, description, o, s.resolvedCondition(), "True", "")
	case s.gw == konnect && o.Kind == "EventGatewayVirtualClusterPolicyChain":
		// A cluster policy chain is a setting of its virtual cluster in
		// Konnect, not an entity of its own: it has no Konnect ID.
		s.assertCondition(name, description, o, "Programmed", "True", "")
	case s.gw == konnect:
		s.use(name, description, suiteTemplates+"assert-programmed-konnect.yaml", objBindings(o))
	default:
		s.assertCondition(name, description, o, "Programmed", "True", "")
	}
}

// restored asserts the object is programmed again once allowed what t
// references: against Konnect, an object denied a reference keeps being
// programmed, its condition reporting the denial only, so that condition must
// be resolved again too.
func (s *steps) restored(name, description string, t refType, o *object) {
	s.ready(name, description, o)
	if s.gw != konnect || o.Kind == "KegDataPlane" {
		return
	}
	d := s.denied(t, o)
	s.assertCondition(name+"-resolved", "The "+d.typ+" condition of the "+o.Kind+" is resolved again.", o, d.typ, "True", "")
}

// resolvedLocally asserts the object, denied a reference, is resolved once
// the reference is to an object of its namespace: programmed, and the
// condition that reported the denial no longer False. Against Konnect, the
// controllers stop reporting ResolvedRefs for references within the
// namespace, so the condition may be gone rather than True.
func (s *steps) resolvedLocally(name, description string, t refType, o *object) {
	s.ready(name, description, o)
	if s.gw != konnect || o.Kind == "KegDataPlane" {
		return
	}
	d := s.denied(t, o)
	s.use(name+"-resolved", "The "+d.typ+" condition of the "+o.Kind+" no longer reports the denial.", suiteTemplates+"assert-condition-not-false.yaml",
		append(objBindings(o), own("condition_type", d.typ)...))
}

func (s *steps) resolvedCondition() string {
	if s.gw == konnect {
		return "KonnectEventGatewayResolved"
	}
	return "OnPremEventGatewayResolved"
}

// denied is the condition of an object a KongReferenceGrant doesn't let
// reference another one.
func (s *steps) denied(t refType, o *object) condition {
	switch {
	case o.Kind == "KegDataPlane":
		return condition{s.resolvedCondition(), "False", "RefNotPermitted"}
	case s.gw == onprem && t == refSibling:
		// As against Konnect, where the condition of the references to
		// other entities than the parent reports it.
		return condition{"Programmed", "False", "ReferenceNotPermitted"}
	case s.gw == onprem:
		return condition{"Programmed", "False", "RefNotPermitted"}
	case t == refSibling:
		return condition{"KonnectReferencesResolved", "False", "ReferenceNotPermitted"}
	default:
		return condition{"ResolvedRefs", "False", "RefNotPermitted"}
	}
}

var onPremGateway = &object{Kind: "OnPremEventGateway", Name: "($gateway_name)", NS: nsGateway, Group: groupEventGateway}

// assertGateway asserts, on-prem, whether an object of the gateway a
// KongReferenceGrant doesn't let reference another holds back its
// configuration: a missing grant to the parent only leaves the object out, as
// it may not belong to the gateway, while one to another entity or to a
// Secret stops the push, as the object belongs to the gateway and would be
// applied without what it references.
func (s *steps) assertGateway(name string, t refType, o *object) {
	if s.gw != onprem || o.Kind == "KegDataPlane" {
		return
	}
	if t == refParent {
		s.assertGatewayNotHeldBack(name+"-gateway-not-held-back",
			"An object not granted its parent doesn't belong to the gateway: it is only left out, and the gateway still pushes its configuration.")
		return
	}
	if o.Kind == "EventGatewayDataPlaneCertificate" {
		// A certificate left out never holds back the configuration:
		// gw-admin only stops trusting it, which is how one is revoked.
		s.assertGatewayNotHeldBack(name+"-gateway-not-held-back",
			"A data plane certificate left out only stops being trusted: the gateway still pushes its configuration.")
		return
	}
	s.assertGatewayHeldBack(name+"-gateway-held-back",
		"An object of the gateway not granted what it references can't be sent: the gateway doesn't push its configuration.", o)
}

// assertGatewayNotHeldBack asserts, on-prem, the gateway still pushes its
// configuration. The gateway was already programmed, so its own condition
// can't tell a push happened since: a backend cluster created after the
// change, a witness, is programmed only once a push including it succeeds,
// with what the change left out.
func (s *steps) assertGatewayNotHeldBack(name, description string) {
	s.heldBack = false
	s.witnesses++
	w := backendCluster(fmt.Sprintf("witness-%d", s.witnesses), nsGateway)
	s.use("create-"+w.Name, "Create "+w.Name+", a backend cluster of the gateway used as a witness: it is programmed only once a push including it succeeds.", suiteTemplates+w.Template, w.bindings())
	s.assertCondition(name, description+" "+w.Name+", created after it, is pushed.", w, "Programmed", "True", "Programmed")
	s.deleteObject("delete-"+w.Name, "Delete "+w.Name+".", w)
}

// assertGatewayHeldBack asserts, on-prem, the gateway doesn't push its
// configuration, an object of it, o, can't be sent.
func (s *steps) assertGatewayHeldBack(name, description string, o *object) {
	s.heldBack = true
	reason := "InvalidEntities"
	if o.Kind == "EventGatewayVirtualClusterPolicyChain" {
		// A chain holds back the configuration as the owner of its virtual
		// cluster, with a reason of its own.
		reason = ""
	}
	s.assertCondition(name, description, onPremGateway, "Programmed", "False", reason)
}

// assertGatewayPushes asserts, on-prem, the gateway pushes its configuration
// again, after it was held back.
func (s *steps) assertGatewayPushes(name string) {
	if s.gw != onprem || !s.heldBack {
		return
	}
	s.heldBack = false
	s.assertCondition(name, "The gateway pushes its configuration again.", onPremGateway, "Programmed", "True", "Programmed")
}

func (s *steps) applyGrant(g grant, name, description string) {
	s.use(name, description, commonTemplates+"apply-assert-kongreferencegrant.yaml", []binding{
		{Name: "kong_reference_grant_name", Value: g.name},
		{Name: "kong_reference_grant_namespace", Value: g.namespace},
		{Name: "from", List: g.from},
		{Name: "to", List: g.to},
	})
}

// grant applies a grant, recording what it allows.
func (s *steps) grant(g grant) {
	k := [2]string{g.namespace, g.name}
	if s.grantNames[k] {
		panic(fmt.Sprintf("grant %s in %s applied twice", g.name, g.namespace))
	}
	s.grantNames[k] = true
	s.allow(g)
	s.applyGrant(g, "grant-"+g.name, g.description)
}

// restore applies again a grant deleted.
func (s *steps) restore(g grant, description string) {
	s.allow(g)
	s.applyGrant(g, "restore-"+g.name, description)
}

// allow records what a grant allows.
func (s *steps) allow(g grant) {
	for _, f := range g.from {
		s.granted[g.key(f)]++
	}
}

// key is what the grant lets objects of its from entry f reference: every
// object of its kind, or the one it names. A test has a single gateway: a
// grant naming it allows every reference to a gateway.
func (g grant) key(f map[string]string) grantKey {
	k := grantKey{f["kind"], f["namespace"], g.to[0]["kind"], g.namespace, ""}
	if !g.allowsKind() {
		k.toName = g.to[0]["name"]
	}
	return k
}

// revoke deletes a grant: what it allows is no longer allowed, unless another
// live grant allows it too.
func (s *steps) revoke(g grant, name, description string) {
	for _, f := range g.from {
		k := g.key(f)
		if s.granted[k]--; s.granted[k] <= 0 {
			delete(s.granted, k)
		}
	}
	s.deleteObject(name, description, &object{Kind: "KongReferenceGrant", Name: g.name, NS: g.namespace, Group: groupConfiguration})
}

func (s *steps) deleteObject(name, description string, o *object) {
	s.use(name, description, suiteTemplates+"delete-and-wait.yaml", objBindings(o))
}

// deleteNoWait deletes an object without waiting for it to be gone, e.g. one
// a finalizer holds.
func (s *steps) deleteNoWait(name, description string, o *object) {
	resource := strings.ToLower(o.Kind) + "s." + o.Group
	s.use(name, description, suiteTemplates+"delete-no-wait.yaml",
		own("object_resource", resource, "object_name", o.Name, "object_namespace", o.NS))
}

// assertFinalizer asserts an object has a finalizer, and whether it is being
// deleted.
func (s *steps) assertFinalizer(name, description string, o *object, finalizer string, beingDeleted bool) {
	s.use(name, description, suiteTemplates+"assert-finalizer.yaml",
		append(objBindings(o), own("finalizer", finalizer, "being_deleted", strconv.FormatBool(beingDeleted))...))
}

func (s *steps) assertGone(name, description string, o *object) {
	s.use(name, description, suiteTemplates+"assert-gone.yaml", objBindings(o))
}

// mustNotBeGranted panics if a live grant lets the objects of k reference
// the one named name: a scenario couldn't then check the reference.
func (s *steps) mustNotBeGranted(k grantKey, name, what string) {
	named := k
	named.toName = name
	if s.granted[k] > 0 || s.granted[named] > 0 {
		panic(fmt.Sprintf("%s: an earlier KongReferenceGrant already allows %v", what, named))
	}
}

// grantFor is the grant letting src reference dst. It is named after src: two
// objects of a scenario can need a grant in the same namespace to the same
// kind, e.g. a child policy and its parent to their virtual cluster.
func grantFor(src, dst *object) grant {
	from := []map[string]string{{"group": src.Group, "kind": src.Kind, "namespace": src.NS}}
	if src.Kind == "KegDataPlane" {
		// The certificate the operator creates for the data plane, in its
		// namespace, references the gateway too.
		from = append(from, map[string]string{"group": groupConfiguration, "kind": "EventGatewayDataPlaneCertificate", "namespace": src.NS})
	}
	return grant{
		name: src.Name + "-to-" + targetSlug(dst), namespace: dst.NS,
		from: from, to: []map[string]string{dst.grantTo()},
		description: "Let the " + src.Kind + " " + src.Name + " reference " + dst.what() + " in another namespace.",
	}
}

func targetSlug(o *object) string {
	switch {
	case o.isGateway():
		return "gateway"
	case o.Kind == "Secret":
		return "secrets"
	default:
		return slug(o.Kind)
	}
}

// allowsKind tells the grant lets every object of its namespace of the kind
// it allows be referenced: it names none, or it names the gateway, the only
// one of a test.
func (g grant) allowsKind() bool {
	return g.to[0]["name"] == "" || g.to[0]["kind"] == gateway.grantTo()["kind"]
}

// narrowed returns the grant naming its target: it allows only that object
// of its kind.
func (g grant) narrowed(dst *object) grant {
	to := map[string]string{}
	maps.Copy(to, g.to[0])
	to["name"] = dst.Name
	g.to = []map[string]string{to}
	g.description += " The KongReferenceGrant names it: it allows that object only."
	return g
}

// wrongGrants are grants close to the one letting src reference dst, which
// must not let it: from another kind, another group or another namespace, in
// another namespace than the one of dst, to another kind, or to another object
// of the kind of dst.
func wrongGrants(src, dst *object) []grant {
	right := grantFor(src, dst)
	otherKind := "EventGatewayListener"
	if src.Kind == otherKind {
		otherKind = "EventGatewayStaticKey"
	}
	otherName := map[string]string{}
	maps.Copy(otherName, dst.grantTo())
	otherName["name"] = "not-" + targetSlug(dst)
	nearMiss := "A near-miss KongReferenceGrant, which must not let the " + src.Kind + " " + src.Name + " reference " + dst.what() + ": "
	return []grant{
		{name: src.Name + "-other-kind", namespace: dst.NS, to: right.to,
			from:        []map[string]string{{"group": groupConfiguration, "kind": otherKind, "namespace": src.NS}},
			description: nearMiss + "it is from the " + otherKind + "s of its namespace."},
		{name: src.Name + "-other-group", namespace: dst.NS, to: right.to,
			// No validation rule restricts the kinds of this group.
			from:        []map[string]string{{"group": "konnect.konghq.com", "kind": src.Kind, "namespace": src.NS}},
			description: nearMiss + "it is from its kind in the group konnect.konghq.com."},
		{name: src.Name + "-other-namespace", namespace: dst.NS, to: right.to,
			from:        []map[string]string{{"group": src.Group, "kind": src.Kind, "namespace": nsThird}},
			description: nearMiss + "it is from its kind in the third namespace."},
		{name: src.Name + "-in-own-namespace", namespace: src.NS, to: right.to, from: right.from,
			description: nearMiss + "it is in the namespace of the " + src.Kind + ", not in the one of what it references."},
		{name: src.Name + "-to-other-kind", namespace: dst.NS, to: []map[string]string{otherTargetKind(dst)}, from: right.from,
			description: nearMiss + "it allows another kind to be referenced."},
		{name: src.Name + "-other-name", namespace: dst.NS, to: []map[string]string{otherName}, from: right.from,
			description: nearMiss + "it names another object, " + otherName["name"] + "."},
	}
}

// otherTargetKind is the "to" entry of a KongReferenceGrant to another kind
// than the one of o, which the validation rules of the grant accept.
func otherTargetKind(o *object) map[string]string {
	switch {
	case o.isGateway():
		return map[string]string{"group": "($gateway_group)", "kind": "($gateway_other_kind)"}
	case o.Kind == "Secret":
		// The core group allows Secrets only.
		return map[string]string{"group": groupConfiguration, "kind": "EventGatewayStaticKey"}
	case o.Kind == "EventGatewayListener":
		return map[string]string{"group": groupConfiguration, "kind": "EventGatewayStaticKey"}
	default:
		return map[string]string{"group": groupConfiguration, "kind": "EventGatewayListener"}
	}
}

func slug(kind string) string {
	var sb strings.Builder
	for i, r := range kind {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				sb.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		sb.WriteRune(r)
	}
	return strings.ReplaceAll(strings.ReplaceAll(sb.String(), "event-gateway-", ""), "t-l-s", "tls")
}

// crossing returns the references of objs crossing a namespace, one per grant
// they need.
func crossing(objs []*object) [][2]*object {
	seen := map[grantKey]bool{}
	var out [][2]*object
	for _, o := range objs {
		for _, r := range o.Refs {
			if o.NS == r.Target.NS {
				continue
			}
			k := keyOf(o, r.Target)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, [2]*object{o, r.Target})
		}
	}
	return out
}
