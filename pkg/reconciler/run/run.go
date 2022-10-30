package run

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tektoncd/pipeline/pkg/names"
	"github.com/tektoncd/pipeline/pkg/substitution"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	clientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	runreconciler "github.com/tektoncd/pipeline/pkg/client/injection/reconciler/pipeline/v1alpha1/run"
	listersalpha "github.com/tektoncd/pipeline/pkg/client/listers/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/reconciler/events"
	"gomodules.xyz/jsonpatch/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
)

const (
	// runLabelKey is the label identifier for a Run.  This label is added to the Run's embedded object.
	runLabelKey = "/run"
)

// Reconciler implements controller.Reconciler for Configuration resources.
type Reconciler struct {
	pipelineClientSet clientset.Interface
	runLister         listersalpha.RunLister
	dynamicClient     dynamic.Interface
}

var (
	// Check that our Reconciler implements runreconciler.Interface
	_                runreconciler.Interface = (*Reconciler)(nil)
	cancelPatchBytes []byte
)

func init() {
	var err error
	patches := []jsonpatch.JsonPatchOperation{{
		Operation: "add",
		Path:      "/spec/status",
		Value:     v1beta1.TaskRunSpecStatusCancelled,
	}}
	cancelPatchBytes, err = json.Marshal(patches)
	if err != nil {
		log.Fatalf("failed to marshal patch bytes in order to cancel: %v", err)
	}
}

// ReconcileKind compares the actual state with the desired, and attempts to converge the two.
// It then updates the Status block of the Run resource with the current status of the resource.
func (c *Reconciler) ReconcileKind(ctx context.Context, run *v1alpha1.Run) pkgreconciler.Event {
	var merr error
	logger := logging.FromContext(ctx)
	logger.Infof("Reconciling Run %s/%s at %v", run.Namespace, run.Name, time.Now())

	// Check that the Run references an embedded spec.
	if run.Spec.Spec == nil {
		logger.Warnf("Received control for a Run %s/%s that does not contain an embedded spec", run.Namespace, run.Name)
		return nil
	}

	// If the Run has not started, initialize the Condition and set the start time.
	if !run.HasStarted() {
		logger.Infof("Starting new Run %s/%s", run.Namespace, run.Name)
		run.Status.InitializeConditions()
		// In case node time was not synchronized, when controller has been scheduled to other nodes.
		if run.Status.StartTime.Sub(run.CreationTimestamp.Time) < 0 {
			logger.Warnf("Run %s createTimestamp %s is after the Run started %s", run.Name, run.CreationTimestamp, run.Status.StartTime)
			run.Status.StartTime = &run.CreationTimestamp
		}
		// Emit events. During the first reconcile the status of the Run may change twice
		// from not Started to Started and then to Running, so we need to sent the event here
		// and at the end of 'Reconcile' again.
		// We also want to send the "Started" event as soon as possible for anyone who may be waiting
		// on the event to perform user facing initialisations, such has reset a CI check status
		afterCondition := run.Status.GetCondition(apis.ConditionSucceeded)
		events.Emit(ctx, nil, afterCondition, run)
	}

	if run.IsDone() {
		logger.Infof("Run %s/%s is done", run.Namespace, run.Name)
		return nil
	}

	// Store the condition before reconcile
	beforeCondition := run.Status.GetCondition(apis.ConditionSucceeded)

	//status := &taskloopv1alpha1.TaskLoopRunStatus{}
	//if err := run.Status.DecodeExtraFields(status); err != nil {
	//	run.Status.MarkRunFailed("InternalError",
	//		"Internal error calling DecodeExtraFields: %v", err)
	//	logger.Errorf("DecodeExtraFields error: %v", err.Error())
	//}

	// Reconcile the Run
	if err := c.reconcile(ctx, run); err != nil {
		logger.Errorf("Reconcile error: %v", err.Error())
		merr = multierror.Append(merr, err)
	}

	//if err := c.updateLabelsAndAnnotations(ctx, run); err != nil {
	//	logger.Warn("Failed to update Run labels/annotations", zap.Error(err))
	//	merr = multierror.Append(merr, err)
	//}

	//if err := run.Status.EncodeExtraFields(status); err != nil {
	//	run.Status.MarkRunFailed("InternalError",
	//		"Internal error calling EncodeExtraFields: %v", err)
	//	logger.Errorf("EncodeExtraFields error: %v", err.Error())
	//}

	afterCondition := run.Status.GetCondition(apis.ConditionSucceeded)
	events.Emit(ctx, beforeCondition, afterCondition, run)

	// Only transient errors that should retry the reconcile are returned.
	return merr
}

func (c *Reconciler) reconcile(ctx context.Context, run *v1alpha1.Run) error {
	logger := logging.FromContext(ctx)

	// Get the TaskLoop referenced by the Run
	u, err := c.GetUnstructuredFromRun(run)
	if err != nil {
		return fmt.Errorf("unable to get unstructured: %+v", err)
	}

	logger.Infof("Ready to create %+v", u)

	gvr := ParseGroupVersionResource(run.Spec.Spec.APIVersion, run.Spec.Spec.Kind)
	logger.Infof("Creating new unstructured %+v", gvr)

	created, err := c.dynamicClient.Resource(gvr).Namespace(u.GetNamespace()).Create(ctx, u, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("unable to create unstructured: %+v", err)
	}

	logger.Infof("Createed %+v", created)

	// Check if the run was cancelled.  Since updateTaskRunStatus() handled cancelling any running TaskRuns
	// the only thing to do here is to determine if all running TaskRuns have finished.
	if run.IsCancelled() {
		// If no TaskRuns are running, mark the Run as failed.
		run.Status.MarkRunFailed(v1alpha1.RunReasonCancelled,
			"Run %s/%s was cancelled", run.Namespace, run.Name)
		return nil
	}

	return nil
}

