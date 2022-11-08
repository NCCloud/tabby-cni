## Example Configuration

```
    {
      "cniVersion": "0.3.1",
      "name": "foo",
      "plugins": [
          {
              "type": "bridge",
              "bridge": "br2724",
              "ipMasq": false,
              "forceAddress": false,
              "hairpinMode": false,
              "mtu": 9000,
              "ipam": {},
              "isDefaultGateway": false
          },
          {
              "type": "tobby",
              "bridge": "br2724",
              "interface": "bond0",
              "vlan": 2724,
              "mtu": 9000,
              "routes": [
                {
                  "to": "192.168.2.0/23",
                  "dev": "br2724",
                  "src": "10.0.12.0/22"
                }
              ]
          }
      ]
    }
```
