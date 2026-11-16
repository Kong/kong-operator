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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	"github.com/kong/kong-operator/v2/internal/utils/createinputs"
)

// The id of a static key is not the UID of its object, unlike the id of any
// other entity. keg names the key that encrypted a record by its id, in the
// kong/enc header of the record, and decrypts the record with the static key
// of that id. Recreating the object of a key, e.g. by a GitOps tool replacing
// it, a restore from a backup or a move to another cluster, changes its UID:
// with the UID as its id, the records it encrypted could no longer be
// decrypted, although its value is the same.

// hashedIDSpace is the name space of the ids hashed from the group, kind,
// namespace and name of an object (see hashedEntityID). Never change it, nor what
// hashedEntityID hashes: every static key would get a new id, and the records
// it encrypted would no longer be decrypted.
var hashedIDSpace = uuid.MustParse("3f6c2a8e-4b1d-4e7a-9c5f-0d8b6e2a1c47")

// hashedEntityID returns an id hashed from the group, kind, namespace and
// name of an object: a version 4 UUID, as gw-admin requires, kept when the
// object is recreated. The group and kind keep the ids of objects of
// different kinds apart, so another kind can take a hashed id without
// changing those of static keys.
func hashedEntityID(gk schema.GroupKind, key client.ObjectKey) uuid.UUID {
	// NUL, which none of them holds, separates them.
	data := gk.Group + "\x00" + gk.Kind + "\x00" + key.Namespace + "\x00" + key.Name
	return uuid.NewHash(sha256.New(), hashedIDSpace, []byte(data), 4)
}

// staticKeyID returns the id of the static key of the object of key, hashed
// from its group, kind, namespace and name (see hashedEntityID). Recreating the
// object keeps the id, so records it encrypted are still decrypted.
//
// The value of the key is left out on purpose: the id is in the header of
// every record the key encrypts, readable by anyone reading the topic in
// Kafka, and an id hashed from the value would let them check guesses of the
// value offline. Records encrypted with a former value fail to be decrypted
// with the new one instead.
func staticKeyID(key client.ObjectKey) uuid.UUID {
	return hashedEntityID(configurationv1alpha1.GroupVersion.WithKind("EventGatewayStaticKey").GroupKind(), key)
}

// entityIDOf returns the id of the entity of the object in gw-admin: the UID
// of the object, but for a static key (see staticKeyID). ok tells the object
// has an id: the UID it takes as its id is a version 4 UUID, as gw-admin
// requires.
func entityIDOf(obj client.Object) (id uuid.UUID, ok bool) {
	if k, isKey := obj.(*configurationv1alpha1.EventGatewayStaticKey); isKey {
		return staticKeyID(client.ObjectKeyFromObject(k)), true
	}
	return uidV4(obj)
}

// entityIDKey returns the id of the entity of the object, as a string, for
// the maps matching the references of the entities sent, by id, to their
// objects. An object without an id is under its UID, which no reference
// holds.
func entityIDKey(obj client.Object) string {
	if id, ok := entityIDOf(obj); ok {
		return id.String()
	}
	return string(obj.GetUID())
}

// The value of a static key never changes under its id once it was pushed:
// keg would hold the new value under the id the records encrypted with the old
// one name, and those records would no longer be decrypted. The first push
// records a fingerprint of its value in its status, in the field of the
// fingerprint of a key created in Konnect (status.createInputsHash, see
// handleCreateInputs in controller/konnect), and the CRD then refuses a change
// of its inline value. A change of the value it reads otherwise, e.g. of its
// Secret, or its reference to another one, can't be refused: the key then stops the push,
// so that gw-admin keeps the value pushed, until the value is put back, or the
// key is deleted: one being deleted is sent as it is, and a new one starts
// with no fingerprint. gw-admin returns the value of a key it holds, but a
// restarted gw-admin pod holds none, so the value pushed can't always be sent
// again in its place: stopping the push behaves the same whether or not a pod
// still holds it.

// keyValueHash returns the fingerprint of the value of a static key, as
// status.createInputsHash records it. Only the value: unlike in Konnect, its
// name, description and labels are updated in place, under the same id, and
// change nothing of what the records it encrypted need.
func keyValueHash(value string) (string, error) {
	data, err := json.Marshal(map[string]string{"value": value})
	if err != nil {
		return "", err
	}
	return createinputs.Hash(data)
}

