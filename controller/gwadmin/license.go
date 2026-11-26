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

package gwadmin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

const licenseVolumeName = "license"

func setLicenseCondition(gw *operatorv1alpha1.GWAdmin, status metav1.ConditionStatus, reason, message string) {
	k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(
		kcfgconsts.ConditionType(operatorv1alpha1.GWAdminConditionTypeLicenseProvisioned),
		status, kcfgconsts.ConditionReason(reason), message, gw.Generation,
	), gw)
}

// ensureLicense reports in the LicenseProvisioned condition whether the
// license Secret of the GWAdmin holds a license gw-admin is pointed at, and
// returns false when it doesn't: gw-admin refuses to start without one. The Secret is mounted, not
// copied, so the kubelet updates the license file of the running pods, which
// gw-admin reads again on each of its periodic license checks: a license
// change does not roll the pods. The Secret is held with
// consts.GWAdminLicenseInUseFinalizer while it is referenced.
func (r *Reconciler) ensureLicense(ctx context.Context, gw *operatorv1alpha1.GWAdmin) (bool, error) {
	ref := gw.Spec.License.SecretRef
	// A GWAdmin stored before the license was required references none.
	if ref.Name == "" {
		setLicenseCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefNotFound,
			"no license Secret is referenced: set spec.license.secretRef")
		return false, nil
	}
	secret, err := r.userSecret(ctx, gw, setLicenseCondition, "license", ref.Name)
	if errors.Is(err, errStop) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	// No finalizer can be added to a Secret being deleted: one already
	// holding it keeps it until no GWAdmin references it. A conflict means
	// the Secret changed, which reconciles the GWAdmin again: the rest of
	// this reconcile goes on.
	if secret.DeletionTimestamp.IsZero() {
		if _, _, err := patch.WithFinalizer(ctx, r.Client, secret, consts.GWAdminLicenseInUseFinalizer); err != nil {
			return false, fmt.Errorf("failed to hold license Secret %s/%s: %w", gw.Namespace, ref.Name, err)
		}
	}
	// gw-admin, not Kong Operator, validates the license: only its presence
	// is checked here.
	if len(bytes.TrimSpace(secret.Data[ref.Key])) == 0 {
		setLicenseCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonInvalidSecret,
			fmt.Sprintf("license Secret %q has no key %q, or it is empty", ref.Name, ref.Key))
		return false, nil
	}
	message := ""
	if !secret.DeletionTimestamp.IsZero() {
		message = fmt.Sprintf("license Secret %q is being deleted: it is kept until no GWAdmin references it. "+
			"Update a license in place: a Secret can't be created again under its name until then", ref.Name)
	}
	setLicenseCondition(gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonLicenseProvisioned, message)
	return true, nil
}

// errLicenseReleaseConflict is returned when a license Secret changed while
// its in-use finalizer was being removed: the reconcile is retried.
var errLicenseReleaseConflict = errors.New("a license Secret changed while it was being released")

// releaseLicenseSecrets removes the in-use finalizer from the license Secrets
// of the namespace of gw that no other GWAdmin, not being deleted, references.
// keep, the Secret gw references, is left held, unless empty, as when gw is
// deleted. Like every read of the controller, it goes through the cache, which
// only holds the Secrets carrying the secret label selector: a held Secret
// that lost the label is not released, until it gets it back.
func (r *Reconciler) releaseLicenseSecrets(ctx context.Context, gw *operatorv1alpha1.GWAdmin, keep string) error {
	secrets := &corev1.SecretList{}
	// The Secrets are only read: the finalizer helpers patch copies.
	if err := r.List(ctx, secrets, client.InNamespace(gw.Namespace),
		client.MatchingFields{IndexFieldSecretLicenseInUse: "true"}, client.UnsafeDisableDeepCopy); err != nil {
		return fmt.Errorf("failed to list the held license Secrets of namespace %s: %w", gw.Namespace, err)
	}
	var held []*corev1.Secret
	for i := range secrets.Items {
		if s := &secrets.Items[i]; s.Name != keep {
			held = append(held, s)
		}
	}
	if len(held) == 0 {
		return nil
	}
	gwAdmins := &operatorv1alpha1.GWAdminList{}
	if err := r.List(ctx, gwAdmins, client.InNamespace(gw.Namespace)); err != nil {
		return fmt.Errorf("failed to list the GWAdmins of namespace %s: %w", gw.Namespace, err)
	}
	for _, s := range held {
		if slices.ContainsFunc(gwAdmins.Items, func(o operatorv1alpha1.GWAdmin) bool {
			return o.UID != gw.UID && o.DeletionTimestamp.IsZero() && o.Spec.License.SecretRef.Name == s.Name
		}) {
			continue
		}
		_, res, err := patch.WithoutFinalizer(ctx, r.Client, s, consts.GWAdminLicenseInUseFinalizer)
		switch {
		case apierrors.IsNotFound(err):
		case err != nil:
			return fmt.Errorf("failed to release license Secret %s/%s: %w", s.Namespace, s.Name, err)
		case !res.IsZero():
			return errLicenseReleaseConflict
		}
	}
	return nil
}

// licenseInUseIndexValues indexes the Secrets held by
// consts.GWAdminLicenseInUseFinalizer, so that the release lists only those.
func licenseInUseIndexValues(obj client.Object) []string {
	if controllerutil.ContainsFinalizer(obj, consts.GWAdminLicenseInUseFinalizer) {
		return []string{"true"}
	}
	return nil
}

// withLicense mounts the license key of the license Secret in the gw-admin
// container and points gw-admin at it and at the entitlements it must grant.
// No subPath is used, so that the kubelet updates the file when the Secret
// changes. The volume is optional: without the Secret or its key the pods
// still start, and gw-admin, missing the file, exits with an error naming it,
// rather than the pods waiting for the volume. A GWAdmin referencing no
// Secret gets no license wiring, which a Secret volume can't be built for.
func withLicense(gw *operatorv1alpha1.GWAdmin, c corev1.Container, volumes []corev1.Volume) (corev1.Container, []corev1.Volume) {
	ref := gw.Spec.License.SecretRef
	if ref.Name == "" {
		return c, volumes
	}
	entitlements := make([]string, 0, len(gw.Spec.License.Entitlements))
	for _, e := range gw.Spec.License.Entitlements {
		entitlements = append(entitlements, string(e))
	}
	c.Env = append(c.Env,
		corev1.EnvVar{Name: consts.GWAdminLicenseFileEnvVar, Value: consts.GWAdminLicenseMountPath + "/" + consts.GWAdminLicenseFileName},
		corev1.EnvVar{Name: consts.GWAdminLicenseEntitlementsEnvVar, Value: strings.Join(entitlements, ",")},
	)
	c.VolumeMounts = append(c.VolumeMounts,
		corev1.VolumeMount{Name: licenseVolumeName, MountPath: consts.GWAdminLicenseMountPath, ReadOnly: true})
	return c, append(volumes, corev1.Volume{
		Name: licenseVolumeName,
		Secret: &corev1.SecretVolumeSource{
			SecretName: ref.Name,
			Items:      []corev1.KeyToPath{{Key: ref.Key, Path: consts.GWAdminLicenseFileName}},
			Optional:   new(true),
		},
	})
}
