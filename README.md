## Sensu EC2 Discovery

The Sensu EC2 Discovery is a Sensu Check Plugin that collects AWS EC2
instance data from the EC2 API and manages a Sensu Proxy Client for
each EC2 instance.

### Installation

Download the latest version of the `sensu-ec2-discovery` binary from
[Releases](https://github.com/portertech/sensu-ec2-discovery/releases).

### Configuration

Example Sensu 1.x check definition:

```
{
  "checks": {
    "ec2_discovery": {
      "command": "sensu-ec2-discovery -api http://user:password@sensuapi.example.com:4567",
      "subscribers": ["discovery"],
      "interval": 3600
    }
  }
}
```

### Usage Examples

Help:

```
$ sensu-ec2-discovery -help
Usage of sensu-ec2-discovery:
  -api value
        api url
  -region value
        region list
  -state value
        state list
  -tag value
        tag key=value list
```

If you do not provide an API:

```
Error:

Missing mandatory flag 'api'.

To discover running instances in every region:
        ./sensu-ec2-discover -api http://user:password@127.0.0.1:4567

To discover running and pending instances in specific regions:
        ./sensu-ec2-discover -api http://user:password@127.0.0.1:4567 -state running -state pending -region us-west-1 -region us-west-2

To discover running instances with a specific tag key/value:
        ./sensu-ec2-discover -api http://user:password@127.0.0.1:4567 -tag environment=production

To balance the Sensu API request load accross several Sensu APIs:
        ./sensu-ec2-discover -api http://user:password@host1:4567 -api http://user:password@host2:4567
```
