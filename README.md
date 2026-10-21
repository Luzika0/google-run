# Google Cloud Run TCP Proxy

A lightweight Go TCP proxy that accepts client connections on the port provided by Cloud Run and forwards traffic to a configured upstream TCP server.

> **Important:** This README describes the improved `main.go` version with `V2RAY_SERVER_PORT`, `DIAL_TIMEOUT`, and `MAX_CONNECTIONS`. Make sure that version of `main.go` is committed and included in the container image you deploy. Updating the README alone does not change the running Cloud Run service.

## How it works

```text
Client
  |
  v
Cloud Run service (:8080 by default)
  |
  v
Go TCP proxy
  |
  v
Upstream TCP server (V2RAY_SERVER_IP:V2RAY_SERVER_PORT)
```

The proxy copies bytes in both directions between the client and the upstream server. It does not itself implement a WebSocket handshake. The client-to-Cloud-Run protocol must be supported by your Cloud Run ingress/request path.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Local listening port. Cloud Run normally supplies this automatically. |
| `V2RAY_SERVER_IP` | Required | IP address or hostname of the upstream server. The process exits if this is missing. |
| `V2RAY_SERVER_PORT` | `80` | TCP port of the upstream server. |
| `DIAL_TIMEOUT` | `10` | Timeout in **seconds** for connecting to the upstream server. Must be a positive integer. |
| `MAX_CONNECTIONS` | `100` | Maximum number of active client proxy sessions handled at once. Must be a positive integer. |

Invalid or missing values for `DIAL_TIMEOUT` and `MAX_CONNECTIONS` fall back to their defaults.

### Example values

For the upstream server at `47.237.149.117:80`:

- `V2RAY_SERVER_IP=47.237.149.117`
- `V2RAY_SERVER_PORT=80`
- `DIAL_TIMEOUT=10`
- `MAX_CONNECTIONS=100`

Set these variables in Cloud Run under **Edit and deploy new revision → Variables & Secrets**. Do not include secrets in source control.

## Connection limits and Cloud Run sizing

The application uses a semaphore to limit active proxy sessions. When the limit is reached, newly accepted clients are closed until a slot becomes available.

Start with a conservative limit and increase it only after testing your actual traffic:

| Setting | Starting point | Notes |
|---|---:|---|
| `MAX_CONNECTIONS` | `100` | Application-level cap; not a throughput guarantee. |
| Cloud Run concurrency | `20` to `100` for testing | Cloud Run request concurrency and the Go application's TCP connection cap are different limits. Choose settings appropriate to the ingress/protocol being used. |
| Cloud Run instances | `--max=1` if you want one instance | Controls instance scaling, not the number of connections the instance can reliably handle. |
| Memory | `256Mi` as a test baseline | Monitor actual memory and CPU before raising connection limits. |

A configured limit of 1,000 does **not** guarantee 1,000 healthy, active proxy sessions. Capacity depends on CPU, memory, traffic, network buffers, upstream limits, and Cloud Run request handling. If you want to control costs with one instance, keep `--max=1` and test gradually; bursts can still result in HTTP 429 responses.

## Build and deploy

Build and push a new container image after changing `main.go`; changing source code or this README on GitHub does not update an already deployed image.

Example deployment (replace the project and image placeholders):

```bash
PROJECT_ID="YOUR_PROJECT_ID"
REGION="asia-southeast1"
SERVICE="google-cloud-run-proxy"
IMAGE="REGION-docker.pkg.dev/YOUR_PROJECT_ID/YOUR_REPOSITORY/YOUR_IMAGE:TAG"

gcloud run deploy "$SERVICE" \
  --project="$PROJECT_ID" \
  --image="$IMAGE" \
  --region="$REGION" \
  --memory="256Mi" \
  --cpu="1" \
  --min="0" \
  --max="1" \
  --concurrency="100" \
  --update-env-vars="V2RAY_SERVER_IP=YOUR_UPSTREAM_IP,V2RAY_SERVER_PORT=80,DIAL_TIMEOUT=10,MAX_CONNECTIONS=100"
```

