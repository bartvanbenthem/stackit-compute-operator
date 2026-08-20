//go:build integration

package controller

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

func TestIntegration_VolumeLifecycle(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-volume", Namespace: "default"}

	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.VolumeSpec{
			ProjectId:        "11111111-1111-1111-1111-111111111111",
			Region:           "eu01",
			AvailabilityZone: "eu01-1",
			Size:             32,
		},
	}
	if err := k8sClient.Create(ctx, volume); err != nil {
		t.Fatalf("creating Volume: %v", err)
	}

	eventually(t, 30*time.Second, "volume becomes Ready=True/Available", func() bool {
		got := &computev1alpha1.Volume{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		if got.Status.VolumeId == "" {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Available"
	})

	got := &computev1alpha1.Volume{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting Volume: %v", err)
	}
	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting Volume: %v", err)
	}

	eventually(t, 30*time.Second, "volume object is fully removed", func() bool {
		cur := &computev1alpha1.Volume{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})
}

func TestIntegration_ImageLifecycle(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-image", Namespace: "default"}

	image := &computev1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.ImageSpec{
			ProjectId:  "11111111-1111-1111-1111-111111111111",
			Region:     "eu01",
			DiskFormat: "qcow2",
		},
	}
	if err := k8sClient.Create(ctx, image); err != nil {
		t.Fatalf("creating Image: %v", err)
	}

	eventually(t, 30*time.Second, "image becomes Ready=True/Available", func() bool {
		got := &computev1alpha1.Image{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		if got.Status.ImageId == "" {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Available"
	})

	got := &computev1alpha1.Image{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting Image: %v", err)
	}
	if got.Status.UploadUrl == "" {
		t.Error("Status.UploadUrl = empty, want non-empty")
	}
	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting Image: %v", err)
	}

	eventually(t, 30*time.Second, "image object is fully removed", func() bool {
		cur := &computev1alpha1.Image{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})
}

func TestIntegration_NetworkLifecycle(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-network", Namespace: "default"}

	network := &computev1alpha1.Network{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.NetworkSpec{
			ProjectId: "11111111-1111-1111-1111-111111111111",
			Region:    "eu01",
		},
	}
	if err := k8sClient.Create(ctx, network); err != nil {
		t.Fatalf("creating Network: %v", err)
	}

	eventually(t, 30*time.Second, "network becomes Ready=True/Created", func() bool {
		got := &computev1alpha1.Network{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		if got.Status.NetworkId == "" {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Created"
	})

	got := &computev1alpha1.Network{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting Network: %v", err)
	}
	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting Network: %v", err)
	}

	eventually(t, 30*time.Second, "network object is fully removed", func() bool {
		cur := &computev1alpha1.Network{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})
}

// TestIntegration_AdoptExistingVolume exercises the "not the owner" path
// against a real apiserver: a Volume with spec.existingId set must only
// observe the pre-seeded STACKIT-side volume (no finalizer, no Create
// call), and deleting the CR must NOT delete the underlying volume.
func TestIntegration_AdoptExistingVolume(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-adopted-volume", Namespace: "default"}
	const existingID = "99999999-8888-7777-6666-555555555555"

	volumeBackend.seedExisting(existingID, 128)

	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.VolumeSpec{
			ProjectId:        "11111111-1111-1111-1111-111111111111",
			Region:           "eu01",
			AvailabilityZone: "eu01-1",
			ExistingID:       ptr(existingID),
		},
	}
	if err := k8sClient.Create(ctx, volume); err != nil {
		t.Fatalf("creating adopted Volume: %v", err)
	}

	eventually(t, 30*time.Second, "adopted volume becomes Ready=True/Available", func() bool {
		got := &computev1alpha1.Volume{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Available" &&
			got.Status.VolumeId == existingID && got.Status.Size == 128
	})

	got := &computev1alpha1.Volume{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting adopted Volume: %v", err)
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("Finalizers = %v, want none on an adopted resource", got.Finalizers)
	}

	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting adopted Volume: %v", err)
	}
	eventually(t, 30*time.Second, "adopted volume CR is fully removed", func() bool {
		cur := &computev1alpha1.Volume{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})

	if !volumeBackend.existsLocked() {
		t.Error("STACKIT-side volume was deleted, want it left untouched (adopted, not owned)")
	}
}

func ptr[T any](v T) *T { return &v }
