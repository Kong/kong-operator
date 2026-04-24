package gateway

import (
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

func TestHandleUpdateError(t *testing.T) {
	obj := &gatewayapi.HTTPRoute{}

	conflict := apierrors.NewConflict(schema.GroupResource{}, "test-obj", errors.New("resource version conflict"))
	other := errors.New("some other error")

	tests := []struct {
		name       string
		inputErr   error
		wantResult ctrl.Result
		wantErr    error
	}{
		{
			name:       "nil error returns zero result and nil error",
			inputErr:   nil,
			wantResult: ctrl.Result{},
			wantErr:    nil,
		},
		{
			// A 409 Conflict must be returned as an immediate requeue, and no error should be returned.
			name:       "conflict error returns zero result with immediate requeue and nil error",
			inputErr:   conflict,
			wantResult: ctrl.Result{Requeue: true},
			wantErr:    nil,
		},
		{
			name:       "non-conflict error returned unchanged",
			inputErr:   other,
			wantResult: ctrl.Result{},
			wantErr:    other,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotResult, gotErr := handleUpdateError(tc.inputErr, logr.Discard(), obj)
			assert.Equal(t, tc.wantResult, gotResult)
			assert.Equal(t, tc.wantErr, gotErr)
		})
	}
}
