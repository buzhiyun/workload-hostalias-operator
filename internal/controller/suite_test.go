package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	hostaliasv1alpha1 "github.com/buzhiyun/workload-hostalias-operator/api/v1alpha1"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{"../../config/crd/bases"},
		BinaryAssetsDirectory: "", // let envtest download binaries
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = hostaliasv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&WorkloadHostAliasReconciler{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Recorder: k8sManager.GetEventRecorderFor("workload-hostalias-operator"),
	}).SetupWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("WorkloadHostAlias Controller", func() {
	const namespace = "default"

	Context("When creating a WorkloadHostAlias for a Deployment", func() {
		var (
			deploy     *appsv1.Deployment
			wha        *hostaliasv1alpha1.WorkloadHostAlias
			whaName    string
			deployName string
		)

		BeforeEach(func() {
			whaName = fmt.Sprintf("test-wha-%d", time.Now().UnixNano())
			deployName = fmt.Sprintf("test-deploy-%d", time.Now().UnixNano())

			deploy = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": deployName},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": deployName},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "main",
								Image: "nginx:latest",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).Should(Succeed())

			wha = &hostaliasv1alpha1.WorkloadHostAlias{
				ObjectMeta: metav1.ObjectMeta{
					Name: whaName,
				},
				Spec: hostaliasv1alpha1.WorkloadHostAliasSpec{
					Target: hostaliasv1alpha1.WorkloadTarget{
						Namespace: namespace,
						Kind:      "Deployment",
						Name:      deployName,
					},
					HostAliases: []hostaliasv1alpha1.HostAliasEntry{
						{IP: "10.0.0.1", Hostnames: []string{"foo.bar.com"}},
						{IP: "10.0.0.2", Hostnames: []string{"bar.baz.com"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wha)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, wha)).Should(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: whaName}, &hostaliasv1alpha1.WorkloadHostAlias{})
			}, 10*time.Second, 1*time.Second).ShouldNot(Succeed())

			Expect(k8sClient.Delete(ctx, deploy)).Should(Succeed())
		})

		It("should inject hostAliases into the Deployment", func() {
			Eventually(func() []corev1.HostAlias {
				updated := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: namespace}, updated)).Should(Succeed())
				return updated.Spec.Template.Spec.HostAliases
			}, 10*time.Second, 1*time.Second).Should(HaveLen(2))
		})

		It("should set managed annotations on the Deployment", func() {
			Eventually(func() string {
				updated := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: namespace}, updated)).Should(Succeed())
				return updated.Annotations[ManagedAnnotationKey]
			}, 10*time.Second, 1*time.Second).Should(Equal("true"))
		})

		It("should update the Synced status condition", func() {
			Eventually(func() string {
				updated := &hostaliasv1alpha1.WorkloadHostAlias{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: whaName}, updated)).Should(Succeed())
				for _, c := range updated.Status.Conditions {
					if c.Type == ConditionTypeSynced {
						return string(c.Status)
					}
				}
				return ""
			}, 10*time.Second, 1*time.Second).Should(Equal("True"))
		})

		It("should clean up hostAliases when WorkloadHostAlias is deleted", func() {
			// Wait for injection
			Eventually(func() []corev1.HostAlias {
				updated := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: namespace}, updated)).Should(Succeed())
				return updated.Spec.Template.Spec.HostAliases
			}, 10*time.Second, 1*time.Second).Should(HaveLen(2))

			// Delete the WorkloadHostAlias
			Expect(k8sClient.Delete(ctx, wha)).Should(Succeed())

			// Wait for cleanup
			Eventually(func() []corev1.HostAlias {
				updated := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: namespace}, updated)).Should(Succeed())
				return updated.Spec.Template.Spec.HostAliases
			}, 10*time.Second, 1*time.Second).Should(HaveLen(0))
		})
	})
})
