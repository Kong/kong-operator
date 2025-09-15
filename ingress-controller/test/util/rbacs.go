package util

import (
	"context"
	"fmt"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"

	"github.com/kong/kong-operator/ingress-controller/test/consts"
)

func DeployRBACsForCluster(ctx context.Context, cluster clusters.Cluster) error {
	fmt.Printf("INFO: deploying Kong RBACs to cluster\n")
	return clusters.KustomizeDeployForCluster(ctx, cluster, kongRBACsKustomize, "-n", consts.ControllerNamespace)
}