If your service uses Direct VPC egress and Cloud NAT, preserve the network settings already configured for your service when deploying. For example, include the existing `--network`, `--subnet`, and `--vpc-egress=all-traffic` values if those are required by your setup.

## Networking

If outbound traffic must use Cloud NAT, verify all of the following:

1. Cloud Run uses the intended VPC network and subnet.
2. VPC egress is configured correctly for the service.
3. Routes and egress firewall rules permit traffic to the upstream IP and port.
4. Cloud NAT covers the selected subnet and has available NAT ports.
5. The upstream firewall permits connections from the expected source IP, if it uses an allowlist.

DNS resolution alone does not prove that a TCP connection can be established. Test the actual upstream IP and port and check the Go application logs.

## Troubleshooting

### `V2RAY_SERVER_IP is required`
Set `V2RAY_SERVER_IP` in the Cloud Run environment variables and deploy a new revision.

### `Cannot connect to upstream` / `no route to host`
Check the upstream address and port, VPC route, egress firewall, Direct VPC egress, Cloud NAT configuration, and upstream firewall. This is a connectivity issue, not a connection-cap setting.

### HTTP 429
Inspect Cloud Run request logs, concurrency, instance count, and resource metrics. With one instance, a busy or unavailable instance may not have capacity for additional requests. Increasing `MAX_CONNECTIONS` does not remove Cloud Run's own limits.

### Connections close when the limit is reached
This version rejects new clients when `MAX_CONNECTIONS` is full. Reduce incoming concurrency or test a carefully increased cap only after checking memory, CPU, and upstream capacity.

### WebSocket handshake succeeds but traffic fails
A successful handshake does not prove the full proxy session is working. Confirm the client protocol is compatible with Cloud Run's request path and check both client and upstream logs.

## Cost and monitoring

Review Cloud Run, Cloud NAT, network data transfer, and logging costs separately in Google Cloud Billing. Monitor:

- Cloud Run request count, HTTP 429/503 responses, CPU, memory, and instance count.
- Application logs for upstream connection failures and connection-limit rejections.
- Cloud NAT flow logs and NAT port allocation issues.
- Actual traffic volume and daily/monthly cost.

## Security

- Do not commit credentials, private keys, or other secrets.
- Restrict ingress and access to the proxy where practical.
- Use authentication and abuse prevention appropriate to your deployment.
- Monitor unexpected traffic and review egress restrictions.


## Lower-cost Cloud Run egress with Standard Tier + Cloud NAT

This project can use **Cloud Run Direct VPC egress** to send outbound traffic through a VPC subnet and **Public Cloud NAT** to provide internet access with a stable outbound public IP. Configure Cloud NAT to allocate its external IP address from **Standard Network Service Tier** when that tier is appropriate for your traffic.

> **Important:** This changes the network path for outbound traffic; it does not make Cloud Run compute free, provide free unlimited bandwidth, or change the Cloud Run service's inbound URL/IP. Standard Tier is intended for public internet traffic and has different network performance/availability characteristics from Premium Tier. Test connectivity and performance before relying on it.

### Architecture

```text
Client
  |
  v
Cloud Run service (HTTPS ingress / run.app URL)
  |
  | outbound traffic: Direct VPC egress, all-traffic
  v
VPC subnet
  |
  v
Cloud Router + Public Cloud NAT (Standard Tier external IP)
  |
  v
Public internet / upstream server
```

Cloud NAT handles **outbound** connections and their return traffic. It does not create an inbound public IP for Cloud Run and does not replace the Cloud Run HTTPS endpoint or a load balancer.

### Before you start

