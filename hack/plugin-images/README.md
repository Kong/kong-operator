# Plugin Images

Tests

- [test/integration/test_kongplugininstallation.go](../../test/integration/test_kongplugininstallation.go)
- [controller/kongplugininstallation/image/image_test.go](../../controller/kongplugininstallation/image/image_test.go)

rely on images built with `Dockerfile`s in this directory and pushed to remote registries. Examine them for more details.

The content of the directory `myheader` is based on the official documentation
[Plugin Distribution - Create a custom plugin](https://docs.konghq.com/gateway-operator/latest/guides/plugin-distribution/#create-a-custom-plugin).

To build the images, run the following command inside this directory:

```shell
docker build -t <IMAGE_NAME> -f <FILE_NAME>.Dockerfile .
```

How to configure GCP registries:

- `northamerica-northeast1-docker.pkg.dev/k8s-team-playground/plugin-example` (public)
- `northamerica-northeast1-docker.pkg.dev/k8s-team-playground/plugin-example-private` (private)

used for tests check [the official GCP documentation](https://cloud.google.com/artifact-registry/docs/docker).
