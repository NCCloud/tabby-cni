## 📖 General Information

### 📄 Summary
Tabby CNI is a Kubernetes operator that provides distributed SNAT, static routing, bridge, and VLAN configuration.

## 🛠 Installation

The most convenient way of installing the operator is via our [tabby-operator](https://github.com/NCCloud/charts/tree/main/charts/tabby-operator) Helm chart.
```bash
helm repo add nccloud https://nccloud.github.io/charts
helm install tabby-operator --namespace tabby-operator --create-namespace nccloud/tabby-operator
```

## 🚀 Usage

### Network Resource

The operator manages network configuration via the `Network` CRD.

The following example will create `NetworkAttachment` resources for nodes that match `nodeSelectors`. On each matching node, the operator will create a bridge interface, enable IP masquerading, add iptables and ebtables rules for SNAT, and add static routes via a bridge:

```
apiVersion: cloud.spaceship.com/v1alpha1
kind: Network
metadata:
  name: test-network
  namespace: default
spec:
  bridge:
  - mtu: 1500
    name: br10
    ports:
    - mtu: 1500
      name: bond0
      vlan: 10
  ipMasq:
    bridge: br10
    egressnetwork: 10.10.10.0/24
    enabled: true
    ignore:
    - 192.168.1.0/23
    - 10.10.10.0/24
    source: 192.168.1.0/23
  nodeSelectors:
  - matchLabels:
      beta.kubernetes.io/arch: amd64
  routes:
  - destination: 192.168.1.0/23
    source: 10.10.10.0/24
    via: br10
```

### KubeVirt Live Migration Gratuitous ARP

If you use [KubeVirt](https://kubevirt.io/) and need to send a gratuitous ARP request upon the completion of a live VM migration, you can enable a controller that will watch `VirtualMachineInstance` events and send a gratuitous ARP request from the target node of the VM.<br>
The controller will go through all VM networks of type `Multus` from the `VirtualMachineInstance` resource and send an ARP request to any network that has IP masquerading enabled in the corresponding `Network` resource.

To enable the controller, you will need to set the `WATCH_KUBEVIRT_MIGRATION=true` environment variable.

## 🏷️ Versioning

We use [SemVer](http://semver.org/) for versioning.
To see the available versions, check the [Releases](https://github.com/NCCloud/tabby-cni/releases) page.

## 🤝 Contribution

We welcome contributions, issues, and feature requests!

Made with <span style="color: #e25555;">&hearts;</span> by [Namecheap Cloud Team](https://github.com/NCCloud)
