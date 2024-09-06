package autoscaler

import (
	"testing"
	"time"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	testutil "github.com/llm-operator/inference-manager/engine/internal/test"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestScale(t *testing.T) {
	const (
		modelID = "md-0"
		name    = "rt-0"
		ns      = "test"

		targetValue      = 10
		maxScaleUpRate   = 3.0
		maxScaleDownRate = 0.5
		ZeroGracePeriod  = 5 * time.Minute
	)
	var (
		now = time.Now()
		nn  = types.NamespacedName{Name: name, Namespace: ns}
	)
	var tests = []struct {
		name string

		metrics float64
		// statefulset spec and status
		specReplicas  int32
		readyReplicas int32
		// scaling config
		maxReplicas int32
		minReplicas int32
		lastZero    time.Time

		wantScale     bool
		wantNonZeroTs bool
		wantReplicas  int32
	}{
		{
			name:          "scale up",
			metrics:       98.2,
			specReplicas:  2,
			readyReplicas: 2,
			wantScale:     true,
			wantReplicas:  5, // ceil(98.2/2/10) = 5
		},
		{
			name:          "scale up (capped by maxReplicas)",
			metrics:       98.2,
			specReplicas:  2,
			readyReplicas: 2,
			maxReplicas:   3,
			wantScale:     true,
			wantReplicas:  3, // ceil(98.2/2/10) = min(3, 5) = 3
		},
		{
			name:          "scale up (capped by maxScaleUp)",
			metrics:       131.8,
			specReplicas:  2,
			readyReplicas: 2,
			maxReplicas:   10,
			wantScale:     true,
			wantReplicas:  6, // ceil(131.8/2/10) = min(ceil(2*3.0=6), 7) = 6
		},
		{
			name:          "scale down",
			metrics:       146.3,
			specReplicas:  5,
			readyReplicas: 5,
			wantScale:     true,
			wantReplicas:  3, // ceil(146.3/5/10) = 3
		},
		{
			name:          "scale down (capped by minReplicas)",
			metrics:       0,
			specReplicas:  3,
			readyReplicas: 3,
			minReplicas:   1,
			wantScale:     true,
			wantReplicas:  1, // ceil(0/3/10) = max(1, 0) = 1
		},
		{
			name:          "scale down (capped by maxScaleDown)",
			metrics:       2,
			specReplicas:  5,
			readyReplicas: 5,
			minReplicas:   1,
			wantScale:     true,
			wantReplicas:  2, // ceil(2/5/10) = max(floor(5*0.5=2.5), 1) = 2
		},
		{
			name:          "within the scale to zero grace period (scale down to 1)",
			metrics:       0,
			specReplicas:  3,
			readyReplicas: 3,
			wantScale:     true,
			wantNonZeroTs: true,
			wantReplicas:  1, // ceil(0/3/10) = 0 (within the grace period, scale down to 1 first)
		},
		{
			name:          "within the scale to zero grace period (do nothing)",
			metrics:       0,
			specReplicas:  1,
			readyReplicas: 1,
			lastZero:      now.Add(-ZeroGracePeriod / 2),
			wantScale:     false,
			wantNonZeroTs: true,
			wantReplicas:  1, // ceil(0/1/10) = 0 (within the grace period)
		},
		{
			name:          "scale down to zero",
			metrics:       0,
			specReplicas:  1,
			readyReplicas: 1,
			lastZero:      now.Add(-ZeroGracePeriod),
			wantScale:     true,
			wantNonZeroTs: true,
			wantReplicas:  0, // ceil(0/3/10) = 0
		},
		{
			name:          "scale up from zero",
			metrics:       1,
			specReplicas:  0,
			readyReplicas: 0,
			lastZero:      now.Add(-time.Hour),
			wantScale:     true,
			wantReplicas:  1, // ceil(1/min(0,1)/10) = 1
		},
		{
			name:          "desired replicas and current replicas are same",
			metrics:       32.3,
			specReplicas:  2,
			readyReplicas: 2,
			maxReplicas:   3,
			wantReplicas:  2, // ceil(32.3/2/10) = 2
		},
		{
			name:          "some pending pods",
			metrics:       98.2,
			specReplicas:  5,
			readyReplicas: 2,
			wantReplicas:  5, // ceil(98.2/min(2,5)/10) = 5
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(test.specReplicas),
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas: test.readyReplicas,
				},
			}
			scaler := scaler{
				modelID:   modelID,
				target:    nn,
				k8sClient: fake.NewFakeClient(sts),
				metrics:   &fakeMetricsProvider{value: test.metrics},
				config: config.ScalingConfig{
					TargetValue:      targetValue,
					MaxReplicas:      test.maxReplicas,
					MinReplicas:      test.minReplicas,
					MaxScaleUpRate:   maxScaleUpRate,
					MaxScaleDownRate: maxScaleDownRate,
				},
				scaleToZeroGracePeriod: ZeroGracePeriod,
				lastTransitToZero:      test.lastZero,
			}

			ctx := testutil.ContextWithLogger(t)
			scale, err := scaler.scale(ctx)
			assert.NoError(t, err)
			assert.Equal(t, test.wantScale, scale)
			assert.Equal(t, test.wantNonZeroTs, !scaler.lastTransitToZero.IsZero())

			var got appsv1.StatefulSet
			err = scaler.k8sClient.Get(ctx, nn, &got)
			assert.NoError(t, err)
			assert.Equal(t, test.wantReplicas, ptr.Deref(got.Spec.Replicas, 0))
		})
	}
}

type fakeMetricsProvider struct {
	value float64
}

func (f *fakeMetricsProvider) Get(modelID string) float64 {
	return f.value
}
