package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostaliasv1alpha1 "github.com/buzhiyun/workload-hostalias-operator/api/v1alpha1"
)

const (
	ManagedAnnotationKey        = "workload-hostalias-operator/managed"
	LastAppliedAnnotationKey    = "workload-hostalias-operator/last-applied-hostaliases"
	OwnerAnnotationKey          = "workload-hostalias-operator/owner"
	FinalizerName               = "workload-hostalias-operator/finalizer"
	ConditionTypeSynced         = "Synced"
	RequeueIntervalWorkloadGone = 30 * time.Second
)

// WorkloadHostAliasReconciler reconciles a WorkloadHostAlias object
type WorkloadHostAliasReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=workload-hostalias-operator.buzhiyun,resources=workloadhostaliases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=workload-hostalias-operator.buzhiyun,resources=workloadhostaliases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=workload-hostalias-operator.buzhiyun,resources=workloadhostaliases/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *WorkloadHostAliasReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)

	defer func() {
		ReconciliationDuration.Observe(time.Since(startTime).Seconds())
	}()

	// 1. Fetch WorkloadHostAlias instance
	var crdInstance hostaliasv1alpha1.WorkloadHostAlias
	if err := r.Get(ctx, req.NamespacedName, &crdInstance); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("WorkloadHostAlias resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		ReconciliationTotal.WithLabelValues("error").Inc()
		return ctrl.Result{}, err
	}

	// 2. Handle finalizer
	if !crdInstance.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &crdInstance)
	}

	if !containsFinalizer(&crdInstance, FinalizerName) {
		if err := r.addFinalizer(ctx, &crdInstance); err != nil {
			ReconciliationTotal.WithLabelValues("error").Inc()
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 3. Fetch the target workload
	workload, err := r.fetchWorkload(ctx, &crdInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Target workload not found, will retry",
				"kind", crdInstance.Spec.Target.Kind,
				"namespace", crdInstance.Spec.Target.Namespace,
				"name", crdInstance.Spec.Target.Name,
			)
			_ = r.updateStatus(ctx, &crdInstance, metav1.ConditionFalse, "WorkloadNotFound",
				fmt.Sprintf("Target workload %s/%s/%s not found",
					crdInstance.Spec.Target.Kind, crdInstance.Spec.Target.Namespace, crdInstance.Spec.Target.Name))
			r.Recorder.Eventf(&crdInstance, corev1.EventTypeWarning, "WorkloadNotFound",
				"Target workload %s/%s/%s not found",
				crdInstance.Spec.Target.Kind, crdInstance.Spec.Target.Namespace, crdInstance.Spec.Target.Name)
			ReconciliationTotal.WithLabelValues("requeue").Inc()
			return ctrl.Result{RequeueAfter: RequeueIntervalWorkloadGone}, nil
		}
		ReconciliationTotal.WithLabelValues("error").Inc()
		return ctrl.Result{}, err
	}

	// 4. Check for conflicting owner
	ownerAnn := getAnnotation(workload, OwnerAnnotationKey)
	expectedOwner := fmt.Sprintf("%s/%s", crdInstance.Namespace, crdInstance.Name)
	if ownerAnn != "" && ownerAnn != expectedOwner {
		logger.Info("Workload is managed by another WorkloadHostAlias, skipping",
			"currentOwner", ownerAnn, "expectedOwner", expectedOwner)
		_ = r.updateStatus(ctx, &crdInstance, metav1.ConditionFalse, "ConflictingOwner",
			fmt.Sprintf("Workload is managed by another WorkloadHostAlias: %s", ownerAnn))
		r.Recorder.Eventf(&crdInstance, corev1.EventTypeWarning, "ConflictingOwner",
			"Workload is managed by another WorkloadHostAlias: %s", ownerAnn)
		ReconciliationTotal.WithLabelValues("error").Inc()
		return ctrl.Result{}, nil
	}

	// 5. Compute desired hostAliases from CRD spec
	desired := toCoreV1HostAliases(crdInstance.Spec.HostAliases)

	// 6. Get current and last-applied hostAliases
	podTemplateSpec, err := getPodTemplateSpec(workload)
	if err != nil {
		ReconciliationTotal.WithLabelValues("error").Inc()
		return ctrl.Result{}, err
	}

	currentHostAliases := podTemplateSpec.Spec.HostAliases
	lastApplied := parseLastAppliedAnnotation(workload)

	// 7. Compute merged result
	merged := MergeHostAliases(currentHostAliases, lastApplied, desired)

	// 8. Check if update is needed
	lastAppliedJSON, _ := json.Marshal(desired)
	annotations := workload.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	needsUpdate := false
	if !hostAliasesSliceEqual(merged, currentHostAliases) {
		needsUpdate = true
	}
	if annotations[ManagedAnnotationKey] != "true" {
		needsUpdate = true
	}
	if annotations[LastAppliedAnnotationKey] != string(lastAppliedJSON) {
		needsUpdate = true
	}
	if annotations[OwnerAnnotationKey] != expectedOwner {
		needsUpdate = true
	}

	if needsUpdate {
		// 9. Update the workload
		podTemplateSpec.Spec.HostAliases = merged
		annotations[ManagedAnnotationKey] = "true"
		annotations[LastAppliedAnnotationKey] = string(lastAppliedJSON)
		annotations[OwnerAnnotationKey] = expectedOwner
		workload.SetAnnotations(annotations)

		if err := r.Update(ctx, workload); err != nil {
			if errors.IsConflict(err) {
				logger.Info("Conflict updating workload, requeuing")
				ReconciliationTotal.WithLabelValues("requeue").Inc()
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "Failed to update workload")
			_ = r.updateStatus(ctx, &crdInstance, metav1.ConditionFalse, "UpdateFailed",
				fmt.Sprintf("Failed to update workload: %v", err))
			ReconciliationTotal.WithLabelValues("error").Inc()
			return ctrl.Result{}, err
		}

		logger.Info("Successfully synced hostAliases to workload",
			"kind", crdInstance.Spec.Target.Kind,
			"namespace", crdInstance.Spec.Target.Namespace,
			"name", crdInstance.Spec.Target.Name,
			"hostAliasCount", len(merged),
		)
		r.Recorder.Eventf(&crdInstance, corev1.EventTypeNormal, "HostAliasesSynced",
			"HostAliases synced to %s/%s/%s (%d entries)",
			crdInstance.Spec.Target.Kind, crdInstance.Spec.Target.Namespace, crdInstance.Spec.Target.Name, len(merged))
	}

	// 10. Update status
	_ = r.updateStatus(ctx, &crdInstance, metav1.ConditionTrue, "HostAliasesSynced",
		fmt.Sprintf("HostAliases synced to %s/%s/%s",
			crdInstance.Spec.Target.Kind, crdInstance.Spec.Target.Namespace, crdInstance.Spec.Target.Name))

	ReconciliationTotal.WithLabelValues("success").Inc()
	return ctrl.Result{}, nil
}

