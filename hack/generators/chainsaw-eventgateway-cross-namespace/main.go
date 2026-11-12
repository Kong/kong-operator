// Command chainsaw-eventgateway-cross-namespace generates the chainsaw suite
// test/e2e/chainsaw/eventgateway-cross-namespace: for each Event Gateway
// resource, a test against a KonnectEventGateway and one against an
// OnPremEventGateway, checking each cross-namespace reference of the resource
// (an axis) in the scenarios of the other *_cross-namespace tests:
//
//   - A: the resource and what it references in the namespace of the gateway,
//     the references without namespace, then naming it: no KongReferenceGrant
//     is needed, and, against Konnect, the resource reports no ResolvedRefs
//     condition.
//   - B: the resource in another namespace reports the missing grant; the
//     right one, naming what the resource references, lets it. Once for each
//     class of references (see wrongClass), grants close to the right one
//     (from another kind, group or namespace, in the wrong namespace, to
//     another kind or to another object) exist from before the resource until
//     the right one is revoked in D, whose reconcile sees them.
//   - C: what it references, in a second namespace, references in turn an
//     object of a third: the grant of the resource alone isn't enough; once
//     programmed, revoking the grant of the second namespace reports it
//     again.
//   - D: starting from B, deleting the grant reports it again, and creating
//     it again restores the resource.
//   - S: with the grant revoked, the reference switched to an object of the
//     namespace of the resource, of the same name, no longer needs it;
//     switched back, it does again.
//   - E: the resource is deleted while its grant is revoked. Against Konnect,
//     created again, it takes its name: its entity was deleted.
//   - N: an object of another namespace named as the resource doesn't take
//     its name: the gateway keeps names unique.
//   - H: on-prem, a static key being deleted, held while a policy uses it,
//     isn't held back by the revocation of the grant letting it read its
//     Secret.
//
// On-prem, the tests also check whether a missing grant holds back the
// configuration of the gateway.
//
// The tests don't sleep: each assertion waits for what the operator reports
// in reaction to a step. That something doesn't change is checked through
// what the same reconcile reports, or through an object created after the
// change, e.g. a backend cluster the gateway pushes only if it isn't held
// back.
//
// The tests only use step templates, of the suite and of common/.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const suiteDir = "test/e2e/chainsaw/eventgateway-cross-namespace"

