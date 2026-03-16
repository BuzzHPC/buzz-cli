#compdef buzz

_buzz() {
  local state

  _arguments \
    '--api-key[BuzzHPC API key]:api key' \
    '--base-url[API base URL]:url' \
    '(-p --project)'{-p,--project}'[Project name]:project' \
    '(-w --workspace)'{-w,--workspace}'[Workspace name]:workspace' \
    '1: :->command' \
    '*: :->args'

  case $state in
    command)
      local commands=(
        'devpod:Manage Developer Pods'
        'pod:Manage Developer Pods'
        'pods:Manage Developer Pods'
        'vm:Manage GPU Virtual Machines'
        'gpu-vm:Manage GPU Virtual Machines'
        'kubernetes:Manage Managed Kubernetes clusters'
        'k8s:Manage Managed Kubernetes clusters'
        'mks:Manage Managed Kubernetes clusters'
        'cluster:Manage Managed Kubernetes clusters'
        'notebook:Manage Jupyter Notebooks'
        'nb:Manage Jupyter Notebooks'
        'jupyter:Manage Jupyter Notebooks'
        'inference:Manage LLM Inference endpoints'
        'llm:Manage LLM Inference endpoints'
        'vllm:Manage LLM Inference endpoints'
        'ai:Manage LLM Inference endpoints'
        'object-storage:Manage Object Storage buckets'
        's3:Manage Object Storage buckets'
        'bucket:Manage Object Storage buckets'
        'obs:Manage Object Storage buckets'
        'shared-fs:Manage Shared Filesystems'
        'nfs:Manage Shared Filesystems'
        'fs:Manage Shared Filesystems'
        'help:Show help'
      )
      _describe 'buzz commands' commands
      ;;
    args)
      local subcommands=(
        'create:Create a resource'
        'list:List resources'
        'ls:List resources'
        'get:Get details of a resource'
        'describe:Get details of a resource'
        'show:Get details of a resource'
        'delete:Delete a resource'
        'destroy:Delete a resource'
        'rm:Delete a resource'
      )
      _describe 'subcommands' subcommands
      ;;
  esac
}

_buzz "$@"
