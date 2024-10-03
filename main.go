package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

type Args struct {
	BuildNumber     string `envconfig:"PLUGIN_BUILD_NUMBER"`
	BuildName       string `envconfig:"PLUGIN_BUILD_NAME"`
	BuildURL        string `envconfig:"PLUGIN_BUILD_URL"`
	DockerImage     string `envconfig:"PLUGIN_DOCKER_IMAGE"`
	URL             string `envconfig:"PLUGIN_URL"`
	AccessToken     string `envconfig:"PLUGIN_ACCESS_TOKEN"`
	Username        string `envconfig:"PLUGIN_USERNAME"`
	Password        string `envconfig:"PLUGIN_PASSWORD"`
	APIKey          string `envconfig:"PLUGIN_API_KEY"`
	Insecure        string `envconfig:"PLUGIN_INSECURE"`
	PEMFileContents string `envconfig:"PLUGIN_PEM_FILE_CONTENTS"`
	PEMFilePath     string `envconfig:"PLUGIN_PEM_FILE_PATH"`
	Level           string `envconfig:"PLUGIN_LOG_LEVEL"`
}

type Artifact struct {
	Sha256 string `json:"sha256"`
}

func main() {
	var args Args
	err := envconfig.Process("", &args)
	if err != nil {
		fmt.Printf("Error processing environment variables: %v\n", err)
		os.Exit(1)
	}

	if err := Exec(context.Background(), args); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func Exec(ctx context.Context, args Args) error {
	repo, imageName, imageTag, err := parseDockerImage(args.DockerImage)
	if err != nil {
		return fmt.Errorf("error parsing Docker image: %v", err)
	}

	sanitizedURL, err := sanitizeURL(args.URL)
	if err != nil {
		return err
	}

	query := map[string]interface{}{
		"files": []map[string]interface{}{
			{
				"aql": map[string]interface{}{
					"items.find": map[string]interface{}{
						"repo": repo,
						"path": imageName + "/" + imageTag,
						"name": "manifest.json",
					},
				},
			},
		},
	}

	queryFile, err := os.Create("query.json")
	if err != nil {
		return fmt.Errorf("error creating query.json file: %v", err)
	}
	defer queryFile.Close()

	encoder := json.NewEncoder(queryFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(query); err != nil {
		return fmt.Errorf("error encoding query to query.json: %v", err)
	}

	cmdArgs := []string{"jfrog", "rt", "s", "--spec=query.json", "--url=" + sanitizedURL}
	cmdArgs, err = setAuthParams(cmdArgs, args)
	if err != nil {
		return fmt.Errorf("error setting auth parameters: %v", err)
	}

	output, err := runCommandAndCaptureOutput(cmdArgs)
	if err != nil {
		return fmt.Errorf("error executing jfrog rt s command: %v", err)
	}

	sha256, err := extractSha256FromOutput(output)
	if err != nil {
		return err
	}

	imageFileContent := fmt.Sprintf("%s/%s:%s@sha256:%s", repo, imageName, imageTag, sha256)
	imageFileName := "image_info.txt"

	imageFile, err := os.Create(imageFileName)
	if err != nil {
		return fmt.Errorf("error creating image file: %v", err)
	}
	defer imageFile.Close()

	if _, err := imageFile.WriteString(imageFileContent); err != nil {
		return fmt.Errorf("error writing to image file: %v", err)
	}

	cmdArgs = []string{"jfrog", "rt", "build-docker-create", repo, "--build-name=" + args.BuildName, "--build-number=" + args.BuildNumber, "--image-file=" + imageFileName, "--url=" + sanitizedURL}
	cmdArgs, err = setAuthParams(cmdArgs, args)
	if err != nil {
		return fmt.Errorf("error setting auth parameters: %v", err)
	}

	if err := runCommand(cmdArgs); err != nil {
		return fmt.Errorf("error executing jfrog rt build-docker-create command: %v", err)
	}

	cmdArgs = []string{"jfrog", "rt", "build-publish", "--build-url=" + args.BuildURL, "--url=" + sanitizedURL, args.BuildName, args.BuildNumber}
	cmdArgs, err = setAuthParams(cmdArgs, args)
	if err != nil {
		return fmt.Errorf("error setting auth parameters: %v", err)
	}

	if err := runCommand(cmdArgs); err != nil {
		return fmt.Errorf("error executing jfrog rt build-publish command: %v", err)
	}

	return nil
}

func extractSha256FromOutput(output string) (string, error) {
	// Split the output into lines
	lines := strings.Split(output, "\n")

	// Find the line where the JSON array starts
	var jsonStr string
	startIndex := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "[") {
			startIndex = i
			break
		}
	}

	if startIndex != -1 {
		jsonStr = strings.Join(lines[startIndex:], "\n")
	} else {
		return "", fmt.Errorf("could not find JSON output in the command response")
	}

	// Parse the JSON output
	var artifacts []Artifact
	err := json.Unmarshal([]byte(jsonStr), &artifacts)
	if err != nil {
		return "", fmt.Errorf("error parsing JSON: %v", err)
	}

	if len(artifacts) == 0 {
		return "", fmt.Errorf("no results found in jfrog output")
	}

	return artifacts[0].Sha256, nil
}

func runCommand(cmdArgs []string) error {
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCommandAndCaptureOutput(cmdArgs []string) (string, error) {
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func setAuthParams(cmdArgs []string, args Args) ([]string, error) {
	if args.Username != "" && args.Password != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--user=%s", args.Username))
		cmdArgs = append(cmdArgs, fmt.Sprintf("--password=%s", args.Password))
	} else if args.APIKey != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--apikey=%s", args.APIKey))
	} else if args.AccessToken != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--access-token=%s", args.AccessToken))
	} else {
		return nil, fmt.Errorf("either username/password, api key or access token needs to be set")
	}
	return cmdArgs, nil
}

// parseDockerImage parses a Docker image string and returns the repo, imageName, and imageTag.
func parseDockerImage(dockerImage string) (repo, imageName, imageTag string, err error) {
	// Split by the last occurrence of ':'
	lastColonIndex := strings.LastIndex(dockerImage, ":")
	if lastColonIndex == -1 {
		return "", "", "", fmt.Errorf("invalid Docker image format: %s", dockerImage)
	}

	imageTag = dockerImage[lastColonIndex+1:]
	imagePath := dockerImage[:lastColonIndex]

	// Split the image path by '/'
	pathParts := strings.Split(imagePath, "/")
	if len(pathParts) < 2 {
		return "", "", "", fmt.Errorf("invalid Docker image format: %s", dockerImage)
	}

	// Check if the first part is in the x.y.z format
	isDomain := strings.Count(pathParts[0], ".") >= 2

	// Extract repo and image name
	if isDomain {
		// The repo is the part immediately after the domain
		repo = pathParts[1]
		imageName = strings.Join(pathParts[2:], "/")
	} else {
		repo = pathParts[0]
		imageName = strings.Join(pathParts[1:], "/")
	}

	return repo, imageName, imageTag, nil
}

// sanitizeURL trims the URL to include only up to the '/artifactory/' path.
func sanitizeURL(inputURL string) (string, error) {
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %s", inputURL)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("invalid URL: %s", inputURL)
	}
	parts := strings.Split(parsedURL.Path, "/artifactory")
	if len(parts) < 2 {
		return "", fmt.Errorf("url does not contain '/artifactory': %s", inputURL)
	}

	// Always set the path to the first part + "/artifactory/"
	parsedURL.Path = parts[0] + "/artifactory/"

	return parsedURL.String(), nil
}
