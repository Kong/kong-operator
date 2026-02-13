package eventually

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForObjectToNotExist waits for the given object to be deleted from the cluster.
// It returns true if the object no longer exists, false otherwise.
// This function accepts an optional message that will be printed in the error
// message if the object still exists after the wait time.
func WaitForObjectToNotExist(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj client.Object,
	waitTime time.Duration,
	tickTime time.Duration,
	msg ...string,
) bool {
	t.Helper()
	nn := client.ObjectKeyFromObject(obj)

	t.Logf("Waiting for %T %s to disappear", obj, nn)

	errMsg := fmt.Sprintf("%T %s still exists", obj, nn)
	if len(msg) > 0 {
		errMsg = fmt.Sprintf("%T %s still exists: %s", obj, nn, msg)
	}

	return assert.EventuallyWithT(t,
		func(c *assert.CollectT) {
			err := cl.Get(ctx, nn, obj)
			assert.True(c, err != nil && apierrors.IsNotFound(err))
		},
		waitTime, tickTime,
		errMsg,
	)
}
