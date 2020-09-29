// k8s-elector
// Copyright (c) 2019 Vapor IO
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package pkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/transport"
	"k8s.io/klog"
)

const (
	// EnvPodName is the environment variable which is checked for the Pod name.
	EnvPodName = "ELECTOR_POD_NAME"

	// ResourcePathPodLabel is the path to the Pod label metadata which is used
	// for updating Pod label values for election status.
	//
	// Note that the label key is the last element in the path. Since the key
	// contains a "/", it is escaped as "~1".
	ResourcePathPodLabel = "/metadata/labels/k8s-elector~1status"

	// StatusStandby is the standby status annotation value.
	StatusStandby = "standby"

	// StatusLeader is the leader status annotation value.
	StatusLeader = "leader"
)

// ElectorNode is a participant node in an election.
type ElectorNode struct {
	cancel        context.CancelFunc
	config        *ElectorConfig
	ctx           context.Context
	currentLeader string
	quit          chan os.Signal

	servingHTTP bool
}

// NewElectorNode creates a new instance of an elector node which will
// participate in an election.
func NewElectorNode(config *ElectorConfig) *ElectorNode {

	ctx, cancel := context.WithCancel(context.Background())

	return &ElectorNode{
		cancel: cancel,
		config: config,
		ctx:    ctx,
		quit:   make(chan os.Signal, 1),
	}
}

// Run the elector node.
//
// This is the entry point that kicks off all of the elector node setup
// and run logic.
func (node *ElectorNode) Run() error {
	// If anything goes wrong, cancel the elector nodes context. This ensures
	// that it will clean up properly and release the lock in a timely manner.
	defer node.cancel()

	// Verify the elector node configuration is valid.
	if err := node.checkConfig(); err != nil {
		return err
	}

	node.config.Log()

	// Run the signal exiter and HTTP server in separate goroutines. The
	// election logic will run in the foreground and block until it is
	// cancelled.
	go node.listenForSignal()
	go node.serveHTTP()

	if err := node.runUntilError(); err != nil {
		return err
	}

	klog.Info("done")
	return nil
}

// IsLeader checks whether the elector node is currently the leader.
func (node *ElectorNode) IsLeader() bool {
	if node.config == nil {
		return false
	}
	return node.config.ID == node.currentLeader
}

// buildConfig builds the config for the Kubernetes client used by the elector node.
func (node *ElectorNode) buildClientConfig() (*rest.Config, error) {
	if node.config == nil {
		return nil, errors.New("no config specified for the elector")
	}

	if node.config.KubeConfig != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", node.config.KubeConfig)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}

	// If no kubeconfig file was specified, default to using in-cluster config.
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return cfg, err
}

// runUntilError runs the elector node and will keep re-running it until an error
// is returned or the context is cancelled.
func (node *ElectorNode) runUntilError() error {
	for {
		errChan := make(chan error, 1)
		go func() {
			errChan <- node.run()
		}()

		select {
		case <-node.ctx.Done():
			klog.Info("terminating: context cancelled")
			return node.ctx.Err()
		case err := <-errChan:
			if err != nil {
				klog.Infof("terminating: run error  (%v)", err)
				return err
			}
		}
		// Sleep a short period of time so the topology has a little
		// bit of time to settle.
		time.Sleep(1 * time.Second)
		klog.Info("re-running election")
	}
}

// run the election.
func (node *ElectorNode) run() error {
	config, err := node.buildClientConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	// Once the context is cancelled, prevent the client from making more requests.
	config.Wrap(transport.ContextCanceller(node.ctx, errors.New("the node is shutting down")))

	// Create the lock object which will be used to determine leadership in the election.
	lock, err := resourcelock.New(
		node.config.LockType,
		node.config.Namespace,
		node.config.Name,
		client.CoreV1(),
		client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      node.config.ID,
			EventRecorder: &lockRecorder{},
		},
	)
	if err != nil {
		return err
	}

	// Start the election.
	leaderelection.RunOrDie(node.ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		Name:            fmt.Sprintf("%s/%s-%s", node.config.Namespace, node.config.Name, node.config.ID),
		ReleaseOnCancel: true,
		LeaseDuration:   node.config.TTL,
		RenewDeadline:   node.config.TTL / 3,
		RetryPeriod:     node.config.TTL / 6,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(i context.Context) {
				klog.Infof("[%s] started leading", node.config.ID)

				// Add/update Pod label marking this instance as the leader.
				if err := updatePodLabel(node.config, client, StatusLeader); err != nil {
					klog.Errorf("failed to set leader annotation: %v", err)
				}
			},
			OnStoppedLeading: func() {
				klog.Infof("[%s] stepping down as leader", node.config.ID)

				// Add/update Pod label marking this instance as not the leader.
				if err := updatePodLabel(node.config, client, StatusStandby); err != nil {
					klog.Errorf("failed to set standby annotation: %v", err)
				}
			},
			OnNewLeader: func(identity string) {
				node.currentLeader = identity

				if node.IsLeader() {
					// This node was elected. Nothing to do here since this node will
					// also call the OnStartedLeading callback.
					return
				}
				klog.Infof("new leader elected: %s", identity)

				// Add/update Pod label marking this instance as a standby node.
				if err := updatePodLabel(node.config, client, StatusStandby); err != nil {
					klog.Errorf("failed to set standby annotation: %v", err)
				}
			},
		},
	})

	return nil
}

