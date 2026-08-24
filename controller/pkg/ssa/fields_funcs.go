package ssa

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FieldsWithRawBytes returns a new FieldsV1 object with the given raw bytes.
func FieldsWithRawBytes(raw []byte) *metav1.FieldsV1 {
	f := &metav1.FieldsV1{}
	f.SetRawBytes(raw)
	return f
}
