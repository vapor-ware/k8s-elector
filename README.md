# k8s-elector
k8s-elector is a minimal Kubernetes-native sidecar elector service, allowing you to
perform simple leader election within your Kubernetes deployment. 

Internally, it uses `"k8s.io/client-go/tools/leaderelection"` to run the election.

## Usage
The k8s-elector is intended to be run as a sidecar to other services to provide
them leader election capabilities, but it can run on its own as well. For an example,
you can run just the elector containers with

```
$ kubectl run k8s-elector --image=vaporio/k8s-elector --replicas=3 -- -election=example
``` 

> **Note** By default, k8s-elector tries to use a Kubernetes LeaseLock. If running a
> version of Kubernetes which does not support this, you can change the lock type with
> the `-lock-type` flag. (valid values: leases, endpoints, configmaps)

This will run 3 instances of the k8s-elector. You can observe their logs to verify
a leader is chosen among them.

Looking through the logs for leadership is not tenable for a deployment to identify
leadership. k8s-elector exposes a basic HTTP API to provide leadership status information.
You can enable and conifgure it with the `-http` flag.

```
$ kubectl run k8s-elector --image=vaporio/k8s-elector --replicas=3 -- -election=example -lock-type=configmaps -http=0.0.0.0:5002
``` 

You can look through the logs to see a leader was elected and you can verify [from within
the cluster](https://kubernetes.io/docs/tasks/debug-application-cluster/debug-service/#running-commands-in-a-pod)
that each pod reports its leadership status correctly.

```console
/ # curl 10.1.0.180:5002/
{"is_leader":false,"leader":"k8s-elector-74c54b485f-hgf9z","node":"k8s-elector-74c54b485f-564ht","timestamp":"2019-05-02T18:28:51Z"}
/ # curl 10.1.0.181:5002/
{"is_leader":false,"leader":"k8s-elector-74c54b485f-hgf9z","node":"k8s-elector-74c54b485f-qztgk","timestamp":"2019-05-02T18:29:21Z"}
/ # curl 10.1.0.179:5002/
{"is_leader":true,"leader":"k8s-elector-74c54b485f-hgf9z","node":"k8s-elector-74c54b485f-hgf9z","timestamp":"2019-05-02T18:29:26Z"}
```

As the above run command specified the lock type as "configmaps", you should also expect to
see a ConfigMap with the same name as the election.

```
$ kubectl get cm
NAME      DATA      AGE
example   0         21m
```

## Configuration
For a full list of configuration options, you can run the elector with the `-h` flags. These
will include both the flags for the elector itself as well as flags for configuring logging.

```
Usage of ./elector:
  -election string
    	The name of the election. This is required.
  -http string
    	The HTTP address (host:port) which leader state will be reported on.
  -id string
    	The ID of the election participant. If not set, the hostname, as reported by the kernel, is used.
  -kubeconfig string
    	The kubeconfig file to use. If not set, in-cluster config will be used.
  -lock-type string
    	The type of Kubernetes object to use for the lock (leases, endpoints, configmaps) (default "leases")
  -namespace string
    	The Kubernetes namespace to run the election in. If not set, elections will run in the default namespace. (default "default")
  -ttl duration
    	The TTL for the election. (default 10s)
```

## API
When enabled, the exposed HTTP API consists of a single endpoint at the URL root.

### `/`

Method: `GET`

#### Example response:
```json
{
  "is_leader": false,
  "leader": "k8s-elector-74c54b485f-hgf9z",
  "node": "k8s-elector-74c54b485f-564ht",
  "timestamp": "2019-05-02T18:28:51Z"
}
```

#### Fields

| Field | Description |
| :---- | :---------- |
| *is_leader* | A boolean describing whether the node being queried is the leader node. |
| *leader* | The ID of the node which is currently the leader. |
| *node* | The ID of the node being queried for leadership status. |
| *timestamp* | The RFC3339-formatted UTC timestamp for when the response was returned. |