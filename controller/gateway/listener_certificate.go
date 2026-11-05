package gateway

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// setKongCertificateListenersStatus gates explicit certificate bindings on their SNI being programmed.
func (g *gatewayConditionsAndListenersAwareT) setKongCertificateListenersStatus(ctx context.Context, cl client.Client) error {
	for i, listener := range g.Spec.Listeners {
		if listener.TLS == nil || refs.IsTLSPassthroughListener(listener) || len(listener.TLS.CertificateRefs) != 1 ||
			!refs.IsKongCertificateRef(listener.TLS.CertificateRefs[0]) {
			continue
		}
		status := listenerConditionsAware(&g.Status.Listeners[i])
		resolved, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.ListenerConditionResolvedRefs), status)
		ready := false
		if ok && resolved.Status == metav1.ConditionTrue {
			ref := listener.TLS.CertificateRefs[0]
			namespace := g.Namespace
			if ref.Namespace != nil && *ref.Namespace != "" {
				namespace = string(*ref.Namespace)
			}
			var cert configurationv1alpha1.KongCertificate
			if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: string(ref.Name)}, &cert); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to get Listener KongCertificate: %w", err)
			}
			var sni configurationv1alpha1.KongSNI
			key := client.ObjectKey{
				Namespace: g.Namespace,
				Name:      namegen.NewKongCertificateName(g.Name, strconv.Itoa(int(listener.Port)), string(listener.Name)),
			}
			if err := cl.Get(ctx, key, &sni); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to get Listener KongSNI: %w", err)
			}
			sniNamespace := sni.Namespace
			if sni.Spec.CertificateRef.Namespace != nil {
				sniNamespace = *sni.Spec.CertificateRef.Namespace
			}
			hostname := "*"
			if listener.Hostname != nil && *listener.Hostname != "" {
				hostname = string(*listener.Hostname)
			}
			programmed, found := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &sni)
			ready = found && programmed.Status == metav1.ConditionTrue && programmed.ObservedGeneration == sni.Generation &&
				sni.DeletionTimestamp.IsZero() && k8sutils.IsOwnedByRefUID(&sni, g.UID) &&
				sni.Spec.Name == hostname && sni.Spec.CertificateRef.Name == string(ref.Name) && sniNamespace == namespace &&
				sni.Status.Konnect != nil && sni.GetKonnectID() != "" && cert.GetKonnectID() != "" &&
				cert.Status.Konnect.ControlPlaneID != "" && sni.Status.Konnect.ControlPlaneID == cert.Status.Konnect.ControlPlaneID &&
				sni.Status.Konnect.CertificateID == cert.GetKonnectID()
		}
		if !ready {
			k8sutils.SetCondition(metav1.Condition{
				Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionFalse,
				Reason: string(gatewayv1.ListenerReasonPending), Message: "Listener certificate binding is not programmed yet.",
				ObservedGeneration: g.Generation, LastTransitionTime: metav1.Now(),
			}, status)
			k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(
				kcfgconsts.ConditionType(gatewayv1.GatewayConditionProgrammed), metav1.ConditionFalse,
				kcfgdataplane.DependenciesNotReadyReason, kcfgdataplane.DependenciesNotReadyMessage, g.Generation,
			), g)
		}
	}
	return nil
}
