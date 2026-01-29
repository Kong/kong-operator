package license

import (
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/mo"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	internal "github.com/kong/kong-operator/ingress-controller/internal/controllers/license"
	"github.com/kong/kong-operator/ingress-controller/internal/util/kubernetes/object/status"
)

type ValidatorFunc = internal.ValidatorFunc
type KongV1Alpha1KongLicenseReconciler = internal.KongV1Alpha1KongLicenseReconciler

const (
	LicenseControllerTypeKIC  = internal.LicenseControllerTypeKIC
	ConditionTypeProgrammed   = internal.ConditionTypeProgrammed
	ConditionTypeLicenseValid = internal.ConditionTypeLicenseValid
)

func NewKongV1Alpha1KongLicenseReconciler(
	client client.Client,
	log logr.Logger,
	scheme *runtime.Scheme,
	licenseCache cache.Store,
	cacheSyncTimeout time.Duration,
	statusQueue *status.Queue,
	licenseControllerType string,
	electionID mo.Option[string],
	licenseValidator mo.Option[ValidatorFunc],
) *KongV1Alpha1KongLicenseReconciler {
	return internal.NewKongV1Alpha1KongLicenseReconciler(
		client,
		log,
		scheme,
		licenseCache,
		cacheSyncTimeout,
		statusQueue,
		licenseControllerType,
		electionID,
		licenseValidator,
	)
}

func NewLicenseCache() cache.Store {
	return internal.NewLicenseCache()
}
