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

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

const (
	// DefaultControlPlaneCPURequest is the default ControlPlane CPU request.
	DefaultControlPlaneCPURequest = "100m"
	// DefaultControlPlaneCPULimit is the default ControlPlane CPU limit.
	DefaultControlPlaneCPULimit = "200m"

	// DefaultControlPlaneMemoryRequest is the default ControlPlane memory request.
	DefaultControlPlaneMemoryRequest = "20Mi"
	// DefaultControlPlaneMemoryLimit is the default ControlPlane memory limit.
	DefaultControlPlaneMemoryLimit = "100Mi"
)

var terminationGracePeriodSeconds = int64(corev1.DefaultTerminationGracePeriodSeconds)

// ApplyDeploymentUserPatches applies user PodTemplateSpec patches to a Deployment. It returns the existing Deployment
// if there are no patches.
func ApplyDeploymentUserPatches(
	deployment *Deployment,
	podTemplateSpec *corev1.PodTemplateSpec,
) (*Deployment, error) {
	if podTemplateSpec != nil {
		patchedPodTemplateSpec, err := StrategicMergePatchPodTemplateSpec(
			&deployment.Spec.Template, podTemplateSpec)
		if err != nil {
			return nil, err
		}
		deployment.Spec.Template = *patchedPodTemplateSpec
	}

	return deployment, nil
}

// GenerateContainerForControlPlaneParams is a parameter struct for GenerateControlPlaneContainer function.
type GenerateContainerForControlPlaneParams struct {
	Image string
	// AdmissionWebhookCertSecretName is the name of the Secret that holds the certificate for the admission webhook.
	// If this is nil, the admission webhook will not be enabled.
	AdmissionWebhookCertSecretName *string
}

