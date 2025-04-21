package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGetGPU(t *testing.T) {
	tcs := []struct {
		name string
		sts  appsv1.StatefulSet
		want int32
	}{
		{
			name: "gpu",
			sts: appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											nvidiaGPUResource: resource.MustParse("1"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "no gpu",
			sts: appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"cpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "multiple containers",
			sts: appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											nvidiaGPUResource: resource.MustParse("1"),
										},
									},
								},
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											nvidiaGPUResource: resource.MustParse("2"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: 3,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := getGPU(&tc.sts)
			assert.Equal(t, tc.want, got)
		})
	}
}
