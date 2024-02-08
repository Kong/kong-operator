package resources

import (
	"fmt"
	"sync"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	pkgapisappsv1 "k8s.io/kubernetes/pkg/apis/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	dputils "github.com/kong/gateway-operator/internal/utils/dataplane"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

const (
	DefaultControlPlaneCPURequest = "100m"
	DefaultControlPlaneCPULimit   = "200m"

	DefaultControlPlaneMemoryRequest = "20Mi"
	DefaultControlPlaneMemoryLimit   = "100Mi"
)

var terminationGracePeriodSeconds = int64(corev1.DefaultTerminationGracePeriodSeconds)

// GenerateNewDeploymentForControlPlane generates a new Deployment for the ControlPlane
func GenerateNewDeploymentForControlPlane(controlplane *operatorv1alpha1.ControlPlane,
	controlplaneImage,
	serviceAccountName,
	certSecretName string,
) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    controlplane.Namespace,
			GenerateName: fmt.Sprintf("%s-%s-", consts.ControlPlanePrefix, controlplane.Name),
			Labels: map[string]string{
				"app": controlplane.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": controlplane.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{},
					Labels: map[string]string{
						"app": controlplane.Name,
					},
				},
				Spec: corev1.PodSpec{
					SecurityContext:               &corev1.PodSecurityContext{},
					RestartPolicy:                 corev1.RestartPolicyAlways,
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					ServiceAccountName:            serviceAccountName,
					DeprecatedServiceAccount:      serviceAccountName,
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 corev1.DefaultSchedulerName,
					Volumes: []corev1.Volume{
						clusterCertificateVolume(certSecretName),
					},
					Containers: []corev1.Container{
						GenerateControlPlaneContainer(controlplaneImage),
					},
				},
			},
		},
	}
	SetDefaultsPodTemplateSpec(&deployment.Spec.Template)
	LabelObjectAsControlPlaneManaged(deployment)

	if controlplane.Spec.Deployment.PodTemplateSpec != nil {
		patchedPodTemplateSpec, err := StrategicMergePatchPodTemplateSpec(&deployment.Spec.Template, controlplane.Spec.Deployment.PodTemplateSpec)
		if err != nil {
			return nil, err
		}
		deployment.Spec.Template = *patchedPodTemplateSpec
	}

	k8sutils.SetOwnerForObject(deployment, controlplane)

	// Set defaults for the deployment so that we don't get a diff when we compare
	// it with what's in the cluster.
	pkgapisappsv1.SetDefaults_Deployment(deployment)

	return deployment, nil
}

func GenerateControlPlaneContainer(image string) corev1.Container {
	return corev1.Container{
		Name:                     consts.ControlPlaneControllerContainerName,
		Image:                    image,
		ImagePullPolicy:          corev1.PullIfNotPresent,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      consts.ClusterCertificateVolume,
				ReadOnly:  true,
				MountPath: consts.ClusterCertificateVolumeMountPath,
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "health",
				ContainerPort: 10254,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		LivenessProbe: &corev1.Probe{
			FailureThreshold:    3,
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/healthz",
					Port:   intstr.FromInt(10254),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			FailureThreshold:    3,
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/readyz",
					Port:   intstr.FromInt(10254),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
		Resources: *DefaultControlPlaneResources(),
	}
}

const (
	DefaultDataPlaneCPURequest = "100m"
	DefaultDataPlaneCPULimit   = "1000m"

	DefaultDataPlaneMemoryRequest = "20Mi"
	DefaultDataPlaneMemoryLimit   = "1000Mi"
)

type DeploymentOpt func(*appsv1.Deployment)

// GenerateNewDeploymentForDataPlane generates a new Deployment for the DataPlane
func GenerateNewDeploymentForDataPlane(
	dataplane *operatorv1beta1.DataPlane,
	dataplaneImage string,
	certSecretName string,
	opts ...DeploymentOpt,
) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: fmt.Sprintf("%s-%s-", consts.DataPlanePrefix, dataplane.Name),
			Labels: map[string]string{
				"app": dataplane.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": dataplane.Name,
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{},
					Labels: map[string]string{
						"app": dataplane.Name,
					},
				},
				Spec: corev1.PodSpec{
					SecurityContext:               &corev1.PodSecurityContext{},
					RestartPolicy:                 corev1.RestartPolicyAlways,
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 corev1.DefaultSchedulerName,
					Volumes: []corev1.Volume{
						clusterCertificateVolume(certSecretName),
					},
					Containers: []corev1.Container{
						GenerateDataPlaneContainer(dataplane.Spec.Deployment, dataplaneImage),
					},
				},
			},
		},
	}

	dpOpts := dataplane.Spec.Deployment
	switch {
	// When the replicas are set and scaling is unset then set the replicas
	// to the value of the replicas field.
	case dpOpts.Replicas != nil && dpOpts.Scaling == nil:
		deployment.Spec.Replicas = dpOpts.Replicas

	// When replicas field is unset and scaling is set, we set the replicas
	// to the minReplicas value (if it's specified).
	// We do this to ensure immediate scaling up to the minReplicas value
	// before the HPA kicks in.
	case dpOpts.Replicas == nil &&
		dpOpts.Scaling != nil &&
		dpOpts.Scaling.HorizontalScaling != nil &&
		dpOpts.Scaling.HorizontalScaling.MinReplicas != nil:
		deployment.Spec.Replicas = dpOpts.Scaling.HorizontalScaling.MinReplicas

	// We set the default to 1 if no replicas or scaling is specified because
	// we cannot set the default in the CRD due to the fact that the default
	// would prevent us from being able to use CRD Validation Rules to enforce
	// wither replicas or scaling sections specified.
	case dpOpts.Replicas == nil && dpOpts.Scaling == nil:
		deployment.Spec.Replicas = lo.ToPtr(int32(1))
	}

	SetDefaultsPodTemplateSpec(&deployment.Spec.Template)
	LabelObjectAsDataPlaneManaged(deployment)

	if dpOpts.PodTemplateSpec != nil {
		patchedPodTemplateSpec, err := StrategicMergePatchPodTemplateSpec(&deployment.Spec.Template, dpOpts.PodTemplateSpec)
		if err != nil {
			return nil, err
		}
		deployment.Spec.Template = *patchedPodTemplateSpec
	}

	dputils.FillDataPlaneProxyContainerEnvs(&deployment.Spec.Template)

	for _, opt := range opts {
		opt(deployment)
	}

	k8sutils.SetOwnerForObject(deployment, dataplane)
	controllerutil.AddFinalizer(deployment, consts.DataPlaneOwnedWaitForOwnerFinalizer)

	// Set defaults for the deployment so that we don't get a diff when we compare
	// it with what's in the cluster.
	pkgapisappsv1.SetDefaults_Deployment(deployment)

	return deployment, nil
}

