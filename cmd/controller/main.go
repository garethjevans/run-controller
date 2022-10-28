package main

import (
	"flag"
	"github.com/garethjevans/run-controller/pkg/reconciler/run"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"
)

const (
	// ControllerLogKey is the name of the logger for the controller cmd
	ControllerLogKey = "run-controller"
)

var (
	namespace = flag.String("namespace", corev1.NamespaceAll, "Namespace to restrict informer to. Optional, defaults to all namespaces.")
)

func main() {
	flag.Parse()
	sharedmain.MainWithContext(injection.WithNamespaceScope(signals.NewContext(), *namespace), ControllerLogKey,
		run.NewController(*namespace),
	)
}
