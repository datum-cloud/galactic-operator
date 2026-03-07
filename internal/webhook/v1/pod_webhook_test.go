package v1

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	galacticv1alpha "github.com/datum-cloud/galactic-operator/api/v1alpha"
)

const VPCAttachmentName = "abcd1234"
const VPCAttachmentInterface = "galactic0"

var _ = Describe("Pod Webhook", func() {
	var (
		pod       *corev1.Pod
		validator PodCustomValidator
		defaulter PodCustomDefaulter
	)

	BeforeEach(func() {
		err := galacticv1alpha.AddToScheme(k8sClient.Scheme())
		Expect(err).NotTo(HaveOccurred())

		vpcAttachment := &galacticv1alpha.VPCAttachment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      VPCAttachmentName,
				Namespace: "default",
			},
			Spec: galacticv1alpha.VPCAttachmentSpec{
				VPC: corev1.ObjectReference{
					APIVersion: "galactic.datumapis.com/v1alpha",
					Kind:       "VPC",
					Name:       "vpc-sample",
					Namespace:  "default",
				},
				Interface: galacticv1alpha.VPCAttachmentInterface{
					Name: VPCAttachmentInterface,
					Addresses: []string{
						"10.1.1.1/24",
						"2001:10:1:1::1/64",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, vpcAttachment)).To(Succeed())
		vpcAttachment.Status.Ready = true
		Expect(k8sClient.Status().Update(ctx, vpcAttachment)).To(Succeed())
	})

	AfterEach(func() {
		var vpcAttachments galacticv1alpha.VPCAttachmentList
		err := k8sClient.List(ctx, &vpcAttachments)
		Expect(err).NotTo(HaveOccurred())
		Expect(vpcAttachments.Items).To(HaveLen(1))
		for _, vpcAttachment := range vpcAttachments.Items {
			Expect(k8sClient.Delete(ctx, &vpcAttachment)).To(Succeed())
		}
	})

	Context("When creating a Pod with valid VPC attachment", func() {
		It("should set the networks annotation", func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						galacticv1alpha.VPCAttachmentAnnotation: VPCAttachmentName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test:latest",
						},
					},
				},
			}

			validator = PodCustomValidator{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(validator.ValidateCreate(ctx, pod)).Error().NotTo(HaveOccurred())

			defaulter = PodCustomDefaulter{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(defaulter.Default(ctx, pod)).Error().NotTo(HaveOccurred())
			Expect(pod.Annotations[PodAnnotationMultusNetworks]).To(Equal(fmt.Sprintf("%s@%s", VPCAttachmentName, VPCAttachmentInterface)))
		})
	})

	Context("When creating a Pod with invalid VPC attachment", func() {
		It("should reject the pod", func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						galacticv1alpha.VPCAttachmentAnnotation: "xxxxxx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test:latest",
						},
					},
				},
			}

			validator = PodCustomValidator{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(validator.ValidateCreate(ctx, pod)).Error().To(HaveOccurred())

			defaulter = PodCustomDefaulter{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(defaulter.Default(ctx, pod)).Error().To(HaveOccurred())
		})
	})
})

var _ = Describe("Pod Webhook Without VPCAttachment Annotation", func() {
	It("should allow the pod without modification", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test:latest",
					},
				},
			},
		}

		defaulter := PodCustomDefaulter{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		Expect(defaulter.Default(ctx, pod)).NotTo(HaveOccurred())
		Expect(pod.Annotations).NotTo(HaveKey(PodAnnotationMultusNetworks))

		validator := PodCustomValidator{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		Expect(validator.ValidateCreate(ctx, pod)).Error().NotTo(HaveOccurred())
	})
})

var _ = Describe("Pod Webhook Race Conditions", func() {
	var (
		pod       *corev1.Pod
		validator PodCustomValidator
		defaulter PodCustomDefaulter
	)

	BeforeEach(func() {
		err := galacticv1alpha.AddToScheme(k8sClient.Scheme())
		Expect(err).NotTo(HaveOccurred())
	})

	Context("When creating a Pod before VPCAttachment exists", func() {
		It("should reject the pod in both webhooks", func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						galacticv1alpha.VPCAttachmentAnnotation: "nonexistent-attachment",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test:latest",
						},
					},
				},
			}

			defaulter = PodCustomDefaulter{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(defaulter.Default(ctx, pod)).To(HaveOccurred())

			validator = PodCustomValidator{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(validator.ValidateCreate(ctx, pod)).Error().To(HaveOccurred())
		})
	})

	Context("When creating a Pod with a VPCAttachment that is not ready", func() {
		var vpcAttachment *galacticv1alpha.VPCAttachment

		BeforeEach(func() {
			vpcAttachment = &galacticv1alpha.VPCAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-ready-attachment",
					Namespace: "default",
				},
				Spec: galacticv1alpha.VPCAttachmentSpec{
					VPC: corev1.ObjectReference{
						APIVersion: "galactic.datumapis.com/v1alpha",
						Kind:       "VPC",
						Name:       "vpc-sample",
						Namespace:  "default",
					},
					Interface: galacticv1alpha.VPCAttachmentInterface{
						Name: "galactic0",
						Addresses: []string{
							"10.1.1.1/24",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, vpcAttachment)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, vpcAttachment)).To(Succeed())
		})

		It("should reject the pod in both webhooks", func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						galacticv1alpha.VPCAttachmentAnnotation: "not-ready-attachment",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test:latest",
						},
					},
				},
			}

			defaulter = PodCustomDefaulter{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(defaulter.Default(ctx, pod)).To(HaveOccurred())

			validator = PodCustomValidator{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(validator.ValidateCreate(ctx, pod)).Error().To(HaveOccurred())
		})
	})

	Context("When creating a Pod with a ready VPCAttachment", func() {
		var vpcAttachment *galacticv1alpha.VPCAttachment

		BeforeEach(func() {
			vpcAttachment = &galacticv1alpha.VPCAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ready-attachment",
					Namespace: "default",
				},
				Spec: galacticv1alpha.VPCAttachmentSpec{
					VPC: corev1.ObjectReference{
						APIVersion: "galactic.datumapis.com/v1alpha",
						Kind:       "VPC",
						Name:       "vpc-sample",
						Namespace:  "default",
					},
					Interface: galacticv1alpha.VPCAttachmentInterface{
						Name: "galactic0",
						Addresses: []string{
							"10.1.1.1/24",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, vpcAttachment)).To(Succeed())
			vpcAttachment.Status.Ready = true
			Expect(k8sClient.Status().Update(ctx, vpcAttachment)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, vpcAttachment)).To(Succeed())
		})

		It("should allow the pod and set the Multus annotation", func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						galacticv1alpha.VPCAttachmentAnnotation: "ready-attachment",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test:latest",
						},
					},
				},
			}

			defaulter = PodCustomDefaulter{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(defaulter.Default(ctx, pod)).NotTo(HaveOccurred())
			Expect(pod.Annotations[PodAnnotationMultusNetworks]).To(Equal(fmt.Sprintf("%s@%s", "ready-attachment", "galactic0")))

			validator = PodCustomValidator{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Expect(validator.ValidateCreate(ctx, pod)).Error().NotTo(HaveOccurred())
		})
	})
})
