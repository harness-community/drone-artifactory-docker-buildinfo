# Artifactory Docker Build Info Plugin

A Drone plugin that publishes Docker image build information to JFrog Artifactory, including build details, VCS information, and SHA256 verification.

## Features

- Publishes Docker image build information to JFrog Artifactory
- Verifies image SHA256 hash
- Adds Git commit information (VCS data) to build metadata
- Creates and publishes detailed build information
- Supports multiple authentication methods

## Requirements

- JFrog Artifactory instance
- Valid authentication credentials (Access Token, API Key, or Username/Password)
- Docker image published to Artifactory registry

## Authentication

The plugin supports three authentication methods:

1. Access Token (Recommended)
2. Username and Password
3. API Key

## Configuration

### Required Settings

| Setting | Description |
|---------|-------------|
| `url` | JFrog Artifactory URL |
| `docker_image` | Full path to Docker image in Artifactory |
| `build_name` | Name of the build |
| `build_number` | Build number (usually pipeline sequence ID) |

### Optional Settings

| Setting | Description |
|---------|-------------|
| `access_token` | JFrog access token for authentication |
| `username` | JFrog username (alternative to access token) |
| `password` | JFrog password (alternative to access token) |
| `api_key` | JFrog API key (alternative to access token) |
| `build_url` | URL to the build in Harness CI |
| `git_path` | Path to Git repository (defaults to workspace) |

## Usage Example

Here's how to use the plugin in your Harness CI pipeline:

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

## Environment Variables

The plugin automatically captures these environment variables if available:

- `DRONE_COMMIT_SHA`: Git commit SHA
- `DRONE_GIT_HTTP_URL`: Git repository URL
- `DRONE_REPO_BRANCH`: Git branch name
- `DRONE_COMMIT_MESSAGE`: Commit message
- `DRONE_WORKSPACE`: Default workspace path

## How It Works

1. Verifies the Docker image exists in Artifactory
2. Extracts and validates the image SHA256 hash
3. Creates build information with Docker image details
4. Adds VCS information if available
5. Publishes the complete build information to Artifactory

## Troubleshooting

Common issues and solutions:

1. Authentication Failures
   - Verify credentials are correct
   - Ensure proper permissions in Artifactory
   - Check URL format includes `/artifactory` path

2. Image Not Found
   - Verify image path is correct
   - Confirm image exists in specified repository
   - Check repository permissions

3. Build Publication Errors
   - Ensure build name and number are unique
   - Verify Artifactory has enough disk space
   - Check network connectivity to Artifactory