func main() {
	if err := removeTests(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	wrongCovered := map[gatewayType]map[string]bool{konnect: {}, onprem: {}}
	for _, r := range resources() {
		for _, gw := range []gatewayType{konnect, onprem} {
			if gw == onprem && r.konnectOnly {
				continue
			}
			if err := write(r, gw, wrongCovered[gw]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}
}

// removeTests removes the tests generated before, all of the suite but its
// step templates, so that a test of a resource removed or renamed doesn't
// stay.
func removeTests() error {
	entries, err := os.ReadDir(suiteDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == "_step_templates" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(suiteDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// scenarios generates the steps of a resource.
type scenarios struct {
	*steps

	res   resource
	ports *ports
	// wrongCovered are the classes of references (see wrongClass) whose
	// grants close to the right one a test of the gateway type already
	// checks: the grants are matched by code shared by the references of a
	// class, so checking them once for it is enough.
	wrongCovered map[string]bool
	// wrongAxes are the axes of the test checking those grants.
	wrongAxes []string
}

// wrongClass is the class of a reference whose grants close to the right one
// are checked once: by type of reference, to the gateway or to an entity,
// and by kind for those resolving their references with code of their own.
func wrongClass(res resource, t refType, target *object) string {
	switch res.kind {
	case "KegDataPlane", "EventGatewayVirtualClusterPolicyChain":
		return res.kind
	}
	if target.isGateway() {
		return string(t) + "-gateway"
	}
	return string(t)
}

func (sc *scenarios) createAll(objs []*object) {
	for _, o := range objs {
		sc.create(o)
	}
}

// grantAll applies, before the objects, the grants of the references crossing
// a namespace other than skip: chainsaw deletes the resources of a test in
// reverse order, so the objects are deleted while their grants exist.
func (sc *scenarios) grantAll(objs []*object, skip ...grantKey) {
	for _, c := range crossing(objs) {
		k := keyOf(c[0], c[1])
		skipped := false
		for _, sk := range skip {
			skipped = skipped || sk == k
		}
		if !skipped && sc.granted[k] == 0 {
			sc.grant(grantFor(c[0], c[1]))
		}
	}
}

// cleanUp deletes, against Konnect, the objects of a scenario, children
// first, while their grants exist, so that they are deleted from Konnect.
func (sc *scenarios) cleanUp(objs []*object, tag string, deleted ...*object) {
	if sc.gw != konnect {
		return
	}
	for _, o := range slices.Backward(objs) {
		skip := false
		for _, d := range deleted {
			skip = skip || d == o
		}
		if skip {
			continue
		}
		sc.deleteObject("delete-"+tag+"-"+slug(o.Kind),
			"Delete the "+o.Kind+" "+o.Name+" while its KongReferenceGrants exist, so that it is deleted from Konnect.", o)
	}
}

// phase sets the scenario and the reference the next steps check, which
// starts their description.
func (sc *scenarios) phase(letter, what string) {
	sc.prefix = "[" + letter + ", " + what + "] "
}

// scenarioA checks references within the namespace of the gateway, without
// namespace or, explicit, naming it.
func (sc *scenarios) scenarioA(a axis, explicit bool) {
	tag := a.name + "-a"
	how := "without namespace"
	if explicit {
		tag += "x"
		how = "naming it explicitly"
	}
	sc.phase("A", a.what)
	b := a.build(tag, placement{nsGateway, nsGateway, nsGateway}, sc.ports)
	for i, o := range b.objs {
		c := *o
		c.ExplicitNS = explicit
		b.objs[i] = &c
		if o == b.referrer {
			b.referrer = &c
		}
	}
	sc.createAll(b.objs)
	sc.ready("assert-"+tag+"-programmed", "Everything is in the namespace of the gateway, the references "+how+": the "+sc.res.kind+" needs no KongReferenceGrant.", b.referrer)
	if sc.gw == konnect && b.referrer.Kind != "KegDataPlane" {
		// Only the Konnect controllers report ResolvedRefs, and not for a
		// data plane, which reports whether it resolves its gateway.
		sc.assertAbsent("assert-"+tag+"-no-resolved-refs", "References within the namespace of the "+sc.res.kind+" report no ResolvedRefs condition.", b.referrer, "ResolvedRefs")
	}
	sc.cleanUp(b.objs, tag)
}

// scenarioB runs B, then D, S and E on the same objects.
func (sc *scenarios) scenarioB(a axis) {
	tag := a.name + "-b"
	sc.phase("B", a.what)
	b := a.build(tag, placement{nsReferrer, nsGateway, nsGateway}, sc.ports)
	axisKey := keyOf(b.referrer, b.target)
	sc.mustNotBeGranted(axisKey, b.target.Name, sc.res.dir+" "+tag)
	refT := b.referrer.Refs[b.referrer.refIndex(b.target)].Type
	denied := sc.denied(refT, b.referrer)

	sc.grantAll(b.objs, axisKey)
	// The grants close to the right one exist before the resource, and stay
	// until D. That the first reconcile reports the missing grant tells
	// little: the cache may not have had them yet. The reconcile reacting to
	// the deletion of the right grant in D does: the cache receives the
	// events of the grants in order, so it has them by then.
	var wrong []grant
	if class := wrongClass(sc.res, refT, b.target); !sc.wrongCovered[class] {
		sc.wrongCovered[class] = true
		sc.wrongAxes = append(sc.wrongAxes, a.name)
		wrong = wrongGrants(b.referrer, b.target)
	}
	if len(wrong) > 0 {
		sc.phase("W", a.what)
	}
	for _, g := range wrong {
		sc.applyGrant(g, "grant-"+g.name, g.description)
	}
	sc.phase("B", a.what)
	sc.createAll(b.objs)
	sc.assertCond("assert-"+tag+"-not-permitted", "Without the right KongReferenceGrant, the "+sc.res.kind+" reports it may not reference "+a.what+".", b.referrer, denied)
	sc.assertGateway("assert-"+tag, refT, b.referrer)

	g1 := grantFor(b.referrer, b.target).narrowed(b.target)
	sc.grant(g1)
	sc.restored("assert-"+tag+"-programmed", "The KongReferenceGrant, naming "+a.what+", lets the "+sc.res.kind+" reference it.", refT, b.referrer)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes")

	sc.phase("D", a.what)
	sc.revoke(g1, "revoke-"+g1.name, "Delete the KongReferenceGrant of the "+sc.res.kind+" to "+a.what+".")
	again := "Without its KongReferenceGrant, the " + sc.res.kind + " reports it again."
	if len(wrong) > 0 {
		again += " The near-miss grants (W), created before it, are seen by the reconcile reacting to its deletion: none of them lets it."
	}
	sc.assertCond("assert-"+tag+"-not-permitted-again", again, b.referrer, denied)
	sc.assertGateway("assert-"+tag+"-revoked", refT, b.referrer)
	for _, g := range wrong {
		sc.deleteObject("delete-"+g.name, "Delete the near-miss KongReferenceGrant "+g.name+".", &object{Kind: "KongReferenceGrant", Name: g.name, NS: g.namespace, Group: groupConfiguration})
	}
	sc.restore(g1, "Create the KongReferenceGrant of the "+sc.res.kind+" to "+a.what+" again.")
	sc.restored("assert-"+tag+"-programmed-again", "The "+sc.res.kind+" is restored.", refT, b.referrer)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes-again")

	var switched []*object
	if a.switchable {
		switched = sc.scenarioS(a, b, g1, denied, refT)
	}

	sc.phase("E", a.what)
	sc.revoke(g1, "revoke-"+g1.name+"-to-delete", "Delete the KongReferenceGrant of the "+sc.res.kind+" to "+a.what+" again.")
	sc.assertCond("assert-"+tag+"-not-permitted-to-delete", "Without its KongReferenceGrant, the "+sc.res.kind+" reports it.", b.referrer, denied)
	sc.assertGateway("assert-"+tag+"-revoked-to-delete", refT, b.referrer)
	sc.deleteObject("delete-"+tag+"-"+slug(b.referrer.Kind)+"-revoked",
		"The "+sc.res.kind+" can be deleted while the KongReferenceGrant it needs is revoked.", b.referrer)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes-deleted")
	sc.restore(g1, "Create the KongReferenceGrant again, so that the other objects of the scenario are deleted with it in place.")
	if sc.gw == konnect && b.referrer.Named {
		// The controller removes its finalizer once the entity is deleted
		// from Konnect, or without deleting it: created again, with another
		// UID, the object is programmed only if the gateway has no entity of
		// its name left (see N). Deleting the gateway wouldn't tell, as
		// Konnect may delete its entities with it.
		sc.update(b.referrer, "recreate-"+tag+"-"+slug(b.referrer.Kind), "Create the "+sc.res.kind+" again, with its name.")
		sc.ready("assert-"+tag+"-recreated", "Its entity was deleted from Konnect: the "+sc.res.kind+" created again takes its name.", b.referrer)
		sc.cleanUp(switched, tag+"-switched")
		sc.cleanUp(b.objs, tag)
		return
	}
	sc.cleanUp(switched, tag+"-switched")
	sc.cleanUp(b.objs, tag, b.referrer)
}

// scenarioS switches the reference of the axis to an object of the namespace
// of the referrer, then back. The grant is revoked first: the resource,
// reporting the missing grant, is resolved again by the switch alone, so it
// no longer needs the grant.
func (sc *scenarios) scenarioS(a axis, b built, g1 grant, denied condition, refT refType) []*object {
	tag := a.name + "-s"
	sc.phase("S", a.what)
	// Built as B, in the namespace of the referrer: the target switched to
	// has the name of the one of B, so that the switch changes only the
	// namespace of the reference, which the resource must then resolve to
	// the right object, and not take the one of its namespace for it.
	s := a.build(a.name+"-b", placement{nsReferrer, nsReferrer, nsReferrer}, sc.ports)
	i := b.referrer.refIndex(b.target)
	target2 := s.referrer.Refs[i].Target
	var created []*object
	if target2.Kind != "Secret" {
		if target2.Named {
			// The gateway keeps names unique: in its API, it has another.
			target2 = target2.withAPIName(target2.Name + "-local")
		}
		created = []*object{target2}
		sc.grantAll(created)
		sc.createAll(created)
	}
	sc.revoke(g1, "revoke-"+g1.name+"-to-switch", "Delete the KongReferenceGrant of the "+sc.res.kind+" to "+a.what+".")
	sc.assertCond("assert-"+tag+"-not-permitted", "Without its KongReferenceGrant, the "+sc.res.kind+" reports it.", b.referrer, denied)
	sc.assertGateway("assert-"+tag+"-revoked", refT, b.referrer)
	switched := b.referrer.withTarget(i, target2)
	sc.update(switched, "switch-"+tag, "Switch the reference of the "+sc.res.kind+" to "+target2.what()+" of its namespace.")
	sc.resolvedLocally("assert-"+tag+"-programmed", "Referencing "+target2.what()+" of its namespace, the "+sc.res.kind+" no longer needs the KongReferenceGrant.", refT, switched)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes-switched")
	sc.update(b.referrer, "switch-back-"+tag, "Switch the reference of the "+sc.res.kind+" back to "+a.what+" of another namespace.")
	sc.assertCond("assert-"+tag+"-switched-back-not-permitted", "Referencing again "+a.what+" of another namespace, the "+sc.res.kind+" reports the missing KongReferenceGrant.", b.referrer, denied)
	sc.assertGateway("assert-"+tag+"-switched-back", refT, b.referrer)
	sc.restore(g1, "Create the KongReferenceGrant of the "+sc.res.kind+" to "+a.what+" again.")
	sc.restored("assert-"+tag+"-programmed-again", "The "+sc.res.kind+" is restored.", refT, b.referrer)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes")
	return created
}

func (sc *scenarios) scenarioC(a axis) {
	tag := a.name + "-c"
	sc.phase("C", a.what)
	b := a.build(tag, placement{nsReferrer, nsTarget, nsThird}, sc.ports)
	if b.hop2 == nil {
		return
	}
	k1, k2 := keyOf(b.referrer, b.target), keyOf(b.target, b.hop2.Target)
	sc.mustNotBeGranted(k1, b.target.Name, sc.res.dir+" "+tag)
	sc.mustNotBeGranted(k2, b.hop2.Target.Name, sc.res.dir+" "+tag)
	refT := b.referrer.Refs[b.referrer.refIndex(b.target)].Type

	sc.grantAll(b.objs, k1, k2)
	sc.createAll(b.objs)
	sc.assertCond("assert-"+tag+"-not-permitted", "Without a KongReferenceGrant, the "+sc.res.kind+" reports it may not reference "+a.what+".", b.referrer, sc.denied(refT, b.referrer))
	sc.assertGateway("assert-"+tag, refT, b.referrer)
	// The grants name what they let be referenced: another axis of the
	// resource may reference objects of the same kinds and namespaces.
	sc.grant(grantFor(b.referrer, b.target).narrowed(b.target))
	targetDenied := sc.denied(b.hop2.Type, b.target)
	sc.assertCond("assert-"+tag+"-target-not-permitted", "The "+b.target.Kind+" in a second namespace reports it may not reference "+b.hop2.Target.what()+" in a third.",
		b.target, targetDenied)
	desc := "The KongReferenceGrant of the " + sc.res.kind + " alone isn't enough: it may reference " + a.what + ", which can't be used."
	sc.assertUnusable(tag, desc, refT, b)
	g2 := grantFor(b.target, b.hop2.Target).narrowed(b.hop2.Target)
	sc.grant(g2)
	sc.restored("assert-"+tag+"-programmed", "With both KongReferenceGrants, the "+sc.res.kind+" is programmed.", refT, b.referrer)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes")

	sc.revoke(g2, "revoke-"+g2.name, "Delete the KongReferenceGrant of the "+b.target.Kind+" to "+b.hop2.Target.what()+" in the third namespace.")
	sc.assertCond("assert-"+tag+"-target-not-permitted-again", "Without its KongReferenceGrant, the "+b.target.Kind+" reports it again.", b.target, targetDenied)
	if sc.gw == onprem {
		// Against Konnect, the entity of the target stays where it is, and
		// so does the one of the resource.
		sc.assertUnusable(tag+"-again", "The "+sc.res.kind+" can't be sent without what it references.", refT, b)
	}
	sc.restore(g2, "Create the KongReferenceGrant of the "+b.target.Kind+" to "+b.hop2.Target.what()+" in the third namespace again.")
	sc.restored("assert-"+tag+"-target-programmed-again", "The "+b.target.Kind+" is restored.", b.hop2.Type, b.target)
	sc.ready("assert-"+tag+"-programmed-again", "The "+sc.res.kind+" is restored.", b.referrer)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes-again")
	sc.cleanUp(b.objs, tag)
}

// assertUnusable asserts the referrer of scenario C, allowed to reference its
// target, isn't programmed, its target not allowed what it references: for
// another reason than its own missing grant.
func (sc *scenarios) assertUnusable(tag, desc string, refT refType, b built) {
	name := "assert-" + tag + "-not-programmed"
	if sc.gw == onprem {
		sc.assertNotReason(name, desc, b.referrer, "Programmed", "False", sc.denied(refT, b.referrer).reason)
		return
	}
	// The reconcile allowed the reference finds what it references isn't
	// programmed, says so, and stops before creating the resource in
	// Konnect: once it says so, the resource isn't programmed.
	if refT == refParent {
		sc.assertCondition(name+"-resolved-refs", desc, b.referrer, "ResolvedRefs", "True", "")
		sc.assertNotProgrammed(name, "The "+sc.res.kind+" isn't programmed: its "+b.target.Kind+" isn't.", b.referrer,
			condition{b.target.Kind + "RefValid", "False", "NotProgrammed"})
		return
	}
	sc.assertNotProgrammed(name, desc+" The "+sc.res.kind+" isn't programmed.", b.referrer,
		condition{"KonnectReferencesResolved", "False", "ReferenceNotProgrammed"})
}

// finalizerInUse is the finalizer holding, on-prem, an entity policies of the
// last configuration sent use.
const finalizerInUse = "gateway-operator.konghq.com/onprem-eventgateway-in-use"

// scenarioH checks, on-prem, an entity being deleted, held while a policy
// uses it, isn't held back by the revocation of a grant it needs, e.g. as its
// namespace is deleted with its grants: left out, it would stop the push that
// drops the policy, which alone releases it.
func (sc *scenarios) scenarioH() {
	tag := "h"
	sc.phase("H", "the Secret of a static key being deleted")
	b := backendCluster("backend-"+tag, nsReferrer)
	v := virtualCluster("vc-"+tag, nsReferrer, b)
	k := staticKey("key-"+tag, nsReferrer, nsTarget)
	p := encrypt("encrypt-"+tag, nsReferrer, v, k)
	objs := []*object{b, v, k, p}
	sec := k.Refs[1].Target
	sc.mustNotBeGranted(keyOf(k, sec), sec.Name, sc.res.dir+" "+tag)
	sc.grantAll(objs, keyOf(k, sec))
	g := grantFor(k, sec).narrowed(sec)
	sc.grant(g)
	sc.createAll(objs)
	sc.ready("assert-"+tag+"-programmed", "The policy using the static key is pushed.", p)
	sc.assertFinalizer("assert-"+tag+"-key-held", "The static key the pushed policy uses has the in-use finalizer.", k, finalizerInUse, false)
	sc.deleteNoWait("delete-"+tag+"-key", "Delete the static key, still used by the policy.", k)
	sc.assertFinalizer("assert-"+tag+"-key-held-deleted", "Being deleted, the static key is still held by the in-use finalizer while the policy uses it.", k, finalizerInUse, true)
	sc.revoke(g, "revoke-"+g.name, "Delete the KongReferenceGrant letting the static key read its Secret.")
	sc.assertGatewayNotHeldBack("assert-"+tag+"-gateway-not-held-back",
		"The static key being deleted is sent as it was last, without reading its Secret: the gateway still pushes its configuration.")
	sc.deleteObject("delete-"+tag+"-policy", "Delete the policy using the static key.", p)
	sc.assertGone("assert-"+tag+"-key-gone", "The push dropping the policy releases the static key.", k)
	sc.assertCondition("assert-"+tag+"-gateway-programmed", "The gateway pushes its configuration.", onPremGateway, "Programmed", "True", "Programmed")
}

// inNamespace returns a copy of o in another namespace, its references
// unchanged.
func (o *object) inNamespace(ns string) *object {
	c := *o
	c.NS = ns
	c.Own = append([]binding(nil), o.Own...)
	for i := range c.Own {
		if c.Own[i].Value == o.NS {
			c.Own[i].Value = ns
		}
	}
	return &c
}

// scenarioN checks an object of another namespace named as the resource
// doesn't take its name.
func (sc *scenarios) scenarioN(a axis) {
	tag := a.name + "-n"
	sc.phase("N", "its name")
	chain := sc.res.dir == "policychain"
	p := placement{nsGateway, nsGateway, nsGateway}
	if chain {
		// The owner of the virtual cluster in another namespace than the
		// gateway: the other chain names it with its namespace.
		p = placement{nsReferrer, nsGateway, nsGateway}
	}
	b := a.build(tag, p, sc.ports)
	if !chain && !b.referrer.Named {
		return
	}
	sc.grantAll(b.objs)
	sc.createAll(b.objs)
	sc.ready("assert-"+tag+"-programmed", "The first "+sc.res.kind+" "+b.referrer.Name+".", b.referrer)
	otherNS := nsReferrer
	if chain {
		otherNS = nsGateway
	}
	dup := b.referrer.inNamespace(otherNS)
	for i := range dup.Own {
		if dup.Own[i].Name == "listener_port" {
			// Its own port, so that only its name conflicts.
			dup.Own[i].Value = sc.ports.take()
		}
	}
	sc.grantAll([]*object{dup})
	sc.create(dup)
	switch {
	case chain:
		sc.assertMessage("assert-"+tag+"-conflicting", "Another chain of the virtual cluster can't claim it: it names the owner with its namespace.",
			dup, condition{"PolicyChainOwnerValid", "False", "ConflictingChain"}, "(join('/', [$referrer_namespace, '"+b.referrer.Name+"']))")
	case sc.gw == onprem:
		sc.assertMessage("assert-"+tag+"-conflicting", "The "+sc.res.kind+" of another namespace named the same can't take the name.",
			dup, condition{"Programmed", "False", "Invalid"}, strconv.Quote(strconv.Quote(b.referrer.Name)))
	default:
		sc.assertCondition("assert-"+tag+"-conflicting", "The "+sc.res.kind+" of another namespace named the same can't be created in Konnect.",
			dup, "Programmed", "False", "FailedToCreate")
	}
	if sc.gw == onprem {
		// A virtual cluster is only left out, as is a chain, which holds back
		// the configuration only as the owner of its virtual cluster.
		if chain || dup.Kind == "EventGatewayVirtualCluster" {
			sc.assertGatewayNotHeldBack("assert-"+tag+"-gateway-not-held-back", "The conflicting "+sc.res.kind+" is only left out.")
		} else {
			sc.assertGatewayHeldBack("assert-"+tag+"-gateway-held-back", "The conflicting "+sc.res.kind+" can't be sent: the gateway doesn't push its configuration.", dup)
		}
	}
	// The reconcile reporting the conflict of the other one gave the name to
	// the first, which it reports first: on-prem, it stays in the
	// configuration, only not pushed while the gateway is held back.
	first := "The first " + sc.res.kind + " keeps its name."
	switch {
	case sc.gw == onprem && sc.heldBack:
		sc.assertCond("assert-"+tag+"-first-kept", first+" It stays in the configuration, which isn't pushed.", b.referrer,
			condition{"Programmed", "False", "GatewayNotProgrammed"})
	case sc.gw == onprem:
		sc.assertCond("assert-"+tag+"-first-kept", first, b.referrer, condition{"Programmed", "True", "Programmed"})
	default:
		sc.ready("assert-"+tag+"-first-kept", first, b.referrer)
	}
	sc.deleteObject("delete-"+tag+"-conflicting", "Delete the conflicting "+sc.res.kind+".", dup)
	sc.assertGatewayPushes("assert-" + tag + "-gateway-pushes")
	sc.cleanUp(b.objs, tag)
}

func write(r resource, gw gatewayType, wrongCovered map[string]bool) error {
	sc := &scenarios{steps: newSteps(gw), res: r, ports: &ports{}, wrongCovered: wrongCovered}
	if gw == konnect {
		sc.use("create-auth-secret", "Create the Secret holding the Konnect token.", commonTemplates+"apply-assert-konnectAuthSecret.yaml", nil)
		sc.use("create-konnect-api-auth", "Create the KonnectAPIAuthConfiguration.", commonTemplates+"apply-assert-konnectAPIAuthConfiguration-secretref.yaml", nil)
		sc.use("create-gateway", "Create the KonnectEventGateway, in the namespace of the test.", commonTemplates+"apply-assert-konnectEventGateway.yaml", nil)
	} else {
		sc.use("create-gateway", "Create the OnPremEventGateway and its GWAdmin, in the namespace of the test.", suiteTemplates+"apply-onpremEventGateway.yaml", nil)
	}
	for _, ns := range []struct{ binding, what string }{
		{"referrer_namespace", "the referencing objects"},
		{"target_namespace", "the referenced objects"},
		{"third_namespace", "what the referenced objects reference"},
	} {
		sc.use("create-namespace-"+strings.Split(ns.binding, "_")[0], "Create the namespace of "+ns.what+".", commonTemplates+"apply-assert-namespace.yaml",
			own("namespace_name", "($"+ns.binding+")"))
	}
	if r.tls {
		for _, ns := range []struct{ ns, what string }{{nsGateway, "test"}, {nsReferrer, "referrer"}, {nsTarget, "target"}, {nsThird, "third"}} {
			sc.use("create-tls-secrets-"+ns.what, "Create the TLS Secrets in the "+ns.what+" namespace.", suiteTemplates+"create-tls-secrets.yaml",
				own("secret_namespace", ns.ns))
		}
	}
	var axes []axis
	for _, a := range r.axes {
		if gw == konnect && r.konnectSkip[a.name] != "" {
			continue
		}
		axes = append(axes, a)
	}
	for i, a := range axes {
		if i == 0 {
			sc.scenarioA(a, false)
			sc.scenarioA(a, true)
		}
		sc.scenarioB(a)
		sc.scenarioC(a)
	}
	if len(axes) == 0 {
		return fmt.Errorf("%s: every axis is skipped against %s", r.dir, gw)
	}
	sc.scenarioN(axes[0])
	if gw == onprem && r.held {
		sc.scenarioH()
	}

	var out strings.Builder
	fmt.Fprintf(&out, "# Code generated by hack/generators/chainsaw-eventgateway-cross-namespace. DO NOT EDIT.\n#\n# Cross-namespace references of %s of %s.\n",
		article(r.kind), gatewayName(gw))
	// The label lets CI run the tests of a kind of gateway in a job of their
	// own.
	fmt.Fprintf(&out, "apiVersion: chainsaw.kyverno.io/v1alpha1\nkind: Test\nmetadata:\n  name: eventgateway-cross-namespace-%s-%s\n  labels:\n    gateway: %s\nspec:\n  description: |\n%s  bindings:\n",
		r.dir, gw, gw, describe(r, gw, axes, sc.wrongAxes))
	out.WriteString(gatewayBindings[gw])
	out.WriteString(namespaceBindings)
	out.WriteString("\n  steps:\n")
	for _, s := range sc.steps.steps {
		out.WriteString(s)
	}
	out.WriteString(catch)
	dir := filepath.Join(suiteDir, r.dir, string(gw))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "chainsaw-test.yaml"), []byte(out.String()), 0o600)
}

func article(word string) string {
	if strings.ContainsRune("AEIOU", rune(word[0])) {
		return "an " + word
	}
	return "a " + word
}

func gatewayName(gw gatewayType) string {
	if gw == konnect {
		return "a KonnectEventGateway"
	}
	return "an OnPremEventGateway"
}

func describe(r resource, gw gatewayType, axes []axis, wrongAxes []string) string {
	kind := r.kind
	lines := []string{
		"Checks how " + article(kind) + " (the object) of " + gatewayName(gw),
		"references objects of other namespaces, which KongReferenceGrants allow" + map[gatewayType]string{konnect: ",", onprem: "."}[gw],
	}
	if gw == konnect {
		lines = append(lines, "against the real Konnect API.")
	}
	lines = append(lines,
		"",
		"Namespaces: the namespace of the test holds the gateway; <test>-referrer the",
		"object whose reference is checked; <test>-target what it references;",
		"<test>-third what that references in turn.",
		"",
		"Scenarios, in the order they run; the description of each step starts with",
		"its scenario and the reference it checks, e.g. [B, "+axes[0].what+"]:",
	)
	scenario := func(letter string, text ...string) {
		lines = append(lines, "  "+letter+"  "+text[0])
		for _, t := range text[1:] {
			lines = append(lines, "     "+t)
		}
	}
	hasC, hasS := false, false
	refs := make([]string, 0, len(axes))
	for _, a := range axes {
		b := a.build("probe", placement{nsReferrer, nsTarget, nsThird}, &ports{})
		letters := []string{"B"}
		if slices.Contains(wrongAxes, a.name) {
			letters = append(letters, "W")
		}
		letters = append(letters, "D")
		if a.switchable {
			letters = append(letters, "S")
			hasS = true
		}
		letters = append(letters, "E")
		if b.hop2 != nil {
			letters = append(letters, "C")
			hasC = true
		}
		refs = append(refs, "  - "+a.what+": "+strings.Join(letters, ", "))
	}
	scenario("A", "Same namespace: with everything in the namespace of the gateway, the",
		"references with or without their namespace need no grant.")
	scenario("B", "Missing grant: in the referrer namespace, the object reports it may not",
		"reference its target, until a grant naming the target allows it.")
	if len(wrongAxes) > 0 {
		scenario("W", "Near misses, part of B: grants almost right (from another kind, group",
			"or namespace, in the wrong namespace, to another kind or to another",
			"object) don't allow it. Checked once per kind of reference in the suite.")
	}
	scenario("D", "Revoked: deleting the grant reports the missing grant again; creating it",
		"again restores the object.")
	if hasS {
		scenario("S", "Switch: the reference pointed at an object of the same name in its own",
			"namespace needs no grant; pointed back, it needs it again.")
	}
	e := []string{"Delete: the object can be deleted while its grant is revoked."}
	if gw == konnect {
		e = append(e, "Created again with its name, it is programmed: its entity left Konnect.")
	}
	scenario("E", e...)
	if hasC {
		scenario("C", "Chain: the target, in a second namespace, references an object of a",
			"third: the object needs both grants, and revoking the second one breaks",
			"it again.")
	}
	scenario("N", "Name clash: an object of another namespace with the same name doesn't",
		"take its name, which the gateway keeps unique.")
	if gw == onprem && r.held {
		scenario("H", "Held: being deleted while a policy uses it, the object doesn't stop the",
			"push that releases it when the grant letting it read its Secret is",
			"revoked.")
	}
	if gw == onprem {
		lines = append(lines, "", "After each change, the test also checks whether the gateway still pushes",
			"its configuration: a missing grant to the parent only leaves the object out,",
			"one to anything else stops the push.")
	}
	lines = append(lines, "", "References checked, with their scenarios:")
	lines = append(lines, refs...)
	if gw == konnect {
		skipped := make([]string, 0, len(r.konnectSkip))
		for k := range r.konnectSkip {
			skipped = append(skipped, k)
		}
		sort.Strings(skipped)
		for _, k := range skipped {
			for _, a := range r.axes {
				if a.name == k {
					lines = append(lines, "  - "+a.what+": checked by "+r.konnectSkip[k]+".")
				}
			}
		}
	}
	var sb strings.Builder
	for _, l := range lines {
		if l == "" {
			sb.WriteString("\n")
			continue
		}
		sb.WriteString("    ")
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return sb.String()
}

var gatewayBindings = map[gatewayType]string{
	konnect: `    - name: test_name
      value: ($namespace)
    - name: konnect_token
      value: (env('KONNECT_TOKEN'))
    - name: konnect_url
      value: (env('KONNECT_SERVER_URL') || 'eu.api.konghq.tech')
    - name: auth_secret_name
      value: (join('-', [($test_name), 'konnect-auth-secret']))
    - name: auth_secret_namespace
      value: ($namespace)
    - name: konnect_auth_name
      value: (join('-', [($test_name), 'konnect-api-auth']))
    - name: konnect_auth_namespace
      value: ($namespace)
    - name: keg_cp_name
      value: (join('-', [($test_name), 'keg']))
    - name: gateway_name
      value: ($keg_cp_name)
    - name: gateway_group
      value: konnect.konghq.com
    - name: gateway_kind
      value: KonnectEventGateway
    - name: gateway_other_kind
      value: KonnectGatewayControlPlane
    - name: gateway_namespace
      value: ($namespace)
    - name: control_plane_ref
      value:
        type: konnectNamespacedRef
        konnectNamespacedRef:
          name: ($gateway_name)
          namespace: ($gateway_namespace)
    - name: control_plane_ref_local
      value:
        type: konnectNamespacedRef
        konnectNamespacedRef:
          name: ($gateway_name)
`,
	onprem: `    - name: gwadmin_name
      value: gw-admin
    - name: gateway_name
      value: gateway
    - name: gateway_group
      value: eventgateway.konghq.com
    - name: gateway_kind
      value: OnPremEventGateway
    - name: gateway_other_kind
      value: GWAdmin
    - name: gateway_namespace
      value: ($namespace)
    - name: control_plane_ref
      value:
        type: onpremNamespacedRef
        onpremNamespacedRef:
          name: ($gateway_name)
          namespace: ($gateway_namespace)
    - name: control_plane_ref_local
      value:
        type: onpremNamespacedRef
        onpremNamespacedRef:
          name: ($gateway_name)
`,
}

const namespaceBindings = `    - name: referrer_namespace
      value: (join('-', [$namespace, 'referrer']))
    - name: target_namespace
      value: (join('-', [$namespace, 'target']))
    - name: third_namespace
      value: (join('-', [$namespace, 'third']))
`

const catch = `
  catch:
    - description: Capture debug snapshot on test failure.
      script:
        env:
          - name: TEST_NAME
            value: ($test.metadata.name)
          - name: NAMESPACE
            value: ($namespace)
          - name: ADDITIONAL_NAMESPACES
            value: (join(',', [$referrer_namespace, $target_namespace, $third_namespace]))
          - name: OPERATOR_NAMESPACE
            value: kong-system
        content: |
          bash ../../../common/scripts/debug_snapshot.sh
`
