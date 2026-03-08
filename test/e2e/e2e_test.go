/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datum-cloud/galactic-common/util"
	"github.com/datum-cloud/galactic-operator/internal/identifier"
	"github.com/datum-cloud/galactic-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "galactic-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "galactic-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "galactic-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "galactic-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=galactic-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring(
				"controller_runtime_reconcile_total",
			))
		})

		It("should provisioned cert-manager", func() {
			By("validating that cert-manager has the certificate Secret")
			verifyCertManager := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "secrets", "webhook-server-cert", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}
			Eventually(verifyCertManager).Should(Succeed())
		})

		It("should have CA injection for mutating webhooks", func() {
			By("checking CA injection for mutating webhooks")
			verifyCAInjection := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"mutatingwebhookconfigurations.admissionregistration.k8s.io",
					"galactic-operator-mutating-webhook-configuration",
					"-o", "go-template={{ range .webhooks }}{{ .clientConfig.caBundle }}{{ end }}")
				mwhOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(mwhOutput)).To(BeNumerically(">", 10))
			}
			Eventually(verifyCAInjection).Should(Succeed())
		})

		It("should have CA injection for validating webhooks", func() {
			By("checking CA injection for validating webhooks")
			verifyCAInjection := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"validatingwebhookconfigurations.admissionregistration.k8s.io",
					"galactic-operator-validating-webhook-configuration",
					"-o", "go-template={{ range .webhooks }}{{ .clientConfig.caBundle }}{{ end }}")
				vwhOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(vwhOutput)).To(BeNumerically(">", 10))
			}
			Eventually(verifyCAInjection).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		// TODO: Customize the e2e test suite with scenarios specific to your project.
		// Consider applying sample/CR(s) and check their status and/or verifying
		// the reconciliation by using the metrics, i.e.:
		// metricsOutput := getMetricsOutput()
		// Expect(metricsOutput).To(ContainSubstring(
		//    fmt.Sprintf(`controller_runtime_reconcile_total{controller="%s",result="success"} 1`,
		//    strings.ToLower(<Kind>),
		// ))
	})

	Context("Full Deployment", func() {
		const testNamespace = "galactic-e2e-test"

		type testCase struct {
			name          string
			interfaceName string
		}

		attachments := []testCase{
			{name: "attachment-1", interfaceName: "net0"},
			{name: "attachment-2", interfaceName: "net0"},
			{name: "attachment-3", interfaceName: "net0"},
		}

		BeforeAll(func() {
			By("creating the test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")
		})

		AfterAll(func() {
			By("cleaning up the test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should apply the kustomize bundle and ensure all resources are correctly created", func() {
			By("applying the kustomize bundle (VPC, VPCAttachments and Deployments simultaneously)")
			cmd := exec.Command("kubectl", "apply", "-k", "test/e2e/testdata/", "-n", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply kustomize bundle")

			By("verifying the VPC is ready and has an identifier")
			verifyVPCReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "vpc", "main",
					"-n", testNamespace,
					"-o", "jsonpath={.status.ready}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"), "VPC should be ready")

				cmd = exec.Command("kubectl", "get", "vpc", "main",
					"-n", testNamespace,
					"-o", "jsonpath={.status.identifier}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "VPC identifier should be set")
				g.Expect(output).To(HaveLen(12), "VPC identifier should be 12 characters")
				vpcID, parseErr := strconv.ParseUint(output, 16, 64)
				g.Expect(parseErr).NotTo(HaveOccurred(), "VPC identifier should be valid hex")
				g.Expect(vpcID).To(BeNumerically(">", 0), "VPC identifier should be greater than 0")
				g.Expect(vpcID).To(BeNumerically("<", identifier.MaxVPC), "VPC identifier should be less than max")
			}
			Eventually(verifyVPCReady, 2*time.Minute, 5*time.Second).Should(Succeed())

			for _, tc := range attachments {
				By(fmt.Sprintf("verifying VPCAttachment %s is ready and has an identifier", tc.name))
				verifyAttachmentReady := func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "vpcattachment", tc.name,
						"-n", testNamespace,
						"-o", "jsonpath={.status.ready}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).To(Equal("true"), fmt.Sprintf("VPCAttachment %s should be ready", tc.name))

					cmd = exec.Command("kubectl", "get", "vpcattachment", tc.name,
						"-n", testNamespace,
						"-o", "jsonpath={.status.identifier}")
					output, err = utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).NotTo(BeEmpty(), fmt.Sprintf("VPCAttachment %s identifier should be set", tc.name))
					g.Expect(output).To(HaveLen(4), fmt.Sprintf("VPCAttachment %s identifier should be 4 characters", tc.name))
					attachmentID, parseErr := strconv.ParseUint(output, 16, 64)
					g.Expect(parseErr).NotTo(HaveOccurred(), fmt.Sprintf("VPCAttachment %s identifier should be valid hex", tc.name))
					g.Expect(attachmentID).To(BeNumerically(">", 0),
						fmt.Sprintf("VPCAttachment %s identifier should be greater than 0", tc.name))
					g.Expect(attachmentID).To(BeNumerically("<", identifier.MaxVPCAttachment),
						fmt.Sprintf("VPCAttachment %s identifier should be less than max", tc.name))
				}
				Eventually(verifyAttachmentReady, 2*time.Minute, 5*time.Second).Should(Succeed())
			}

			for _, tc := range attachments {
				By(fmt.Sprintf("verifying NetworkAttachmentDefinition %s is created with correct CNI config", tc.name))
				verifyNAD := func(g Gomega) {
					// Get the VPC identifier and convert to base62
					cmd := exec.Command("kubectl", "get", "vpc", "main",
						"-n", testNamespace,
						"-o", "jsonpath={.status.identifier}")
					vpcHexID, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					vpcBase62, err := util.HexToBase62(vpcHexID)
					g.Expect(err).NotTo(HaveOccurred(), "VPC identifier should convert to base62")

					// Get the VPCAttachment identifier and convert to base62
					cmd = exec.Command("kubectl", "get", "vpcattachment", tc.name,
						"-n", testNamespace,
						"-o", "jsonpath={.status.identifier}")
					attachmentHexID, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					attachmentBase62, err := util.HexToBase62(attachmentHexID)
					g.Expect(err).NotTo(HaveOccurred(), "VPCAttachment identifier should convert to base62")

					// Get the NAD config
					cmd = exec.Command("kubectl", "get", "network-attachment-definition", tc.name,
						"-n", testNamespace,
						"-o", "jsonpath={.spec.config}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred(),
						fmt.Sprintf("NetworkAttachmentDefinition %s should exist", tc.name))
					g.Expect(output).NotTo(BeEmpty(),
						fmt.Sprintf("NetworkAttachmentDefinition %s should have a config", tc.name))

					g.Expect(output).To(ContainSubstring(`"cniVersion":"0.4.0"`),
						"CNI config should have cniVersion 0.4.0")
					g.Expect(output).To(ContainSubstring(`"type":"galactic"`),
						"CNI config should use the galactic plugin")
					g.Expect(output).To(ContainSubstring(`"mtu":1372`),
						"CNI config should set MTU to 1372")
					g.Expect(output).To(ContainSubstring(`"type":"static"`),
						"CNI config IPAM should be static")

					// Verify the base62-encoded identifiers match
					g.Expect(output).To(ContainSubstring(fmt.Sprintf(`"vpc":"%s"`, vpcBase62)),
						"CNI config VPC identifier should match base62-encoded VPC status identifier")
					g.Expect(output).To(ContainSubstring(fmt.Sprintf(`"vpcattachment":"%s"`, attachmentBase62)),
						"CNI config VPCAttachment identifier should match base62-encoded VPCAttachment status identifier")
				}
				Eventually(verifyNAD, 2*time.Minute, 5*time.Second).Should(Succeed())
			}

			for _, tc := range attachments {
				By(fmt.Sprintf("waiting for %s deployment to have ready pods", tc.name))
				verifyDeploymentReady := func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "deployment", tc.name,
						"-n", testNamespace,
						"-o", "jsonpath={.status.readyReplicas}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).To(Equal("1"), fmt.Sprintf("deployment %s should have 1 ready replica", tc.name))
				}
				Eventually(verifyDeploymentReady, 5*time.Minute, 5*time.Second).Should(Succeed())

				By(fmt.Sprintf("verifying %s pods have the Multus network annotation", tc.name))
				verifyMultusAnnotation := func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", fmt.Sprintf("app.kubernetes.io/name=%s", tc.name),
						"-n", testNamespace,
						"-o", `jsonpath={.items[0].metadata.annotations.k8s\.v1\.cni\.cncf\.io/networks}`)
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					expectedAnnotation := fmt.Sprintf("%s@%s", tc.name, tc.interfaceName)
					g.Expect(output).To(Equal(expectedAnnotation),
						fmt.Sprintf("pod for %s should have Multus annotation %q, got %q", tc.name, expectedAnnotation, output))
				}
				Eventually(verifyMultusAnnotation, 2*time.Minute, 5*time.Second).Should(Succeed())
			}
		})

		It("should reject pods when VPCAttachment does not exist", func() {
			By("creating a deployment referencing a non-existent VPCAttachment")
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace, "-f", "-")
			cmd.Stdin = strings.NewReader(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: missing-attachment
  labels:
    app.kubernetes.io/name: missing-attachment
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: missing-attachment
  template:
    metadata:
      labels:
        app.kubernetes.io/name: missing-attachment
      annotations:
        k8s.v1alpha.galactic.datumapis.com/vpc-attachment: does-not-exist
    spec:
      containers:
      - name: main-container
        image: alpine
        command: ["/bin/ash", "-c", "sleep infinity"]
`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create missing-attachment deployment")

			By("verifying that pods fail to be created (webhook should reject)")
			// Give the deployment controller time to attempt pod creation
			verifyPodsRejected := func(g Gomega) {
				// Check that no pods are running for this deployment
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", "app.kubernetes.io/name=missing-attachment",
					"-n", testNamespace,
					"-o", "jsonpath={.items}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("[]"), "no pods should be running for missing-attachment")

				// Check ReplicaSet events for webhook rejection
				cmd = exec.Command("kubectl", "get", "events",
					"-n", testNamespace,
					"--field-selector", "reason=FailedCreate",
					"-o", "jsonpath={.items[*].message}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("does-not-exist"),
					"should see webhook rejection event referencing the missing VPCAttachment")
			}
			Eventually(verifyPodsRejected, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("cleaning up the missing-attachment deployment")
			cmd = exec.Command("kubectl", "delete", "deployment", "missing-attachment",
				"-n", testNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() string {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	metricsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
	Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
	return metricsOutput
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
