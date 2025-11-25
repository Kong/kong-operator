package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCalculateHash(t *testing.T) {
	t.Run("hash of a Pod", func(t *testing.T) {
		obj := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "ns",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test",
						Image: "test",
					},
				},
			},
		}

		hash, err := CalculateHash(obj)
		require.NoError(t, err)
		require.Equal(t, "c05cada5d4a3cf70", hash)

		hash1, err := CalculateHash(obj)
		require.NoError(t, err)
		require.Equal(t, "c05cada5d4a3cf70", hash1)

		// Same object should produce the same hash
		hash2, err := CalculateHash(obj)
		require.NoError(t, err)
		assert.Equal(t, hash1, hash2)
	})

	t.Run("hash of different objects should be different", func(t *testing.T) {
		type testStruct struct {
			Field string
		}

		obj1 := testStruct{Field: "value1"}
		obj2 := testStruct{Field: "value2"}

		hash1, err := CalculateHash(obj1)
		require.NoError(t, err)

		hash2, err := CalculateHash(obj2)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})
}
