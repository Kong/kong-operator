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

// GenerateNewWebhookCertificateConfigJobs generates the create and patch jobs for the certificateConfig
func GenerateNewWebhookCertificateConfigJobs(namespace,
	serviceAccountName,
	imageName,
	secretName,
	webhookName string,
) (createJob, patchJob *batchv1.Job) {
	createJob = newWebhookCertificateConfigJobCommon(namespace, serviceAccountName, imageName, func(j *batchv1.Job) {
		j.GenerateName = fmt.Sprintf("%s-create-", consts.WebhookCertificateConfigName)
		j.Spec.Template.Spec.Containers[0].Name = "create"
		j.Spec.Template.Spec.Containers[0].Args = []string{
			"create",
			fmt.Sprintf("--host=gateway-operator-validating-webhook,gateway-operator-validating-webhook.%s.svc", namespace),
			fmt.Sprintf("--namespace=%s", namespace),
			fmt.Sprintf("--secret-name=%s", secretName),
		}
	})

	patchJob = newWebhookCertificateConfigJobCommon(namespace, serviceAccountName, imageName, func(j *batchv1.Job) {
		j.GenerateName = fmt.Sprintf("%s-patch-", consts.WebhookCertificateConfigName)
		j.Spec.Template.Spec.Containers[0].Name = "patch"
		j.Spec.Template.Spec.Containers[0].Args = []string{
			"patch",
			fmt.Sprintf("--webhook-name=%s", webhookName),
			fmt.Sprintf("--namespace=%s", namespace),
			"--patch-mutating=false",
			"--patch-validating=true",
			fmt.Sprintf("--secret-name=%s", secretName),
			"--patch-failure-policy=Fail",
		}
	})

	return createJob, patchJob
}

func newWebhookCertificateConfigJobCommon(namespace, serviceAccountName, imageName string, options ...func(*batchv1.Job)) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           imageName,
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
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
