# Sensu EC2 Discovery

## Overview

Sensu EC2 Discovery is a `sensuctl` command plugin that collects AWS
EC2 instance data from the EC2 API and registers a Sensu Proxy Entity
for each EC2 instance.

## Installation and usage

1. Install the plugin using `sensuctl`:

   ```shell
   $ sensuctl command install ec2-discovery portertech/sensu-ec2-discovery:0.1.0
   ```

2. Verify installation using `sensuctl`:

   ```shell
   $ sensuctl command exec ec2-discovery -h
   ```

3. Discover EC2 instances using `sensuctl`:

   ```shell
   $ sensuctl command exec ec2-discovery --region us-west-2
   ```

## Configuration


The `sensu-ec2-discovery` plugin expects two environment variables for
AWS API authentication.

```
export AWS_ACCESS_KEY_ID=""
export AWS_SECRET_ACCESS_KEY=""
```
