package crdsvalidation

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validKonnectConfigStoreSync(ns string) *konnectv1alpha1.KonnectConfigStoreSync {
	return &konnectv1alpha1.KonnectConfigStoreSync{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.KonnectConfigStoreSyncSpec{
			ConfigStoreRef: commonv1alpha1.NamespacedRef{
				Name: "test-config-store",
			},
			SecretRef: commonv1alpha1.NamespacedRef{
				Name: "test-secret",
			},
			Combined: &konnectv1alpha1.KonnectConfigStoreSyncCombined{},
		},
	}
}

func validKonnectConfigStoreSyncSplit(ns string) *konnectv1alpha1.KonnectConfigStoreSync {
	obj := validKonnectConfigStoreSync(ns)
	obj.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
	obj.Spec.Combined = nil
	obj.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{
		Entries: []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
			{
				Field:    "tls.crt",
				StoreKey: new("mytls-crt"),
			},
		},
	}
	return obj
}

func TestKonnectConfigStoreSync(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("spec validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectConfigStoreSync]{
			{
				Name:       "valid Combined mode resource is accepted",
				TestObject: validKonnectConfigStoreSync(ns.Name),
			},
			{
				Name: "valid Combined mode resource with explicit storeKey is accepted",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new("mytls")
					return obj
				}(),
			},
			{
				Name: "valid Combined mode resource with custom cert/key fields is accepted",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.CertificateField = "cert.pem"
					obj.Spec.Combined.KeyField = "key.pem"
					return obj
				}(),
			},
			{
				Name:       "valid Split mode resource is accepted",
				TestObject: validKonnectConfigStoreSyncSplit(ns.Name),
			},
			{
				Name: "configStoreRef is required",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.ConfigStoreRef = commonv1alpha1.NamespacedRef{}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.configStoreRef.name: Required value"),
			},
			{
				Name: "secretRef is required",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.SecretRef = commonv1alpha1.NamespacedRef{}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.secretRef.name: Required value"),
			},
			{
				Name: "invalid mode is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Mode = "Bogus"
					return obj
				}(),
				ExpectedErrorMessage: new(`Unsupported value: "Bogus"`),
			},
			{
				Name: "invalid deletionPolicy is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.DeletionPolicy = "Bogus"
					return obj
				}(),
				ExpectedErrorMessage: new(`Unsupported value: "Bogus"`),
			},
			{
				Name: "Combined mode requires combined block",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined = nil
					return obj
				}(),
				ExpectedErrorMessage: new("exactly one of spec.combined or spec.split must be set, matching spec.mode"),
			},
			{
				Name: "Combined mode rejects split block",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{
						Entries: []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{{Field: "tls.crt"}},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("exactly one of spec.combined or spec.split must be set, matching spec.mode"),
			},
			{
				Name: "Split mode requires split block",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
					return obj
				}(),
				ExpectedErrorMessage: new("exactly one of spec.combined or spec.split must be set, matching spec.mode"),
			},
			{
				Name: "Split mode rejects empty entries",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSyncSplit(ns.Name)
					obj.Spec.Split.Entries = []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.split.entries in body should have at least 1 items"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("combined validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectConfigStoreSync]{
			{
				Name: "certificateField equal to keyField is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.CertificateField = "same.pem"
					obj.Spec.Combined.KeyField = "same.pem"
					return obj
				}(),
				ExpectedErrorMessage: new("spec.combined.certificateField and spec.combined.keyField must be different"),
			},
			{
				Name: "certificateField with invalid characters is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.CertificateField = "not/a/key"
					return obj
				}(),
				ExpectedErrorMessage: new("spec.combined.certificateField in body should match"),
			},
			{
				Name: "storeKey with a slash is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new("not/a-key")
					return obj
				}(),
				ExpectedErrorMessage: new("spec.combined.storeKey in body should match"),
			},
			{
				Name: "storeKey with a hash is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new("not#a-key")
					return obj
				}(),
				ExpectedErrorMessage: new("spec.combined.storeKey in body should match"),
			},
			{
				Name: "storeKey with a percent is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new("not%a-key")
					return obj
				}(),
				ExpectedErrorMessage: new("spec.combined.storeKey in body should match"),
			},
			{
				Name: "storeKey with a space is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new("not a-key")
					return obj
				}(),
				ExpectedErrorMessage: new("spec.combined.storeKey in body should match"),
			},
			{
				Name: "storeKey of 512 characters is accepted",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new(strings.Repeat("a", 512))
					return obj
				}(),
			},
			{
				Name: "storeKey of 513 characters is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new(strings.Repeat("a", 513))
					return obj
				}(),
				ExpectedErrorMessage: new("spec.combined.storeKey: Too long"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("split validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectConfigStoreSync]{
			{
				Name: "duplicate fields are rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSyncSplit(ns.Name)
					obj.Spec.Split.Entries = append(obj.Spec.Split.Entries,
						konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.crt"},
					)
					return obj
				}(),
				ExpectedErrorMessage: new("Duplicate value"),
			},
			{
				Name: "duplicate storeKeys are rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSyncSplit(ns.Name)
					obj.Spec.Split.Entries = append(obj.Spec.Split.Entries,
						konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key", StoreKey: new("mytls-crt")},
					)
					return obj
				}(),
				ExpectedErrorMessage: new("spec.split.entries storeKeys must be unique"),
			},
			{
				Name: "entry storeKey with a slash is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSyncSplit(ns.Name)
					obj.Spec.Split.Entries[0].StoreKey = new("not/a-key")
					return obj
				}(),
				ExpectedErrorMessage: new("storeKey in body should match"),
			},
			{
				Name: "entry storeKey of 513 characters is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSyncSplit(ns.Name)
					obj.Spec.Split.Entries[0].StoreKey = new(strings.Repeat("a", 513))
					return obj
				}(),
				ExpectedErrorMessage: new("storeKey: Too long"),
			},
			{
				Name: "entries without storeKeys are accepted",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSyncSplit(ns.Name)
					obj.Spec.Split.Entries = []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
						{Field: "tls.crt"},
						{Field: "tls.key"},
					}
					return obj
				}(),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("immutability", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectConfigStoreSync]{
			{
				Name:       "mode is immutable",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
					obj.Spec.Combined = nil
					obj.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{
						Entries: []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{{Field: "tls.crt"}},
					}
				},
				ExpectedUpdateErrorMessage: new("spec.mode is immutable"),
			},
			{
				Name:       "configStoreRef.name is immutable",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.ConfigStoreRef.Name = "other-config-store"
				},
				ExpectedUpdateErrorMessage: new("spec.configStoreRef.name is immutable"),
			},
			{
				Name:       "configStoreRef.namespace is mutable",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.ConfigStoreRef.Namespace = new("other-namespace")
				},
			},
			{
				Name:       "secretRef is mutable",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.SecretRef.Name = "other-secret"
				},
			},
			{
				Name:       "deletionPolicy is mutable",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.DeletionPolicy = konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete
				},
			},
			{
				Name: "combined.storeKey is immutable once set",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new("mytls")
					return obj
				}(),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Combined.StoreKey = new("othertls")
				},
				ExpectedUpdateErrorMessage: new("spec.combined.storeKey is immutable once set"),
			},
			{
				Name: "combined.storeKey cannot be unset once set",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSync(ns.Name)
					obj.Spec.Combined.StoreKey = new("mytls")
					return obj
				}(),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Combined.StoreKey = nil
				},
				ExpectedUpdateErrorMessage: new("spec.combined.storeKey is immutable once set"),
			},
			{
				Name:       "combined.storeKey can be set when previously unset",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Combined.StoreKey = new("mytls")
				},
			},
			{
				Name:       "combined certificateField and keyField are mutable",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Combined.CertificateField = "cert.pem"
					obj.Spec.Combined.KeyField = "key.pem"
				},
			},
			{
				Name:       "split entry storeKey is immutable for entries that keep their field",
				TestObject: validKonnectConfigStoreSyncSplit(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Split.Entries[0].StoreKey = new("othertls-crt")
				},
				ExpectedUpdateErrorMessage: new("spec.split.entries storeKey is immutable for entries that keep their field"),
			},
			{
				Name:       "split entries can be added",
				TestObject: validKonnectConfigStoreSyncSplit(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Split.Entries = append(obj.Spec.Split.Entries,
						konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key", StoreKey: new("mytls-key")},
					)
				},
			},
			{
				Name: "split entries can be removed",
				TestObject: func() *konnectv1alpha1.KonnectConfigStoreSync {
					obj := validKonnectConfigStoreSyncSplit(ns.Name)
					obj.Spec.Split.Entries = append(obj.Spec.Split.Entries,
						konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key", StoreKey: new("mytls-key")},
					)
					return obj
				}(),
				Update: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Spec.Split.Entries = obj.Spec.Split.Entries[:1]
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("status validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectConfigStoreSync]{
			{
				Name:       "valid status update is accepted",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				StatusUpdate: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Status = konnectv1alpha1.KonnectConfigStoreSyncStatus{
						StoreID:                       "5f9b1a2c-3d4e-4f50-8a6b-7c8d9e0f1a2b",
						ControlPlaneID:                "1a2b3c4d-5e6f-4a5b-8c9d-0e1f2a3b4c5d",
						ObservedSecretResourceVersion: "12481",
						EntriesSynced:                 1,
						EntriesTotal:                  1,
						Entries: []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
							{
								StoreKey:     "mytls",
								SourceFields: []string{"tls.crt", "tls.key"},
								Hash:         "sha256:" + strings.Repeat("ab", 32),
								ValueBytes:   2877,
								KeyBytes:     5,
							},
						},
						References: []konnectv1alpha1.KonnectConfigStoreSyncReference{
							{SubField: "certificate", Suffix: "mytls/certificate"},
							{SubField: "key", Suffix: "mytls/key"},
						},
					}
				},
			},
			{
				Name:       "status update without conditions gets the four default conditions",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				StatusUpdate: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Status = konnectv1alpha1.KonnectConfigStoreSyncStatus{
						StoreID: "5f9b1a2c-3d4e-4f50-8a6b-7c8d9e0f1a2b",
					}
				},
			},
			{
				Name:       "status update with fewer than four conditions is rejected",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				StatusUpdate: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Status.Conditions = []metav1.Condition{
						{Type: "ConfigStoreRefValid", Status: metav1.ConditionUnknown, Reason: "Pending", Message: "Waiting for controller", LastTransitionTime: metav1.Now()},
						{Type: "SecretRefValid", Status: metav1.ConditionUnknown, Reason: "Pending", Message: "Waiting for controller", LastTransitionTime: metav1.Now()},
						{Type: "PairValid", Status: metav1.ConditionUnknown, Reason: "Pending", Message: "Waiting for controller", LastTransitionTime: metav1.Now()},
					}
				},
				ExpectedStatusUpdateErrorMessage: new("status.conditions"),
			},
			{
				Name:       "entry hash must be a sha256 digest",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				StatusUpdate: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Status.Entries = []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
						{
							StoreKey:     "mytls",
							SourceFields: []string{"tls.crt"},
							Hash:         "plaintext-value",
						},
					}
				},
				ExpectedStatusUpdateErrorMessage: new("should match '^sha256:[0-9a-f]{64}$'"),
			},
			{
				Name:       "split mode references without subfield are accepted",
				TestObject: validKonnectConfigStoreSyncSplit(ns.Name),
				StatusUpdate: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Status.References = []konnectv1alpha1.KonnectConfigStoreSyncReference{
						{Suffix: "mytls-crt"},
						{Suffix: "mytls-key"},
					}
				},
			},
			{
				Name:       "duplicate reference suffixes are rejected",
				TestObject: validKonnectConfigStoreSync(ns.Name),
				StatusUpdate: func(obj *konnectv1alpha1.KonnectConfigStoreSync) {
					obj.Status.References = []konnectv1alpha1.KonnectConfigStoreSyncReference{
						{SubField: "certificate", Suffix: "mytls/certificate"},
						{SubField: "key", Suffix: "mytls/certificate"},
					}
				},
				ExpectedStatusUpdateErrorMessage: new("Duplicate value"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
