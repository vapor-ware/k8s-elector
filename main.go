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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/transport"
)

var (
	// Version information for the program. These are set via build-time
	// variables on the build server.
	Version   string
	Commit    string
	BuildDate string
	Tag       string
	GoVersion string

	// Variables for command line flag values. The command line flags are
	// bound on program init.
	id         string
	kubeconfig string
	name       string
	namespace  string
	ttl        time.Duration
)

func init() {
	flag.StringVar(&id, "id", "", "The ID of the election participant. If not set, the hostname, as reported by the kernel, is used.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "The kubeconfig file to use. If not set, in-cluster config will be used.")
	flag.StringVar(&name, "election", "", "The name of the election. This is required.")
	flag.StringVar(&namespace, "namespace", "default", "The Kubernetes namespace to run the election in. If not set, elections will run in the default namespace.")
	flag.DurationVar(&ttl, "ttl", 10*time.Second, "The TTL for the election.")
}

// buildConfig builds the config for the Kubernetes client used by the election participant.
func buildConfig() (*rest.Config, error) {
	if kubeconfig != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
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

// parseFlags parses command line flags and performs simple validation, ensuring
// that required flags are specified and option defaults are set.
//
// If a require flag is not set, this will terminate the program.
func parseFlags() {
	flag.Parse()

	// If the participant ID was not provided via command-line, attempt to use the
	// machine's hostname as the ID.
	if id == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Fatal("failed to get hostname for default '-id' value")
		}
		id = hostname
	}

	if name == "" {
		log.Fatal("required flag '-election' was not specified (use '--help' for usage)")
	}
}

func main() {
	// This program performs leader election by using the Kubernetes API to write
	// to a lock object (LeaseLock). The election participant which holds the lock
	// is designated as the leader. Conflicting writes are detected and handled
	// independently by each participant.

	log.WithFields(log.Fields{
		"version":    Version,
		"commit":     Commit,
		"tag":        Tag,
		"go_version": GoVersion,
		"build_date": BuildDate,
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
	}).Info("running k8s-elector")

	// Parse command line flags to load the program configuration. If required
	// flags are missing, this will terminate the program.
	parseFlags()

	log.WithFields(log.Fields{
		"id":         id,
		"kubeconfig": kubeconfig,
		"name":       name,
		"namespace":  namespace,
		"ttl":        ttl,
	}).Info("parsed run configuration")

	config, err := buildConfig()
	if err != nil {
		log.Fatal(err)
	}
	client := kubernetes.NewForConfigOrDie(config)

	lock := &resourcelock.LeaseLock{
		LeaseMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	// Create a context that will be used to cancel this participants participation
	// in the election. This will either stop it from trying to become the leader or,
	// if it is the leader, to step down as leader.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Once the context is cancelled, prevent the client from making any more requests.
	config.Wrap(transport.ContextCanceller(ctx, fmt.Errorf("the leader (%s) is shutting down", id)))

	// Listen for SIGINT, SIGKILL, or SIGTERM. Any of these tell the participant to cancel
	// its context and end participation in the election.
	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func() {
		sig := <-term
		log.WithField("signal", sig).Info("received termination signal, shutting down")
		cancel()
	}()

	// Start the leader election.
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		Name:            fmt.Sprintf("%s/%s-%s", namespace, name, id),
		ReleaseOnCancel: true,
		LeaseDuration:   60 * time.Second, // fixme: ttl?
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(i context.Context) {
				log.WithField("leader", id).Info("this participant is now the leader")
			},
			OnStoppedLeading: func() {
				log.WithField("leader", id).Info("stepping down as leader")
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					// This participant was elected.
					return
				}
				log.WithField("leader", identity).Info("new leader elected")
			},
		},
	})

	// When we make it here, we have exited the leader election loop. The context should have
	// been canceled, so this participant's client should no longer issue requests and instead
	// report an error.
	_, err = client.CoordinationV1().Leases(namespace).Get(name, v1.GetOptions{})
	if err == nil || !strings.Contains(err.Error(), "is shutting down") {
		log.Fatalf("%s: expected to get an error when trying to make a client call on shutdown: %v", id, err)
	}

	// This participant no longer holds the lease.
	log.Infof("%s: done", id)
}
