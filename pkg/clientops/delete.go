package clientops

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
)

// DeleteAllFromList deletes all items in the given list one by one.
func DeleteAllFromList[
	TList interface {
		GetItems() []T
	},
	TListPtr interface {
		*TList
		client.ObjectList
		GetItems() []T
	},
	T constraints.SupportedKonnectEntityType,
	TT constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	list TListPtr,
) error {
	return DeleteAll[T, TT](ctx, cl, list.GetItems())
}

// DeleteAll deletes all provided items one by one.
func DeleteAll[
	T any,
	TPtr interface {
		*T
		client.Object
	},
](
	ctx context.Context,
	cl client.Client,
	list []T,
) error {
	for _, obj := range list {
		typ := fmt.Sprintf("%T", obj)
		if objGetType, ok := any(obj).(interface {
			GetTypeName() string
		}); ok {
			typ = objGetType.GetTypeName()
		}

		var objPtr TPtr = &obj

		nn := client.ObjectKeyFromObject(objPtr)
		if err := cl.Delete(ctx, objPtr); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed deleting %s %s: %w", typ, nn, err)
		}
	}
	return nil
}
