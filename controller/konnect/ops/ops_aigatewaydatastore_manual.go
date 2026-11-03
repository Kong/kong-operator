package ops

import (
	"context"
	"fmt"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// AIGatewayDatastoreInUseError is returned when Konnect rejects the deletion
// of an AIGatewayDatastore because other entities still use it. The deletion
// is blocked until those stop referencing it.
type AIGatewayDatastoreInUseError struct {
	// DatastoreName is the Konnect name of the datastore.
	DatastoreName string
	// Users names the Konnect entities still referencing the datastore, as
	// "<type>/<name>". It is empty when Konnect didn't report them.
	Users []string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e AIGatewayDatastoreInUseError) Error() string {
	return fmt.Sprintf(
		"datastore %s is in use by %s, deletion blocked: %v",
		e.DatastoreName, strings.Join(e.Users, ", "), e.Err,
	)
}

// Unwrap returns the underlying Konnect API error.
func (e AIGatewayDatastoreInUseError) Unwrap() error {
	return e.Err
}

// DeletionBlockedMessage returns a user-facing message naming what still uses
// the datastore.
func (e AIGatewayDatastoreInUseError) DeletionBlockedMessage() string {
	if len(e.Users) == 0 {
		return "deletion blocked: the datastore is still in use; remove it from the entities " +
			"that reference it and the deletion will proceed automatically"
	}
	return fmt.Sprintf(
		"deletion blocked: the datastore is in use by %s; remove it from them and the deletion will "+
			"proceed automatically",
		strings.Join(e.Users, ", "),
	)
}

// deleteAIGatewayDatastoreGuarded deletes an AIGatewayDatastore, but when
// Konnect refuses the deletion because the datastore is still referenced, it
// names the referencing entities in the returned error, so the reconciler can
// report a DeletionBlocked condition. The AIGatewayDatastore controller watches
// the referring entities, so the deletion resumes once the last one drops the
// reference.
func deleteAIGatewayDatastoreGuarded(
	ctx context.Context,
	sdk sdkkonnectgo.AIGatewayDatastoresSDK,
	obj *aiconfigurationv1alpha1.AIGatewayDatastore,
) error {
	err := deleteAIGatewayDatastore(ctx, sdk, obj)
	if err == nil || (!errorIsBadRequest(err) && !ErrorIsCreateConflict(err)) {
		return err
	}

	inUse := AIGatewayDatastoreInUseError{DatastoreName: obj.GetKonnectName(), Err: err}
	// shortcut: only the first page of users is reported, which is enough for
	// the DeletionBlocked message; paginate if the list ever needs to be exact.
	usage, usageErr := sdk.ListAiGatewayDatastoreUsage(ctx, sdkkonnectops.ListAiGatewayDatastoreUsageRequest{
		GatewayID:         obj.GetGatewayID(),
		DatastoreIDOrName: obj.GetKonnectStatus().GetKonnectID(),
	})
	if usageErr != nil {
		ctrllog.FromContext(ctx).Info("failed to list the entities using the datastore",
			"type", obj.GetTypeName(), "id", obj.GetKonnectStatus().GetKonnectID(), "error", usageErr.Error(),
		)
		return inUse
	}
	for _, u := range usage.GetListAIGatewayDatastoreUsageResponse().GetData() {
		inUse.Users = append(inUse.Users, fmt.Sprintf("%s/%s", u.GetType(), u.GetName()))
	}
	if len(inUse.Users) == 0 {
		return err
	}
	return inUse
}
