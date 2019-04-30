package pkg

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
)

// lockRecorder implements the EventRecorder which is used to log events
// on the Kubernetes object being used as the election lock.
type lockRecorder struct{}

func (recorder *lockRecorder) Eventf(obj runtime.Object, eventType, reason, message string, args ...interface{}) {
	klog.Infof("lock event [%s] %s: %s", eventType, reason, message)
}
