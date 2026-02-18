package validatingwebhook

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/test/integration"
)

type testCaseIngressValidation struct {
	Name                   string
	Ingress                *netv1.Ingress
	WantCreateErrSubstring string
}

func TestAdmissionWebhook_Ingress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ns, _, ingressClass, _, _ := bootstrapGateway(ctx, t, integration.GetEnv(), integration.GetClients().MgrClient) //nolint:dogsled

	k8sClient := integration.GetEnv().Cluster().Client()

	testCases := []testCaseIngressValidation{
		{
			Name: "a valid ingress passes validation",
			Ingress: newIngress(uuid.NewString(), ingressClass,
				constructIngressRuleWithPathsImplSpecific("", "/foo"),
			),
		},
		{
			Name: "an invalid ingress passes validation when Ingress class is not set to KIC's (it's not ours)",
			Ingress: newIngress(uuid.NewString(), "third-party-ingress-class",
				constructIngressRuleWithPathsImplSpecific("", "/foo", "/~/foo[[["),
			),
		},
		{
			Name: "an invalid ingress passes validation when Ingress class is not set to KIC's (it's not ours), usage of legacy annotation",
			Ingress: newIngressWithLegacyClassAnnotation(uuid.NewString(), "third-party-ingress-class",
				constructIngressRuleWithPathsImplSpecific("", "/foo", "/~/foo[[["),
			),
		},
		{
			Name: "valid Ingress with multiple hosts, paths (with valid regex expressions) passes validation",
			Ingress: newIngressWithLegacyClassAnnotation(uuid.NewString(), "third-party-ingress-class",
				constructIngressRuleWithPathsImplSpecific("foo.com", "/foo", "/bar[1-9]"),
				constructIngressRuleWithPathsImplSpecific("bar.com", "/baz"),
				constructIngressRuleWithPathsImplSpecific("", "/test", "/~/foo[1-9]"),
			),
		},
		{
			Name: "fail when path in Ingress does not start with '/' (K8s builtin Ingress validation)",
			Ingress: newIngress(uuid.NewString(), ingressClass,
				constructIngressRuleWithPathsImplSpecific("", "~/foo[1-9]", "/bar"),
			),
			WantCreateErrSubstring: "Invalid value: \"~/foo[1-9]\": must be an absolute path",
		},
		{
			Name: "valid path format with invalid regex expression fails validation",
			Ingress: newIngress(uuid.NewString(), ingressClass,
				constructIngressRuleWithPathsImplSpecific("", "/bar", "/~/baz[1-9]"),
				constructIngressRuleWithPathsImplSpecific("", "/~/foo[[["),
			),
			WantCreateErrSubstring: "/foo[[[",
		},
	}

	for _, tC := range testCases {
		t.Run(tC.Name, func(t *testing.T) {
			_, err := k8sClient.NetworkingV1().Ingresses(ns.Name).Create(ctx, tC.Ingress, metav1.CreateOptions{})
			if tC.WantCreateErrSubstring != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tC.WantCreateErrSubstring)
			} else {
				require.NoError(t, err)
				t.Cleanup(func() {
					_ = k8sClient.NetworkingV1().Ingresses(ns.Name).Delete(ctx, tC.Ingress.Name, metav1.DeleteOptions{})
				})
			}
		})
	}
}

func newIngress(name string, class string, rules ...netv1.IngressRule) *netv1.Ingress {
	var classToSet *string
	if class != "" {
		classToSet = &class
	}
	return &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: make(map[string]string),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		Spec: netv1.IngressSpec{
			IngressClassName: classToSet,
			Rules:            rules,
		},
	}
}

func newIngressWithLegacyClassAnnotation(name string, class string, rules ...netv1.IngressRule) *netv1.Ingress {
	ingress := newIngress(name, "", rules...)
	ingress.Annotations["kubernetes.io/ingress.class"] = class
	return ingress
}

func constructIngressRuleWithPathsImplSpecific(host string, paths ...string) netv1.IngressRule {
	var pathsToSet []netv1.HTTPIngressPath
	for _, path := range paths {
		pathsToSet = append(
			pathsToSet,
			netv1.HTTPIngressPath{
				Path:     path,
				PathType: new(netv1.PathTypeImplementationSpecific),
				Backend: netv1.IngressBackend{
					Service: &netv1.IngressServiceBackend{
						Name: "foo",
						Port: netv1.ServiceBackendPort{
							Number: 80,
						},
					},
				},
			},
		)
	}
	return netv1.IngressRule{
		Host: host,
		IngressRuleValue: netv1.IngressRuleValue{
			HTTP: &netv1.HTTPIngressRuleValue{
				Paths: pathsToSet,
			},
		},
	}
}
