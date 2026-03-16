# buzz — BuzzHPC CLI

Manage GPU cloud resources from the command line. Create and deploy Developer Pods, GPU VMs, Kubernetes clusters, Jupyter Notebooks, LLM Inference endpoints, Object Storage buckets, and Shared Filesystems.

---

## Installation

### macOS

```bash
curl -fsSL https://raw.githubusercontent.com/BuzzHPC/buzz-cli/main/install.sh | sh
```

Or download directly from the [latest release](https://github.com/BuzzHPC/buzz-cli/releases/latest):

| Apple Silicon (M1/M2/M3) | Intel |
|---|---|
| `buzz_0.1.0_darwin_arm64.tar.gz` | `buzz_0.1.0_darwin_amd64.tar.gz` |

```bash
tar -xzf buzz_*_darwin_*.tar.gz
sudo mv buzz /usr/local/bin/buzz
```

### Linux

```bash
curl -fsSL https://raw.githubusercontent.com/BuzzHPC/buzz-cli/main/install.sh | sh
```

Or download directly:

| x86_64 | ARM64 |
|---|---|
| `buzz_0.1.0_linux_amd64.tar.gz` | `buzz_0.1.0_linux_arm64.tar.gz` |

```bash
tar -xzf buzz_*_linux_*.tar.gz
sudo mv buzz /usr/local/bin/buzz
```

### Windows

Download `buzz_0.1.0_windows_amd64.zip` from the [latest release](https://github.com/BuzzHPC/buzz-cli/releases/latest), extract it, and add the `buzz.exe` to your PATH.

---

## Configuration

Set your API key and it will be used automatically for every command:

```bash
export BUZZHPC_API_KEY=your-api-key
```

Or pass it per-command:

```bash
buzz --api-key your-api-key vm list
```

**Environment variables:**

| Variable | Description |
|---|---|
| `BUZZHPC_API_KEY` | Your BuzzHPC API key (required) |
| `BUZZHPC_PROJECT` | Default project name (default: `defaultproject`) |
| `BUZZHPC_WORKSPACE` | Default workspace name |
| `BUZZHPC_BASE_URL` | Override API base URL |

---

## Global Flags

These flags are available on every command:

```
--api-key string     BuzzHPC API key (or set BUZZHPC_API_KEY)
--base-url string    API base URL (or set BUZZHPC_BASE_URL)
-p, --project string     Project name (or set BUZZHPC_PROJECT)
-w, --workspace string   Workspace name (or set BUZZHPC_WORKSPACE)
```

Use `-w` to filter any list/get/create/delete command to a specific workspace.

---

## Commands

### devpod — Developer Pods

**Aliases:** `pod`, `pods`, `devpods`

Manage GPU-backed developer pods with SSH access and persistent storage.

```
buzz devpod list
buzz devpod get <name>
buzz devpod create [flags]
buzz devpod delete <name>
```

**Create flags:**

```
-n, --name string        Name of the DevPod (required)
    --node-type string   GPU node type (default "H200")
    --gpu-count int      Number of GPUs (default 1)
    --sku string         SKU: managed-developer-pods-v2-ca-qc-2 (H200) or managed-developer-pods-v2 (A40/H100)
    --no-deploy          Create without deploying
```

**Examples:**

```bash
buzz devpod list
buzz devpod get my-pod
buzz devpod create --name my-pod
buzz pod create --name my-pod --node-type H100 --gpu-count 2
buzz devpod delete my-pod
buzz devpod delete my-pod --force
```

---

### vm — GPU Virtual Machines

**Aliases:** `gpu-vm`, `virtual-machine`

Manage full GPU virtual machines with SSH access.

```
buzz vm list
buzz vm get <name>
buzz vm create [flags]
buzz vm delete <name>
```

**Create flags:**

```
-n, --name string        Name of the VM (required)
    --node-type string   GPU node type: H200 (default "H200")
    --gpu-count int      Number of GPUs (default 1)
    --sku string         SKU (default: no-gpu-vm)
    --no-deploy          Create without deploying
```

**Examples:**

```bash
buzz vm list
buzz vm get my-vm
buzz vm create --name my-vm
buzz gpu-vm create --name my-vm --node-type H200 --gpu-count 2
buzz vm delete my-vm
```

The `get` command returns full VM details including CPU count, GPU count, memory, OS, public IP, private IP, username, password, and SSH connection info.

---

### kubernetes — Managed Kubernetes

**Aliases:** `k8s`, `mks`, `cluster`

Manage Managed Kubernetes Service (MKS) clusters. Clusters are provisioned on full GPU nodes — specify node type and count.

```
buzz kubernetes list
buzz kubernetes get <name>
buzz kubernetes create [flags]
buzz kubernetes delete <name>
```

**Create flags:**

```
-n, --name string        Name of the cluster (required)
    --node-type string   GPU node type: H200, A40, H100, CPU (default "H200")
    --nodes int          Number of nodes (default 1)
    --sku string         SKU: mks-oneclick (default), mks-k8s-ca-qc-2, mks-k8s
    --no-deploy          Create without deploying
```

**Examples:**

```bash
buzz k8s list
buzz k8s get my-cluster
buzz k8s create --name my-cluster
buzz k8s create --name my-cluster --node-type A40 --nodes 2
buzz k8s create --name prod-cluster --node-type H200 --nodes 4
buzz cluster delete my-cluster
```

---

### notebook — Jupyter Notebooks

**Aliases:** `nb`, `jupyter`

Manage GPU-backed Jupyter Notebook instances. Each notebook is accessible via a public URL at `https://<name>.notebook.buzzperformancecloud.com`.

```
buzz notebook list
buzz notebook get <name>
buzz notebook create [flags]
buzz notebook delete <name>
```

**Create flags:**

```
-n, --name string        Name of the notebook — also sets the URL subdomain (required)
    --node-type string   GPU node type (default "H200")
    --gpu-count int      Number of GPUs (default 1)
    --image string       Jupyter container image (default "jupyter/minimal-notebook:latest")
    --sku string         SKU: jupyter-notebook-v4-ca-qc-2 (H200) or jupyter-notebook-v4 (A40/H100)
    --no-deploy          Create without deploying
```

**Examples:**

```bash
buzz notebook list
buzz notebook get my-nb
buzz notebook create --name my-nb
buzz jupyter create --name my-nb --node-type H100 --gpu-count 2 --image jupyter/scipy-notebook:latest
buzz nb delete my-nb
```

---

### inference — LLM Inference Endpoints

**Aliases:** `llm`, `vllm`, `ai`

Deploy vLLM-powered LLM inference endpoints from any HuggingFace model. Supports gated/private models via HuggingFace tokens and tensor parallelism across multiple GPUs.

```
buzz inference list
buzz inference get <name>
buzz inference create [flags]
buzz inference delete <name>
```

**Create flags:**

```
-n, --name string         Name of the inference endpoint (required)
-m, --model string        HuggingFace model ID (default "facebook/opt-125m")
    --node-type string    GPU node type (default "H200")
    --gpu-count int       Number of GPUs — use >1 for tensor parallelism (default 1)
    --hf-token string     HuggingFace token for gated/private models
    --extra-args string   Extra vLLM CLI args (e.g. '--max-model-len 8192')
    --sku string          SKU: inference-vllm-v1 (H200) or inference-vllm-v1-h100 (A40/H100)
    --no-deploy           Create without deploying
```

**Examples:**

```bash
buzz llm list
buzz llm get my-llm
buzz llm create --name my-llm --model meta-llama/Llama-3.1-8B-Instruct
buzz llm create --name gated-model --model meta-llama/Llama-3.1-8B-Instruct --hf-token hf_xxx
buzz llm create --name big-model --model meta-llama/Llama-3.1-70B-Instruct --gpu-count 4
buzz llm create --name my-llm --model facebook/opt-125m --extra-args '--max-model-len 8192'
buzz ai delete my-llm
```

---

### object-storage — S3-Compatible Object Storage

**Aliases:** `s3`, `obs`, `bucket`

Manage S3-compatible object storage buckets backed by VAST Data. The `get` command returns your S3 endpoint URL, bucket name, access key, and secret key.

```
buzz object-storage list
buzz object-storage get <name>
buzz object-storage create [flags]
buzz object-storage delete <name>
```

**Create flags:**

```
-n, --name string   Name of the bucket (required)
-s, --size int      Storage quota in GB (default 10)
    --sku string    SKU: object-storage-vast-ca-qc-2 (CA-QC-2) or object-storage-vast (CA-QC-1)
    --no-deploy     Create without deploying
```

**Examples:**

```bash
buzz s3 list
buzz s3 get my-bucket
buzz s3 create --name my-bucket
buzz s3 create --name my-bucket --size 100
buzz bucket create --name my-bucket --sku object-storage-vast --size 200
buzz s3 delete my-bucket
```

---

### shared-fs — Shared Filesystems (NFS)

**Aliases:** `nfs`, `shared-filesystem`, `fs`

Manage shared NFS filesystems that can be mounted across multiple pods and VMs. The `get` command returns the server IP, mount path, and a ready-to-use mount command.

```
buzz shared-fs list
buzz shared-fs get <name>
buzz shared-fs create [flags]
buzz shared-fs delete <name>
```

**Create flags:**

```
-n, --name string   Name of the filesystem (required)
-s, --size int      Volume size in GB (default 50)
    --no-deploy     Create without deploying
```

**Examples:**

```bash
buzz fs list
buzz fs get my-fs
buzz shared-fs create --name my-fs --size 50
buzz nfs create --name datasets --size 500
buzz fs create --name model-weights --size 200
buzz nfs delete my-fs
```

---

## Common Patterns

**List everything across all workspaces:**
```bash
buzz devpod list
buzz vm list
buzz k8s list
buzz notebook list
buzz llm list
buzz s3 list
buzz fs list
```

**Filter by workspace:**
```bash
buzz vm list -w my-workspace
buzz devpod get my-pod -w my-workspace
```

**Create and deploy in one step:**
```bash
buzz devpod create --name my-pod
# Resource is created and deployed automatically.
# Pass --no-deploy to create without deploying.
```

**Delete without confirmation prompt:**
```bash
buzz vm delete my-vm --force
buzz devpod delete my-pod -f
```
