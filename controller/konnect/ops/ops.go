package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// Response is the interface for the response from the Konnect API.
type Response interface {
	GetContentType() string
	GetStatusCode() int
}

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

// Create creates a Konnect entity.
func Create[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	sdk SDKWrapper,
	cl client.Client,
	e TEnt,
) (*T, error) {
	var (
		err    error
		id     string
		reason consts.ConditionReason
		start  = time.Now()
	)
	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectGatewayControlPlane:
		id, reason, err = createControlPlane(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
		// TODO: modify the create* operation wrappers to return Konnect ID and error reason.
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
		err = createKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), ent)
		// ---------------------------------------------------------------------
		// TODO: add other Konnect types
	default:
		return nil, fmt.Errorf("unsupported entity type %T", ent)
	}

	var errConflict *sdkkonnecterrs.ConflictError
	switch {
	case errors.As(err, &errConflict):
		// If there was a conflict on the create request, we can assume the entity already exists.
		// We'll get its Konnect ID by listing all entities of its type filtered by the Kubernetes object UID.
		switch ent := any(e).(type) {
		case *konnectv1alpha1.KonnectGatewayControlPlane:
			id, err = getControlPlaneForUID(ctx, sdk.GetControlPlaneSDK(), ent)
			// ---------------------------------------------------------------------
			// TODO: add other Konnect types
		default:

		}

		if err == nil && id != "" {
			e.SetKonnectID(id)
			SetKonnectEntityProgrammedCondition(e)
		} else {
			SetKonnectEntityProgrammedConditionFalse(e, consts.KonnectEntitiesFailedToCreateReason, err.Error())
		}
	case err != nil:
		if id != "" {
			e.SetKonnectID(id)
		}
		if reason == "" {
			reason = consts.KonnectEntitiesFailedToCreateReason
		}
		SetKonnectEntityProgrammedConditionFalse(e, reason, err.Error())
	case id != "":
		// TODO: remove the check after all create* returns Konnect ID.
		e.SetKonnectID(id)
		SetKonnectEntityProgrammedCondition(e)
	default:
	}

	logOpComplete[T, TEnt](ctx, start, CreateOp, e, err)

	return e, err
}

// Delete deletes a Konnect entity.
// It returns an error if the entity does not have a Konnect ID or if the operation fails.
func Delete[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, sdk SDKWrapper, cl client.Client, e *T) error {
	ent := TEnt(e)
	if ent.GetKonnectStatus().GetKonnectID() == "" {
		return fmt.Errorf(
			"can't delete %T %s when it does not have the Konnect ID",
			ent, client.ObjectKeyFromObject(ent),
		)
	}

	var (
		err   error
		start = time.Now()
	)
	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectGatewayControlPlane:
		err = deleteControlPlane(ctx, sdk.GetControlPlaneSDK(), ent)
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
		err = deleteKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), ent)
		// ---------------------------------------------------------------------
		// TODO: add other Konnect types
	default:
		return fmt.Errorf("unsupported entity type %T", ent)
	}

	logOpComplete[T, TEnt](ctx, start, DeleteOp, e, err)

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
			"no need for update, requeueing after configured sync period", ent,
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
	sdk SDKWrapper,
	syncPeriod time.Duration,
	cl client.Client,
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
		err    error
		id     string
		reason consts.ConditionReason
	)
	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectGatewayControlPlane:
		id, reason, err = updateControlPlane(ctx, sdk.GetControlPlaneSDK(), sdk.GetControlPlaneGroupSDK(), cl, ent)
	case *configurationv1alpha1.KongService:
		// TODO: modify the update* operation wrappers to return Konnect ID and error reason.
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

	switch {
	case err != nil:
		if id != "" {
			e.SetKonnectID(id)
		}
		if reason == "" {
			reason = consts.KonnectEntitiesFailedToUpdateReason
		}
		SetKonnectEntityProgrammedConditionFalse(e, reason, err.Error())

	case id != "":
		// TODO: remove the check after all update* returns Konnect ID.
		e.SetKonnectID(id)
		SetKonnectEntityProgrammedCondition(e)
	default:
	}

	logOpComplete[T, TEnt](ctx, now, UpdateOp, e, err)

	return ctrl.Result{}, err
}

func logOpComplete[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, start time.Time, op Op, e TEnt, err error) {
	keysAndValues := []interface{}{
		"op", op,
		"duration", time.Since(start).String(),
	}

	// Only add the Konnect ID if it exists and it's a create operation.
	// Otherwise the Konnect ID is already set in the logger.
	if id := e.GetKonnectStatus().GetKonnectID(); id != "" && op == CreateOp {
		keysAndValues = append(keysAndValues, "konnect_id", id)
	}
	logger := ctrllog.FromContext(ctx).WithValues(keysAndValues...)

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