func ParseGroupVersionResource(apiVersion string, kind string) schema.GroupVersionResource {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		panic(err)
	}

	return schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: strings.ToLower(kind) + "s",
	}
}

func (c *Reconciler) GetUnstructuredFromRun(run *v1alpha1.Run) (*unstructured.Unstructured, error) {
	// Create name for Unstructured from Run name, kind, all lowercase
	unstructuredName := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix(
		strings.ToLower(
			fmt.Sprintf("%s-%s", run.Name, run.Spec.Spec.Kind)))

	replacements := make(map[string]string)
	for _, param := range run.Spec.Params {
		replacements["params."+param.Name] = param.Value.StringVal
	}

	raw := string(run.Spec.Spec.Spec.Raw)
	updated := substitution.ApplyReplacements(raw, replacements)

	m := map[string]interface{}{}
	err := json.Unmarshal([]byte(updated), &m)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal: %+v", err)
	}

	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": run.Spec.Spec.APIVersion,
			"kind":       run.Spec.Spec.Kind,
			"metadata": map[string]interface{}{
				"name":        unstructuredName,
				"namespace":   run.Namespace,
				"annotations": getRunAnnotations(run),
				"labels":      getRunLabels(run, true),
			},
			"spec": m,
		},
	}

	u.SetOwnerReferences(getOwnerReferences(run))

	return &u, nil
}

func getOwnerReferences(run *v1alpha1.Run) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion: "tekton.dev/v1alpha1",
		Kind:       "Run",
		Name:       run.Name,
		UID:        run.UID,
	}}
}

//func (c *Reconciler) retryTaskRun(ctx context.Context, tr *v1beta1.TaskRun) (*v1beta1.TaskRun, error) {
//	newStatus := *tr.Status.DeepCopy()
//	newStatus.RetriesStatus = nil
//	tr.Status.RetriesStatus = append(tr.Status.RetriesStatus, newStatus)
//	tr.Status.StartTime = nil
//	tr.Status.CompletionTime = nil
//	tr.Status.PodName = ""
//	tr.Status.SetCondition(&apis.Condition{
//		Type:   apis.ConditionSucceeded,
//		Status: corev1.ConditionUnknown,
//	})
//	return c.pipelineClientSet.TektonV1beta1().TaskRuns(tr.Namespace).UpdateStatus(ctx, tr, metav1.UpdateOptions{})
//}

//func getParameters(run *v1alpha1.Run, tls *taskloopv1alpha1.TaskLoopSpec, iteration int) []v1beta1.Param {
//	out := make([]v1beta1.Param, len(run.Spec.Params))
//	for i, p := range run.Spec.Params {
//		if p.Name == tls.IterateParam {
//			if p.Value.Type == v1beta1.ParamTypeString {
//				// If we got a string param, split it into an array, one item per line
//				p.Value.ArrayVal = strings.Split(strings.TrimSuffix(p.Value.StringVal, "\n"), "\n")
//			}
//			out[i] = v1beta1.Param{
//				Name:  p.Name,
//				Value: v1beta1.ArrayOrString{Type: v1beta1.ParamTypeString, StringVal: p.Value.ArrayVal[iteration-1]},
//			}
//		} else {
//			out[i] = run.Spec.Params[i]
//		}
//	}
//	return out
//}

func getRunAnnotations(run *v1alpha1.Run) map[string]string {
	// Propagate annotations from Run to TaskRun.
	annotations := make(map[string]string, len(run.ObjectMeta.Annotations)+1)
	for key, val := range run.ObjectMeta.Annotations {
		if key != "kubectl.kubernetes.io/last-applied-configuration" {
			annotations[key] = val
		}
	}
	return annotations
}

func getRunLabels(run *v1alpha1.Run, includeRunLabels bool) map[string]string {
	// Propagate labels from Run to TaskRun.
	labels := make(map[string]string, len(run.ObjectMeta.Labels)+1)
	if includeRunLabels {
		for key, val := range run.ObjectMeta.Labels {
			labels[key] = val
		}
	}
	// Note: The Run label uses the normal Tekton group name.
	labels[pipeline.GroupName+runLabelKey] = run.Name
	return labels
}

//func propagateTaskLoopLabelsAndAnnotations(run *v1alpha1.Run, meta *metav1.ObjectMeta) {
//	// Propagate labels from TaskLoop to Run.
//	if run.ObjectMeta.Labels == nil {
//		run.ObjectMeta.Labels = make(map[string]string, len(meta.Labels)+1)
//	}
//	for key, value := range meta.Labels {
//		run.ObjectMeta.Labels[key] = value
//	}
//	run.ObjectMeta.Labels["kpack.io/v1alpha2"+taskLoopLabelKey] = meta.Name
//
//	// Propagate annotations from TaskLoop to Run.
//	if run.ObjectMeta.Annotations == nil {
//		run.ObjectMeta.Annotations = make(map[string]string, len(meta.Annotations))
//	}
//
//	for key, value := range meta.Annotations {
//		run.ObjectMeta.Annotations[key] = value
//	}
//}

//func storeTaskLoopSpec(status *taskloopv1alpha1.TaskLoopRunStatus, tls *taskloopv1alpha1.TaskLoopSpec) {
//	// Only store the TaskLoopSpec once, if it has never been set before.
//	if status.TaskLoopSpec == nil {
//		status.TaskLoopSpec = tls
//	}
//}
