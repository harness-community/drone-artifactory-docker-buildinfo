package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"
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
	GitPath         string `envconfig:"PLUGIN_GIT_PATH"`
	CommitSha       string `envconfig:"DRONE_COMMIT_SHA"`
	RepoURL         string `envconfig:"DRONE_GIT_HTTP_URL"`
	BranchName      string `envconfig:"DRONE_REPO_BRANCH"`
	TagName         string `envconfig:"DRONE_TAG"`
	CommitMessage   string `envconfig:"DRONE_COMMIT_MESSAGE"`
	DefaultPath     string `envconfig:"DRONE_WORKSPACE"`
	BuildTrigger    string `envconfig:"DRONE_BUILD_TRIGGER"`
}

// Artifact represents a Docker image artifact with its SHA256 hash.
type Artifact struct {
	Sha256 string `json:"sha256"`
}

// Configure logrus to use a custom formatter
func init() {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp:       true,  // Remove timestamp
		DisableQuote:           true,  // Remove quotes around strings
		DisableLevelTruncation: false, // Keep log level
	})
}

func main() {
	var args Args
	// Process environment variables into the Args struct
	if err := envconfig.Process("", &args); err != nil {
		logrus.Fatalln("Error processing environment variables:", err)
	}

	// Execute the main functionality of the program
	if err := Exec(context.Background(), args); err != nil {
		logrus.Fatalln("Error:", err)
	}
}

