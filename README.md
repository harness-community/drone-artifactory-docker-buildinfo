# Artifactory Docker Publish Build Info Plugin

## Synopsis

A Drone plugin that publishes Docker image build information to JFrog Artifactory, including build details, VCS information, and SHA256 verification.

To learn how to utilize Drone plugins in Harness CI, please consult the provided [documentation](https://developer.harness.io/docs/continuous-integration/use-ci/use-drone-plugins/run-a-drone-plugin-in-ci).

## Plugin Image

The plugin `plugins/artifactory-publish-docker-buildinfo` is available for the following architectures:

| OS            | Tag                   |
| ------------- | --------------------- |
| latest        | `linux-amd64,linux-arm64` |
| linux/amd64   | `linux-amd64`         |
| linux/arm64   | `linux-arm64`         |

## Requirements

- JFrog Artifactory instance
- Valid authentication credentials (Access Token, API Key, or Username/Password)
- Docker image published to Artifactory registry

## Configuration

| Parameter | Choices/<span style="color:blue;">Defaults</span> | Comments |
| :------------------------------------------------------------------------------------------------------------------------------------- | :------------------------------------------------ | --------------------------------------------------------------- |
| `url` <span style="font-size: 10px"><br/>`string`</span>                  | Required | JFrog Artifactory URL |
| `docker_image` <span style="font-size: 10px"><br/>`string`</span>          | Required | Full path to Docker image in Artifactory |
| `build_name` <span style="font-size: 10px"><br/>`string`</span>           | Required | Name of the build |
| `build_number` <span style="font-size: 10px"><br/>`string`</span>         | Required | Build number (usually pipeline sequence ID) |
| `access_token` <span style="font-size: 10px"><br/>`string`</span>                                                                  | Either Access_token or Username Password or API key is required | JFrog access token for authentication |
| `username` <span style="font-size: 10px"><br/>`string`</span>                                                                      | Either Access_token or Username Password or API key is required| JFrog username (alternative to access token) |
| `password` <span style="font-size: 10px"><br/>`string`</span>                                                                      | Either Access_token or Username Password or API key is required| JFrog password (alternative to access token) |
| `api_key` <span style="font-size: 10px"><br/>`string`</span>                                                                       | Either Access_token or Username Password or API key is required| JFrog API key (alternative to access token) |
| `build_url` <span style="font-size: 10px"><br/>`string`</span>                                                                     | Optional | URL to the build in Harness CI |
| `git_path` <span style="font-size: 10px"><br/>`string`</span>                                                                      | Optional | Path to Git repository (defaults to workspace) |

## Usage Example

Here's how to use the plugin in your Harness CI pipeline 

- using access token:

```yaml
- step:
    type: Plugin
    name: Publish Build Info
    identifier: publish_build_info
    spec:
      connectorRef: docker_registry
      image: plugins/artifactory-publish-docker-buildinfo:1.1.0
      settings:
        url: https://artifactory.example.com/artifactory
        access_token: <+secrets.getValue("artifactory_token")>
        build_name: <+pipeline.name>
        build_url: <+pipeline.executionUrl>
        docker_image: artifactory.example.com/repo/image:tag
        build_number: <+pipeline.sequenceId>
```

- using username password:

```yaml
- step:
    type: Plugin
    name: Publish Build Info
    identifier: publish_build_info
    spec:
      connectorRef: docker_registry
      image: plugins/artifactory-publish-docker-buildinfo:1.1.0
      settings:
        url: https://artifactory.example.com/artifactory
        username: <+secrets.getValue("artifactory_username")>
        password: <+secrets.getValue("artifactory_password")>
        build_name: <+pipeline.name>
        build_url: <+pipeline.executionUrl>
        docker_image: artifactory.example.com/repo/image:tag
        build_number: <+pipeline.sequenceId>
```

- using API key:

```yaml
- step:
    type: Plugin
    name: Publish Build Info
    identifier: publish_build_info
    spec:
      connectorRef: docker_registry 
      image: plugins/artifactory-publish-docker-buildinfo:1.1.0
      settings:
        url: https://artifactory.example.com/artifactory
        api_key: <+secrets.getValue("artifactory_api_key")>
        build_name: <+pipeline.name>
        build_url: <+pipeline.executionUrl>
        docker_image: artifactory.example.com/repo/image:tag
        build_number: <+pipeline.sequenceId>
```