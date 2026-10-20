/*
Copyright 2026 Kong, Inc.

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

package gwadmin

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func setCertificateCondition(gw *operatorv1alpha1.GWAdmin, status metav1.ConditionStatus, reason, message string) {
	k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(
		kcfgconsts.ConditionType(operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned),
		status, kcfgconsts.ConditionReason(reason), message, gw.Generation,
	), gw)
}

func setReadyCondition(gw *operatorv1alpha1.GWAdmin, ready bool, reason, message string) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(
		kcfgconsts.ConditionType(operatorv1alpha1.GWAdminConditionTypeReady),
		status, kcfgconsts.ConditionReason(reason), message, gw.Generation,
	), gw)
}

// cpHost returns the in-cluster host name of the cp Service.
func cpHost(gw *operatorv1alpha1.GWAdmin, clusterDomain string) string {
	host := cpServiceName(gw) + "." + gw.Namespace + ".svc"
	if clusterDomain != "" {
		host += "." + clusterDomain
	}
	return host
}

// setStatus publishes how to reach gw-admin and which Secrets hold its trust
// material.
func setStatus(gw *operatorv1alpha1.GWAdmin, clusterDomain string) {
	gw.Status.API = &operatorv1alpha1.GWAdminAPIStatus{
		ServiceName: apiServiceName(gw),
		PortName:    consts.GWAdminAPIPortName,
		Port:        consts.GWAdminAPIPort,
	}
	gw.Status.CP = &operatorv1alpha1.GWAdminCPStatus{
		ServerURL: fmt.Sprintf("https://%s:%d", cpHost(gw, clusterDomain), cpServicePort(gw)),
	}
	gw.Status.TLS = &operatorv1alpha1.GWAdminTLSStatus{
		CPServerCABundleSecretName: cpServerCABundleSecretName(gw),
		APICABundleSecretName:      apiCABundleSecretName(gw),
	}
}
