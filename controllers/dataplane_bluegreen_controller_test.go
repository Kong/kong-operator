package controllers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/gateway-operator/apis/v1beta1"
)

func TestCanProceedWithPromotion(t *testing.T) {
	dpWithPromotionStrategy := func(promotionStrategy v1beta1.PromotionStrategy) v1beta1.DataPlane {
		return v1beta1.DataPlane{
			Spec: v1beta1.DataPlaneSpec{
				DataPlaneOptions: v1beta1.DataPlaneOptions{
					Deployment: v1beta1.DataPlaneDeploymentOptions{
						Rollout: &v1beta1.Rollout{
							Strategy: v1beta1.RolloutStrategy{
								BlueGreen: &v1beta1.BlueGreenStrategy{
									Promotion: v1beta1.Promotion{
										Strategy: promotionStrategy,
									},
								},
							},
						},
					},
				},
			},
		}
	}
	testCases := []struct {
		name               string
		dataplane          v1beta1.DataPlane
		expectedCanProceed bool
		expectedErr        error
	}{
		{
			name:               "AutomaticPromotion strategy",
			dataplane:          dpWithPromotionStrategy(v1beta1.AutomaticPromotion),
			expectedCanProceed: true,
		},
		{
			name:               "BreakBeforePromotion strategy, no annotation",
			dataplane:          dpWithPromotionStrategy(v1beta1.BreakBeforePromotion),
			expectedCanProceed: false,
		},
		{
			name: "BreakBeforePromotion strategy, annotation false",
			dataplane: func() v1beta1.DataPlane {
				dp := dpWithPromotionStrategy(v1beta1.BreakBeforePromotion)
				dp.Annotations = map[string]string{
					v1beta1.DataPlanePromoteWhenReadyAnnotationKey: "false",
				}
				return dp
			}(),
			expectedCanProceed: false,
		},
		{
			name: "BreakBeforePromotion strategy, annotation true",
			dataplane: func() v1beta1.DataPlane {
				dp := dpWithPromotionStrategy(v1beta1.BreakBeforePromotion)
				dp.Annotations = map[string]string{
					v1beta1.DataPlanePromoteWhenReadyAnnotationKey: v1beta1.DataPlanePromoteWhenReadyAnnotationTrue,
				}
				return dp
			}(),
			expectedCanProceed: true,
		},
		{
			name:        "unknown strategy",
			dataplane:   dpWithPromotionStrategy("unknown"),
			expectedErr: errors.New(`unknown promotion strategy: "unknown"`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			canProceed, err := canProceedWithPromotion(tc.dataplane)
			if tc.expectedErr != nil {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedCanProceed, canProceed)
		})
	}
}
