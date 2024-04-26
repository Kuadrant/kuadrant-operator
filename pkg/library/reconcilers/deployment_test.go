package reconcilers_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kuadrant/limitador-operator/pkg/reconcilers"
)

var _ = Describe("Deployment", func() {
	var desired *appsv1.Deployment

	BeforeEach(func() {
		desired = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sample",
				Namespace: "test",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "expected",
							},
						},
					},
				},
			},
		}
	})
	Describe("DeploymentContainerListMutator()", func() {
		It("Container image length is correct", func() {
			existing := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sample",
					Namespace: "test",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "expected",
								},
							},
						},
					},
				},
			}

			result := reconcilers.DeploymentContainerListMutator(desired, existing)

			Expect(result).To(Equal(false))

		})

		It("Container spec has too many containers", func() {
			existing := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sample",
					Namespace: "test",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "expected",
								},
								{
									Name: "unexpected",
								},
							},
						},
					},
				},
			}

			result := reconcilers.DeploymentContainerListMutator(desired, existing)

			Expect(result).To(Equal(true))
			Expect(len(existing.Spec.Template.Spec.Containers)).To(Equal(len(desired.Spec.Template.Spec.Containers)))

		})
	})
})

func TestDeploymentResourcesMutator(t *testing.T) {
	deploymentFactory := func(requirements corev1.ResourceRequirements) *appsv1.Deployment {
		return &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: requirements,
							},
						},
					},
				},
			},
		}
	}

	requirementsFactory := func(reqCPU, reqMem, limCPU, limMem string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(reqCPU),
				corev1.ResourceMemory: resource.MustParse(reqMem),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(limCPU),
				corev1.ResourceMemory: resource.MustParse(limMem),
			},
		}
	}

	requirementsA := requirementsFactory("1m", "1Mi", "2m", "2Mi")
	requirementsB := requirementsFactory("2m", "2Mi", "4m", "4Mi")

	t.Run("test false when desired and existing are the same", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentResourcesMutator(deploymentFactory(requirementsA), deploymentFactory(requirementsA)), false)
	})

	t.Run("test true when desired and existing are different", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentResourcesMutator(deploymentFactory(requirementsA), deploymentFactory(requirementsB)), true)
	})
}

func TestDeploymentEnvMutator(t *testing.T) {
	deploymentFactory := func(env []corev1.EnvVar) *appsv1.Deployment {
		return &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Env: env,
							},
						},
					},
				},
			},
		}
	}

	envFactory := func(name string) []corev1.EnvVar {
		return []corev1.EnvVar{
			{
				Name: name,
			},
		}
	}

	envA := envFactory("envA")
	envB := envFactory("envB")

	t.Run("test false when desired and existing are the same", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentEnvMutator(deploymentFactory(envA), deploymentFactory(envA)), false)
	})

	t.Run("test true when desired and existing are different", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentEnvMutator(deploymentFactory(envA), deploymentFactory(envB)), true)
	})
}

func TestDeploymentVolumesMutator(t *testing.T) {
	deploymentFactory := func(volumes []corev1.Volume) *appsv1.Deployment {
		return &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec: corev1.PodSpec{
						Volumes: volumes,
					},
				},
			},
		}
	}

	existing := deploymentFactory([]corev1.Volume{
		{
			Name: "A",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "secretA",
					},
				},
			},
		},
	})

	desired := deploymentFactory([]corev1.Volume{
		{
			Name: "B",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "secretB",
					},
				},
			},
		},
	})

	desiredCopy := desired.DeepCopyObject()

	t.Run("test false when desired and existing are the same", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentVolumesMutator(existing, existing), false)
	})

	t.Run("test true when desired and existing are different", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentVolumesMutator(desired, existing), true)
		assert.DeepEqual(subT, desired, desiredCopy)
		assert.DeepEqual(subT, desired, existing)
	})
}

func TestDeploymentVolumeMountsMutator(t *testing.T) {
	deploymentFactory := func(volumeMounts []corev1.VolumeMount) *appsv1.Deployment {
		return &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								VolumeMounts: volumeMounts,
							},
						},
					},
				},
			},
		}
	}

	existing := deploymentFactory([]corev1.VolumeMount{
		{
			Name:      "A",
			MountPath: "/path/A",
		},
	})

	desired := deploymentFactory([]corev1.VolumeMount{
		{
			Name:      "B",
			MountPath: "/path/B",
		},
	})

	desiredCopy := desired.DeepCopyObject()

	t.Run("test false when desired and existing are the same", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentVolumeMountsMutator(existing, existing), false)
	})

	t.Run("test true when desired and existing are different", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentVolumeMountsMutator(desired, existing), true)
		assert.DeepEqual(subT, desired, desiredCopy)
		assert.DeepEqual(subT, desired, existing)
	})
}