func GenerateDataPlaneContainer(opts operatorv1beta1.DataPlaneDeploymentOptions, image string) corev1.Container {
	return corev1.Container{
		Name: consts.DataPlaneProxyContainerName,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      consts.ClusterCertificateVolume,
				ReadOnly:  true,
				MountPath: consts.ClusterCertificateVolumeMountPath,
			},
		},
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						"kong quit",
					},
				},
			},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		Ports: []corev1.ContainerPort{
			{
				Name:          "proxy",
				ContainerPort: consts.DataPlaneProxyPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "proxy-ssl",
				ContainerPort: consts.DataPlaneProxySSLPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "metrics",
				ContainerPort: consts.DataPlaneMetricsPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "admin-ssl",
				ContainerPort: consts.DataPlaneAdminAPIPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		ReadinessProbe: GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusEndpoint),
		Resources:      *DefaultDataPlaneResources(),
	}
}

// GenerateDataPlaneReadinessProbe generates a dataplane probe that uses the specified endpoint.
func GenerateDataPlaneReadinessProbe(endpoint string) *corev1.Probe {
	return &corev1.Probe{
		FailureThreshold:    3,
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      1,
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   endpoint,
				Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}
}

var (
	_defaultDataPlaneResourcesOnce    sync.Once
	_dataPlaneDefaultResources        corev1.ResourceRequirements
	_defaultControlPlaneResourcesOnce sync.Once
	_controlPlaneDefaultResources     corev1.ResourceRequirements
)

func DefaultDataPlaneResources() *corev1.ResourceRequirements {
	_defaultDataPlaneResourcesOnce.Do(func() {
		// Initialize just once, no need to do it more.
		_dataPlaneDefaultResources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(DefaultDataPlaneCPURequest),
				corev1.ResourceMemory: resource.MustParse(DefaultDataPlaneMemoryRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(DefaultDataPlaneCPULimit),
				corev1.ResourceMemory: resource.MustParse(DefaultDataPlaneMemoryLimit),
			},
		}
	})
	return _dataPlaneDefaultResources.DeepCopy()
}

func DefaultControlPlaneResources() *corev1.ResourceRequirements {
	_defaultControlPlaneResourcesOnce.Do(func() {
		_controlPlaneDefaultResources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(DefaultControlPlaneCPURequest),
				corev1.ResourceMemory: resource.MustParse(DefaultControlPlaneMemoryRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(DefaultControlPlaneCPULimit),
				corev1.ResourceMemory: resource.MustParse(DefaultControlPlaneMemoryLimit),
			},
		}
	})
	return _controlPlaneDefaultResources.DeepCopy()
}

func clusterCertificateVolume(certSecretName string) corev1.Volume {
	clusterCertificateVolume := corev1.Volume{}
	clusterCertificateVolume.Secret = &corev1.SecretVolumeSource{}
	SetDefaultsVolume(&clusterCertificateVolume)
	clusterCertificateVolume.Name = consts.ClusterCertificateVolume
	clusterCertificateVolume.VolumeSource.Secret = &corev1.SecretVolumeSource{
		SecretName: certSecretName,
		Items: []corev1.KeyToPath{
			{
				Key:  "tls.crt",
				Path: "tls.crt",
			},
			{
				Key:  "tls.key",
				Path: "tls.key",
			},
			{
				Key:  "ca.crt",
				Path: "ca.crt",
			},
		},
	}
	return clusterCertificateVolume
}