- Select the correct Google Cloud project and region. The Cloud Run service, subnet, Cloud Router, and Cloud NAT should be configured in the same region.
- Enable the Compute Engine API and make sure your account has permissions to configure VPC networking and Cloud Run.
- Use an existing VPC/subnet/router/NAT if already configured. Do not create duplicate resources without checking first.
- The commands below are examples. Replace every placeholder with your real values. Review the current configuration before changing an existing service.

```bash
PROJECT_ID="YOUR_PROJECT_ID"
REGION="asia-southeast1"
SERVICE="google-cloud-run-proxy"
NETWORK="YOUR_VPC_NETWORK"
SUBNET="YOUR_SUBNET"
ROUTER="YOUR_CLOUD_ROUTER"
NAT="YOUR_CLOUD_NAT"

gcloud config set project "$PROJECT_ID"
gcloud services enable compute.googleapis.com run.googleapis.com
```

### 1. Check your existing network resources

```bash
gcloud run services describe "$SERVICE" --region="$REGION"
gcloud compute networks list
gcloud compute networks subnets list --regions="$REGION"
gcloud compute routers list --regions="$REGION"
```

If you already have the VPC subnet and Cloud Router, reuse them. If not, create them only after choosing a non-overlapping subnet range appropriate for your network.

Example only:

```bash
gcloud compute networks create "$NETWORK" --subnet-mode=custom

gcloud compute networks subnets create "$SUBNET" \
  --network="$NETWORK" \
  --region="$REGION" \
  --range="10.10.0.0/26"

gcloud compute routers create "$ROUTER" \
  --network="$NETWORK" \
  --region="$REGION"
```

Do not run these create commands if the named resources already exist. Direct VPC egress requires enough free subnet IP addresses for Cloud Run instances and rollout overlap; Google recommends an IPv4 subnet of at least /26 for this use case.

### 2. Create Public Cloud NAT using Standard Tier

If you do not already have a suitable NAT gateway, create one for the Cloud Run subnet:

```bash
gcloud compute routers nats create "$NAT" \
  --router="$ROUTER" \
  --region="$REGION" \
  --nat-custom-subnet-ip-ranges="$SUBNET" \
  --endpoint-types=ENDPOINT_TYPE_VM \
  --auto-allocate-nat-external-ips \
  --auto-network-tier=STANDARD
```

This selects **Standard Tier for automatically allocated Cloud NAT external IPs**. If your NAT already exists, inspect it first:

```bash
gcloud compute routers nats describe "$NAT" \
  --router="$ROUTER" \
  --region="$REGION"
```

For an existing gateway, you may be able to update its automatic allocation tier, but changing NAT IPs can interrupt existing connections. Plan a maintenance window and confirm the gateway's allocation mode before updating it. If NAT uses manually assigned IPs, those addresses must be reserved from the intended tier and configured appropriately.

### 3. Attach Cloud Run to the VPC and route all outbound traffic through NAT

For a service using an existing container image:

```bash
gcloud run services update "$SERVICE" \
  --region="$REGION" \
  --network="$NETWORK" \
  --subnet="$SUBNET" \
  --vpc-egress=all-traffic
```

Use `all-traffic` so public internet egress also traverses the VPC and Cloud NAT. With `private-ranges-only`, ordinary public destinations generally bypass this NAT path, so it is not the setting to use when you need NAT's Standard Tier external IP for public outbound connections.

If deploying a new revision with `gcloud run deploy`, include the same `--network`, `--subnet`, and `--vpc-egress=all-traffic` flags alongside your image and environment variables. Preserve your existing settings. Do not change the service's image, environment variables, ingress policy, or scaling limits unintentionally.

### 4. Verify the outbound public IP

After the revision is ready, make an outbound request from the application/container to a trusted IP-echo endpoint, or use a temporary diagnostic endpoint that runs inside the same Cloud Run service and reports the observed public source IP. The result should match an external IP allocated to the NAT gateway.

Inspect the NAT configuration and address information:

