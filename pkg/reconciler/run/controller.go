package run

import (
	"context"
	"knative.dev/pkg/injection/clients/dynamicclient"

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
		//buildInformer := buildinformer.Get(ctx)

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
			//FilterFunc: pipelinecontroller.FilterRunRef("kpack.io/v1alpha2", "Build"),
			FilterFunc: all,
			Handler:    controller.HandleAll(impl.Enqueue),
		})

		// Add event handler for TaskRuns controlled by Run
		//dynamicInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		//	FilterFunc: pipelinecontroller.FilterOwnerRunRef(runInformer.Lister(), "kpack.io/v1alpha2", "Build"),
		//	Handler:    controller.HandleAll(impl.EnqueueControllerOf),
		//})

		return impl
	}
}

func all(obj interface{}) bool {
	return true
}
