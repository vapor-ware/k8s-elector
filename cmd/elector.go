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
	"flag"
	"runtime"
	"time"

	"github.com/vapor-ware/k8s-elector/pkg"
	"k8s.io/klog"
)

// Version information for the elector. These are set via build-time variables
// on the build server.
var (
	Version   string
	BuildDate string
	Commit    string
	Tag       string
	GoVersion string
	Arch      = runtime.GOARCH
	OS        = runtime.GOOS
)

// Command line configuration flag values. The command line values are
// bound on elector start.
var (
	address    string
	id         string
	kubeconfig string
	lockType   string
	name       string
	namespace  string
	ttl        time.Duration
)

// logVersion is a helper function to log the build-time version information
// for the elector.
func logVersion() {
	klog.Info("k8s-elector")
	klog.Infof("  version    : %s", Version)
	klog.Infof("  commit     : %s", Commit)
	klog.Infof("  tag        : %s", Tag)
	klog.Infof("  go version : %s", GoVersion)
	klog.Infof("  build date : %s", BuildDate)
	klog.Infof("  os         : %s", Arch)
	klog.Infof("  arch       : %s", OS)
}

func main() {
	// Log elector version info before doing anything else.
	logVersion()

	// Bind the flags to variables.
	flag.StringVar(&address, "http", "", "The HTTP address (host:port) which leader state will be reported on.")
	flag.StringVar(&id, "id", "", "The ID of the election participant. If not set, the hostname, as reported by the kernel, is used.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "The kubeconfig file to use. If not set, in-cluster config will be used.")
	flag.StringVar(&lockType, "lock-type", "leases", "The type of Kubernetes object to use for the lock (leases, endpoints, configmaps)")
	flag.StringVar(&name, "election", "", "The name of the election. This is required.")
	flag.StringVar(&namespace, "namespace", "default", "The Kubernetes namespace to run the election in. If not set, elections will run in the default namespace.")
	flag.DurationVar(&ttl, "ttl", 10*time.Second, "The TTL for the election.")
	flag.Parse()

	elector := pkg.NewElectorNode(&pkg.ElectorConfig{
		Address:    address,
		ID:         id,
		KubeConfig: kubeconfig,
		LockType:   lockType,
		Namespace:  namespace,
		Name:       name,
		TTL:        ttl,
	})

	if err := elector.Run(); err != nil {
		klog.Fatalf("error running elector: %v", err)
	}
}
