# Google Cloud Run Proxy

Deploy a lightweight proxy service on **Google Cloud Run** with **Direct VPC egress** and **Cloud NAT** for outbound traffic.

This repository documents an example Cloud Run proxy deployment and its VPC networking configuration.

## Architecture

```text
Client
  | HTTPS / WebSocket
  v
Google Cloud Run service
  | Direct VPC egress
  v
VPC: cloud-run-sg
  | Subnet: cloudrun-standard-vpc-sg
  v
Cloud Router: nat-sg
  |
  v
Cloud NAT -> Internet / destination server
```

> The public IP `34.2.150.61` was observed in an example NAT flow log. Your NAT IP may differ depending on your configuration.

## Prerequisites

- A Google Cloud project with billing enabled.
- Google Cloud CLI (`gcloud`) installed and authenticated.
- Cloud Run and required networking APIs enabled.
- A container image available in Artifact Registry or another supported registry.
- A VPC network and subnet in the same region as the Cloud Run service.
- A Cloud Router and Cloud NAT configured for the subnet and Cloud Run Direct VPC egress.
- IAM permissions to deploy Cloud Run and inspect networking resources.

## Example network configuration

| Resource | Value |
|---|---|
| Region | `asia-southeast1` |
| VPC network | `cloud-run-sg` |
| Subnet | `cloudrun-standard-vpc-sg` |
| Cloud Router | `nat-sg` |
| NAT public IP observed in logs | `34.2.150.61` |

Change these values to match your project. Resource names and IP addresses are examples, not universal defaults.

## Deploy to Cloud Run

Set your project, service name, and container image:

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
  --max="1"
```

The CPU, memory, and scaling values are examples. Adjust them to your application and verify they are supported for your Cloud Run configuration.

### Public access and authentication

Do not enable unauthenticated access unless the application is designed to be public and has appropriate authentication and abuse protections. Review Cloud Run ingress, IAM, and application-level access controls before exposing a proxy service.

## Configure networking in the Cloud Run console

1. Open **Networking** while creating or editing the service.
2. Select **Connect to a VPC for outbound traffic**.
3. Choose **Send traffic directly to a VPC** (Direct VPC egress).
4. Select network `cloud-run-sg`.
5. Select subnet `cloudrun-standard-vpc-sg`.
6. Choose **Route all traffic to the VPC**.
7. Deploy the revision.

You normally do not need a separate Serverless VPC Access connector when using Direct VPC egress. You can reuse the existing Cloud NAT if it covers the selected subnet and is configured for the relevant Cloud Run endpoint type.

## Verify the deployment

```bash
gcloud run services describe "$SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --format="yaml(metadata.name,spec.template.metadata.annotations,status.url)"
```

Generate outbound traffic from the running service, then inspect Cloud NAT flow logs in Cloud Logging. A successful flow may include:

- `allocation_status: "OK"`
- `connection.src_ip`: private source IP
- `connection.dest_ip` and `connection.dest_port`: destination address and port
- `connection.nat_ip`: public NAT IP used for that connection
- `vpc.vpc_name` and `vpc.subnetwork_name`: VPC and subnet

A NAT flow with `allocation_status: "OK"` confirms NAT allocation for that recorded connection. It does not prove every connection uses NAT or that the complete proxy session is transferring data.

### Example Logs Explorer query

Replace `YOUR_PROJECT_ID` with your project ID:

```text
logName="projects/YOUR_PROJECT_ID/logs/compute.googleapis.com%2Fnat_flows"
jsonPayload.vpc.vpc_name="cloud-run-sg"
jsonPayload.allocation_status="OK"
```

Cloud Run request logs can also help diagnose incoming requests. HTTP `101 Switching Protocols` indicates a successful WebSocket handshake, not necessarily a healthy long-lived connection or successful data transfer.

## Cost and monitoring

Possible charges include Cloud Run CPU and memory usage, Cloud NAT usage and data processing, network data transfer, and logging or monitoring usage. Check current rates in [Google Cloud Pricing](https://cloud.google.com/pricing) and review Billing reports. Billing data can be delayed, so configure budgets and alerts.

Monitor:

- Cloud Run request count, latency, instance count, CPU, memory, and restarts.
- Cloud NAT flow logs, dropped packets, and port allocation errors.
- Actual proxy data transfer, not just successful handshakes.
- Daily and monthly Cloud Run, Cloud NAT, and network transfer costs.

## Troubleshooting

**DNS works but outbound traffic fails**
- DNS resolution alone does not verify connectivity.
- Confirm the Cloud Run VPC egress mode and inspect application logs.
- Verify routes, firewall rules, NAT subnet coverage, and NAT endpoint-type configuration.

**NAT flow logs do not appear**
- Generate a real outbound connection from the Cloud Run container.
- Confirm NAT logging is enabled and inspect the correct project, region, gateway, and time range.

**The WebSocket handshake succeeds but data does not transfer**
- Check application-level proxy configuration and destination reachability.
- Verify the connection remains open and both directions forward bytes.
- Review Cloud Run timeouts, concurrency, scaling, and resource limits.

**Unexpected costs**
- Review Cloud Run, Cloud NAT, and network data transfer as separate billing categories.
- Check scaling and traffic volume before increasing resource limits or maximum instances.

## Security

- Never commit passwords, tokens, private keys, or other secrets.
- Store credentials in Secret Manager or another appropriate secret store.
- Protect proxy endpoints with authentication and abuse prevention.
- Restrict ingress and egress where practical, and monitor unexpected traffic.
- Never expose diagnostic endpoints that execute arbitrary commands.

## References

- [Cloud Run Direct VPC egress](https://cloud.google.com/run/docs/configuring/vpc-direct-vpc)
- [Connect Cloud Run to a VPC](https://cloud.google.com/run/docs/configuring/connecting-vpc)
- [Cloud NAT overview](https://cloud.google.com/nat/docs/overview)
- [Cloud Run pricing](https://cloud.google.com/run/pricing)