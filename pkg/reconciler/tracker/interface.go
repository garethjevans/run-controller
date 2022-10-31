package tracker

//go:generate go run -modfile ../../../hack/tools/go.mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//counterfeiter:generate . StampedTracker
type StampedTracker interface {
	Watch(log logr.Logger, stampedObj runtime.Object, handler handler.EventHandler, p ...predicate.Predicate) error
}
