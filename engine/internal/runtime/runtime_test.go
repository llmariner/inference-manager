package runtime

import (
	"testing"
	"time"

	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestRuntimeAddressSet(t *testing.T) {
	now := time.Now()

	tcs := []struct {
		name   string
		setup  func(r *runtimeAddressSet)
		want   string
		wantOK bool
	}{
		{
			name: "single address",
			setup: func(s *runtimeAddressSet) {
				s.add("test")
			},
			want:   "test",
			wantOK: true,
		},
		{
			name: "multiple addresses",
			setup: func(s *runtimeAddressSet) {
				s.add("test0")
				s.add("test1")
			},
			want:   "test0",
			wantOK: true,
		},
		{
			name: "multiple addresses with next call",
			setup: func(s *runtimeAddressSet) {
				s.add("test0")
				s.add("test1")
				s.nextTarget = 1
			},
			want:   "test1",
			wantOK: true,
		},
		{
			name: "blacklisted",
			setup: func(s *runtimeAddressSet) {
				s.add("test0")
				s.add("test1")
				s.blacklistAddress("test0", now)
			},
			want:   "test1",
			wantOK: true,
		},
		{
			name: "all blacklisted",
			setup: func(s *runtimeAddressSet) {
				s.add("test0")
				s.add("test1")
				s.blacklistAddress("test0", now)
				s.blacklistAddress("test1", now)
			},
			wantOK: false,
		},
		{
			name: "blacklisted but expired",
			setup: func(s *runtimeAddressSet) {
				s.add("test0")
				s.add("test1")
				s.blacklistAddress("test0", now.Add(-defaultBlacklistDuration*2))
			},
			want:   "test0",
			wantOK: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			s := newRuntimeAddressSet(testutil.NewTestLogger(t))
			if tc.setup != nil {
				tc.setup(s)
			}

			got, ok := s.get(now)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.want, got)
			}

		})
	}
}

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
