# How to test cni plugin locally

CNI_PATH=$(pwd)/tabby-cni NETCONFPATH=$(pwd)/tabby-cni/tests cnitool add vm-network /var/run/netns/testing