// Exec contains the main logic for executing commands related to Docker images and JFrog.
func Exec(ctx context.Context, args Args) error {

	// If GitPath is null, assign default value
	if args.GitPath == "" {
		args.GitPath = args.DefaultPath
	}

	// Parse the Docker image to extract repository, image name, and tag
	repo, imageName, imageTag, err := parseDockerImage(args.DockerImage)
	if err != nil {
		logrus.Fatalln("error parsing Docker image:", err)
	}

	// Sanitize the URL for JFrog
	sanitizedURL, err := sanitizeURL(args.URL)
	if err != nil {
		return err
	}

	// Create a query to find the manifest.json file in JFrog
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

	// Create a JSON file to hold the query
	logrus.Infof("Creating query file: query.json")
	queryFile, err := os.Create("query.json")
	if err != nil {
		logrus.Fatalln("error creating query.json file:", err)
	}
	defer queryFile.Close()

	// Encode the query into the JSON file
	encoder := json.NewEncoder(queryFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(query); err != nil {
		logrus.Fatalln("failed to encode query to query.json:", err)
	}

	queryFileContents, err := os.ReadFile("query.json")
	if err == nil {
		logrus.Infof("Query file contents:\n%s", string(queryFileContents))
	}

	// Prepare the command to search for the manifest file in JFrog
	cmdArgs := []string{"jfrog", "rt", "s", "--spec=query.json", "--url=" + sanitizedURL}
	cmdArgs, err = setAuthParams(cmdArgs, args)
	if err != nil {
		logrus.Errorf("error setting auth parameters: %v", err)
	}

	// Run the command and capture the output
	output, err := runCommandAndCaptureOutput(cmdArgs)
	if err != nil {
		logrus.Fatalln("error executing jfrog rt s command: ", err)
	}

	// Extract the SHA256 hash from the command output
	sha256, err := extractSha256FromOutput(output)
	if err != nil {
		return err
	}

	// Prepare the content for the image file
	imageFileContent := fmt.Sprintf("%s/%s:%s@sha256:%s", repo, imageName, imageTag, sha256)
	imageFileName := "image_info.txt"

	// Create a file to store the image information
	logrus.Infof("Creating image info file: %s", imageFileName)
	imageFile, err := os.Create(imageFileName)
	if err != nil {
		logrus.Errorf("error creating image file: %v", err)
	}
	defer imageFile.Close()

	// Write the image information to the file
	if _, err := imageFile.WriteString(imageFileContent); err != nil {
		logrus.Errorf("error writing to image file: %v", err)
	}

	logrus.Infof("Image info file contents: %s", imageFileContent)

	// Command to create the Docker build in JFrog
	logrus.Infof("Setting Build Properties to %s", args.DockerImage)
	cmdArgs = []string{"jfrog", "rt", "build-docker-create", repo, "--build-name=" + args.BuildName, "--build-number=" + args.BuildNumber, "--image-file=" + imageFileName, "--url=" + sanitizedURL}
	cmdArgs, err = setAuthParams(cmdArgs, args)
	if err != nil {
		logrus.Errorf("error setting auth parameters: %v", err)
	}

	// Execute the build creation command
	if err := runCommand(cmdArgs); err != nil {
		logrus.Fatalln("error executing jfrog rt build-docker-create command:", err)
	}

	// If Git information is available, add it to the build info
	logrus.Info("Setting Git Properties")
	hasVCSInfo := args.RepoURL != "" && args.CommitSha != "" &&
		(args.BranchName != "" || args.TagName != "")

	if hasVCSInfo {
		logrus.WithFields(logrus.Fields{
			"repo_url":    args.RepoURL,
			"commit_sha":  args.CommitSha,
			"branch_name": args.BranchName,
			"tag_name":    args.TagName,
		}).Info("Adding VCS information")

		cmdArgs = []string{"jfrog", "rt", "build-add-git", args.BuildName, args.BuildNumber, args.GitPath}
		if err := runCommand(cmdArgs); err != nil {
			logrus.Warnf("error executing jfrog rt build-add-git command: %v", err)
		}
	}

	// Command to publish the build information to JFrog
	logrus.Info("Publishing Build Info")
	cmdArgs = []string{"jfrog", "rt", "build-publish", "--build-url=" + args.BuildURL, "--url=" + sanitizedURL, args.BuildName, args.BuildNumber}
	cmdArgs, err = setAuthParams(cmdArgs, args)
	if err != nil {
		logrus.Errorf("error setting auth parameters: %v", err)
	}

	// Execute the build publish command
	if err := runCommand(cmdArgs); err != nil {
		logrus.Fatalln("error executing jfrog rt build-publish command:", err)
	}

	// Add Principal directly via REST API using build trigger
	if args.BuildTrigger != "" {
		logrus.WithFields(logrus.Fields{
			"principal": args.BuildTrigger,
		}).Info("Adding Principal information via REST API")

		// Poll for build info to be available in the API
		pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Wait for build info to be available
		if err := pollForBuildInfo(pollCtx, args); err != nil {
			logrus.Warnf("error waiting for build info: %v", err)
			return nil
		}

		// Get the build info via API, modify it, and re-upload it
		if err := addPrincipalToBuildInfo(pollCtx, args, args.BuildTrigger); err != nil {
			logrus.Warnf("error adding principal to build info: %v", err)
		}
	}

	return nil
}

// extractSha256FromOutput extracts the SHA256 hash from the command output.
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
		return "", fmt.Errorf("no artifacts found in JFrog output")
	}

	return artifacts[0].Sha256, nil
}

// runCommand executes a command and logs its output.
func runCommand(cmdArgs []string) error {
	logrus.Infof("Executing command: %s", strings.Join(cmdArgs, " "))
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()
	logrus.Infof("Command output:\n%s\n", string(output))
	if err != nil {
		logrus.Errorf("Error executing command: %v", err)
		return err
	}
	return nil
}

// runCommandAndCaptureOutput executes a command and captures its output as a string.
func runCommandAndCaptureOutput(cmdArgs []string) (string, error) {
	logrus.Infof("Executing command and capturing output: %s", strings.Join(cmdArgs, " "))
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()

	// Replace literal \n with actual newlines
	formattedOutput := strings.ReplaceAll(string(output), "\\n", "\n")

	return formattedOutput, err
}

