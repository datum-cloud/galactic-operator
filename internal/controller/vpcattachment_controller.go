package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	galacticv1alpha "github.com/datum-cloud/galactic-operator/api/v1alpha"
)

type VPCAttachmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=galactic.datumapis.com,resources=vpcattachments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=galactic.datumapis.com,resources=vpcattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=galactic.datumapis.com,resources=vpcattachments/finalizers,verbs=update

func (r *VPCAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var vpcAttachment galacticv1alpha.VPCAttachment
	if err := r.Get(ctx, req.NamespacedName, &vpcAttachment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	vpcNamespacedName := types.NamespacedName{
		Namespace: vpcAttachment.Spec.VPC.Namespace,
		Name:      vpcAttachment.Spec.VPC.Name,
	}
	var vpc galacticv1alpha.VPC
	if err := r.Get(ctx, vpcNamespacedName, &vpc); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("VPC", "vpc", vpc)

	return ctrl.Result{}, nil
}

func (r *VPCAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&galacticv1alpha.VPCAttachment{}).
		Named("vpcattachment").
		Complete(r)
}
