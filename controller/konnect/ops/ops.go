package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	kcfgkonnect "github.com/kong/kong-operator/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/internal/metrics"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// Op is the type for the operation type of a Konnect entity.
type Op string

const (
	// CreateOp is the operation type for creating a Konnect entity.
	CreateOp Op = "create"
	// GetOp is the operation type for getting a Konnect entity.
	GetOp Op = "get"
	// UpdateOp is the operation type for updating a Konnect entity.
	UpdateOp Op = "update"
	// DeleteOp is the operation type for deleting a Konnect entity.
	DeleteOp Op = "delete"
	// AdoptOp is the operation type for adopting an existing Konnect entity.
	AdoptOp Op = "adopt"
)

// Create creates a Konnect entity.
func Create[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	sdk sdkops.SDKWrapper,
	cl client.Client,
	metricRecorder metrics.Recorder,
	e TEnt,
) (*T, error) {
	var (
		err   error
		start = time.Now()

		entityType = e.GetTypeName()
		statusCode int
	)

	switch ent := any(e).(type) {
	case *konnectv1alpha2.KonnectGatewayControlPlane:
		err = ensureControlPlane(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
	case *konnectv1alpha1.KonnectCloudGatewayNetwork:
		err = createKonnectNetwork(ctx, sdk.GetCloudGatewaysSDK(), ent)
	case *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration:
		err = createKonnectDataPlaneGroupConfiguration(ctx, sdk.GetCloudGatewaysSDK(), cl, ent, sdk.GetServer().Region())
	case *konnectv1alpha1.KonnectCloudGatewayTransitGateway:
		err = createKonnectTransitGateway(ctx, sdk.GetCloudGatewaysSDK(), ent)
	case *configurationv1alpha1.KongService:
		err = createService(ctx, sdk.GetServicesSDK(), ent)
	case *configurationv1alpha1.KongRoute:
		err = createRoute(ctx, sdk.GetRoutesSDK(), ent)
	case *configurationv1.KongConsumer:
		err = createConsumer(ctx, sdk.GetConsumersSDK(), sdk.GetConsumerGroupsSDK(), cl, ent)
	case *configurationv1beta1.KongConsumerGroup:
		err = createConsumerGroup(ctx, sdk.GetConsumerGroupsSDK(), ent)
	case *configurationv1alpha1.KongPluginBinding:
		err = createPlugin(ctx, cl, sdk.GetPluginSDK(), ent)
	case *configurationv1alpha1.KongUpstream:
		err = createUpstream(ctx, sdk.GetUpstreamsSDK(), ent)
	case *configurationv1alpha1.KongCredentialBasicAuth:
		err = createKongCredentialBasicAuth(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialAPIKey:
		err = createKongCredentialAPIKey(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialACL:
		err = createKongCredentialACL(ctx, sdk.GetACLCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialJWT:
		err = createKongCredentialJWT(ctx, sdk.GetJWTCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialHMAC:
		err = createKongCredentialHMAC(ctx, sdk.GetHMACCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCACertificate:
		err = createCACertificate(ctx, sdk.GetCACertificatesSDK(), ent)
	case *configurationv1alpha1.KongCertificate:
		err = createCertificate(ctx, sdk.GetCertificatesSDK(), ent)
	case *configurationv1alpha1.KongTarget:
		err = createTarget(ctx, sdk.GetTargetsSDK(), ent)
	case *configurationv1alpha1.KongVault:
		err = createVault(ctx, sdk.GetVaultSDK(), ent)
	case *configurationv1alpha1.KongKey:
		err = createKey(ctx, sdk.GetKeysSDK(), ent)
	case *configurationv1alpha1.KongKeySet:
		err = createKeySet(ctx, sdk.GetKeySetsSDK(), ent)
	case *configurationv1alpha1.KongSNI:
		err = createSNI(ctx, sdk.GetSNIsSDK(), ent)
	case *configurationv1alpha1.KongDataPlaneClientCertificate:
		err = CreateKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), ent)
		// ---------------------------------------------------------------------
		// TODO: add other Konnect types
	default:
		return nil, fmt.Errorf("unsupported entity type %T", ent)
	}

	var (
		errRelationsFailed KonnectEntityCreatedButRelationsFailedError
		errSDK             *sdkkonnecterrs.SDKError
		errGet             error
	)
	switch {
	case ErrorIsCreateConflict(err):
		// If there was a conflict on the create request, we can assume the entity already exists.
		// We'll get its Konnect ID by listing all entities of its type filtered by the Kubernetes object UID.
		var id string
		switch ent := any(e).(type) {
		case *konnectv1alpha2.KonnectGatewayControlPlane:
			id, errGet = getControlPlaneForUID(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
		case *konnectv1alpha1.KonnectCloudGatewayNetwork:
			// NOTE: since Cloud Gateways resource do not support labels/tags,
			// we can't reliably get the Konnect ID for a Cloud Gateway Network
			// given a K8s object UID.
			// For now this code uses a list, using a name filter, to get the Konnect ID.
			id, err = getKonnectNetworkMatchingSpecName(ctx, sdk.GetCloudGatewaysSDK(), ent)
		case *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration:
			// TODO: can't get the ID for a DataPlaneGroupConfiguration
			// as this resource type does not support labels/tags.
		case *konnectv1alpha1.KonnectCloudGatewayTransitGateway:
			id, err = getKonnectTransitGatewayMatchingSpecName(ctx, sdk.GetCloudGatewaysSDK(), ent)
		case *configurationv1alpha1.KongService:
			id, errGet = getKongServiceForUID(ctx, sdk.GetServicesSDK(), ent)
		case *configurationv1alpha1.KongRoute:
			id, errGet = getKongRouteForUID(ctx, sdk.GetRoutesSDK(), ent)
		case *configurationv1alpha1.KongSNI:
			id, errGet = getKongSNIForUID(ctx, sdk.GetSNIsSDK(), ent)
		case *configurationv1.KongConsumer:
			id, errGet = getKongConsumerForUID(ctx, sdk.GetConsumersSDK(), ent)
		case *configurationv1beta1.KongConsumerGroup:
			id, errGet = getKongConsumerGroupForUID(ctx, sdk.GetConsumerGroupsSDK(), ent)
		case *configurationv1alpha1.KongKeySet:
			id, errGet = getKongKeySetForUID(ctx, sdk.GetKeySetsSDK(), ent)
		case *configurationv1alpha1.KongKey:
			id, errGet = getKongKeyForUID(ctx, sdk.GetKeysSDK(), ent)
		case *configurationv1alpha1.KongUpstream:
			id, errGet = getKongUpstreamForUID(ctx, sdk.GetUpstreamsSDK(), ent)
		case *configurationv1alpha1.KongTarget:
			id, errGet = getKongTargetForUID(ctx, sdk.GetTargetsSDK(), ent)
		case *configurationv1alpha1.KongPluginBinding:
			id, errGet = getPluginForUID(ctx, sdk.GetPluginSDK(), ent)
		case *configurationv1alpha1.KongVault:
			id, errGet = getKongVaultForUID(ctx, sdk.GetVaultSDK(), ent)
		case *configurationv1alpha1.KongCredentialHMAC:
			id, errGet = getKongCredentialHMACForUID(ctx, sdk.GetHMACCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialJWT:
			id, errGet = getKongCredentialJWTForUID(ctx, sdk.GetJWTCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialBasicAuth:
			id, errGet = getKongCredentialBasicAuthForUID(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialAPIKey:
			id, errGet = getKongCredentialAPIKeyForUID(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialACL:
			id, errGet = getKongCredentialACLForUID(ctx, sdk.GetACLCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCertificate:
			id, errGet = getKongCertificateForUID(ctx, sdk.GetCertificatesSDK(), ent)
		case *configurationv1alpha1.KongCACertificate:
			id, errGet = getKongCACertificateForUID(ctx, sdk.GetCACertificatesSDK(), ent)
			// ---------------------------------------------------------------------
			// TODO: add other Konnect types
		default:
			return e, fmt.Errorf("conflict on create request for %T %s, but no conflict handling implemented: %w",
				e, client.ObjectKeyFromObject(e), err,
			)
		}

		if errGet == nil && id != "" {
			e.SetKonnectID(id)
			SetKonnectEntityProgrammedConditionTrue(e)
		} else {
			if errGet != nil {
				err = fmt.Errorf("trying to find a matching Konnect entity matching the ID failed: %w, %w", errGet, err)
			}

			SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToCreateReason, err)
		}

	case errors.As(err, &errSDK):
		statusCode = errSDK.StatusCode
		SetKonnectEntityProgrammedConditionFalse(
			e, kcfgkonnect.KonnectEntitiesFailedToCreateReason, errSDK)
	case errors.As(err, &errRelationsFailed):
		e.SetKonnectID(errRelationsFailed.KonnectID)
		SetKonnectEntityProgrammedConditionFalse(e, errRelationsFailed.Reason, errRelationsFailed.Err)
	case err != nil:
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToCreateReason, err)
	default:
		SetKonnectEntityProgrammedConditionTrue(e)
	}

	if err != nil {
		metricRecorder.RecordKonnectEntityOperationFailure(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationCreate,
			entityType,
			time.Since(start),
			statusCode,
		)
	} else {
		metricRecorder.RecordKonnectEntityOperationSuccess(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationCreate,
			entityType,
			time.Since(start),
		)
	}

	// For mirrorable entities, we need to check if the source type is mirror
	// and set the mirrored condition accordingly.
	if isMirrorableEntity(e) {
		if isMirrorEntity(e) {
			if err == nil {
				SetKonnectEntityMirroredConditionTrue(e)
			} else {
				SetKonnectEntityMirroredConditionFalse(e, err)
			}
		}
	}

	logOpComplete(ctx, start, CreateOp, e, err)

	return e, IgnoreUnrecoverableAPIErr(err, loggerForEntity(ctx, e, CreateOp))
}

// Delete deletes a Konnect entity.
// It returns an error if the entity does not have a Konnect ID or if the operation fails.
func Delete[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, sdk sdkops.SDKWrapper, cl client.Client, metricRecorder metrics.Recorder, ent TEnt) error {
	if ent.GetKonnectStatus().GetKonnectID() == "" {
		cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
		if ok && cond.Status == metav1.ConditionTrue {
			return fmt.Errorf(
				"can't delete %T %s when it does not have the Konnect ID",
				ent, client.ObjectKeyFromObject(ent),
			)
		}
		return nil
	}

	var (
		err        error
		start      = time.Now()
		entityType = ent.GetTypeName()
		statusCode int
	)
	switch ent := any(ent).(type) {
	case *konnectv1alpha2.KonnectGatewayControlPlane:
		err = deleteControlPlane(ctx, sdk.GetControlPlaneSDK(), ent)
	case *konnectv1alpha1.KonnectCloudGatewayNetwork:
		err = deleteKonnectNetwork(ctx, sdk.GetCloudGatewaysSDK(), ent)
	case *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration:
		err = deleteKonnectDataPlaneGroupConfiguration(ctx, sdk.GetCloudGatewaysSDK(), ent, sdk.GetServer().Region())
	case *konnectv1alpha1.KonnectCloudGatewayTransitGateway:
		err = deleteKonnectTransitGateway(ctx, sdk.GetCloudGatewaysSDK(), ent)
	case *configurationv1alpha1.KongService:
		err = deleteService(ctx, sdk.GetServicesSDK(), ent)
	case *configurationv1alpha1.KongRoute:
		err = deleteRoute(ctx, sdk.GetRoutesSDK(), ent)
	case *configurationv1.KongConsumer:
		err = deleteConsumer(ctx, sdk.GetConsumersSDK(), ent)
	case *configurationv1beta1.KongConsumerGroup:
		err = deleteConsumerGroup(ctx, sdk.GetConsumerGroupsSDK(), ent)
	case *configurationv1alpha1.KongPluginBinding:
		err = deletePlugin(ctx, sdk.GetPluginSDK(), ent)
	case *configurationv1alpha1.KongUpstream:
		err = deleteUpstream(ctx, sdk.GetUpstreamsSDK(), ent)
	case *configurationv1alpha1.KongCredentialBasicAuth:
		err = deleteKongCredentialBasicAuth(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialAPIKey:
		err = deleteKongCredentialAPIKey(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialACL:
		err = deleteKongCredentialACL(ctx, sdk.GetACLCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialJWT:
		err = deleteKongCredentialJWT(ctx, sdk.GetJWTCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialHMAC:
		err = deleteKongCredentialHMAC(ctx, sdk.GetHMACCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCACertificate:
		err = deleteCACertificate(ctx, sdk.GetCACertificatesSDK(), ent)
	case *configurationv1alpha1.KongCertificate:
		err = deleteCertificate(ctx, sdk.GetCertificatesSDK(), ent)
	case *configurationv1alpha1.KongTarget:
		err = deleteTarget(ctx, sdk.GetTargetsSDK(), ent)
	case *configurationv1alpha1.KongVault:
		err = deleteVault(ctx, sdk.GetVaultSDK(), ent)
	case *configurationv1alpha1.KongKey:
		err = deleteKey(ctx, sdk.GetKeysSDK(), ent)
	case *configurationv1alpha1.KongKeySet:
		err = deleteKeySet(ctx, sdk.GetKeySetsSDK(), ent)
	case *configurationv1alpha1.KongSNI:
		err = deleteSNI(ctx, sdk.GetSNIsSDK(), ent)
	case *configurationv1alpha1.KongDataPlaneClientCertificate:
		err = DeleteKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), ent)
		// ---------------------------------------------------------------------
		// TODO: add other Konnect types
	default:
		return fmt.Errorf("unsupported entity type %T", ent)
	}

	if err != nil {
		var errSDK *sdkkonnecterrs.SDKError
		if errors.As(err, &errSDK) {
			statusCode = errSDK.StatusCode
		}
		metricRecorder.RecordKonnectEntityOperationFailure(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationDelete,
			entityType,
			time.Since(start),
			statusCode,
		)
	} else {
		metricRecorder.RecordKonnectEntityOperationSuccess(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationDelete,
			entityType,
			time.Since(start),
		)
	}
	logOpComplete(ctx, start, DeleteOp, ent, err)

	// Clear the instance field from the error to avoid requeueing the resource
	// because of the trace ID in the instance field is different for each request.
	err = ClearInstanceFromError(err)

	return err
}

func shouldUpdate[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	ent TEnt,
	syncPeriod time.Duration,
	now time.Time,
) (bool, ctrl.Result) {
	var (
		condProgrammed, ok = k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
		timeFromLastUpdate = time.Since(condProgrammed.LastTransitionTime.Time)
	)

	// If the entity is already programmed and the last update was less than
	// the configured sync period, requeue after the remaining time.
	if ok &&
		condProgrammed.Status == metav1.ConditionTrue &&
		condProgrammed.Reason == konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed &&
		condProgrammed.ObservedGeneration == ent.GetObjectMeta().GetGeneration() &&
		timeFromLastUpdate <= syncPeriod {
		requeueAfter := syncPeriod - timeFromLastUpdate
		log.Debug(ctrllog.FromContext(ctx),
			"no need for update, requeueing after configured sync period",
			"last_update", condProgrammed.LastTransitionTime.String(),
			"time_from_last_update", timeFromLastUpdate.String(),
			"requeue_after", requeueAfter.String(),
			"requeue_at", now.Add(requeueAfter).String(),
		)
		return false, ctrl.Result{
			RequeueAfter: requeueAfter,
		}
	}

	return true, ctrl.Result{}
}

// Update updates a Konnect entity.
// It returns an error if the entity does not have a Konnect ID or if the operation fails.
func Update[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	sdk sdkops.SDKWrapper,
	syncPeriod time.Duration,
	cl client.Client,
	metricRecorder metrics.Recorder,
	e TEnt,
) (ctrl.Result, error) {
	now := time.Now()

	if ok, res := shouldUpdate(ctx, e, syncPeriod, now); !ok {
		return res, nil
	}

	if e.GetKonnectStatus().GetKonnectID() == "" {
		return ctrl.Result{}, fmt.Errorf(
			"can't update %T %s when it does not have the Konnect ID",
			e, client.ObjectKeyFromObject(e),
		)
	}

	var (
		err        error
		entityType = e.GetTypeName()
		statusCode int
		start      = time.Now()
	)
	switch ent := any(e).(type) {
	case *konnectv1alpha2.KonnectGatewayControlPlane:
		// if the ControlPlane is of type origin, enforce the spec on Konnect.
		if *ent.Spec.Source == commonv1alpha1.EntitySourceOrigin {
			err = updateControlPlane(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
		}
	case *konnectv1alpha1.KonnectCloudGatewayNetwork:
		err = updateKonnectNetwork(ctx, sdk.GetCloudGatewaysSDK(), ent)
	case *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration:
		err = updateKonnectDataPlaneGroupConfiguration(ctx, sdk.GetCloudGatewaysSDK(), cl, ent, sdk.GetServer())
	case *konnectv1alpha1.KonnectCloudGatewayTransitGateway:
		err = updateKonnectTransitGateway(ctx, sdk.GetCloudGatewaysSDK(), ent)
	case *configurationv1alpha1.KongService:
		err = updateService(ctx, sdk.GetServicesSDK(), ent)
	case *configurationv1alpha1.KongRoute:
		err = updateRoute(ctx, sdk.GetRoutesSDK(), ent)
	case *configurationv1.KongConsumer:
		err = updateConsumer(ctx, sdk.GetConsumersSDK(), sdk.GetConsumerGroupsSDK(), cl, ent)
	case *configurationv1beta1.KongConsumerGroup:
		err = updateConsumerGroup(ctx, sdk.GetConsumerGroupsSDK(), ent)
	case *configurationv1alpha1.KongPluginBinding:
		err = updatePlugin(ctx, sdk.GetPluginSDK(), cl, ent)
	case *configurationv1alpha1.KongUpstream:
		err = updateUpstream(ctx, sdk.GetUpstreamsSDK(), ent)
	case *configurationv1alpha1.KongCredentialBasicAuth:
		err = updateKongCredentialBasicAuth(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialAPIKey:
		err = updateKongCredentialAPIKey(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialACL:
		err = updateKongCredentialACL(ctx, sdk.GetACLCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialJWT:
		err = updateKongCredentialJWT(ctx, sdk.GetJWTCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialHMAC:
		err = updateKongCredentialHMAC(ctx, sdk.GetHMACCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCACertificate:
		err = updateCACertificate(ctx, sdk.GetCACertificatesSDK(), ent)
	case *configurationv1alpha1.KongCertificate:
		err = updateCertificate(ctx, sdk.GetCertificatesSDK(), ent)
	case *configurationv1alpha1.KongTarget:
		err = updateTarget(ctx, sdk.GetTargetsSDK(), ent)
	case *configurationv1alpha1.KongVault:
		err = updateVault(ctx, sdk.GetVaultSDK(), ent)
	case *configurationv1alpha1.KongKey:
		err = updateKey(ctx, sdk.GetKeysSDK(), ent)
	case *configurationv1alpha1.KongKeySet:
		err = updateKeySet(ctx, sdk.GetKeySetsSDK(), ent)
	case *configurationv1alpha1.KongSNI:
		err = updateSNI(ctx, sdk.GetSNIsSDK(), ent)
	case *configurationv1alpha1.KongDataPlaneClientCertificate:
		err = nil // DataPlaneCertificates are immutable.
		// ---------------------------------------------------------------------
		// TODO: add other Konnect types

	default:
		return ctrl.Result{}, fmt.Errorf("unsupported entity type %T", ent)
	}

	var (
		errRelationsFailed KonnectEntityCreatedButRelationsFailedError
		errSDK             *sdkkonnecterrs.SDKError
	)

	switch {
	case errors.As(err, &errSDK):
		statusCode = errSDK.StatusCode
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToUpdateReason, errSDK)
	case errors.As(err, &errRelationsFailed):
		e.SetKonnectID(errRelationsFailed.KonnectID)
		SetKonnectEntityProgrammedConditionFalse(e, errRelationsFailed.Reason, err)
	case err != nil:
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToUpdateReason, err)
	default:
		SetKonnectEntityProgrammedConditionTrue(e)
	}

	if err != nil {
		metricRecorder.RecordKonnectEntityOperationFailure(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationUpdate,
			entityType,
			time.Since(start),
			statusCode,
		)
	} else {
		metricRecorder.RecordKonnectEntityOperationSuccess(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationUpdate,
			entityType,
			time.Since(start),
		)
	}

	// For mirrorable entities, we need to check if the source type is mirror
	// and set the mirrored condition accordingly.
	if isMirrorableEntity(e) {
		if isMirrorEntity(e) {
			if err == nil {
				SetKonnectEntityMirroredConditionTrue(e)
			} else {
				SetKonnectEntityMirroredConditionFalse(e, err)
			}
		}
	}

	logOpComplete(ctx, start, UpdateOp, e, err)

	return ctrl.Result{}, IgnoreUnrecoverableAPIErr(err, loggerForEntity(ctx, e, UpdateOp))
}

// Adopt adopts an exiting entity in Konnect and take over the management of the entity.
func Adopt[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	sdk sdkops.SDKWrapper,
	syncPeriod time.Duration,
	cl client.Client,
	metricRecorder metrics.Recorder,
	e TEnt,
	adoptOptions commonv1alpha1.AdoptOptions,
) (ctrl.Result, error) {

	var (
		err        error
		entityType = e.GetTypeName()
		statusCode int
		start      = time.Now()
	)

	switch ent := any(e).(type) {
	case *configurationv1alpha1.KongService:
		err = adoptService(ctx, sdk.GetServicesSDK(), ent)
	case *konnectv1alpha1.KonnectCloudGatewayNetwork:
		switch {
		case adoptOptions.Konnect == nil || adoptOptions.Konnect.ID == "":
			err = fmt.Errorf("konnect ID must be provided for adoption")
		case adoptOptions.Mode != "" && adoptOptions.Mode != commonv1alpha1.AdoptModeMatch:
			err = fmt.Errorf("only match mode adoption is supported for cloud gateway resources, got mode: %q", adoptOptions.Mode)
		default:
			err = adoptKonnectCloudGatewayNetworkMatch(ctx, sdk.GetCloudGatewaysSDK(), ent, adoptOptions.Konnect.ID)
		}
	case *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration:
		switch {
		case adoptOptions.Konnect == nil || adoptOptions.Konnect.ID == "":
			err = fmt.Errorf("konnect ID must be provided for adoption")
		case adoptOptions.Mode != "" && adoptOptions.Mode != commonv1alpha1.AdoptModeMatch:
			err = fmt.Errorf("only match mode adoption is supported for cloud gateway resources, got mode: %q", adoptOptions.Mode)
		default:
			err = adoptKonnectDataPlaneGroupConfigurationMatch(ctx, sdk.GetCloudGatewaysSDK(), cl, ent, adoptOptions.Konnect.ID)
		}
	case *konnectv1alpha1.KonnectCloudGatewayTransitGateway:
		switch {
		case adoptOptions.Konnect == nil || adoptOptions.Konnect.ID == "":
			err = fmt.Errorf("konnect ID must be provided for adoption")
		case adoptOptions.Mode != "" && adoptOptions.Mode != commonv1alpha1.AdoptModeMatch:
			err = fmt.Errorf("only match mode adoption is supported for cloud gateway resources, got mode: %q", adoptOptions.Mode)
		default:
			err = adoptKonnectTransitGatewayMatch(ctx, sdk.GetCloudGatewaysSDK(), ent, adoptOptions.Konnect.ID)
		}
	// TODO: implement adoption for other types.
	default:
		return ctrl.Result{}, fmt.Errorf("unsupported entity type %T", ent)
	}

	// Set "Adopted" and "Programmed" conditions of the object based on errors returned in the adopt operation.
	var (
		errSDK         *sdkkonnecterrs.SDKError
		errFetch       KonnectEntityAdoptionFetchError
		errUIDConflict KonnectEntityAdoptionUIDTagConflictError
		errNotMatch    KonnectEntityAdoptionNotMatchError
	)

	switch {
	// If the adoption process failed to fetch the entity, we use the "FetchFailed" reason in the "adopted" condition.
	case errors.As(err, &errFetch):
		if errors.As(errFetch.Err, &errSDK) {
			statusCode = errSDK.StatusCode
		}
		SetKonnectEntityAdoptedConditionFalse(e, konnectv1alpha1.KonnectEntityAdoptedReasonFetchFailed, err)
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
	case errors.As(err, &errUIDConflict):
		SetKonnectEntityAdoptedConditionFalse(e, konnectv1alpha1.KonnectEntityAdoptedReasonUIDConflict, errUIDConflict)
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, errUIDConflict)
	case errors.As(err, &errNotMatch):
		SetKonnectEntityAdoptedConditionFalse(e, konnectv1alpha1.KonnectEntityAdoptedReasonNotMatch, errNotMatch)
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, errNotMatch)
	case errors.As(err, &errSDK):
		statusCode = errSDK.StatusCode
		SetKonnectEntityAdoptedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, errSDK)
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, errSDK)
	case err != nil:
		SetKonnectEntityAdoptedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
		SetKonnectEntityProgrammedConditionFalse(e, kcfgkonnect.KonnectEntitiesFailedToAdoptReason, err)
	default:
		SetKonnectEntityAdoptedConditionTrue(e)
		SetKonnectEntityProgrammedConditionTrue(e)
	}

	if err != nil {
		metricRecorder.RecordKonnectEntityOperationFailure(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationAdopt,
			entityType,
			time.Since(start),
			statusCode,
		)
	} else {
		metricRecorder.RecordKonnectEntityOperationSuccess(
			sdk.GetServerURL(),
			metrics.KonnectEntityOperationAdopt,
			entityType,
			time.Since(start),
		)
	}

	logOpComplete(ctx, start, AdoptOp, e, err)
	return ctrl.Result{}, IgnoreUnrecoverableAPIErr(err, loggerForEntity(ctx, e, AdoptOp))
}

func loggerForEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, e TEnt, op Op) logr.Logger {
	keysAndValues := []any{
		"op", op,
	}

	// Only add the Konnect ID if it exists and it's a create operation.
	// Otherwise the Konnect ID is already set in the logger.
	if id := e.GetKonnectStatus().GetKonnectID(); id != "" && op == CreateOp {
		keysAndValues = append(keysAndValues, "konnect_id", id)
	}
	return ctrllog.FromContext(ctx).WithValues(keysAndValues...)
}

func logOpComplete[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, start time.Time, op Op, e TEnt, err error) {
	// if the entity is a Mirror, don't log the konnect operation,
	// as no operation occurred.
	if isMirrorableEntity(e) {
		if isMirrorEntity(e) {
			return
		}
	}

	logger := loggerForEntity(ctx, e, op).
		WithValues("duration", time.Since(start).String())

	if err != nil {
		// NOTE: We don't want to print stack trace information here so skip 99 frames
		// just in case.
		logger.WithCallDepth(99).Error(err, "operation in Konnect API failed")
		return
	}
	logger.Info("operation in Konnect API complete")
}

// wrapErrIfKonnectOpFailed checks the response from the Konnect API and returns a uniform
// error for all Konnect entities if the operation failed.
func wrapErrIfKonnectOpFailed[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](err error, op Op, e TEnt) error {
	if err != nil {
		var errBadRequest *sdkkonnecterrs.BadRequestError
		if errors.As(err, &errBadRequest) {
			errBadRequest.Instance = ""
			err = errBadRequest
		}

		entityTypeName := constraints.EntityTypeName[T]()
		if e == nil {
			return fmt.Errorf("failed to %s %s: %w",
				op, entityTypeName, err,
			)
		}
		return fmt.Errorf("failed to %s %s %s: %w",
			op, entityTypeName, client.ObjectKeyFromObject(e), err,
		)
	}
	return nil
}

func logEntityNotFoundRecreating[
	T constraints.SupportedKonnectEntityType,
](ctx context.Context, _ *T, id string) {
	ctrllog.FromContext(ctx).
		Info(
			"entity not found in Konnect, trying to recreate",
			"type", constraints.EntityTypeName[T](),
			"id", id,
		)
}

// sliceToEntityWithIDPtrSlice converts a slice of entities to a slice of entityWithIDPtr.
func sliceToEntityWithIDPtrSlice[
	T any,
	TPtr interface {
		*T
		GetID() *string
	},
](
	slice []T,
) []TPtr {
	result := make([]TPtr, 0, len(slice))
	for _, item := range slice {
		result = append(result, TPtr(&item))
	}
	return result
}

// sliceToEntityWithIDSlice converts a slice of entities to a slice of entityWithID.
func sliceToEntityWithIDSlice[
	T any,
	TPtr interface {
		*T
		GetID() string
	},
](
	slice []T,
) []TPtr {
	result := make([]TPtr, 0, len(slice))
	for _, item := range slice {
		result = append(result, TPtr(&item))
	}
	return result
}

// extractedEntityID represents the ID of an entity.
// The type is defined to satisfy the interface in the parameter of getMatchingEntryFromListResponseData.
// Some types of responses do not provide a GetID in sdk-konnect-go
// so we need to extract the ID and return this type for getMatchingEntryFromListResponseData.
type extractedEntityID string

// GetID returns the extracted ID. Implements the interface in the parameter of getMatchingEntryFromListResponseData.
func (e extractedEntityID) GetID() string {
	return string(e)
}

// getMatchingEntryFromListResponseData returns the ID of the first entry in the list response data.
// It returns an error if no entry with a non-empty ID was found.
// It is used in conjunction with the list operation to get the ID of the entity that matches the UID
// hence no filtering is done here because it is assumed that the provided list response data is already filtered.
func getMatchingEntryFromListResponseData[
	T interface {
		GetID() IDType
	},
	IDType string | *string,
](
	data []T,
	entity entity,
) (string, error) {
	var id string
	for _, entry := range data {
		entryID := entry.GetID()
		switch entryID := any(entryID).(type) {
		case string:
			if entryID != "" {
				id = entryID
				break
			}
		case *string:
			if entryID != nil && *entryID != "" {
				id = *entryID
				break
			}
		}
	}

	if id == "" {
		return "", EntityWithMatchingUIDNotFoundError{
			Entity: entity,
		}
	}

	return id, nil
}

// ClearInstanceFromError clears the instance field from the error.
// This is needed because the instance field contains the trace ID which changes
// with each request and makes the reconciliation loop requeue the resource
// instead of performing the backoff.
func ClearInstanceFromError(err error) error {
	var errBadRequest *sdkkonnecterrs.BadRequestError
	if errors.As(err, &errBadRequest) {
		errBadRequest.Instance = ""
		return errBadRequest
	}

	var errConflict *sdkkonnecterrs.ConflictError
	if errors.As(err, &errConflict) {
		errConflict.Instance = ""
		return errConflict
	}

	var errNotFound *sdkkonnecterrs.NotFoundError
	if errors.As(err, &errNotFound) {
		errNotFound.Instance = ""
		return errNotFound
	}

	return err
}

// isMirrorableEntity checks if the entity is mirrorable.
// This is used to determine if the entity can be mirrored to Konnect.
func isMirrorableEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt) bool {
	switch any(ent).(type) {
	case *konnectv1alpha2.KonnectGatewayControlPlane:
		return true
	default:
		return false
	}
}

// isMirrorEntity checks if the entity is a mirror entity.
// This is used to determine if the entity is a mirror of a Konnect entity.
func isMirrorEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt) bool {
	switch cp := any(ent).(type) {
	case *konnectv1alpha2.KonnectGatewayControlPlane:
		return cp.Spec.Source != nil && *cp.Spec.Source == commonv1alpha1.EntitySourceMirror
	default:
		return false
	}
}

// findUIDTag finds tags annotating the k8s UID.
func findUIDTag(tags []string) (string, bool) {
	return lo.Find(tags, func(s string) bool {
		return strings.HasPrefix(s, "k8s-uid:")
	})
}

// extractUIDFromTag extracts the k8s UID from the tag.
func extractUIDFromTag(tag string) string {
	return strings.TrimPrefix(tag, "k8s-uid:")
}

// equalWithDefault compares two values from the pointers and fallback to
// the given default value if the pointer is nil or the value is the zero value for the given type.
func equalWithDefault[T comparable](
	a *T, b *T, defaultValue T,
) bool {
	var aVal, bVal T
	if a == nil || lo.IsEmpty(*a) {
		aVal = defaultValue
	}
	if b == nil || lo.IsEmpty(*b) {
		bVal = defaultValue
	}
	return aVal == bVal
}