// setAuthParams sets authentication parameters for the command based on the provided args.
func setAuthParams(cmdArgs []string, args Args) ([]string, error) {
	if args.Username != "" && args.Password != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--user=%s", args.Username))
		cmdArgs = append(cmdArgs, fmt.Sprintf("--password=%s", args.Password))
	} else if args.APIKey != "" && args.Username != "" {
		// For JFrog CLI: API key is used as password with provided username
		cmdArgs = append(cmdArgs, fmt.Sprintf("--user=%s", args.Username))
		cmdArgs = append(cmdArgs, fmt.Sprintf("--password=%s", args.APIKey))
	} else if args.AccessToken != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--access-token=%s", args.AccessToken))
	} else {
		logrus.Errorf("either username/password, api key or access token needs to be set")
	}
	return cmdArgs, nil
}

// parseDockerImage parses a Docker image string and returns the repo, imageName, and imageTag.
func parseDockerImage(dockerImage string) (repo, imageName, imageTag string, err error) {
	// Split by the last occurrence of ':'
	lastColonIndex := strings.LastIndex(dockerImage, ":")
	if lastColonIndex == -1 {
		logrus.Errorf("invalid Docker image format: %s", dockerImage)
	}

	imageTag = dockerImage[lastColonIndex+1:]
	imagePath := dockerImage[:lastColonIndex]

	// Split the image path by '/'
	pathParts := strings.Split(imagePath, "/")
	if len(pathParts) < 2 {
		logrus.Errorf("invalid Docker image format: %s", dockerImage)
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

type BuildPrincipal struct {
	Principal string `json:"principal"`
}

// addPrincipalToBuildInfo adds the principal field to the build info via REST API
// pollForBuildInfo polls the JFrog API until the build info is available or timeout occurs
func pollForBuildInfo(ctx context.Context, args Args) error {
	logrus.Info("Polling for build info availability...")

	sanitizedURL, err := sanitizeURL(args.URL)
	if err != nil {
		return fmt.Errorf("error sanitizing URL: %v", err)
	}
	sanitizedURL = strings.TrimSuffix(sanitizedURL, "/")

	encodedBuildName := strings.ReplaceAll(url.QueryEscape(args.BuildName), "+", "%20")
	encodedBuildNumber := strings.ReplaceAll(url.QueryEscape(args.BuildNumber), "+", "%20")
	apiURL := fmt.Sprintf("%s/api/build/%s/%s", sanitizedURL, encodedBuildName, encodedBuildNumber)

	// Poll with exponential backoff
	backoff := 1 * time.Second
	maxBackoff := 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for build info to be available")
		default:
			if err := checkBuildInfoExists(ctx, apiURL, args); err == nil {
				logrus.Info("Build info is now available")
				return nil
			}

			logrus.Debugf("Build info not yet available, retrying in %v", backoff)
			time.Sleep(backoff)

			// Increase backoff, but don't exceed max
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// checkBuildInfoExists checks if the build info is available in JFrog and validates its content
func checkBuildInfoExists(ctx context.Context, apiURL string, args Args) error {
	// Using GET instead of HEAD to get the actual content
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}

	if err := setAuthHeaders(req, args); err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("build info not available yet: status code %d", resp.StatusCode)
	}

	// Read and validate the build info content
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	// Parse the JSON response
	var buildInfoResponse map[string]interface{}
	if err := json.Unmarshal(body, &buildInfoResponse); err != nil {
		return fmt.Errorf("error parsing build info: %v", err)
	}

	// Get the buildInfo object
	buildInfoObj, ok := buildInfoResponse["buildInfo"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("buildInfo field not found or has unexpected format")
	}

	// We only need to verify the build number to ensure we're looking at the right build
	number, ok := buildInfoObj["number"].(string)
	if !ok || number != args.BuildNumber {
		return fmt.Errorf("build number mismatch or missing: expected %s, got %v", args.BuildNumber, number)
	}

	logrus.Debug("Build info validation successful")
	return nil
}

func addPrincipalToBuildInfo(ctx context.Context, args Args, principal string) error {
	sanitizedURL, err := sanitizeURL(args.URL)
	if err != nil {
		return fmt.Errorf("error sanitizing URL: %v", err)
	}

	// Remove trailing slash if present
	sanitizedURL = strings.TrimSuffix(sanitizedURL, "/")

	// Build the API URL for getting build info
	// Manually encode the build name and number to ensure compatibility with JFrog API
	// Replace spaces with %20 instead of + to match JFrog's expectations
	encodedBuildName := strings.ReplaceAll(url.QueryEscape(args.BuildName), "+", "%20")
	encodedBuildNumber := strings.ReplaceAll(url.QueryEscape(args.BuildNumber), "+", "%20")
	apiURL := fmt.Sprintf("%s/api/build/%s/%s", sanitizedURL, encodedBuildName, encodedBuildNumber)
	logrus.Infof("Fetching build info from: %s", apiURL)

	// Create the HTTP client
	client := &http.Client{}

	// Create the GET request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	// Add authentication headers
	if err := setAuthHeaders(req, args); err != nil {
		return err
	}

	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error executing request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logrus.Errorf("API call failed with status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("API request failed with status: %d - check logs for details", resp.StatusCode)
	}

	// Read the build info
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	// Parse the JSON into a map
	var buildInfoData map[string]interface{}
	if err := json.Unmarshal(body, &buildInfoData); err != nil {
		return fmt.Errorf("error unmarshaling build info: %v", err)
	}

	// Get the buildInfo object
	buildInfoObj, ok := buildInfoData["buildInfo"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("buildInfo not found or has unexpected format")
	}

	// Create a BuildPrincipal struct
	principalData := BuildPrincipal{
		Principal: principal,
	}

	// Add the principal field using the struct
	buildInfoObj["principal"] = principalData.Principal
	logrus.Infof("Adding principal '%s' to build info JSON structure", principalData.Principal)

	// Marshal the buildInfo object for the PUT request - JFrog API expects this format
	updatedBody, err := json.Marshal(buildInfoObj)
	if err != nil {
		return fmt.Errorf("error marshaling updated build info: %v", err)
	}

	// Log at debug level for troubleshooting if needed
	logrus.Debugf("Sending build info update to API")

	// Create the PUT request to update the build info
	apiURL = fmt.Sprintf("%s/api/build", sanitizedURL)
	req, err = http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(updatedBody)))
	if err != nil {
		return fmt.Errorf("error creating update request: %v", err)
	}

	// Add authentication headers
	if err := setAuthHeaders(req, args); err != nil {
		return err
	}

	// Add content type header
	req.Header.Set("Content-Type", "application/json")

	// Execute the update request
	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("error executing update request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		logrus.Errorf("API PUT call failed with status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("API PUT request failed with status: %d - check logs for details", resp.StatusCode)
	}

	logrus.Info("Successfully updated build info with principal")
	return nil
}

// setAuthHeaders adds the appropriate authentication headers to an HTTP request
func setAuthHeaders(req *http.Request, args Args) error {
	if args.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+args.AccessToken)
	} else if args.APIKey != "" && args.Username != "" {
		// Use API key as password with provided username
		req.SetBasicAuth(args.Username, args.APIKey)
	} else if args.Username != "" && args.Password != "" {
		req.SetBasicAuth(args.Username, args.Password)
	} else {
		return fmt.Errorf("no authentication method provided")
	}
	return nil
}

func sanitizeURL(inputURL string) (string, error) {
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		logrus.Errorf("invalid URL: %s", inputURL)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		logrus.Errorf("invalid URL: %s", inputURL)
	}
	parts := strings.Split(parsedURL.Path, "/artifactory")
	if len(parts) < 2 {
		logrus.Errorf("url does not contain '/artifactory': %s", inputURL)
	}

	// Always set the path to the first part + "/artifactory/"
	parsedURL.Path = parts[0] + "/artifactory/"

	return parsedURL.String(), nil
}