// keyHashes returns, by id, the fingerprints of the values of the static
// keys (see keyValueHash).
func keyHashes(keys []gwadminapi.EventGatewayStaticKeyOnPrem) (map[uuid.UUID]string, error) {
	hashes := make(map[uuid.UUID]string, len(keys))
	for _, k := range keys {
		hash, err := keyValueHash(k.Value)
		if err != nil {
			return nil, err
		}
		hashes[k.Id] = hash
	}
	return hashes, nil
}

// recoverKeyHashes remembers, for each static key of the configuration with no
// fingerprint recorded, the one of the value a gw-admin pod holds for it, as
// if it were recorded (see Reconciler.inputsHashes): a value pushed whose
// fingerprint couldn't be recorded, e.g. as the operator stopped between the
// push and the write of the status of the key, may be served, and the key
// keeps it. A key held with another creation time is another object of the
// same name, e.g. one deleted and created again: its value isn't kept. Only a
// previous process may have left such a value: the pods of a gateway are read
// once after a start, until every pod could be read, so that a key deleted and
// created again within the same second, as its creation time tells, isn't
// taken for the one it replaces while the operator runs. It tells whether it
// remembered any: the configuration is then built again.
func (r *Reconciler) recoverKeyHashes(
	ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, gatewayID uuid.UUID, cfg configuration,
) bool {
	if _, done := r.keysRecovered.Load(gateway.UID); done {
		return false
	}
	unrecorded := map[uuid.UUID]*configurationv1alpha1.EventGatewayStaticKey{}
	for _, obj := range cfg.allObjects() {
		if k, ok := obj.(*configurationv1alpha1.EventGatewayStaticKey); ok && cfg.recordedHash(k) == "" {
			unrecorded[staticKeyID(client.ObjectKeyFromObject(k))] = k
		}
	}
	if len(unrecorded) == 0 {
		r.keysRecovered.Store(gateway.UID, struct{}{})
		return false
	}
	target, err := r.resolveGWAdmin(ctx, gateway, true)
	if err != nil {
		return false
	}
	var mu sync.Mutex
	held := map[uuid.UUID]string{}
	pending, err := r.GWAdminSync.ForEachPod(ctx, target, target.Pods,
		func(ctx context.Context, _ gwadminsync.Pod, gwAdmin gwadminapi.ClientInterface) error {
			entities, ok, err := getEntities(ctx, gwAdmin, gatewayID)
			if err != nil || !ok {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			for _, h := range entities.Entities.StaticKeys {
				k, ok := unrecorded[h.Id]
				if _, seen := held[h.Id]; seen || !ok || h.Value == "" || !h.CreatedAt.Equal(createdAt(k)) {
					continue
				}
				if hash, err := keyValueHash(h.Value); err == nil {
					held[h.Id] = hash
				}
			}
			return nil
		})
	if err == nil && len(pending) == 0 {
		r.keysRecovered.Store(gateway.UID, struct{}{})
	}
	for id, hash := range held {
		k := unrecorded[id]
		r.inputsHashes.Store(client.ObjectKeyFromObject(k), recordedKeyHash{uid: k.GetUID(), hash: hash})
	}
	return len(held) > 0
}

// keyInputs is the fingerprint of the value of a static key, as it reads now.
type keyInputs struct {
	// hash is the fingerprint, or "" when it can't be computed, e.g. as the
	// Secret it reads is gone.
	hash string
}

// staticKeyRequest returns the request of a static key, checking its
// fingerprint against the one recorded: one whose value changed can't be
// sent (see keyInputs).
func (c *configuration) staticKeyRequest(ctx context.Context, cl client.Client, k *configurationv1alpha1.EventGatewayStaticKey) (any, error) {
	req, err := k.ToEventGatewayStaticKeyCreate(ctx, cl)
	var hash string
	if err == nil {
		hash, err = keyValueHash(req.Value)
	}
	if err != nil {
		c.keyInputs[k.UID] = keyInputs{}
		return nil, err
	}
	recorded := c.recordedHash(k)
	changed := recorded != "" && hash != recorded
	c.keyInputs[k.UID] = keyInputs{hash: hash}
	// One being deleted, held while the configuration sent uses it, is sent
	// with the value it has: its deletion is the way out of a value that
	// changed, and accepts that the records it encrypted are no longer
	// decrypted. Stopping the push, it would keep the configuration sent, and
	// so the hold, forever; left out, the configuration without it could be
	// stopped too, e.g. by a decrypt policy needing a static key.
	if changed && k.GetDeletionTimestamp().IsZero() {
		return nil, errors.New("its value " + keyValueChanged)
	}
	return req, nil
}

// keyValueChanged tells why a static key whose value changed since it was
// pushed can't be sent, and how to change it.
const keyValueChanged = "changed since it was pushed, and records encrypted with the value pushed " +
	"would no longer be decrypted: put the value back, or create a new EventGatewayStaticKey, point the policies at it and delete this one, " +
	"or, when no record needs the value pushed, delete the policies using this static key and the key, and create them again"

// recordedHash returns the fingerprint recorded for the static key: in its
// status, or, when the cache doesn't show it yet, the one the controller
// recorded (see Reconciler.inputsHashes).
func (c *configuration) recordedHash(k *configurationv1alpha1.EventGatewayStaticKey) string {
	return inputsHashOf(c.inputsHashes, k)
}

// recordedKeyHash is a fingerprint the controller recorded in the status of
// the static key of its UID (see Reconciler.inputsHashes).
type recordedKeyHash struct {
	uid  types.UID
	hash string
}

// inputsHashOf returns the fingerprint recorded for the object, when it is a
// static key: in its status, or, when the cache doesn't show it yet, in
// recorded, which may be nil, for that object: not for another one of the same
// name, e.g. one created again.
func inputsHashOf(recorded *sync.Map, obj client.Object) string {
	if hash := recordedInputsHash(obj); hash != "" || recorded == nil {
		return hash
	}
	if r, ok := recorded.Load(client.ObjectKeyFromObject(obj)); ok && r.(recordedKeyHash).uid == obj.GetUID() {
		return r.(recordedKeyHash).hash
	}
	return ""
}

// recordedInputsHash returns the fingerprint recorded in the status of the
// object, when it is a static key, or "".
func recordedInputsHash(obj client.Object) string {
	if k, ok := obj.(*configurationv1alpha1.EventGatewayStaticKey); ok {
		return k.GetCreateInputsHash()
	}
	return ""
}

// keyInputsStatus returns the fingerprint to record in the status of a static
// key, and its CreateInputsApplied condition, or nil when it has none: the
// fingerprint recorded, or else pushed: the one of the value gw-admin may
// hold, or "" when it holds none. Its value now, in inputs, is compared with
// it.
func keyInputsStatus(k *configurationv1alpha1.EventGatewayStaticKey, recorded string, inputs keyInputs, pushed string) (string, *metav1.Condition) {
	hash := recorded
	reason, status := konnectv1alpha1.CreateInputsReasonUnchanged, metav1.ConditionTrue
	message := "Current inputs match the recorded create-input fingerprint"
	if hash == "" {
		if pushed == "" {
			return "", nil
		}
		hash = pushed
		reason, message = konnectv1alpha1.CreateInputsReasonApplied, "The value pushed is recorded: it can't change while this static key exists"
	}
	switch {
	case inputs.hash != "" && inputs.hash != hash:
		reason, status = konnectv1alpha1.CreateInputsReasonValueChanged, metav1.ConditionFalse
		message = "The value " + keyValueChanged
	case inputs.hash == "":
		reason, status = konnectv1alpha1.CreateInputsReasonInputsUnavailable, metav1.ConditionUnknown
		// Not converted: its value can't be read, e.g. its Secret is gone, or
		// it isn't read at all, e.g. a grant is missing. Programmed and
		// SecretRefValid tell which.
		message = "Cannot compare its value with the recorded fingerprint, as it can't be read now (see Programmed and SecretRefValid)"
	}
	cond := entityCondition(k, konnectv1alpha1.CreateInputsAppliedConditionType, status, reason, message)
	return hash, &cond
}
