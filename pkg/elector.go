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

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/transport"
	"k8s.io/klog"
)

type electorNode struct {
	cancel        context.CancelFunc
	config        *ElectorConfig
	ctx           context.Context
	currentLeader string
	quit          chan os.Signal
}

// NewElectorNode creates a new instance of an elector node which will
// participate in an election.
func NewElectorNode(config *ElectorConfig) (*electorNode, error) {

	ctx, cancel := context.WithCancel(context.Background())

	return &electorNode{
		cancel: cancel,
		config: config,
		ctx:    ctx,
		quit:   make(chan os.Signal, 1),
	}, nil
}

// Run the elector node.
//
// This is the entry point that kicks off all of the elector node setup
// and run logic.
func (node *electorNode) Run() error {
	// If anything goes wrong, cancel the elector nodes context. This ensures
	// that it will clean up properly and release the lock in a timely manner.
	defer node.cancel()

	// Verify the elector node configuration is valid.
	if err := node.checkConfig(); err != nil {
		return err
	}

	// Run the signal exiter and HTTP server in separate goroutines. The
	// election logic will run in the foreground and block until it is
	// cancelled.
	go node.listenForSignal()
	go node.serveHTTP()

	if err := node.run(); err != nil {
		return err
	}

	klog.Info("done")
	return nil
}

// IsLeader checks whether the elector node is currently the leader.
func (node *electorNode) IsLeader() bool {
	if node.config == nil {
		return false
	}
	return node.config.ID == node.currentLeader
}

// buildConfig builds the config for the Kubernetes client used by the elector node.
func (node *electorNode) buildClientConfig() (*rest.Config, error) {
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

// run the election.
func (node *electorNode) run() error {
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
				klog.Info("started leading")
			},
			OnStoppedLeading: func() {
				klog.Infof("stepping down as leader")
			},
			OnNewLeader: func(identity string) {
				node.currentLeader = identity

				if node.IsLeader() {
					// This node was elected. Nothing to do here since this node will
					// also call the OnStartedLeading callback.
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	})

	return nil
}

// checkConfig checks that the elector node's configuration is valid.
//
// If required configuration fields are missing, this will return an error.
// This function also sets default values for fields which can can have a
// reasonable default to fall back to.
func (node *electorNode) checkConfig() error {
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

	// If the elector node was not provided with an ID, use the machine's
	// hostname as the default ID value.
	if node.config.ID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}
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
func (node *electorNode) listenForSignal() {
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
func (node *electorNode) serveHTTP() {
	if node.config.Address == "" {
		klog.Info("http server will not be started: no address given")
		return
	}

	klog.Infof("starting HTTP server on %v", node.config.Address)
	http.HandleFunc("/", node.httpLeaderInfo)
	err := http.ListenAndServe(node.config.Address, nil)
	if err != nil {
		klog.Fatalf("failed to start the HTTP server: %v", err)
	}
}

// httpLeaderInfo is the handler for the endpoint which provides leader info.
func (node *electorNode) httpLeaderInfo(res http.ResponseWriter, req *http.Request) {
	data, err := json.Marshal(map[string]interface{}{
		"node":      node.config.ID,
		"leader":    node.currentLeader,
		"is_leader": node.IsLeader(),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		res.WriteHeader(http.StatusInternalServerError)
		if _, e := res.Write([]byte(err.Error())); e != nil {
			klog.Error("failed writing http error response (%v): %v", err, e)
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
