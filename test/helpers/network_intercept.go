package helpers

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
)

const (
	telepresenceInterceptDeploymentName = "gateway-operator-intercept"
	telepresenceInterceptName           = telepresenceInterceptDeploymentName
	telepresenceInterceptPort           = 18080
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

	envFile, err := os.CreateTemp("", "telepresence-env-*.sh")
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary env file for telepresence: %w", err)
	}
	envFilePath := envFile.Name()
	_ = envFile.Close()

	fmt.Println("INFO: establishing telepresence intercept for controller identity")
	var (
		labelOverridePath string
	)
	args := []string{
		"intercept",
		telepresenceInterceptName,
		"--port", fmt.Sprintf("%d:%d", telepresenceInterceptPort, telepresenceInterceptPort),
		"--env-file", envFilePath,
		"--mount", "true",
	}

	cmd := exec.CommandContext(ctx, telepresenceExec, args...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if !bytes.Contains(output, []byte("already exists")) {
			_ = os.Remove(envFilePath)
			return nil, fmt.Errorf("failed to create telepresence intercept: %w, %s", err, string(output))
		}
		fmt.Println("WARN: telepresence intercept already exists, reusing the existing session")
	}

	if err := applyTelepresenceEnvFile(envFilePath); err != nil {
		leaveCmd := exec.CommandContext(ctx, telepresenceExec, "leave", telepresenceInterceptName)
		_ = leaveCmd.Run()
		_ = os.Remove(envFilePath)
		return nil, fmt.Errorf("failed to apply telepresence environment: %w", err)
	}

	overridePath, err := writePodLabelsOverride(interceptPodLabels())
	if err != nil {
		return nil, fmt.Errorf("failed to prepare pod labels override: %w", err)
	}
	labelOverridePath = overridePath
	if err := os.Setenv("KONG_OPERATOR_POD_LABELS_FILE", overridePath); err != nil {
		return nil, fmt.Errorf("failed to set pod labels override env: %w", err)
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

		if err := os.Remove(envFilePath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("WARN: failed to remove telepresence env file %q: %v\n", envFilePath, err)
		}
		if labelOverridePath != "" {
			if err := os.Remove(labelOverridePath); err != nil && !os.IsNotExist(err) {
				fmt.Printf("WARN: failed to remove pod labels override file %q: %v\n", labelOverridePath, err)
			}
		}
	}

	return cleanup, nil
}

func ensureInterceptDeployment(ctx context.Context, clients testutils.K8sClients, namespace string) error {
	labels := interceptPodLabels()

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      telepresenceInterceptDeploymentName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "gateway-operator",
				"app.kubernetes.io/component": "ko",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptrTo[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "gateway-operator-controller-manager",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
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
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "podinfo",
									MountPath: "/etc/podinfo",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "podinfo",
							VolumeSource: corev1.VolumeSource{
								DownwardAPI: &corev1.DownwardAPIVolumeSource{
									Items: []corev1.DownwardAPIVolumeFile{
										{
											Path: "labels",
											FieldRef: &corev1.ObjectFieldSelector{
												FieldPath: "metadata.labels",
											},
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

	_, err := clients.K8sClient.AppsV1().Deployments(namespace).Create(ctx, deploy, metav1.CreateOptions{})
	if err != nil {
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
			Name:      telepresenceInterceptDeploymentName,
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

func applyTelepresenceEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open telepresence env file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("failed setting environment variable %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed parsing telepresence env file %s: %w", path, err)
	}
	return nil
}

func ptrTo[T any](value T) *T {
	return &value
}

func writePodLabelsOverride(labels map[string]string) (string, error) {
	file, err := os.CreateTemp("", "ko-pod-labels-*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create pod labels temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to close pod labels temp file: %w", err)
	}

	var keys []string
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%q", key, labels[key]))
	}

	if err := os.WriteFile(file.Name(), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return "", fmt.Errorf("failed to write pod labels temp file: %w", err)
	}
	return file.Name(), nil
}
