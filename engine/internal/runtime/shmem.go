package runtime

import (
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
)

const (
	shmVolName = "devshm"
)

func shmemVolume() *corev1apply.VolumeApplyConfiguration {
	return corev1apply.Volume().WithName(shmVolName).
		// TODO(kenji): Set the limit.
		WithEmptyDir(corev1apply.EmptyDirVolumeSource())
}

func shmemVolumeMount() *corev1apply.VolumeMountApplyConfiguration {
	return corev1apply.VolumeMount().
		WithName(shmVolName).
		WithMountPath("/dev/shm")
}
