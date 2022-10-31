package run

import (
	"context"
	pipelinecontroller "github.com/tektoncd/pipeline/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"knative.dev/pkg/injection/clients/dynamicclient"
	"time"

	pipelineclient "github.com/tektoncd/pipeline/pkg/client/injection/client"
	runinformer "github.com/tektoncd/pipeline/pkg/client/injection/informers/pipeline/v1alpha1/run"
	runreconciler "github.com/tektoncd/pipeline/pkg/client/injection/reconciler/pipeline/v1alpha1/run"

	//buildinformer "github.com/pivotal/kpack/pkg/client/informers/externalversions/build/v1alpha2"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
)

// NewController instantiates a new controller.Impl from knative.dev/pkg/controller
func NewController(namespace string) func(context.Context, configmap.Watcher) *controller.Impl {
	return func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {

		logger := logging.FromContext(ctx)
		pipelineClientSet := pipelineclient.Get(ctx)
		runInformer := runinformer.Get(ctx)
		dynamicClient := dynamicclient.Get(ctx)

		resource := schema.GroupVersionResource{Group: "kpack.io", Version: "v1alpha2", Resource: "builds"}
		factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, time.Minute, corev1.NamespaceAll, nil)
		dynamicInformer := factory.ForResource(resource).Informer()

		c := &Reconciler{
			pipelineClientSet: pipelineClientSet,
			runLister:         runInformer.Lister(),
			dynamicClient:     dynamicClient,
		}

		impl := runreconciler.NewImpl(ctx, c, func(impl *controller.Impl) controller.Options {
			return controller.Options{
				AgentName: "run-controller",
			}
		})

		logger.Info("Setting up event handlers")

		// Add event handler for Runs
		runInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: pipelinecontroller.FilterRunRef(resource.GroupVersion().String(), "Build"),
			//FilterFunc: all,
			Handler: controller.HandleAll(impl.Enqueue),
		})

		// Add event handler for TaskRuns controlled by Run
		dynamicInformer.AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: pipelinecontroller.FilterOwnerRunRef(runInformer.Lister(), resource.GroupVersion().String(), "Build"),
			Handler:    controller.HandleAll(impl.EnqueueControllerOf),
		})

		return impl
	}
}

func all(obj interface{}) bool {
	return true
}