```bash
gcloud compute routers nats describe "$NAT" \
  --router="$ROUTER" \
  --region="$REGION"

gcloud compute routers get-nat-ip-info "$ROUTER" \
  --region="$REGION"
```

Do not assume the Cloud Run `run.app` hostname resolves to the NAT IP. The NAT IP is for **outbound source traffic**, not the public inbound address of the service.

If an upstream server uses an IP allowlist, add the actual NAT external IP to that allowlist. If NAT automatically allocates multiple IPs as demand grows, account for all allocated addresses; use manually reserved NAT IPs if you require a controlled, stable allowlist address and understand the cost and capacity implications.

### 5. Cost trade-offs and ways to keep costs low

Cloud NAT can reduce the applicable network-tier cost for eligible outbound traffic, but it adds its own charges. Review the current prices for your region before choosing this design.

- **Cloud Run:** Set minimum instances to zero if cold starts are acceptable, and use sensible CPU, memory, concurrency, timeout, and maximum-instance settings. Avoid a high minimum-instance count unless you need it.
- **Cloud NAT:** Charges can include gateway usage, data processed by NAT, and external IP addresses. A Standard Tier IP does not make NAT processing free.
- **Internet egress:** Standard Tier data transfer charges still apply to eligible outbound data. Check current regional rates and the free-tier conditions.
- **Reduce bytes transferred:** Cache content, avoid repeated downloads, compress where appropriate, and keep Cloud Run close to upstream resources when possible.
- **Use only required subnet ranges:** Configure NAT for the Cloud Run subnet rather than every subnet when that meets your needs.
- **Monitor usage:** Set a billing budget and alerts; review Cloud Run, VPC, Cloud NAT, external IP, and network egress charges separately.

For a low-traffic service, compare the fixed/hourly and per-GiB NAT costs against the potential Standard Tier savings. NAT may cost more overall if the traffic volume is small. Google Cloud pricing can change, so calculate using the current pricing page rather than treating any estimate as guaranteed.

### Troubleshooting

- **Outbound requests fail after enabling VPC egress:** Check NAT subnet coverage, route configuration, egress firewall rules, DNS, available NAT ports, and the upstream firewall.
- **Observed public IP is not the NAT IP:** Confirm the deployed revision has `--vpc-egress=all-traffic`, uses the intended network/subnet, and is covered by the correct Public NAT gateway. Verify the IP-echo request originates from the Cloud Run instance.
- **Existing connections drop after changing NAT tier/IPs:** NAT address replacement can terminate connections. Allow for a planned restart/reconnect and verify the gateway's new IP allocation.
- **Performance is worse on Standard Tier:** Standard Tier has different network characteristics from Premium Tier. Benchmark latency, throughput, and reliability from your actual client locations and revert if the trade-off is unacceptable.
- **Cloud NAT has charges while traffic is low:** Check the gateway, external IP, and processed-data line items. Compare with the cost of leaving the service on its original egress path.

## Official networking and pricing documentation

- [Cloud Run Direct VPC egress](https://cloud.google.com/run/docs/configuring/vpc-direct-vpc)
- [Cloud Run networking best practices](https://cloud.google.com/run/docs/configuring/networking-best-practices)
- [Cloud NAT and Direct VPC egress interactions](https://cloud.google.com/nat/docs/nat-product-interactions)
- [Create and manage Public Cloud NAT](https://cloud.google.com/nat/docs/set-up-manage-network-address-translation)
- [Google Cloud network pricing](https://cloud.google.com/vpc/network-pricing)

## References

- [Cloud Run Direct VPC egress](https://cloud.google.com/run/docs/configuring/vpc-direct-vpc)
- [Connect Cloud Run to a VPC](https://cloud.google.com/run/docs/configuring/connecting-vpc)
- [Cloud NAT overview](https://cloud.google.com/nat/docs/overview)
- [Cloud Run pricing](https://cloud.google.com/run/pricing)
