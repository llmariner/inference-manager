package runtime

import (
	corev1 "k8s.io/api/core/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
)

const (
	shmVolName = "devshm"
)

func shmemVolume() *corev1apply.VolumeApplyConfiguration {
	return corev1apply.Volume().WithName(shmVolName).
		// TODO(kenji): Set the limit.
		WithEmptyDir(corev1apply.EmptyDirVolumeSource().
			WithMedium(corev1.StorageMediumMemory))
}

func shmemVolumeMount() *corev1apply.VolumeMountApplyConfiguration {
	return corev1apply.VolumeMount().
		WithName(shmVolName).
		WithMountPath("/dev/shm")
}
