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

## References

- [Cloud Run Direct VPC egress](https://cloud.google.com/run/docs/configuring/vpc-direct-vpc)
- [Connect Cloud Run to a VPC](https://cloud.google.com/run/docs/configuring/connecting-vpc)
- [Cloud NAT overview](https://cloud.google.com/nat/docs/overview)
- [Cloud Run pricing](https://cloud.google.com/run/pricing)
