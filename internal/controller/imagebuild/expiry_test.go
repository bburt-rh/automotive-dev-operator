package imagebuild

import (
	"context"
	"testing"
	"time"

	automotivev1alpha1 "github.com/centos-automotive-suite/automotive-dev-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newExpiryReconciler(objs ...automotivev1alpha1.ImageBuild) *ImageBuildReconciler {
	scheme := newTestSchemeWithTekton()
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for i := range objs {
		builder = builder.WithStatusSubresource(&objs[i])
		builder = builder.WithObjects(&objs[i])
	}
	return &ImageBuildReconciler{
		Client:   builder.Build(),
		Scheme:   scheme,
		Log:      logr.Discard(),
		Recorder: record.NewFakeRecorder(10),
	}
}

func newTestImageBuild(name string, phase string, ttl string, completedAgo time.Duration) automotivev1alpha1.ImageBuild {
	now := time.Now()
	ib := automotivev1alpha1.ImageBuild{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "test-ns",
			CreationTimestamp: metav1.NewTime(now.Add(-completedAgo - time.Hour)),
		},
		Spec: automotivev1alpha1.ImageBuildSpec{
			TTL: ttl,
		},
		Status: automotivev1alpha1.ImageBuildStatus{
			Phase: phase,
		},
	}
	if phase == phaseCompleted || phase == phaseFailed {
		ct := metav1.NewTime(now.Add(-completedAgo))
		ib.Status.CompletionTime = &ct
	}
	return ib
}

func TestCheckExpiry_ExpiredBuild(t *testing.T) {
	ib := newTestImageBuild("expired-build", phaseCompleted, "1h", 2*time.Hour)
	r := newExpiryReconciler(ib)

	_, expired, err := r.checkExpiry(context.Background(), &ib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !expired {
		t.Fatal("expected build to be expired")
	}

	got := &automotivev1alpha1.ImageBuild{}
	err = r.Get(context.Background(), types.NamespacedName{Name: "expired-build", Namespace: "test-ns"}, got)
	if err == nil {
		t.Fatal("expected build to be deleted, but it still exists")
	}
}

func TestCheckExpiry_NotYetExpired(t *testing.T) {
	ib := newTestImageBuild("fresh-build", phaseCompleted, "24h", 1*time.Hour)
	r := newExpiryReconciler(ib)

	result, expired, err := r.checkExpiry(context.Background(), &ib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expired {
		t.Fatal("build should not be expired yet")
	}
	if result.RequeueAfter < 22*time.Hour || result.RequeueAfter > 24*time.Hour {
		t.Errorf("expected RequeueAfter ~23h, got %v", result.RequeueAfter)
	}
}

func TestCheckExpiry_NoExpireAnnotation(t *testing.T) {
	ib := newTestImageBuild("pinned-build", phaseCompleted, "1h", 2*time.Hour)
	ib.Annotations = map[string]string{
		automotivev1alpha1.NoExpireAnnotation: "true",
	}
	r := newExpiryReconciler(ib)

	_, expired, err := r.checkExpiry(context.Background(), &ib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expired {
		t.Fatal("pinned build should not be expired")
	}

	got := &automotivev1alpha1.ImageBuild{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "pinned-build", Namespace: "test-ns"}, got); err != nil {
		t.Fatalf("pinned build should still exist: %v", err)
	}
}

func TestCheckExpiry_TTLZeroDisablesExpiry(t *testing.T) {
	ib := newTestImageBuild("forever-build", phaseCompleted, "0", 999*time.Hour)
	r := newExpiryReconciler(ib)

	result, expired, err := r.checkExpiry(context.Background(), &ib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expired {
		t.Fatal("TTL=0 build should never expire")
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got %v", result.RequeueAfter)
	}
}

func TestCheckExpiry_InProgressUsesCreationTimestamp(t *testing.T) {
	ib := newTestImageBuild("building", phaseBuilding, "30m", 0)
	ib.CreationTimestamp = metav1.NewTime(time.Now().Add(-2 * time.Hour))
	r := newExpiryReconciler(ib)

	_, expired, err := r.checkExpiry(context.Background(), &ib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !expired {
		t.Fatal("in-progress build past TTL from creation should expire")
	}
}

func TestCheckExpiry_FailedBuildExpires(t *testing.T) {
	ib := newTestImageBuild("failed-old", phaseFailed, "1h", 2*time.Hour)
	r := newExpiryReconciler(ib)

	_, expired, err := r.checkExpiry(context.Background(), &ib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !expired {
		t.Fatal("failed build past TTL should expire")
	}
}

func TestCheckExpiry_SetsExpiresAtInStatus(t *testing.T) {
	ib := newTestImageBuild("with-status", phaseCompleted, "24h", 1*time.Hour)
	r := newExpiryReconciler(ib)

	_, expired, err := r.checkExpiry(context.Background(), &ib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expired {
		t.Fatal("build should not be expired")
	}

	got := &automotivev1alpha1.ImageBuild{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "with-status", Namespace: "test-ns"}, got); err != nil {
		t.Fatalf("failed to get build: %v", err)
	}
	if got.Status.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt to be set in status")
	}
	expectedExpiry := ib.Status.CompletionTime.Add(24 * time.Hour)
	diff := got.Status.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("ExpiresAt = %v, want ~%v (diff %v)", got.Status.ExpiresAt.Time, expectedExpiry, diff)
	}
}

func TestResolveEffectiveTTL(t *testing.T) {
	cases := []struct {
		name           string
		specTTL        string
		configBuildTTL string
		hasConfig      bool
		expectedTTL    time.Duration
	}{
		{"spec overrides OperatorConfig", "48h", "72h", true, 48 * time.Hour},
		{"OperatorConfig default", "", "72h", true, 72 * time.Hour},
		{"hardcoded fallback (no config)", "", "", false, 24 * time.Hour},
		{"spec zero disables expiry", "0", "", false, 0},
		{"OperatorConfig zero disables expiry", "", "0", true, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ib := newTestImageBuild("test-build", phaseCompleted, tc.specTTL, 1*time.Hour)
			scheme := newTestSchemeWithTekton()
			builder := fake.NewClientBuilder().WithScheme(scheme)

			if tc.hasConfig {
				builder = builder.WithObjects(&automotivev1alpha1.OperatorConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: OperatorNamespace},
					Spec: automotivev1alpha1.OperatorConfigSpec{
						OSBuilds: &automotivev1alpha1.OSBuildsConfig{
							DefaultBuildTTL: tc.configBuildTTL,
						},
					},
				})
			}

			r := &ImageBuildReconciler{
				Client: builder.Build(),
				Scheme: scheme,
				Log:    logr.Discard(),
			}

			ttl, err := r.resolveEffectiveTTL(context.Background(), &ib)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ttl != tc.expectedTTL {
				t.Errorf("expected %v, got %v", tc.expectedTTL, ttl)
			}
		})
	}
}
