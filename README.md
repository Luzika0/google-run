# 🚀 Google Cloud Run Proxy

A lightweight Go TCP stream forwarder running on **Google Cloud Run**, with **Direct VPC egress** and **Cloud NAT** for outbound traffic. The application accepts TCP connections on Cloud Run's configured container port and forwards bytes to a configured upstream TCP server. The deployed client-to-service protocol must be compatible with Cloud Run's HTTP(S)/WebSocket request handling; this application does not implement a WebSocket handshake itself.

## 📚 Contents

- [Architecture](#-architecture)
- [Prerequisites](#-prerequisites)
- [Configuration](#️-configuration)
- [Pre-deployment checklist](#-pre-deployment-checklist)
- [CPU, RAM, and connection sizing](#-cpu-ram-and-connection-sizing)
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
| `PORT` | `8080` | Local port on which the Go process listens. Cloud Run provides this variable automatically. |
| `V2RAY_SERVER_IP` | Required | Upstream server IP address. The application exits if this is missing. |
| `DIAL_TIMEOUT` | `10s` | Maximum time to establish the upstream TCP connection. Accepts Go duration syntax, such as `5s`, `10s`, or `1m`. |

The current `main.go` hardcodes the upstream port to `80`. It does not implement `V2RAY_SERVER_PORT`, `MAX_CONNECTIONS`, or an idle timeout. Each accepted client connection starts a handler, so the application itself currently has no explicit concurrent-connection cap.

### 🔌 Configure the upstream and dial timeout

Set the upstream IP and dial timeout. Replace `YOUR_PROJECT_ID` with your actual project ID:

```bash
gcloud run services update google-cloud-run-proxy \
  --project="YOUR_PROJECT_ID" \
  --region="asia-southeast1" \
  --update-env-vars="V2RAY_SERVER_IP=YOUR_UPSTREAM_IP,DIAL_TIMEOUT=5s"
```

Use the real upstream IP address for `YOUR_UPSTREAM_IP`. The example sets a 5-second connection-establishment timeout; omit `DIAL_TIMEOUT` to use the code default of 10 seconds.

> ⚠️ **Resource note:** The current Go code does not enforce `MAX_CONNECTIONS`. Cloud Run concurrency controls the number of in-flight requests per instance, but it is not a substitute for application-level backpressure in every connection model. Choose a conservative Cloud Run concurrency and memory setting, then test and monitor real traffic. Each proxy session opens an upstream TCP connection.

## 🧪 Pre-deployment checklist

Complete these checks **before deploying a new revision**:

1. **Confirm the upstream is reachable.** Verify the destination IP and port are correct, the upstream service is listening, and its firewall allows traffic from your Cloud NAT public IP (if the upstream uses an IP allowlist).
2. **Confirm the network path.** Check the Cloud Run region, VPC network, subnet, Direct VPC egress setting, routes, egress firewall rules, Cloud Router, and Cloud NAT coverage. Use `all-traffic` only when the VPC/NAT path is correctly configured for internet egress.
3. **Choose conservative Cloud Run concurrency.** Start with a low concurrency setting for testing and increase it only after checking memory, CPU, errors, and actual connection behavior. The current application does not enforce its own `MAX_CONNECTIONS` limit.
4. **Choose enough memory.** Use `256Mi` as the recommended initial test profile for this proxy rather than assuming `128Mi` is enough for many long-lived sessions. Memory usage depends on connection count, traffic, buffers, and runtime overhead.
5. **Keep scaling limits intentional.** `--max=1` limits the service to one instance and `--min=0` allows scale-to-zero. This reduces potential instance scaling but also means there is no second instance to absorb bursts.
6. **Check the image.** Build and push a new container image after changing source code. Updating the README or GitHub source alone does not update a deployed Cloud Run revision.
7. **Plan a test and rollback.** Record the current service settings, deploy, test with a small number of real clients, and inspect logs/metrics before increasing limits.

### 🔬 Test progressively

Start with a small number of real client sessions, then increase Cloud Run concurrency in stages only after the previous test is stable. At each step, check memory, CPU, latency, disconnects, HTTP 429/503 responses, and upstream connection errors. These are test stages, not guaranteed capacity levels.

Do not assume that a Cloud Run concurrency setting or a large number of Go goroutines guarantees 1,000 healthy long-lived sessions. More concurrent sessions consume resources and do not add CPU, memory, network bandwidth, or Cloud Run instances.

## 🧠 CPU, RAM, and connection sizing

**Cloud Run uses RAM, not GPU VRAM.** VRAM is memory on a graphics card used for GPU workloads; this TCP proxy does not need a GPU. In Cloud Run, configure the container's **memory** and **CPU** resources.

- **CPU (1 vCPU):** handles connection setup, Go runtime work, copying data in both directions, and logging. More active traffic or many busy connections can increase CPU demand. A higher connection limit does not make the CPU faster.
- **RAM (128/256/512 MiB):** is shared by the Go runtime, goroutines, network buffers, connection state, and the container. Each session uses resources on both the client and upstream sides. Actual usage varies with traffic and buffering, so there is no reliable fixed RAM-per-connection value.
- **Cloud Run concurrency:** maximum number of requests Cloud Run allows to be in flight on one instance. Long-lived WebSocket requests occupy concurrency for their lifetime. The exact behavior depends on the protocol path supported by the deployed service.
- **Application connection cap:** the current `main.go` has no `MAX_CONNECTIONS` semaphore, so each accepted TCP connection starts a handler. Use conservative Cloud Run settings and consider implementing application-level backpressure if you need a hard cap.
- **Maximum instances:** `--max=1` limits the service to one instance. It does not guarantee a specific throughput or 1,000 simultaneous healthy connections.

| Profile | Cloud Run CPU | Cloud Run memory | Cloud Run concurrency | Guidance |
|---|---:|---:|---:|---|
| Small test | 1 vCPU | 128 MiB | 10–20 | Light testing only; limited memory headroom |
| Recommended starting test | 1 vCPU | 256 MiB | 20–50 | A conservative baseline to measure |
| Larger test | 1 vCPU | 512 MiB | 50–100 | Increase only if metrics show sufficient headroom |
| High connection target | Based on load tests | 512 MiB or more to evaluate | Increase gradually | Not guaranteed; load-test and monitor before production |

The table is a **starting guide, not a Cloud Run capacity guarantee**. A 1 vCPU / 256 MiB instance may still be overloaded by high-throughput connections. If you need 1,000 simultaneous long-lived sessions, load-test using your real traffic pattern and increase resources based on observed memory/CPU. With `--max=1`, increasing resources may raise per-instance cost; increasing `--max` can add capacity but may increase cost and is outside the one-instance design.

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
  --memory="256Mi" \
  --cpu="1" \
  --min="0" \
  --max="1" \
  --concurrency="20" \
  --update-env-vars="DIAL_TIMEOUT=5s"
```

Update the image reference to the image you actually built and pushed. If you deploy an existing image, code changes in GitHub will not be included until you rebuild and push a new image.

> ℹ️ This example starts with `256Mi`, concurrency `20`, and `DIAL_TIMEOUT=5s` as a conservative test profile. These values are not a capacity guarantee. `--max=1` limits the service to one instance; `--min=0` allows scale-to-zero. With only one instance, bursts or long-lived requests can still result in HTTP 429 responses when no instance capacity is available. The current code does not enforce an application-level connection cap.

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
- 🔁 Active proxy sessions, upstream connection failures, and connection termination logs.
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
- The current `main.go` does not read `MAX_CONNECTIONS`. Adjust Cloud Run concurrency and scaling deliberately, and implement an application-level cap if required; neither setting guarantees that platform-level 429 responses disappear.

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