func (r *WorkloadHostAliasReconciler) handleDeletion(ctx context.Context, crdInstance *hostaliasv1alpha1.WorkloadHostAlias) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !containsFinalizer(crdInstance, FinalizerName) {
		return ctrl.Result{}, nil
	}

	// Fetch the target workload to clean up
	workload, err := r.fetchWorkload(ctx, crdInstance)
	if err == nil {
		// Clean up managed hostAliases
		podTemplateSpec, perr := getPodTemplateSpec(workload)
		if perr == nil {
			lastApplied := parseLastAppliedAnnotation(workload)
			cleaned := RemoveManagedHostAliases(podTemplateSpec.Spec.HostAliases, lastApplied)
			podTemplateSpec.Spec.HostAliases = cleaned

			annotations := workload.GetAnnotations()
			delete(annotations, ManagedAnnotationKey)
			delete(annotations, LastAppliedAnnotationKey)
			delete(annotations, OwnerAnnotationKey)
			workload.SetAnnotations(annotations)

			if err := r.Update(ctx, workload); err != nil {
				if errors.IsConflict(err) {
					logger.Info("Conflict updating workload during cleanup, requeuing")
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "Failed to clean up workload during deletion")
				return ctrl.Result{}, err
			}
			logger.Info("Cleaned up managed hostAliases from workload",
				"kind", crdInstance.Spec.Target.Kind,
				"namespace", crdInstance.Spec.Target.Namespace,
				"name", crdInstance.Spec.Target.Name,
			)
		}
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// Remove finalizer
	if err := r.removeFinalizer(ctx, crdInstance); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("WorkloadHostAlias deleted, finalizer removed")
	return ctrl.Result{}, nil
}

