# 🚀 Google Cloud Run Proxy

A lightweight Go TCP/WebSocket proxy running on **Google Cloud Run**, with **Direct VPC egress** and **Cloud NAT** for outbound traffic.

## 📚 Contents

- [Architecture](#-architecture)
- [Prerequisites](#-prerequisites)
- [Configuration](#️-configuration)
- [Deploy](#-deploy-to-cloud-run)
- [Networking](#-networking)
- [Verify](#-verify-the-deployment)
- [Cost and monitoring](#-cost-and-monitoring)
- [Troubleshooting](#-troubleshooting)
- [Security](#-security)

## 🏗️ Architecture

```text
Client
  | HTTPS / WebSocket
  v
Google Cloud Run service
  | Direct VPC egress (all traffic)
  v
VPC: cloud-run-sg
  | Subnet: cloudrun-standard-vpc-sg
  v
Cloud Router: nat-sg
  |
  v
Cloud NAT -> Internet -> destination server
```

> ℹ️ The public IP `34.2.150.61` was observed in an example NAT flow log. Your NAT IP may differ.

## ✅ Prerequisites

- Google Cloud project with billing enabled.
- Google Cloud CLI (`gcloud`) installed and authenticated.
- Cloud Run and required networking APIs enabled.
- Container image in Artifact Registry or another supported registry.
- VPC network and subnet in the same region as the Cloud Run service.
- Cloud Router and Cloud NAT configured for the Cloud Run network path.
- IAM permissions to deploy Cloud Run and inspect networking resources.

## ⚙️ Configuration

The Go proxy supports these environment variables:

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Port Cloud Run routes requests to. |
| `V2RAY_SERVER_PORT` | `80` | Upstream TCP port. |
| `MAX_CONNECTIONS` | `500` | Maximum concurrent proxy sessions handled by the application per instance. Set a lower value for small-memory instances. |

### 🔌 Set the upstream and connection limit

For example, set the upstream server and cap the application at **20 concurrent proxy sessions** (a cautious starting point for a 128 MiB instance):

```bash
gcloud run services update google-cloud-run-proxy \
  --project="YOUR_PROJECT_ID" \
  --region="asia-southeast1" \
  --update-env-vars="V2RAY_SERVER_IP=47.237.149.117,V2RAY_SERVER_PORT=80,MAX_CONNECTIONS=20"
```

Replace `YOUR_PROJECT_ID` with your actual project ID. If the upstream variables are already configured correctly, you can update only the connection limit:

```bash
gcloud run services update google-cloud-run-proxy \
  --project="YOUR_PROJECT_ID" \
  --region="asia-southeast1" \
  --update-env-vars="MAX_CONNECTIONS=20"
```

> ⚠️ **Resource note:** `MAX_CONNECTIONS=20` is a starting point, not a guarantee that 60 sessions fit safely in 128 MiB RAM. Watch memory and errors, then adjust gradually. Increasing this variable does not merge separate client sessions into one upstream connection.

## 🚢 Deploy to Cloud Run

Set your project, service, and image:

```bash
PROJECT_ID="YOUR_PROJECT_ID"
REGION="asia-southeast1"
SERVICE="google-cloud-run-proxy"
IMAGE="REGION-docker.pkg.dev/YOUR_PROJECT_ID/YOUR_REPOSITORY/YOUR_IMAGE:TAG"
```

Deploy using Direct VPC egress and route all outbound traffic through the VPC:

```bash
gcloud run deploy "$SERVICE" \
  --project="$PROJECT_ID" \
  --image="$IMAGE" \
  --region="$REGION" \
  --network="cloud-run-sg" \
  --subnet="cloudrun-standard-vpc-sg" \
  --vpc-egress="all-traffic" \
  --memory="128Mi" \
  --cpu="1" \
  --min="0" \
  --max="1" \
  --concurrency="1000" \
  --update-env-vars="MAX_CONNECTIONS=20"
```

Update the image reference to the image you actually built and pushed. If you deploy an existing image, code changes in GitHub will not be included until you rebuild and push a new image.

> ℹ️ `--max=1` limits the service to one instance; `--min=0` allows scale-to-zero. With only one instance, bursts or long-lived WebSocket sessions can still result in HTTP 429 responses when no instance capacity is available. No connection-limit value guarantees zero 429 errors.

### 🔐 Public access and authentication

Do not enable unauthenticated access unless the application is intended to be public and has suitable authentication and abuse protections. Review Cloud Run ingress, IAM, and application-level access controls before exposing a proxy service.

## 🌐 Networking

To configure networking in the Cloud Run console:

1. Open **Networking** while creating or editing the service.
2. Select **Connect to a VPC for outbound traffic**.
3. Choose **Send traffic directly to a VPC** (Direct VPC egress).
4. Select network `cloud-run-sg`.
5. Select subnet `cloudrun-standard-vpc-sg`.
6. Choose **Route all traffic to the VPC**.
7. Deploy the revision.

You normally do not need a separate Serverless VPC Access connector when using Direct VPC egress. Reuse Cloud NAT only if it covers the selected subnet and is configured for the relevant Cloud Run endpoint type.

### 🧭 Example network resources

| Resource | Example value |
|---|---|
| Region | `asia-southeast1` |
| VPC network | `cloud-run-sg` |
| Subnet | `cloudrun-standard-vpc-sg` |
| Cloud Router | `nat-sg` |
| Example NAT IP observed in logs | `34.2.150.61` |

These are examples; change them to match your own project.

## 🔎 Verify the deployment

Check the service configuration and URL:

```bash
gcloud run services describe "$SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --format="yaml(metadata.name,spec.template.metadata.annotations,status.url)"
```

Generate outbound traffic, then inspect Cloud NAT flow logs in Cloud Logging. Useful fields may include:

- `allocation_status: "OK"`
- `connection.src_ip`: private source IP
- `connection.dest_ip` and `connection.dest_port`: destination address and port
- `connection.nat_ip`: public NAT IP used for the connection
- `vpc.vpc_name` and `vpc.subnetwork_name`: VPC and subnet

A NAT flow with `allocation_status: "OK"` confirms NAT allocation for that recorded connection. It does not prove every connection uses NAT or that the complete proxy session is transferring data.

### 🧾 Example Logs Explorer query

Replace `YOUR_PROJECT_ID` with your project ID:

```text
logName="projects/YOUR_PROJECT_ID/logs/compute.googleapis.com%2Fnat_flows"
jsonPayload.vpc.vpc_name="cloud-run-sg"
jsonPayload.allocation_status="OK"
```

HTTP `101 Switching Protocols` means the WebSocket handshake succeeded. It does not guarantee that a long-lived connection is healthy or transferring data.

## 📊 Cost and monitoring

Possible charges include Cloud Run CPU and memory usage, Cloud NAT usage and data processing, network data transfer, and logging or monitoring. Check [Google Cloud Pricing](https://cloud.google.com/pricing) and review Billing reports. Billing data can be delayed, so configure budgets and alerts.

Monitor:

- 📈 Cloud Run request count, latency, instance count, CPU, memory, and restarts.
- 🔁 Active WebSocket/proxy sessions and application connection-limit logs.
- 🌐 Cloud NAT flow logs, dropped packets, and port allocation errors.
- 📦 Actual proxy data transfer, not just successful handshakes.
- 💳 Daily and monthly Cloud Run, Cloud NAT, and network transfer costs.

## 🛠️ Troubleshooting

### DNS works but outbound traffic fails
- DNS resolution alone does not verify connectivity.
- Confirm the Cloud Run VPC egress mode and inspect application logs.
- Verify routes, firewall rules, NAT subnet coverage, and NAT endpoint-type configuration.

### NAT flow logs do not appear
- Generate a real outbound connection from the Cloud Run container.
- Confirm NAT logging is enabled and inspect the correct project, region, gateway, and time range.

### WebSocket handshake succeeds but data does not transfer
- Check proxy configuration and destination reachability.
- Verify the connection stays open and both directions forward bytes.
- Review Cloud Run timeouts, concurrency, scaling, and resource limits.

### Cloud Run returns HTTP 429
- Inspect Cloud Run request logs and instance metrics around the same timestamp.
- With `--max=1`, one busy or unavailable instance can leave new requests without capacity.
- Increasing `MAX_CONNECTIONS` only changes the application's own session cap; it does not add Cloud Run instances or guarantee that platform-level 429 responses disappear.

### Unexpected costs
- Review Cloud Run, Cloud NAT, and network data transfer as separate billing categories.
- Check scaling and traffic volume before increasing resource limits or maximum instances.

## 🛡️ Security

- Never commit passwords, tokens, private keys, or other secrets.
- Store credentials in Secret Manager or another appropriate secret store.
- Protect proxy endpoints with authentication and abuse prevention.
- Restrict ingress and egress where practical, and monitor unexpected traffic.
- Never expose diagnostic endpoints that execute arbitrary commands.

## 📎 References

- [Cloud Run Direct VPC egress](https://cloud.google.com/run/docs/configuring/vpc-direct-vpc)
- [Connect Cloud Run to a VPC](https://cloud.google.com/run/docs/configuring/connecting-vpc)
- [Cloud NAT overview](https://cloud.google.com/nat/docs/overview)
- [Cloud Run pricing](https://cloud.google.com/run/pricing)
