package managedfields

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Minimal fake runtime.Object for testing

type fakeObjectKind struct{ gvk schema.GroupVersionKind }

func (f *fakeObjectKind) GroupVersionKind() schema.GroupVersionKind { return f.gvk }
func (f *fakeObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	f.gvk = gvk
}

type fakeObject struct{ kind *fakeObjectKind }

func (f *fakeObject) GetObjectKind() schema.ObjectKind { return f.kind }
func (f *fakeObject) DeepCopyObject() runtime.Object   { return f }

func TestDeriveSchemaName(t *testing.T) {
	cases := []struct {
		name   string
		gvk    schema.GroupVersionKind
		expect string
		hasErr bool
	}{
		{
			name:   "configuration group",
			gvk:    schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1", Kind: "Foo"},
			expect: "com.github.kong.kong-operator.api.configuration.v1.Foo",
			hasErr: false,
		},
		{
			name:   "konnect group",
			gvk:    schema.GroupVersionKind{Group: "konnect.konghq.com", Version: "v2", Kind: "Bar"},
			expect: "com.github.kong.kong-operator.api.konnect.v2.Bar",
			hasErr: false,
		},
		{
			name:   "gateway-operator group",
			gvk:    schema.GroupVersionKind{Group: "gateway-operator.konghq.com", Version: "v3", Kind: "Baz"},
			expect: "com.github.kong.kong-operator.api.gateway-operator.v3.Baz",
			hasErr: false,
		},
		{
			name:   "unsupported group",
			gvk:    schema.GroupVersionKind{Group: "other.com", Version: "v1", Kind: "Qux"},
			expect: "",
			hasErr: true,
		},
	}
	for _, tc := range cases {
		name, err := deriveSchemaName(tc.gvk)
		if tc.hasErr {
			assert.Error(t, err, tc.name)
		} else {
			assert.NoError(t, err, tc.name)
			assert.Equal(t, tc.expect, name, tc.name)
		}
	}
}
func TestGetParseableTypeByName(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		valid  bool
	}{
		{
			name:   "valid schema name",
			schema: "com.github.kong.kong-operator.api.configuration.v1alpha1.KongDataPlaneClientCertificate",
			valid:  true,
		},
		{
			name:   "invalid schema name",
			schema: "com.github.kong.kong-operator.api.nonexistent.v1.Bar",
			valid:  false,
		},
	}

	for _, tc := range cases {
		_, ok := getParseableTypeByName(tc.schema)
		assert.Equal(t, tc.valid, ok, tc.name)
		assert.Equal(t, tc.valid, ok, tc.name)
	}
}
func TestGetObjectType(t *testing.T) {
	cases := []struct {
		name    string
		gvk     schema.GroupVersionKind
		valid   bool
		wantErr bool
	}{
		{
			name:    "valid object type",
			gvk:     schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongDataPlaneClientCertificate"},
			valid:   true,
			wantErr: false,
		},
		{
			name:    "invalid object type",
			gvk:     schema.GroupVersionKind{Group: "other.com", Version: "v1", Kind: "Qux"},
			valid:   false,
			wantErr: true,
		},
		{
			name:    "supported group but nonexistent kind",
			gvk:     schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "NonexistentKind"},
			valid:   false,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		obj := &fakeObject{kind: &fakeObjectKind{gvk: tc.gvk}}
		pt, err := GetObjectType(obj)
		if tc.wantErr {
			assert.Error(t, err, tc.name)
			assert.False(t, tc.valid, tc.name)
		} else {
			assert.NoError(t, err, tc.name)
			assert.True(t, pt.IsValid(), tc.name)
		}
	}
}
