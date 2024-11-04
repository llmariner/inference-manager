# Inference Manager Engine

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/inference-manager-engine)](https://artifacthub.io/packages/search?repo=inference-manager-engine)

The inference-manager-engine is a sub-component of [LLMariner](https://github.com/llmariner/llmariner). It manages inference runtimes (e.g., vLLM and Ollama) in containers, load models, and process requests. Also, auto-scale runtimes based on the number of in-flight requests. See [Technical Details](https://llmariner.ai/docs/dev/architecture/) document for details.

> [!NOTE]
> This is a worker subcomponent, so it is typically not installed on its own except for testing. See [Installation](https://llmariner.ai/docs/setup/install/) guide for LLMariner installation.

## Configuration

See [Customizing the Chart Before Installing](https://helm.sh/docs/intro/using_helm/#customizing-the-chart-before-installing). To see all configurable options with detailed comments, visit the chart's [values.yaml](./values.yaml), or run these configuration commands:

```console
helm show values oci://public.ecr.aws/cloudnatix/llmariner-charts/inference-manager-engine
```

## Install Chart

```console
helm install <RELEASE_NAME> oci://public.ecr.aws/cloudnatix/llmariner-charts/inference-manager-engine
```

See [configuration](#configuration) below.
See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation.

## Uninstall Chart

```console
helm uninstall <RELEASE_NAME>
```

This removes all the Kubernetes components associated with the chart and deletes the release.
See [helm uninstall](https://helm.sh/docs/helm/helm_uninstall/) for command documentation.

## Upgrading Chart

```console
helm upgrade <RELEASE_NAME> oci://public.ecr.aws/cloudnatix/llmariner-charts/inference-manager-engine
```

See [helm upgrade](https://helm.sh/docs/helm/helm_upgrade/) for command documentation.
