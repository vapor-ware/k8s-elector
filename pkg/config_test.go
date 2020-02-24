package pkg

import (
	"bytes"
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/klog"
)

func init() {
	flagset := flag.FlagSet{}
	klog.InitFlags(&flagset)
	flagset.Set("logtostderr", "false")
	flagset.Set("alsologtostderr", "false")
}

func TestElectorConfig_Log_nil(t *testing.T) {
	var conf *ElectorConfig

	var buf bytes.Buffer
	klog.SetOutput(&buf)

	conf.Log()

	assert.Contains(t, buf.String(), "elector config: nil")
}

func TestElectorConfig_Log_empty(t *testing.T) {
	conf := ElectorConfig{}

	var buf bytes.Buffer
	klog.SetOutput(&buf)

	conf.Log()

	assert.Contains(t, buf.String(), "ID:         ")
	assert.Contains(t, buf.String(), "Name:       ")
	assert.Contains(t, buf.String(), "Namespace:  ")
	assert.Contains(t, buf.String(), "Address:    ")
	assert.Contains(t, buf.String(), "LockType:   ")
	assert.Contains(t, buf.String(), "KubeConfig: ")
	assert.Contains(t, buf.String(), "TTL:        0s")
}

func TestElectorConfig_Log(t *testing.T) {
	conf := ElectorConfig{
		ID:   "123",
		Name: "election",
		TTL:  1 * time.Second,
	}

	var buf bytes.Buffer
	klog.SetOutput(&buf)

	conf.Log()

	assert.Contains(t, buf.String(), "ID:         123")
	assert.Contains(t, buf.String(), "Name:       election")
	assert.Contains(t, buf.String(), "Namespace:  ")
	assert.Contains(t, buf.String(), "Address:    ")
	assert.Contains(t, buf.String(), "LockType:   ")
	assert.Contains(t, buf.String(), "KubeConfig: ")
	assert.Contains(t, buf.String(), "TTL:        1s")
}
