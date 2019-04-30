package pkg

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/klog"
)

func TestLockRecorder_Eventf(t *testing.T) {
	rec := lockRecorder{}

	var buf bytes.Buffer
	klog.SetOutput(&buf)

	rec.Eventf(nil, "TestEvent", "test reason", "test message")

	assert.Contains(
		t,
		buf.String(),
		"lock event [TestEvent] test reason: test message",
	)
}
