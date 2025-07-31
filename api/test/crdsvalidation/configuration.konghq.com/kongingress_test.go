package configuration_test

import (
	"testing"

	"github.com/samber/lo"
	"github.com/kong/go-kong/kong"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKongIngress(t *testing.T) {
	t.Run("deprecated fields validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1.KongIngress]{
			{
				Name: "using proxy field should fail",
				TestObject: &configurationv1.KongIngress{
					ObjectMeta: common.CommonObjectMeta,
					Proxy: &configurationv1.KongIngressService{
						ConnectTimeout: lo.ToPtr(5000),
						ReadTimeout:    lo.ToPtr(5000),
						WriteTimeout:   lo.ToPtr(5000),
						Retries:        lo.ToPtr(5),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'proxy' field is no longer supported, use Service's annotations instead"),
			},
			{
				Name: "using route field should fail",
				TestObject: &configurationv1.KongIngress{
					ObjectMeta: common.CommonObjectMeta,
					Route: &configurationv1.KongIngressRoute{
						Methods:      []*string{lo.ToPtr("GET"), lo.ToPtr("POST")},
						StripPath:    lo.ToPtr(true),
						PreserveHost: lo.ToPtr(false),
						Protocols: []*configurationv1.KongProtocol{
							lo.ToPtr(configurationv1.KongProtocol("http")),
							lo.ToPtr(configurationv1.KongProtocol("https")),
						},
						HTTPSRedirectStatusCode: lo.ToPtr(301),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'route' field is no longer supported, use Ingress' annotations instead"),
			},
			{
				Name: "using both proxy and route fields should fail",
				TestObject: &configurationv1.KongIngress{
					ObjectMeta: common.CommonObjectMeta,
					Proxy: &configurationv1.KongIngressService{
						ConnectTimeout: lo.ToPtr(5000),
						ReadTimeout:    lo.ToPtr(5000),
						WriteTimeout:   lo.ToPtr(5000),
						Retries:        lo.ToPtr(5),
					},
					Route: &configurationv1.KongIngressRoute{
						Methods:      []*string{lo.ToPtr("GET"), lo.ToPtr("POST")},
						StripPath:    lo.ToPtr(true),
						PreserveHost: lo.ToPtr(false),
						Protocols: []*configurationv1.KongProtocol{
							lo.ToPtr(configurationv1.KongProtocol("http")),
							lo.ToPtr(configurationv1.KongProtocol("https")),
						},
						HTTPSRedirectStatusCode: lo.ToPtr(301),
					},
				},
				ExpectedErrorMessage: lo.ToPtr("'proxy' field is no longer supported, use Service's annotations instead"),
			},
			{
				Name: "valid KongIngress should succeed",
				TestObject: &configurationv1.KongIngress{
					ObjectMeta: common.CommonObjectMeta,
					Upstream: &configurationv1.KongIngressUpstream{
						Algorithm: lo.ToPtr("round-robin"),
						Slots:     lo.ToPtr(1000),
						Healthchecks: &kong.Healthcheck{
							Active: &kong.ActiveHealthcheck{
								Concurrency: lo.ToPtr(10),
								HTTPPath:    lo.ToPtr("/health"),
								Timeout:     lo.ToPtr(1),
								Healthy: &kong.Healthy{
									Interval:     lo.ToPtr(5),
									Successes:    lo.ToPtr(3),
									HTTPStatuses: []int{200, 302},
								},
								Unhealthy: &kong.Unhealthy{
									Interval:     lo.ToPtr(5),
									HTTPFailures: lo.ToPtr(3),
									HTTPStatuses: []int{429, 503},
									TCPFailures:  lo.ToPtr(3),
									Timeouts:     lo.ToPtr(3),
								},
							},
						},
					},
				},
			},
		}.Run(t)
	})
}
