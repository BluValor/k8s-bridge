//go:build ignore

// Reference unit test setup pattern for SlurmJobReconciler and JobTeardownReconciler.
// Source: internal/controller/slurmjob/slurmjob_controller_helper_test.go & teardown_reconciler_test.go
package slurmjob

import (
	"context"
	"testing"

	slurmapi "github.com/SlinkyProject/slurm-client/api/v0044"
	slurmclientfake "github.com/SlinkyProject/slurm-client/pkg/client/fake"
	slurmclientinterceptor "github.com/SlinkyProject/slurm-client/pkg/client/interceptor"
	slurmclienttypes "github.com/SlinkyProject/slurm-client/pkg/types"
	slinkyv1beta1 "github.com/SlinkyProject/slurm-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	jobsetv1alpha2 "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	k8sbridgev1alpha1 "github.com/GoogleCloudPlatform/k8s-bridge/api/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-bridge/internal/clientmap"
	"github.com/GoogleCloudPlatform/k8s-bridge/test/utils"
)

// Pattern A: Testing SlurmJobReconciler, SlurmPoller, or JobTeardownReconciler with &utils.SlurmClient{}
func exampleReconcilerTestSetup(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = k8sbridgev1alpha1.AddToScheme(scheme)
	_ = jobsetv1alpha2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = slinkyv1beta1.AddToScheme(scheme)

	wm := &k8sbridgev1alpha1.WorkloadMixing{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "slurm"},
		Spec: k8sbridgev1alpha1.WorkloadMixingSpec{
			ControllerRef: corev1.LocalObjectReference{Name: "slurm"},
			PartitionsMapping: []k8sbridgev1alpha1.PartitionMapping{
				{Partition: "k8s-partition", LocalQueue: "slurm-queue"},
			},
		},
	}
	controllerCR := &slinkyv1beta1.Controller{
		ObjectMeta: metav1.ObjectMeta{Name: "slurm", Namespace: "slurm"},
	}

	fakeK8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wm, controllerCR).Build()

	mockSlurm := &utils.SlurmClient{}
	mockSlurm.SetPendingJobs("k8s-partition", []slurmclienttypes.V0044JobInfo{
		{
			V0044JobInfo: slurmapi.V0044JobInfo{
				JobId:     ptr.To(int32(42)),
				Name:      ptr.To("test-job"),
				Partition: ptr.To("k8s-partition"),
				Hold:      ptr.To(true),
				Comment:   ptr.To("k8s-bridge-auto-hold"),
				JobState:  &[]slurmapi.V0044JobInfoJobState{slurmapi.V0044JobInfoJobStatePENDING},
				NodeCount: &slurmapi.V0044Uint32NoValStruct{Set: ptr.To(true), Number: ptr.To(int32(1))},
			},
		},
	})

	cMap := clientmap.NewClientMap()
	cMap.Add(ctx, types.NamespacedName{Namespace: "slurm", Name: "slurm"}, mockSlurm)

	reconciler := &SlurmJobReconciler{
		Client:     fakeK8s,
		Scheme:     scheme,
		ClientMap:  cMap,
		Translator: NewJobSetTranslator(),
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "slurm", Name: "slurm-job-default-42"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Pattern B: Custom List/Delete error injection or V0041ControllerPing with slurmclientfake
func exampleNodeDeleteMock() client.Reader {
	_ = slurmclientfake.NewClientBuilder().WithInterceptorFuncs(slurmclientinterceptor.Funcs{
		Delete: func(ctx context.Context, obj slurmclienttypes.Object, opts ...slurmclienttypes.DeleteOption) error {
			return nil
		},
	}).Build()
	return nil
}
