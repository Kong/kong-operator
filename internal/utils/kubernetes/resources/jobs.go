package resources

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// Jobs generators
// -----------------------------------------------------------------------------

// GenerateNewWebhookCertificateConfigJob generates the create and patch jobs for the certificateConfig
func GenerateNewWebhookCertificateConfigJob(namespace,
	serviceAccountName,
	imageName,
	secretName,
	webhookName string,
) *batchv1.Job {
	return newWebhookCertificateConfigJobCommon(namespace, serviceAccountName, func(j *batchv1.Job) {
		j.GenerateName = fmt.Sprintf("%s-", consts.WebhookCertificateConfigName)

		// We rely on the order of execution for creating and patching the certificates
		// hence we use the init containers for that purpose.
		// The "done" container is only present for the purposes of not breaking
		// the spec: Jobs require at least one container to be present in the spec.

		j.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name: "create",
				Args: []string{
					"create",
					fmt.Sprintf("--host=gateway-operator-validating-webhook,gateway-operator-validating-webhook.%s.svc", namespace),
					fmt.Sprintf("--namespace=%s", namespace),
					fmt.Sprintf("--secret-name=%s", secretName),
				},
				Image:           imageName,
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
			{
				Name: "patch",
				Args: []string{
					"patch",
					fmt.Sprintf("--webhook-name=%s", webhookName),
					fmt.Sprintf("--namespace=%s", namespace),
					"--patch-mutating=false",
					"--patch-validating=true",
					fmt.Sprintf("--secret-name=%s", secretName),
					"--patch-failure-policy=Fail",
				},
				Image:           imageName,
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
		}

		j.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:            "done",
				Image:           "busybox",
				Args:            []string{"echo", "done"},
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
		}
	})
}

func newWebhookCertificateConfigJobCommon(namespace, serviceAccountName string, options ...func(*batchv1.Job)) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: serviceAccountName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: pointer.Bool(true),
						RunAsUser:    pointer.Int64(2000),
					},
				},
			},
		},
	}

	for _, o := range options {
		o(job)
	}
	return job
}
