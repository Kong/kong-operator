package crdsvalidation_test

import (
	"testing"
	"time"

	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha2"
	common "github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestKonnectExtension(t *testing.T) {
	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha2.KonnectExtension]{
			{
				Name: "konnect controlplane, manual provisioning, valid secret",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
						},
						ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
							CertificateSecret: konnectv1alpha2.CertificateSecret{
								Provisioning: lo.ToPtr(konnectv1alpha2.ManualSecretProvisioning),
								CertificateSecretRef: &konnectv1alpha2.SecretRef{
									Name: "test-secret",
								},
							},
						},
					},
				},
			},
			{
				Name: "konnect controlplane, manual provisioning, secret",
				ExpectedErrorEventuallyConfig: common.EventuallyConfig{
					Timeout: 1 * time.Second,
				},
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
						},
						ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
							CertificateSecret: konnectv1alpha2.CertificateSecret{
								Provisioning: lo.ToPtr(konnectv1alpha2.ManualSecretProvisioning),
								CertificateSecretRef: &konnectv1alpha2.SecretRef{
									Name: "test-secret",
								},
							},
						},
					},
				},
			},
			{
				Name: "konnect controlplane, manual provisioning, no secret",
				ExpectedErrorEventuallyConfig: common.EventuallyConfig{
					Timeout: 1 * time.Second,
				},
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
						},
						ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
							CertificateSecret: konnectv1alpha2.CertificateSecret{
								Provisioning: lo.ToPtr(konnectv1alpha2.ManualSecretProvisioning),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("secretRef must be set when provisioning is set to Manual."),
			},
			{
				Name: "konnect controlplane, automatic provisioning, secret",
				ExpectedErrorEventuallyConfig: common.EventuallyConfig{
					Timeout: 1 * time.Second,
				},
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
						},
						ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
							CertificateSecret: konnectv1alpha2.CertificateSecret{
								Provisioning: lo.ToPtr(konnectv1alpha2.AutomaticSecretProvisioning),
								CertificateSecretRef: &konnectv1alpha2.SecretRef{
									Name: "test-secret",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("secretRef must not be set when provisioning is set to Automatic."),
			},
			{
				Name: "kic controlplane",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKIC,
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("kic type not supported as controlPlaneRef."),
			},
		}.Run(t)
	})
	t.Run("dataPlane labels", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha2.KonnectExtension]{
			{
				Name: "valid labels",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"valid-key": "valid.value",
								},
							},
						},
					},
				},
			},
			{
				Name: "invalid label value 1",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"valid-key": ".invalid.value",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'"),
			},
			{
				Name: "invalid label value 2",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"valid-key": "invalid.value.",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'"),
			},
			{
				Name: "invalid label value 3",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"valid-key": "invalid$value.",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'"),
			},
			{
				Name: "invalid label value 4",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"valid-key": "Xv9gTq2LmNZp4WJdCYKfRB86oAhsMEytkPUOQGV7Dbx53cHFnwzjL1rS0vqIXv9gTq2LmNZp4WJdCYKfRB86oAhsMEytkPUOQGV7Dbx53cHFnwzjL1rS0vqI",
								},
							},
						},
					},
				},
				// NOTE: Kubernetes 1.32 changed the validation error for values exceeding the maximum length.
				// It used to be:
				// "Too long: may not be longer than 63"
				// In 1.32+ it is:
				// "Too long: may not be more than 63 bytes"
				// We're using here the common part of the error message to avoid breaking the test when upgrading Kubernetes.
				ExpectedErrorMessage: lo.ToPtr("Too long: may not be "),
			},
			{
				Name: "invalid label key 1",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									".invalid.key": "valid.value",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'"),
			},
			{
				Name: "invalid label value 2",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"invalid.key.": "valid.value",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'"),
			},
			{
				Name: "invalid label value 3",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"invalid$key": "valid.value",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'"),
			},
			{
				Name: "invalid label value 4",
				TestObject: &konnectv1alpha2.KonnectExtension{
					ObjectMeta: common.CommonObjectMeta,
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-konnect-control-plane",
									},
								},
							},
							DataPlane: &konnectv1alpha2.KonnectExtensionDataPlane{
								Labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
									"Xv9gTq2LmNZp4WJdCYKfRB86oAhsMEytkPUOQGV7Dbx53cHFnwzjL1rS0vqIXv9gTq2LmNZp4WJdCYKfRB86oAhsMEytkPUOQGV7Dbx53cHFnwzjL1rS0vqI": "valid.value",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Too long: may not be more than 63 bytes"),
			},
		}.Run(t)
	})
}
