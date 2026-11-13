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
	"crypto/sha256"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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