// GenerateControlPlaneContainer generates a control plane container.
func GenerateControlPlaneContainer(params GenerateContainerForControlPlaneParams) corev1.Container {
	c := corev1.Container{
		Name:                     consts.ControlPlaneControllerContainerName,
		Image:                    params.Image,
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
		LivenessProbe:  GenerateControlPlaneProbe("/healthz", intstr.FromInt(10254)),
		ReadinessProbe: GenerateControlPlaneProbe("/readyz", intstr.FromInt(10254)),
		Resources:      *DefaultControlPlaneResources(),
	}
	// Only add the admission webhook volume mount and port if the secret name is provided.
	if params.AdmissionWebhookCertSecretName != nil && *params.AdmissionWebhookCertSecretName != "" {
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      consts.ControlPlaneAdmissionWebhookVolumeName,
			ReadOnly:  true,
			MountPath: consts.ControlPlaneAdmissionWebhookVolumeMountPath,
		})
		c.Ports = append(c.Ports, corev1.ContainerPort{
			Name:          consts.ControlPlaneAdmissionWebhookPortName,
			ContainerPort: consts.ControlPlaneAdmissionWebhookListenPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	return c
}

const (
	// DefaultDataPlaneCPURequest is the default DataPlane CPU request.
	DefaultDataPlaneCPURequest = "100m"
	// DefaultDataPlaneCPULimit is the default DataPlane CPU limit.
	DefaultDataPlaneCPULimit = "1000m"

	// DefaultDataPlaneMemoryRequest is the default DataPlane memory request.
	DefaultDataPlaneMemoryRequest = "20Mi"
	// DefaultDataPlaneMemoryLimit is the default DataPlane memory limit.
	DefaultDataPlaneMemoryLimit = "1000Mi"
)

// DeploymentOpt is an option for Deployment generators.
type DeploymentOpt func(*appsv1.Deployment)

// GenerateNewDeploymentForDataPlane generates a new Deployment for the DataPlane
func GenerateNewDeploymentForDataPlane(
	dataplane *operatorv1beta1.DataPlane,
	dataplaneImage string,
	opts ...DeploymentOpt,
) (*Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-%s-", consts.DataPlanePrefix, dataplane.Name)),
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
					Volumes:                       []corev1.Volume{},
					Containers: []corev1.Container{
						GenerateDataPlaneContainer(dataplaneImage),
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

	for _, opt := range opts {
		if opt != nil {
			opt(deployment)
		}
	}

	k8sutils.SetOwnerForObject(deployment, dataplane)
	controllerutil.AddFinalizer(deployment, consts.DataPlaneOwnedWaitForOwnerFinalizer)

	// Set defaults for the deployment so that we don't get a diff when we compare
	// it with what's in the cluster.
	pkgapisappsv1.SetDefaults_Deployment(deployment)

	wrapped := Deployment(*deployment)
	return &wrapped, nil
}

// GenerateDataPlaneContainer generates a DataPlane container.
func GenerateDataPlaneContainer(image string) corev1.Container {
	return corev1.Container{
		Name:            consts.DataPlaneProxyContainerName,
		VolumeMounts:    []corev1.VolumeMount{},
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

// GenerateControlPlaneProbe generates a controlplane probe that uses the specified endpoint.
// This is currently used both for readiness and liveness.
func GenerateControlPlaneProbe(endpoint string, port intstr.IntOrString) *corev1.Probe {
	return &corev1.Probe{
		FailureThreshold:    3,
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      1,
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   endpoint,
				Port:   port,
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

// DefaultDataPlaneResources generates a ResourceRequirements with the DataPlane defaults.
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

// DefaultControlPlaneResources generates a ResourceRequirements with the ControlPlane defaults.
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

// ClusterCertificateVolume returns a volume holding a cluster certificate given a Secret holding a certificate.
func ClusterCertificateVolume(certSecretName string) corev1.Volume {
	clusterCertificateVolume := corev1.Volume{}
	clusterCertificateVolume.Secret = &corev1.SecretVolumeSource{}
	clusterCertificateVolume.Name = consts.ClusterCertificateVolume
	SetDefaultsVolume(&clusterCertificateVolume)
	clusterCertificateVolume.Secret = &corev1.SecretVolumeSource{
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

// ClusterCertificateVolumeMount returns a volume mount for the cluster certificate.
func ClusterCertificateVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.ClusterCertificateVolume,
		ReadOnly:  true,
		MountPath: consts.ClusterCertificateVolumeMountPath,
	}
}

// Deployment is a wrapper for appsv1.Deployment. It provides additional methods to modify parts of the Deployment,
// such as to add a Volume or set an environment variable. These "With" methods do not return errors to allow chaining,
// and may no-op if target subsection is not available or overwrite existing conflicting configuration. If the presence
// of existing configuration is uncertain, you must check before invoking them.
type Deployment appsv1.Deployment

func (d *Deployment) Unwrap() *appsv1.Deployment {
	return (*appsv1.Deployment)(d)
}

// The various ApplyConfig types used in SSA (see
// https://kubernetes.io/blog/2021/08/06/server-side-apply-ga/#using-server-side-apply-in-a-controller(
// seem to roughly provide their own equivalents to these, e.g.
// https://pkg.go.dev/k8s.io/client-go@v0.29.1/applyconfigurations/core/v1#PodSpecApplyConfiguration.WithVolumes
// but they operate on the <Foo>ApplyConfiguration variants of the modified type. These types allow any field to be
// empty, even when the normal variant of the type would require a non-nil value, but doing so would require further
// research into how SSA uses them.

// WithVolume appends a volume to a Deployment. It overwrites any existing Volume with the same name.
func (d *Deployment) WithVolume(v corev1.Volume) *Deployment {
	found := false
	for i, existing := range d.Spec.Template.Spec.Volumes {
		if v.Name == existing.Name {
			d.Spec.Template.Spec.Volumes[i] = v
			found = true
		}
	}
	if !found {
		d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, v)
	}
	return d
}

// WithVolumeMount appends a volume mount to a Deployment's container. It overwrites any existing VolumeMount with the
// same path. It takes no action if the container does not exist.
func (d *Deployment) WithVolumeMount(v corev1.VolumeMount, container string) *Deployment {
	found := false
	for i, c := range d.Spec.Template.Spec.Containers {
		if c.Name == container {
			for j, existing := range d.Spec.Template.Spec.Containers[i].VolumeMounts {
				if v.MountPath == existing.MountPath {
					d.Spec.Template.Spec.Containers[i].VolumeMounts[j] = v
					found = true
				}
			}
			if !found {
				d.Spec.Template.Spec.Containers[i].VolumeMounts = append(d.Spec.Template.Spec.Containers[i].VolumeMounts, v)
			}
		}
	}
	return d
}

// WithEnvVar sets an environment variable in a container. It overwrites any existing environment variable with the
// same name. It takes no action if the container does not exist.
func (d *Deployment) WithEnvVar(v corev1.EnvVar, container string) *Deployment {
	for i, c := range d.Spec.Template.Spec.Containers {
		if c.Name == container {
			for j, ev := range c.Env {
				if ev.Name == v.Name {
					d.Spec.Template.Spec.Containers[i].Env[j] = v
					return d
				}
			}
			d.Spec.Template.Spec.Containers[i].Env = append(d.Spec.Template.Spec.Containers[i].Env, v)
			return d
		}
	}
	return d
}
