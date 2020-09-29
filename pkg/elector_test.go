package pkg

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/klog"
)

func TestNewElectorNode(t *testing.T) {
	cases := []struct {
		description string
		config      *ElectorConfig
	}{
		{
			description: "initialize with nil config",
			config:      nil,
		},
		{
			description: "initialize with an empty config",
			config:      &ElectorConfig{},
		},
		{
			description: "initialize with a populated config",
			config: &ElectorConfig{
				ID:        "test-1",
				Name:      "test-name",
				Namespace: "test-ns",
			},
		},
	}

	for _, c := range cases {
		node := NewElectorNode(c.config)

		assert.Equal(t, node.config, c.config, c.description)
		assert.NotNil(t, node.cancel, c.description)
		assert.NotNil(t, node.ctx, c.description)
		assert.NotNil(t, node.quit, c.description)
	}
}

func TestElectorNode_Run_badConfig(t *testing.T) {
	node := NewElectorNode(&ElectorConfig{})
	assert.Nil(t, node.ctx.Err())

	err := node.Run()
	assert.Error(t, err)
	assert.Error(t, node.ctx.Err())
}

func TestElectorNode_IsLeader(t *testing.T) {
	cases := []struct {
		description string
		config      *ElectorConfig
		expected    bool
	}{
		{
			description: "check with nil config specified",
			config:      nil,
			expected:    false,
		},
		{
			description: "current leader does not match node ID",
			config: &ElectorConfig{
				ID: "test-1",
			},
			expected: false,
		},
		{
			description: "current leader matches node ID",
			config: &ElectorConfig{
				ID: "test-2",
			},
			expected: true,
		},
	}

	for _, c := range cases {
		node := ElectorNode{
			config:        c.config,
			currentLeader: "test-2",
		}

		actual := node.IsLeader()
		assert.Equal(t, c.expected, actual, c.description)
	}
}

func TestElectorNode_checkConfig_error(t *testing.T) {
	cases := []struct {
		description string
		config      *ElectorConfig
	}{
		{
			description: "config is nil",
			config:      nil,
		},
		{
			description: "config missing required name",
			config:      &ElectorConfig{},
		},
	}

	for _, c := range cases {
		node := ElectorNode{
			config: c.config,
		}

		err := node.checkConfig()
		assert.Error(t, err, c.description)
	}
}

func TestElectorNode_checkConfig_ok(t *testing.T) {
	cases := []struct {
		description string
		config      *ElectorConfig
	}{
		{
			description: "config missing optional ID field",
			config: &ElectorConfig{
				Name:      "test-name",
				Namespace: "test-ns",
			},
		},
		{
			description: "config has all fields",
			config: &ElectorConfig{
				Name:       "test-name",
				Namespace:  "test-ns",
				ID:         "test-id",
				Address:    "localhost:5001",
				KubeConfig: "./config",
				LockType:   "configmaps",
				TTL:        1 * time.Second,
			},
		},
	}

	for _, c := range cases {
		node := ElectorNode{
			config: c.config,
		}

		err := node.checkConfig()
		assert.NoError(t, err, c.description)
	}
}

func TestElectorNode_listenForSignal(t *testing.T) {
	cases := []struct {
		description string
		sig         os.Signal
	}{
		{
			description: "listen for an interrupt",
			sig:         os.Interrupt,
		},
		{
			description: "listen for an interrupt",
			sig:         os.Kill,
		},
		{
			description: "listen for an interrupt",
			sig:         syscall.SIGTERM,
		},
	}

	for _, c := range cases {
		node := NewElectorNode(&ElectorConfig{})

		go func() {
			node.listenForSignal()
		}()

		node.quit <- c.sig

		select {
		case <-node.ctx.Done():
			err := node.ctx.Err()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "context canceled")
		case <-time.After(3 * time.Second):
			assert.Failf(t, "failed to close context on signal", c.description)
		}
	}
}

// FIXME: tests in CI run in a cluster, this test needs to be reworked since
//   it originally assumed tests do not run on a cluster.
//func TestElectorNode_buildClientConfig_error(t *testing.T) {
//	cases := []struct {
//		description string
//		config      *ElectorConfig
//	}{
//		{
//			description: "config is nil",
//			config:      nil,
//		},
//		{
//			description: "config doesn't specify config file, not running on cluster",
//			config:      &ElectorConfig{},
//		},
//		{
//			description: "kubeconfig specified but not found",
//			config: &ElectorConfig{
//				KubeConfig: "./test-kubeconfig-file",
//			},
//		},
//	}
//
//	for _, c := range cases {
//		node := ElectorNode{
//			config: c.config,
//		}
//
//		cfg, err := node.buildClientConfig()
//		assert.Nil(t, cfg, c.description)
//		assert.Error(t, err, c.description)
//	}
//}

func TestElectorNode_buildClientConfig_ok(t *testing.T) {
	node := ElectorNode{
		config: &ElectorConfig{
			KubeConfig: "./testdata/config",
		},
	}

	cfg, err := node.buildClientConfig()
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestElectorNode_serveHTTP_noAddress(t *testing.T) {
	node := ElectorNode{
		config: &ElectorConfig{
			Address: "",
		},
	}

	var buf bytes.Buffer
	klog.SetOutput(&buf)

	node.serveHTTP()
	assert.False(t, node.servingHTTP)
	assert.Contains(t, buf.String(), "no address given")
}

func TestElectorNode_httpHandler_noLeader(t *testing.T) {
	node := NewElectorNode(&ElectorConfig{
		ID: "test-node-1",
	})

	req := httptest.NewRequest("GET", "localhost:3333/", nil)
	w := httptest.NewRecorder()

	node.httpLeaderInfo(w, req)

	resp := w.Result()

	data := map[string]interface{}{}
	d := json.NewDecoder(resp.Body)
	err := d.Decode(&data)
	assert.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	assert.NotNil(t, data["timestamp"])
	assert.Equal(t, "test-node-1", data["node"])
	assert.Equal(t, "", data["leader"])
	assert.Equal(t, false, data["is_leader"])
}

func TestElectorNode_httpHandler_otherNodeIsLeader(t *testing.T) {
	node := NewElectorNode(&ElectorConfig{
		ID: "test-node-1",
	})
	node.currentLeader = "test-node-2"

	req := httptest.NewRequest("GET", "localhost:3333/", nil)
	w := httptest.NewRecorder()

	node.httpLeaderInfo(w, req)

	resp := w.Result()

	data := map[string]interface{}{}
	d := json.NewDecoder(resp.Body)
	err := d.Decode(&data)
	assert.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	assert.NotNil(t, data["timestamp"])
	assert.Equal(t, "test-node-1", data["node"])
	assert.Equal(t, "test-node-2", data["leader"])
	assert.Equal(t, false, data["is_leader"])
}

func TestElectorNode_httpHandler_isLeader(t *testing.T) {
	node := NewElectorNode(&ElectorConfig{
		ID: "test-node-1",
	})
	node.currentLeader = "test-node-1"

	req := httptest.NewRequest("GET", "localhost:3333/", nil)
	w := httptest.NewRecorder()

	node.httpLeaderInfo(w, req)

	resp := w.Result()

	data := map[string]interface{}{}
	d := json.NewDecoder(resp.Body)
	err := d.Decode(&data)
	assert.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	assert.NotNil(t, data["timestamp"])
	assert.Equal(t, "test-node-1", data["node"])
	assert.Equal(t, "test-node-1", data["leader"])
	assert.Equal(t, true, data["is_leader"])
}
