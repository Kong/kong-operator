package helpers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/samber/lo"
)

const (
	telepresenceInterceptDeploymentName = "gateway-operator-controller-manager"
	telepresenceInterceptName           = telepresenceInterceptDeploymentName
	telepresenceInterceptPort           = 5443
)

func interceptPodLabels() map[string]string {
	return map[string]string{
		"app":                          "gateway-operator-controller-manager",
		"app.kubernetes.io/name":       "gateway-operator",
		"app.kubernetes.io/instance":   "gateway-operator",
		"app.kubernetes.io/component":  "ko",
		"control-plane":                "controller-manager",
		"telepresence.kubernetes.io":   "true",
		"konghq.com/network-intercept": "true",
	}
}

// SetupNetworkIntercepts ensures that network policies which rely on in-cluster identities
// can be exercised when the controller runs outside the cluster (e.g. during integration
// testing). It creates a lightweight deployment that mimics the operator pod identity and
// establishes a Telepresence intercept so that the current process receives the same
// environment and filesystem view as the pod.
func SetupNetworkIntercepts(ctx context.Context, clients testutils.K8sClients) (func(), error) {
	if test.IsTelepresenceDisabled() {
		fmt.Println("INFO: telepresence is disabled, skipping network intercept setup")
		return func() {}, nil
	}

	telepresenceExec, err := resolveTelepresenceExecutable()
	if err != nil {
		return nil, err
	}

	controllerNamespace := os.Getenv("POD_NAMESPACE")
	if controllerNamespace == "" {
		controllerNamespace = "kong-system"
	}

	if err := ensureInterceptDeployment(ctx, clients, controllerNamespace); err != nil {
		return nil, fmt.Errorf("failed to ensure telepresence intercept deployment: %w", err)
	}
	if err := ensureInterceptService(ctx, clients, controllerNamespace); err != nil {
		return nil, fmt.Errorf("failed to ensure telepresence intercept service: %w", err)
	}

	if err := waitForDeploymentReady(ctx, clients, controllerNamespace, telepresenceInterceptDeploymentName, 2*time.Minute); err != nil {
		return nil, fmt.Errorf("telepresence intercept deployment not ready: %w", err)
	}

	fmt.Println("INFO: connecting to the cluster with telepresence!!!")
	// NOTE: We need to specify --manager-namespace to connect to the traffic-manager
	// installed in kong-system namespace above.
	connectArgs := []string{"connect", "--manager-namespace", "kong-system", "--namespace", "kong-system"}
	out, err := exec.CommandContext(ctx, telepresenceExec, connectArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the cluster with telepresence: %w, %s", err, string(out))
	}

	// envFile, err := os.CreateTemp("", "telepresence-env-*.sh")
	// if err != nil {
	// 	return nil, fmt.Errorf("failed creating temporary env file for telepresence: %w", err)
	// }
	// envFilePath := envFile.Name()
	// _ = envFile.Close()

	fmt.Println("INFO: establishing telepresence intercept for controller identity")
	// var (
	// 	labelOverridePath string
	// )
	intercept := func(portPair int, svcName string) []string {
		return []string{
			"replace",
			svcName,
			"--port", fmt.Sprintf("%d:%d", portPair, portPair),
		}
	}

	cmd := exec.CommandContext(ctx, telepresenceExec, intercept(telepresenceInterceptPort, telepresenceInterceptName)...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		// if !bytes.Contains(output, []byte("already exists")) {
		// 	_ = os.Remove(envFilePath)
		// 	return nil, fmt.Errorf("failed to create telepresence intercept: %w, %s", err, string(output))
		// }
		fmt.Println("WARN: telepresence intercept already exists, reusing the existing session")
	}
	fmt.Println(">>>\n", string(output), "\n<<<")

	if err := ensureAdmissionRegistration(ctx, clients.K8sClient, k8stypes.NamespacedName{
		Namespace: controllerNamespace,
		Name:      "gateway-operator-webhook-service",
	}, controllerNamespace); err != nil {
		return nil, fmt.Errorf("failed to ensure admission registration: %w", err)
	}

	cleanup := func() {
		fmt.Println("INFO: cleaning up telepresence intercept for controller identity")
		if err := exec.CommandContext(context.Background(), telepresenceExec, "leave", telepresenceInterceptName).Run(); err != nil {
			fmt.Printf("WARN: failed to leave telepresence intercept %q: %v\n", telepresenceInterceptName, err)
		}

		background := context.Background()
		if err := clients.K8sClient.AppsV1().Deployments(controllerNamespace).Delete(background, telepresenceInterceptDeploymentName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			fmt.Printf("WARN: failed to delete intercept deployment %s/%s: %v\n", controllerNamespace, telepresenceInterceptDeploymentName, err)
		}
		if err := clients.K8sClient.CoreV1().Services(controllerNamespace).Delete(background, telepresenceInterceptDeploymentName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			fmt.Printf("WARN: failed to delete intercept service %s/%s: %v\n", controllerNamespace, telepresenceInterceptDeploymentName, err)
		}

		// if err := os.Remove(envFilePath); err != nil && !os.IsNotExist(err) {
		// 	fmt.Printf("WARN: failed to remove telepresence env file %q: %v\n", envFilePath, err)
		// }
		// if labelOverridePath != "" {
		// 	if err := os.Remove(labelOverridePath); err != nil && !os.IsNotExist(err) {
		// 		fmt.Printf("WARN: failed to remove pod labels override file %q: %v\n", labelOverridePath, err)
		// 	}
		// }
	}

	return cleanup, nil
}

func ensureInterceptDeployment(ctx context.Context, clients testutils.K8sClients, namespace string) error {
	labels := interceptPodLabels()
	const name = "gateway-operator-controller-manager"
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "gateway-operator",
				"app.kubernetes.io/component": "ko",
			},
			// Annotations: map[string]string{
			// 	"telepresence.getambassador.io/inject-traffic-agent": "enabled",
			// },
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: lo.ToPtr[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "gateway-operator-controller-manager",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"telepresence.io/inject-traffic-agent": "enabled",
					},
				},
				Spec: corev1.PodSpec{
					PriorityClassName: "system-cluster-critical",
					Containers: []corev1.Container{
						{
							Name:  "identity",
							Image: "registry.k8s.io/pause:3.10",
							Ports: []corev1.ContainerPort{
								{
									Name:          "placeholder",
									ContainerPort: telepresenceInterceptPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := clients.K8sClient.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if _, err := clients.K8sClient.AppsV1().Deployments(namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

func ensureInterceptService(ctx context.Context, clients testutils.K8sClients, namespace string) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-operator-webhook-service",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "gateway-operator",
				"app.kubernetes.io/component": "ko",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "gateway-operator-controller-manager",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "placeholder",
					Port:       telepresenceInterceptPort,
					TargetPort: intstr.FromInt(telepresenceInterceptPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	_, err := clients.K8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func waitForDeploymentReady(ctx context.Context, clients testutils.K8sClients, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		deploy, err := clients.K8sClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if deploy.Status.ReadyReplicas >= 1 {
			return true, nil
		}
		return false, nil
	})
}
