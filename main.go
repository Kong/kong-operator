/*
Copyright 2022 Kong Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/kong/gateway-operator/internal/manager"
	"github.com/kong/gateway-operator/internal/manager/metadata"
)

func main() {
	var (
		metricsAddr                  string
		probeAddr                    string
		disableLeaderElection        bool
		controllerName               string
		anonymousReports             bool
		apiServerHost                string
		kubeconfigPath               string
		clusterCASecret              string
		clusterCASecretNamespace     string
		enableControllerGateway      bool
		enableControllerControlPlane bool
		enableControllerDataPlane    bool
		enableValidatingWebhook      bool
		version                      bool
		controllerNamespace          string
	)

	flagSet := flag.NewFlagSet("", flag.ExitOnError)

	flagSet.BoolVar(&anonymousReports, "anonymous-reports", true, "Send anonymized usage data to help improve Kong")
	flagSet.StringVar(&apiServerHost, "apiserver-host", "", "The Kubernetes API server URL. If not set, the operator will use cluster config discovery.")
	flagSet.StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file.")

	flagSet.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flagSet.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flagSet.BoolVar(&disableLeaderElection, "no-leader-election", false,
		"Disable leader election for controller manager. Disabling this will not ensure there is only one active controller manager.")
	flagSet.StringVar(&controllerName, "controller-name", "", "a controller name to use if other than the default, only needed for multi-tenancy")
	flagSet.StringVar(&clusterCASecret, "cluster-ca-secret", "kong-operator-ca", "name of the Secret containing the cluster CA certificate")
	flagSet.StringVar(&clusterCASecretNamespace, "cluster-ca-secret-namespace", "", "name of the namespace for Secret containing the cluster CA certificate")

	flagSet.BoolVar(&enableControllerGateway, "enable-controller-gateway", true, "Enable the Gateway controller.")
	flagSet.BoolVar(&enableControllerControlPlane, "enable-controller-controlplane", true, "Enable the ControlPlane controller.")
	flagSet.BoolVar(&enableControllerDataPlane, "enable-controller-dataplane", true, "Enable the DataPlane controller.")
	flagSet.BoolVar(&enableValidatingWebhook, "enable-validating-webhook", true, "Enable the validating webhook.")

	flagSet.BoolVar(&version, "version", false, "Print version information")

	developmentModeEnabled := manager.DefaultConfig().DevelopmentMode
	if v := os.Getenv("CONTROLLER_DEVELOPMENT_MODE"); v == "true" { // TODO: clean env handling https://github.com/Kong/gateway-operator/issues/19
		fmt.Println("INFO: development mode has been enabled")
		developmentModeEnabled = true
	}

	webhookCertDir := manager.DefaultConfig().WebhookCertDir
	if certDir := os.Getenv("WEBHOOK_CERT_DIR"); certDir != "" { // TODO: clean env handling https://github.com/Kong/gateway-operator/issues/19
		webhookCertDir = certDir
	}

	loggerOpts := manager.DefaultConfig().LoggerOpts
	loggerOpts.Development = developmentModeEnabled
	loggerOpts.BindFlags(flagSet)
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if version {
		type Version struct {
			Release string `json:"release"`
			Repo    string `json:"repo"`
			Commit  string `json:"commit"`
		}
		out, err := json.Marshal(Version{
			Release: metadata.Release,
			Repo:    metadata.Repo,
			Commit:  metadata.Commit,
		})
		if err != nil {
			fmt.Printf("ERROR: failed to print version information: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s\n", out)
		os.Exit(0)
	}

	leaderElection := manager.DefaultConfig().LeaderElection
	if disableLeaderElection {
		fmt.Println("INFO: leader election has been disabled")
		leaderElection = false
	}

	controllerNamespace = os.Getenv("POD_NAMESPACE")
	if controllerNamespace == "" {
		controllerNamespace = manager.DefaultConfig().ControllerNamespace
	}
	if clusterCASecretNamespace == "" {
		if controllerNamespace == "" {
			fmt.Println("WARN: -cluster-ca-secret-namespace unset and POD_NAMESPACE env is empty. Please provide namespace for cluster CA secret")
			os.Exit(1)
		} else {
			// If the flag has not been provided then fall back to POD_NAMESPACE env which
			// is normally provided in k8s environment.
			clusterCASecretNamespace = controllerNamespace
		}
	}

	cfg := manager.Config{
		DevelopmentMode:               developmentModeEnabled,
		MetricsAddr:                   metricsAddr,
		ProbeAddr:                     probeAddr,
		LeaderElection:                leaderElection,
		ControllerName:                controllerName,
		ControllerNamespace:           controllerNamespace,
		AnonymousReports:              anonymousReports,
		APIServerPath:                 apiServerHost,
		KubeconfigPath:                kubeconfigPath,
		ClusterCASecretName:           clusterCASecret,
		ClusterCASecretNamespace:      clusterCASecretNamespace,
		GatewayControllerEnabled:      enableControllerGateway,
		ControlPlaneControllerEnabled: enableControllerControlPlane,
		DataPlaneControllerEnabled:    enableControllerDataPlane,
		ValidatingWebhookEnabled:      enableValidatingWebhook,
		LoggerOpts:                    loggerOpts,
		WebhookCertDir:                webhookCertDir,
		WebhookPort:                   manager.DefaultConfig().WebhookPort,
	}

	if err := manager.Run(cfg); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
