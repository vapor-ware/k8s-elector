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

import "time"

type ElectorConfig struct {
	// Address is the HTTP address[:port] that the elector will host an endpoint
	// on (at '/') to provide information on the node and if it is the leader. If
	// not set, an HTTP endpoint will not be set up.
	Address string

	// The ID of the elector node participating in the election. This is required
	// for an election and must be unique. If not specified, the elector will try
	// using the HOSTNAME as its ID.
	ID string

	// KubeConfig is the path to the kubeconfig file to use for setting up the
	// elector node's Kubernetes client. If no kubeconfig is specified, the node
	// will default to using in-cluster configuration.
	KubeConfig string

	// LockType specifies the kind of Kubernetes object to use as the lock mechanism
	// to determine node leadership. If not specified, the node will use "leases"
	// by default.
	//
	// The valid LockTypes are: "endpoints", "configmaps", and "leases".
	LockType string

	// The Name of the election. The election name gets used as the name for the
	// Kubernetes object used as the election lock. This is required by the node
	// to join or create an election.
	Name string

	// The Namespace in Kubernetes to run the election in. The Kubernetes object
	// used as the election lock will be created in this namespace. If not specified,
	// "default" is used.
	Namespace string

	// The TTL for the election determines the lease duration (the time non-leader
	// candidates will wait to force acquire leadership), the renew deadline (the
	// duration that the acting master will retry refreshing leadership), and the
	// retry period (the duration that elector nodes should wait between retry
	// actions).
	TTL time.Duration
}