// patchLabel specifies a patch operation for a label string.
type patchLabel struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// updatePodLabel updates the label for the k8s-elector Pod to designate its
// leadership status.
//
// If the elector instance becomes the leader, a value of "leader" is set. Otherwise, a
// value of "standby" is set.
func updatePodLabel(cfg *ElectorConfig, clientset *kubernetes.Clientset, value string) error {

	// First, get the Pod. We want to first check whether or not the Pod has the
	// label key or not. If not, add it; if so, update it.
	pod, err := clientset.CoreV1().Pods(cfg.Namespace).Get(cfg.PodName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Determine the operation by whether or not the label key exists.
	var operation string
	if _, ok := pod.Labels[ResourcePathPodLabel]; ok {
		operation = "replace"
	} else {
		operation = "add"
	}

	// Patch the Pod label.
	payload := []patchLabel{{
		Op:    operation,
		Path:  ResourcePathPodLabel,
		Value: value,
	}}
	payloadBytes, _ := json.Marshal(payload)
	_, err = clientset.CoreV1().Pods(cfg.Namespace).Patch(
		cfg.PodName,
		types.JSONPatchType,
		payloadBytes,
	)
	return err
}

// checkConfig checks that the elector node's configuration is valid.
//
// If required configuration fields are missing, this will return an error.
// This function also sets default values for fields which can can have a
// reasonable default to fall back to.
func (node *ElectorNode) checkConfig() error {
	if node.config == nil {
		return errors.New("no config specified for elector")
	}

	// The elector node needs the name of the election to be specified,
	// otherwise it will not know which election to create/join.
	if node.config.Name == "" {
		return errors.New(
			"missing required value: election name was not specified (see '--help' for usage)",
		)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	// Get the name of the Pod. This is used to assign the leadership status
	// annotation. If the Pod name is not set via Env, it will default to the
	// hostname.
	if val := os.Getenv(EnvPodName); val != "" {
		node.config.PodName = val
	} else {
		klog.Infof("pod name not specified, using hostname: %s", hostname)
		node.config.PodName = hostname
	}

	// If the elector node was not provided with an ID, use the machine's
	// hostname as the default ID value.
	if node.config.ID == "" {
		klog.Infof("no ID specified for elector node, using hostname: %s", hostname)
		node.config.ID = hostname
	}

	return nil
}

// listenForSignal sets up the elector node's termination channel to listen for
// system signals which designate that the node should terminate.
//
// The signals that are listened for are: SIGINT, SIGKILL, SIGTERM. Any of these
// will cause the node to terminate gracefully.
func (node *ElectorNode) listenForSignal() {
	signal.Notify(node.quit, os.Interrupt, os.Kill, syscall.SIGTERM)

	klog.Info("listening for shutdown signals...")

	sig := <-node.quit
	klog.Infof("shutting down: received termination signal %v", sig)
	node.cancel()
	close(node.quit)
}

// serveHTTP starts the HTTP server which exposes the leader information.
//
// If the elector is not configured with an address (via the -http flag), the
// HTTP server will not be started.
func (node *ElectorNode) serveHTTP() {
	if node.config.Address == "" {
		klog.Info("http server will not be started: no address given")
		return
	}

	klog.Infof("starting HTTP server on %v", node.config.Address)
	http.HandleFunc("/", node.httpLeaderInfo)
	node.servingHTTP = true
	err := http.ListenAndServe(node.config.Address, nil)
	if err != nil {
		klog.Fatalf("failed to start the HTTP server: %v", err)
	}
}

// httpLeaderInfo is the handler for the endpoint which provides leader info.
func (node *ElectorNode) httpLeaderInfo(res http.ResponseWriter, req *http.Request) {
	klog.Infof("received incoming http request: %s %s (%s)", req.Method, req.URL, req.RemoteAddr)
	data, err := json.Marshal(map[string]interface{}{
		"node":      node.config.ID,
		"leader":    node.currentLeader,
		"is_leader": node.IsLeader(),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		res.WriteHeader(http.StatusInternalServerError)
		if _, e := res.Write([]byte(err.Error())); e != nil {
			klog.Errorf("failed writing http error response (%v): %v", err, e)
		}
		return
	}

	res.Header()["Content-Type"] = []string{"application/json"}
	res.WriteHeader(http.StatusOK)
	_, err = res.Write(data)
	if err != nil {
		klog.Errorf("failed to write leader info http response: %v", err)
	}
}