func (r *WorkloadHostAliasReconciler) fetchWorkload(ctx context.Context, crdInstance *hostaliasv1alpha1.WorkloadHostAlias) (client.Object, error) {
	key := types.NamespacedName{
		Namespace: crdInstance.Spec.Target.Namespace,
		Name:      crdInstance.Spec.Target.Name,
	}

	switch crdInstance.Spec.Target.Kind {
	case "Deployment":
		var deploy appsv1.Deployment
		if err := r.Get(ctx, key, &deploy); err != nil {
			return nil, err
		}
		return &deploy, nil
	case "DaemonSet":
		var ds appsv1.DaemonSet
		if err := r.Get(ctx, key, &ds); err != nil {
			return nil, err
		}
		return &ds, nil
	case "StatefulSet":
		var sts appsv1.StatefulSet
		if err := r.Get(ctx, key, &sts); err != nil {
			return nil, err
		}
		return &sts, nil
	default:
		return nil, fmt.Errorf("unsupported workload kind: %s", crdInstance.Spec.Target.Kind)
	}
}

func (r *WorkloadHostAliasReconciler) updateStatus(ctx context.Context, crdInstance *hostaliasv1alpha1.WorkloadHostAlias, status metav1.ConditionStatus, reason, message string) error {
	condition := metav1.Condition{
		Type:               ConditionTypeSynced,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&crdInstance.Status.Conditions, condition)
	crdInstance.Status.ObservedGeneration = crdInstance.Generation
	now := metav1.Now()
	crdInstance.Status.LastSyncTime = &now

	if err := r.Status().Update(ctx, crdInstance); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update WorkloadHostAlias status")
		return err
	}
	return nil
}

func (r *WorkloadHostAliasReconciler) addFinalizer(ctx context.Context, crdInstance *hostaliasv1alpha1.WorkloadHostAlias) error {
	containsFinalizer(crdInstance, FinalizerName) // ensure no-op if already present
	crdInstance.Finalizers = append(crdInstance.Finalizers, FinalizerName)
	return r.Update(ctx, crdInstance)
}

func (r *WorkloadHostAliasReconciler) removeFinalizer(ctx context.Context, crdInstance *hostaliasv1alpha1.WorkloadHostAlias) error {
	crdInstance.Finalizers = removeString(crdInstance.Finalizers, FinalizerName)
	return r.Update(ctx, crdInstance)
}

// findWorkloadHostAliasForWorkload maps workload changes back to the owning WorkloadHostAlias.
func (r *WorkloadHostAliasReconciler) findWorkloadHostAliasForWorkload(ctx context.Context, obj client.Object) []reconcile.Request {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	if annotations[ManagedAnnotationKey] != "true" {
		return nil
	}

	ownerRef := annotations[OwnerAnnotationKey]
	if ownerRef == "" {
		return nil
	}

	parts := strings.SplitN(ownerRef, "/", 2)
	if len(parts) != 2 {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: parts[0],
				Name:      parts[1],
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkloadHostAliasReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hostaliasv1alpha1.WorkloadHostAlias{}).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.findWorkloadHostAliasForWorkload),
		).
		Watches(
			&appsv1.DaemonSet{},
			handler.EnqueueRequestsFromMapFunc(r.findWorkloadHostAliasForWorkload),
		).
		Watches(
			&appsv1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.findWorkloadHostAliasForWorkload),
		).
		Complete(r)
}

// --- Helper functions ---

func getPodTemplateSpec(obj client.Object) (*corev1.PodTemplateSpec, error) {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return &o.Spec.Template, nil
	case *appsv1.DaemonSet:
		return &o.Spec.Template, nil
	case *appsv1.StatefulSet:
		return &o.Spec.Template, nil
	default:
		return nil, fmt.Errorf("unsupported workload type: %T", obj)
	}
}

func getAnnotation(obj client.Object, key string) string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations[key]
}

func parseLastAppliedAnnotation(obj client.Object) []corev1.HostAlias {
	ann := getAnnotation(obj, LastAppliedAnnotationKey)
	if ann == "" {
		return nil
	}
	var result []corev1.HostAlias
	if err := json.Unmarshal([]byte(ann), &result); err != nil {
		return nil
	}
	return result
}

func toCoreV1HostAliases(entries []hostaliasv1alpha1.HostAliasEntry) []corev1.HostAlias {
	result := make([]corev1.HostAlias, 0, len(entries))
	for _, e := range entries {
		result = append(result, corev1.HostAlias{
			IP:        e.IP,
			Hostnames: e.Hostnames,
		})
	}
	return result
}

func containsFinalizer(obj *hostaliasv1alpha1.WorkloadHostAlias, finalizer string) bool {
	for _, f := range obj.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

func hostAliasesSliceEqual(a, b []corev1.HostAlias) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !hostAliasesEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}
