package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/internal/metrics"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// Op is the type for the operation type of a Konnect entity.
type Op string

const (
	// CreateOp is the operation type for creating a Konnect entity.
	CreateOp Op = "create"
	// UpdateOp is the operation type for updating a Konnect entity.
	UpdateOp Op = "update"
	// DeleteOp is the operation type for deleting a Konnect entity.
	DeleteOp Op = "delete"
)

// EntityTypeName is the type of the Konnect entity name used for distinguish operations on different types of entities in the prometheus metrics.
type EntityTypeName string

// Entity type names for Konnect entity as labels in metrics.
const (
	// Entity type names used in metrics.
	// REVIEW: Should we use the path inside the API as the type names? These are not very consistent in
	EntityTypeControlPlane         EntityTypeName = "control_planes"
	EntityTypeService              EntityTypeName = "services"
	EntityTypeRoute                EntityTypeName = "routes"
	EntityTypeConsumer             EntityTypeName = "consumers"
	EntityTypeConsumerGroup        EntityTypeName = "consumer_groups"
	EntityTypePlugin               EntityTypeName = "plugins"
	EntityTypeUpstream             EntityTypeName = "upstreams"
	EntityTypeTarget               EntityTypeName = "targets"
	EntityTypeBasicAuthCredential  EntityTypeName = "basic_auth_credentials" //nolint:gosec
	EntityTypeAPIKeyCredential     EntityTypeName = "api_key_credentials"    //nolint:gosec
	EntityTypeACLCredential        EntityTypeName = "acl_credentials"
	EntityTypeJWTCredential        EntityTypeName = "jwt_credentials"
	EntityTypeHMACCredential       EntityTypeName = "hmac_credentials"
	EntityTypeCACertificate        EntityTypeName = "ca_certificates"
	EntityTypeCertificate          EntityTypeName = "certificates"
	EntityTypeSNI                  EntityTypeName = "snis"
	EntityTypeKey                  EntityTypeName = "keys"
	EntityTypeKeySet               EntityTypeName = "key_sets"
	EntityTypeVault                EntityTypeName = "vaults"
	EntityTypeDataPlaneCertificate EntityTypeName = "data_plane_certificates"
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

		entityType EntityTypeName
		statusCode int
	)
	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectGatewayControlPlane:
		entityType = EntityTypeControlPlane
		err = createControlPlane(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
	case *configurationv1alpha1.KongService:
		entityType = EntityTypeService
		err = createService(ctx, sdk.GetServicesSDK(), ent)
	case *configurationv1alpha1.KongRoute:
		entityType = EntityTypeRoute
		err = createRoute(ctx, sdk.GetRoutesSDK(), ent)
	case *configurationv1.KongConsumer:
		entityType = EntityTypeConsumer
		err = createConsumer(ctx, sdk.GetConsumersSDK(), sdk.GetConsumerGroupsSDK(), cl, ent)
	case *configurationv1beta1.KongConsumerGroup:
		entityType = EntityTypeConsumerGroup
		err = createConsumerGroup(ctx, sdk.GetConsumerGroupsSDK(), ent)
	case *configurationv1alpha1.KongPluginBinding:
		entityType = EntityTypePlugin
		err = createPlugin(ctx, cl, sdk.GetPluginSDK(), ent)
	case *configurationv1alpha1.KongUpstream:
		entityType = EntityTypeUpstream
		err = createUpstream(ctx, sdk.GetUpstreamsSDK(), ent)
	case *configurationv1alpha1.KongCredentialBasicAuth:
		entityType = EntityTypeBasicAuthCredential
		err = createKongCredentialBasicAuth(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialAPIKey:
		entityType = EntityTypeAPIKeyCredential
		err = createKongCredentialAPIKey(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialACL:
		entityType = EntityTypeACLCredential
		err = createKongCredentialACL(ctx, sdk.GetACLCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialJWT:
		entityType = EntityTypeJWTCredential
		err = createKongCredentialJWT(ctx, sdk.GetJWTCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialHMAC:
		entityType = EntityTypeHMACCredential
		err = createKongCredentialHMAC(ctx, sdk.GetHMACCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCACertificate:
		entityType = EntityTypeCACertificate
		err = createCACertificate(ctx, sdk.GetCACertificatesSDK(), ent)
	case *configurationv1alpha1.KongCertificate:
		entityType = EntityTypeCertificate
		err = createCertificate(ctx, sdk.GetCertificatesSDK(), ent)
	case *configurationv1alpha1.KongTarget:
		entityType = EntityTypeTarget
		err = createTarget(ctx, sdk.GetTargetsSDK(), ent)
	case *configurationv1alpha1.KongVault:
		entityType = EntityTypeVault
		err = createVault(ctx, sdk.GetVaultSDK(), ent)
	case *configurationv1alpha1.KongKey:
		entityType = EntityTypeKey
		err = createKey(ctx, sdk.GetKeysSDK(), ent)
	case *configurationv1alpha1.KongKeySet:
		entityType = EntityTypeKeySet
		err = createKeySet(ctx, sdk.GetKeySetsSDK(), ent)
	case *configurationv1alpha1.KongSNI:
		entityType = EntityTypeSNI
		err = createSNI(ctx, sdk.GetSNIsSDK(), ent)
	case *configurationv1alpha1.KongDataPlaneClientCertificate:
		entityType = EntityTypeDataPlaneCertificate
		err = createKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), ent)
		// ---------------------------------------------------------------------
		// TODO: add other Konnect types
	default:
		return nil, fmt.Errorf("unsupported entity type %T", ent)
	}

	var (
		errRelationsFailed KonnectEntityCreatedButRelationsFailedError
		errSDK             *sdkkonnecterrs.SDKError
	)
	switch {
	case ErrorIsCreateConflict(err):
		// If there was a conflict on the create request, we can assume the entity already exists.
		// We'll get its Konnect ID by listing all entities of its type filtered by the Kubernetes object UID.
		var id string
		switch ent := any(e).(type) {
		case *konnectv1alpha1.KonnectGatewayControlPlane:
			id, err = getControlPlaneForUID(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
		case *configurationv1alpha1.KongService:
			id, err = getKongServiceForUID(ctx, sdk.GetServicesSDK(), ent)
		case *configurationv1alpha1.KongRoute:
			id, err = getKongRouteForUID(ctx, sdk.GetRoutesSDK(), ent)
		case *configurationv1alpha1.KongSNI:
			id, err = getKongSNIForUID(ctx, sdk.GetSNIsSDK(), ent)
		case *configurationv1.KongConsumer:
			id, err = getKongConsumerForUID(ctx, sdk.GetConsumersSDK(), ent)
		case *configurationv1beta1.KongConsumerGroup:
			id, err = getKongConsumerGroupForUID(ctx, sdk.GetConsumerGroupsSDK(), ent)
		case *configurationv1alpha1.KongKeySet:
			id, err = getKongKeySetForUID(ctx, sdk.GetKeySetsSDK(), ent)
		case *configurationv1alpha1.KongKey:
			id, err = getKongKeyForUID(ctx, sdk.GetKeysSDK(), ent)
		case *configurationv1alpha1.KongUpstream:
			id, err = getKongUpstreamForUID(ctx, sdk.GetUpstreamsSDK(), ent)
		case *configurationv1alpha1.KongTarget:
			id, err = getKongTargetForUID(ctx, sdk.GetTargetsSDK(), ent)
		case *configurationv1alpha1.KongPluginBinding:
			id, err = getPluginForUID(ctx, sdk.GetPluginSDK(), ent)
		case *configurationv1alpha1.KongVault:
			id, err = getKongVaultForUID(ctx, sdk.GetVaultSDK(), ent)
		case *configurationv1alpha1.KongCredentialHMAC:
			id, err = getKongCredentialHMACForUID(ctx, sdk.GetHMACCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialJWT:
			id, err = getKongCredentialJWTForUID(ctx, sdk.GetJWTCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialBasicAuth:
			id, err = getKongCredentialBasicAuthForUID(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialAPIKey:
			id, err = getKongCredentialAPIKeyForUID(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCredentialACL:
			id, err = getKongCredentialACLForUID(ctx, sdk.GetACLCredentialsSDK(), ent)
		case *configurationv1alpha1.KongCertificate:
			id, err = getKongCertificateForUID(ctx, sdk.GetCertificatesSDK(), ent)
		case *configurationv1alpha1.KongCACertificate:
			id, err = getKongCACertificateForUID(ctx, sdk.GetCACertificatesSDK(), ent)
			// ---------------------------------------------------------------------
			// TODO: add other Konnect types
		default:
			return e, fmt.Errorf("conflict on create request for %T %s, but no conflict handling implemented: %w",
				e, client.ObjectKeyFromObject(e), err,
			)
		}

		if err == nil && id != "" {
			e.SetKonnectID(id)
			SetKonnectEntityProgrammedCondition(e)
		} else {
			SetKonnectEntityProgrammedConditionFalse(e, consts.KonnectEntitiesFailedToCreateReason, err.Error())
		}

	case errors.As(err, &errSDK):
		statusCode = errSDK.StatusCode
		SetKonnectEntityProgrammedConditionFalse(e, consts.KonnectEntitiesFailedToCreateReason, errSDK.Error())
	case errors.As(err, &errRelationsFailed):
		e.SetKonnectID(errRelationsFailed.KonnectID)
		SetKonnectEntityProgrammedConditionFalse(e, errRelationsFailed.Reason, errRelationsFailed.Err.Error())
	case err != nil:
		SetKonnectEntityProgrammedConditionFalse(e, consts.KonnectEntitiesFailedToCreateReason, err.Error())
	default:
		SetKonnectEntityProgrammedCondition(e)
	}

	if err != nil {
		metricRecorder.RecordKonnectEntityOperationFailure(
			metrics.KonnectEntityOperationCreate,
			string(entityType),
			time.Since(start),
			statusCode,
		)
	} else {
		metricRecorder.RecordKonnectEntityOperationSuccess(
			metrics.KonnectEntityOperationCreate,
			string(entityType),
			time.Since(start),
		)
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
		err   error
		start = time.Now()

		entityType EntityTypeName
		statusCode int
	)
	switch ent := any(ent).(type) {
	case *konnectv1alpha1.KonnectGatewayControlPlane:
		entityType = EntityTypeControlPlane
		err = deleteControlPlane(ctx, sdk.GetControlPlaneSDK(), ent)
	case *configurationv1alpha1.KongService:
		entityType = EntityTypeService
		err = deleteService(ctx, sdk.GetServicesSDK(), ent)
	case *configurationv1alpha1.KongRoute:
		entityType = EntityTypeRoute
		err = deleteRoute(ctx, sdk.GetRoutesSDK(), ent)
	case *configurationv1.KongConsumer:
		entityType = EntityTypeConsumer
		err = deleteConsumer(ctx, sdk.GetConsumersSDK(), ent)
	case *configurationv1beta1.KongConsumerGroup:
		entityType = EntityTypeConsumerGroup
		err = deleteConsumerGroup(ctx, sdk.GetConsumerGroupsSDK(), ent)
	case *configurationv1alpha1.KongPluginBinding:
		entityType = EntityTypePlugin
		err = deletePlugin(ctx, sdk.GetPluginSDK(), ent)
	case *configurationv1alpha1.KongUpstream:
		entityType = EntityTypeUpstream
		err = deleteUpstream(ctx, sdk.GetUpstreamsSDK(), ent)
	case *configurationv1alpha1.KongCredentialBasicAuth:
		entityType = EntityTypeBasicAuthCredential
		err = deleteKongCredentialBasicAuth(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialAPIKey:
		entityType = EntityTypeAPIKeyCredential
		err = deleteKongCredentialAPIKey(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialACL:
		entityType = EntityTypeACLCredential
		err = deleteKongCredentialACL(ctx, sdk.GetACLCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialJWT:
		entityType = EntityTypeJWTCredential
		err = deleteKongCredentialJWT(ctx, sdk.GetJWTCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialHMAC:
		entityType = EntityTypeHMACCredential
		err = deleteKongCredentialHMAC(ctx, sdk.GetHMACCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCACertificate:
		entityType = EntityTypeCACertificate
		err = deleteCACertificate(ctx, sdk.GetCACertificatesSDK(), ent)
	case *configurationv1alpha1.KongCertificate:
		entityType = EntityTypeCertificate
		err = deleteCertificate(ctx, sdk.GetCertificatesSDK(), ent)
	case *configurationv1alpha1.KongTarget:
		entityType = EntityTypeTarget
		err = deleteTarget(ctx, sdk.GetTargetsSDK(), ent)
	case *configurationv1alpha1.KongVault:
		entityType = EntityTypeVault
		err = deleteVault(ctx, sdk.GetVaultSDK(), ent)
	case *configurationv1alpha1.KongKey:
		entityType = EntityTypeKey
		err = deleteKey(ctx, sdk.GetKeysSDK(), ent)
	case *configurationv1alpha1.KongKeySet:
		entityType = EntityTypeKeySet
		err = deleteKeySet(ctx, sdk.GetKeySetsSDK(), ent)
	case *configurationv1alpha1.KongSNI:
		entityType = EntityTypeSNI
		err = deleteSNI(ctx, sdk.GetSNIsSDK(), ent)
	case *configurationv1alpha1.KongDataPlaneClientCertificate:
		entityType = EntityTypeDataPlaneCertificate
		err = deleteKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), ent)
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
			metrics.KonnectEntityOperationDelete,
			string(entityType),
			time.Since(start),
			statusCode,
		)
	} else {
		metricRecorder.RecordKonnectEntityOperationSuccess(
			metrics.KonnectEntityOperationDelete,
			string(entityType),
			time.Since(start),
		)
	}
	logOpComplete(ctx, start, DeleteOp, ent, err)

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
			"last_update", condProgrammed.LastTransitionTime.Time.String(),
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
		err error

		entityType EntityTypeName
		statusCode int
		start      = time.Now()
	)
	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectGatewayControlPlane:
		entityType = EntityTypeControlPlane
		err = updateControlPlane(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
	case *configurationv1alpha1.KongService:
		entityType = EntityTypeService
		err = updateService(ctx, sdk.GetServicesSDK(), ent)
	case *configurationv1alpha1.KongRoute:
		entityType = EntityTypeRoute
		err = updateRoute(ctx, sdk.GetRoutesSDK(), ent)
	case *configurationv1.KongConsumer:
		entityType = EntityTypeConsumer
		err = updateConsumer(ctx, sdk.GetConsumersSDK(), sdk.GetConsumerGroupsSDK(), cl, ent)
	case *configurationv1beta1.KongConsumerGroup:
		entityType = EntityTypeConsumerGroup
		err = updateConsumerGroup(ctx, sdk.GetConsumerGroupsSDK(), ent)
	case *configurationv1alpha1.KongPluginBinding:
		entityType = EntityTypePlugin
		err = updatePlugin(ctx, sdk.GetPluginSDK(), cl, ent)
	case *configurationv1alpha1.KongUpstream:
		entityType = EntityTypeUpstream
		err = updateUpstream(ctx, sdk.GetUpstreamsSDK(), ent)
	case *configurationv1alpha1.KongCredentialBasicAuth:
		entityType = EntityTypeBasicAuthCredential
		err = updateKongCredentialBasicAuth(ctx, sdk.GetBasicAuthCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialAPIKey:
		entityType = EntityTypeAPIKeyCredential
		err = updateKongCredentialAPIKey(ctx, sdk.GetAPIKeyCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialACL:
		entityType = EntityTypeACLCredential
		err = updateKongCredentialACL(ctx, sdk.GetACLCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialJWT:
		entityType = EntityTypeJWTCredential
		err = updateKongCredentialJWT(ctx, sdk.GetJWTCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCredentialHMAC:
		entityType = EntityTypeJWTCredential
		err = updateKongCredentialHMAC(ctx, sdk.GetHMACCredentialsSDK(), ent)
	case *configurationv1alpha1.KongCACertificate:
		entityType = EntityTypeCACertificate
		err = updateCACertificate(ctx, sdk.GetCACertificatesSDK(), ent)
	case *configurationv1alpha1.KongCertificate:
		entityType = EntityTypeCertificate
		err = updateCertificate(ctx, sdk.GetCertificatesSDK(), ent)
	case *configurationv1alpha1.KongTarget:
		entityType = EntityTypeTarget
		err = updateTarget(ctx, sdk.GetTargetsSDK(), ent)
	case *configurationv1alpha1.KongVault:
		entityType = EntityTypeVault
		err = updateVault(ctx, sdk.GetVaultSDK(), ent)
	case *configurationv1alpha1.KongKey:
		entityType = EntityTypeKey
		err = updateKey(ctx, sdk.GetKeysSDK(), ent)
	case *configurationv1alpha1.KongKeySet:
		entityType = EntityTypeKeySet
		err = updateKeySet(ctx, sdk.GetKeySetsSDK(), ent)
	case *configurationv1alpha1.KongSNI:
		entityType = EntityTypeSNI
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
		SetKonnectEntityProgrammedConditionFalse(e, consts.KonnectEntitiesFailedToUpdateReason, errSDK.Body)
	case errors.As(err, &errRelationsFailed):
		e.SetKonnectID(errRelationsFailed.KonnectID)
		SetKonnectEntityProgrammedConditionFalse(e, errRelationsFailed.Reason, err.Error())
	case err != nil:
		SetKonnectEntityProgrammedConditionFalse(e, consts.KonnectEntitiesFailedToUpdateReason, err.Error())
	default:
		SetKonnectEntityProgrammedCondition(e)
	}

	if err != nil {
		metricRecorder.RecordKonnectEntityOperationFailure(
			metrics.KonnectEnttiyOperationUpdate,
			string(entityType),
			time.Since(start),
			statusCode,
		)
	} else {
		metricRecorder.RecordKonnectEntityOperationSuccess(
			metrics.KonnectEnttiyOperationUpdate,
			string(entityType),
			time.Since(start),
		)
	}
	logOpComplete(ctx, start, UpdateOp, e, err)

	return ctrl.Result{}, IgnoreUnrecoverableAPIErr(err, loggerForEntity(ctx, e, UpdateOp))
}

func loggerForEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, e TEnt, op Op) logr.Logger {
	keysAndValues := []interface{}{
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