func TestDeploymentCommandMutator(t *testing.T) {
	deploymentFactory := func(command []string) *appsv1.Deployment {
		return &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Command: command,
							},
						},
					},
				},
			},
		}
	}

	existing := deploymentFactory([]string{"A", "B"})

	desired := deploymentFactory([]string{"C", "D"})

	desiredCopy := desired.DeepCopyObject()

	t.Run("test false when desired and existing are the same", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentCommandMutator(existing, existing), false)
	})

	t.Run("test true when desired and existing are different", func(subT *testing.T) {
		assert.Equal(subT, reconcilers.DeploymentCommandMutator(desired, existing), true)
		assert.DeepEqual(subT, desired, desiredCopy)
		assert.DeepEqual(subT, desired, existing)
	})
}

func TestDeploymentMutator(t *testing.T) {
	newExistingDeployment := func() *appsv1.Deployment {
		return &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "originalName",
								Command: []string{"original", "name"},
								Image:   "example.com/limitador-operator:original",
							},
						},
					},
				},
			},
		}
	}

	t.Run("desired object is not deployment", func(subT *testing.T) {
		emptyMutator := reconcilers.DeploymentMutator()
		existing := &appsv1.Deployment{}
		desired := &corev1.Service{}
		_, err := emptyMutator(existing, desired)
		assert.Error(subT, err, "*v1.Service is not a *appsv1.Deployment")
	})

	t.Run("existing object is not deployment", func(subT *testing.T) {
		emptyMutator := reconcilers.DeploymentMutator()
		existing := &corev1.Service{}
		desired := &appsv1.Deployment{}
		_, err := emptyMutator(existing, desired)
		assert.Error(subT, err, "*v1.Service is not a *appsv1.Deployment")
	})

	t.Run("no mutator", func(subT *testing.T) {
		existing := newExistingDeployment()
		emptyMutator := reconcilers.DeploymentMutator()
		updated, err := emptyMutator(existing, &appsv1.Deployment{})
		assert.NilError(subT, err)
		assert.Assert(subT, !updated)
		// object has not been mutated
		assert.DeepEqual(subT, existing, newExistingDeployment())
	})

	t.Run("all mutators return false", func(subT *testing.T) {
		mutatorList := make([]reconcilers.DeploymentMutateFn, 10)
		for i := 0; i < len(mutatorList); i++ {
			mutatorList[i] = func(_, _ *appsv1.Deployment) bool { return false }
		}

		mutator := reconcilers.DeploymentMutator(mutatorList...)
		updated, err := mutator(&appsv1.Deployment{}, &appsv1.Deployment{})
		assert.NilError(subT, err)
		assert.Assert(subT, !updated)
	})

	t.Run("all mutators are applied", func(subT *testing.T) {
		nameMutator := func(_, existing *appsv1.Deployment) bool {
			existing.Spec.Template.Spec.Containers[0].Name = "newName"
			return true
		}
		commandMutator := func(_, existing *appsv1.Deployment) bool {
			existing.Spec.Template.Spec.Containers[0].Command = []string{"new", "command"}
			return true
		}
		imageMutator := func(_, existing *appsv1.Deployment) bool {
			existing.Spec.Template.Spec.Containers[0].Image = "newImage"
			return true
		}

		existing := newExistingDeployment()
		mutator := reconcilers.DeploymentMutator(
			nameMutator, commandMutator, imageMutator,
		)
		updated, err := mutator(existing, &appsv1.Deployment{})
		assert.NilError(subT, err)
		assert.Assert(subT, updated)
		assert.Equal(subT, existing.Spec.Template.Spec.Containers[0].Name, "newName")
		assert.DeepEqual(subT, existing.Spec.Template.Spec.Containers[0].Command, []string{"new", "command"})
		assert.Equal(subT, existing.Spec.Template.Spec.Containers[0].Image, "newImage")
	})
}
