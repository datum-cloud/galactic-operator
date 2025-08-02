package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	galacticv1alpha "github.com/datum-cloud/galactic-operator/api/v1alpha"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
)

var _ = Describe("VPCAttachment Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		vpcName := "test-vpc"
		vpcTypeNamespacedName := types.NamespacedName{
			Name:      vpcName,
			Namespace: "default",
		}

		BeforeEach(func() {
			err := nadv1.AddToScheme(k8sClient.Scheme())
			Expect(err).NotTo(HaveOccurred())

			By("creating the custom resource for the Kind VPC")
			resource := &galacticv1alpha.VPC{}
			err = k8sClient.Get(ctx, vpcTypeNamespacedName, resource)
			if err != nil && errors.IsNotFound(err) {
				resource := &galacticv1alpha.VPC{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vpcName,
						Namespace: "default",
					},
					Spec: galacticv1alpha.VPCSpec{
						Networks: []string{
							"10.1.1.0/24",
							"2001:10:1:1::/64",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &galacticv1alpha.VPC{}
			err := k8sClient.Get(ctx, vpcTypeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("cleanup the specific resource instance VPC")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("creating the custom resource for the Kind VPCAttachment")

			vpcAttachmentName := "test-vpcattachment"
			vpcAttachmentTypeNamespacedName := types.NamespacedName{
				Name:      vpcAttachmentName,
				Namespace: "default",
			}

			resource := &galacticv1alpha.VPCAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vpcAttachmentName,
					Namespace: "default",
				},
				Spec: galacticv1alpha.VPCAttachmentSpec{
					VPC: corev1.ObjectReference{
						APIVersion: "galactic.datumapis.com/v1alpha",
						Kind:       "VPC",
						Name:       vpcName,
						Namespace:  "default",
					},
					Interface: galacticv1alpha.VPCAttachmentInterface{
						Name: "galactic0",
						Addresses: []string{
							"10.1.1.1/24",
							"2001:10:1:1::1/64",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("reconciling the created resource")
			controllerReconciler := &VPCAttachmentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: vpcTypeNamespacedName,
			})
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: vpcAttachmentTypeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			nadResource := &nadv1.NetworkAttachmentDefinition{}
			err = k8sClient.Get(ctx, vpcAttachmentTypeNamespacedName, nadResource)
			Expect(err).NotTo(HaveOccurred())
			Expect(nadResource.Spec.Config).To(Equal(`{}`))
		})
	})
})
