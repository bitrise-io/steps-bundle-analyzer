package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

// Config holds the step configuration
type Config struct {
	ArtifactPath      string `env:"artifact_path"`
	OutputFormats     string `env:"output_formats,required"`
	PostGithubComment string `env:"post_github_comment,opt[auto|yes|no]"`
	GithubToken       string `env:"github_token"`
	FailOnLargeSize   string `env:"fail_on_large_size"`
}

// BundleMetrics holds the parsed bundle analysis metrics
type BundleMetrics struct {
	SizeBytes              int64
	SizeMB                 string
	PotentialSavingsBytes  int64
}

// ReportPaths holds the paths to generated reports
type ReportPaths struct {
	Markdown string
	HTML     string
	JSON     string
}

func main() {
	logger := log.NewLogger()

	// Parse configuration
	var cfg Config
	if err := stepconf.Parse(&cfg); err != nil {
		logger.Errorf("Failed to parse configuration: %s", err)
		os.Exit(1)
	}
	stepconf.Print(cfg)

	logger.Println()
	logger.Infof("Bundle Analyzer Step")
	logger.Println()

	// Detect artifact path
	artifactPath, err := detectArtifact(cfg, logger)
	if err != nil {
		logger.Errorf("Failed to detect artifact: %s", err)
		os.Exit(1)
	}
	logger.Infof("Analyzing artifact: %s", artifactPath)

	// Validate artifact exists
	if _, err := os.Stat(artifactPath); os.IsNotExist(err) {
		logger.Errorf("Artifact file does not exist: %s", artifactPath)
		os.Exit(1)
	}

	// Run bundle-inspector
	logger.Println()
	logger.Infof("Running bundle-inspector analysis...")
	if err := runBundleInspector(artifactPath, cfg.OutputFormats, logger); err != nil {
		logger.Errorf("Bundle analysis failed: %s", err)
		os.Exit(1)
	}

	// Parse JSON report to extract metrics
	var metrics BundleMetrics
	formats := strings.Split(cfg.OutputFormats, ",")
	if contains(formats, "json") {
		logger.Println()
		logger.Infof("Parsing JSON report for metrics...")
		jsonPath := "analysis.json"
		metrics, err = parseJSONReport(jsonPath, logger)
		if err != nil {
			logger.Warnf("Failed to parse JSON report (will use empty metrics): %s", err)
		}
	}

	// Deploy reports to BITRISE_DEPLOY_DIR
	deployDir := os.Getenv("BITRISE_DEPLOY_DIR")
	var reportPaths ReportPaths
	if deployDir != "" {
		logger.Println()
		logger.Infof("Deploying reports to: %s", deployDir)
		reportPaths, err = deployReports(formats, deployDir, logger)
		if err != nil {
			logger.Warnf("Failed to deploy reports: %s", err)
		}
	} else {
		logger.Warnf("BITRISE_DEPLOY_DIR not set, reports will remain in working directory")
		// Set paths to local files
		reportPaths = getLocalReportPaths(formats)
	}

	// Handle GitHub PR comments
	commentPosted := false
	if cfg.PostGithubComment != "no" && contains(formats, "markdown") {
		if isPullRequest() {
			logger.Println()
			logger.Infof("Pull request detected, preparing GitHub comment...")

			markdownPath := reportPaths.Markdown
			if markdownPath == "" {
				markdownPath = "analysis.md"
			}

			err := postGitHubComment(markdownPath, cfg.GithubToken, logger)
			if err != nil {
				if cfg.PostGithubComment == "yes" {
					logger.Errorf("Failed to post GitHub comment: %s", err)
					os.Exit(1)
				} else {
					logger.Warnf("Failed to post GitHub comment (non-fatal in auto mode): %s", err)
				}
			} else {
				commentPosted = true
				logger.Donef("GitHub PR comment posted successfully")
			}
		} else {
			logger.Infof("Not a pull request build, skipping GitHub comment")
		}
	}

	// Export outputs
	logger.Println()
	logger.Infof("Exporting outputs...")
	if err := exportOutputs(metrics, reportPaths, commentPosted, logger); err != nil {
		logger.Warnf("Failed to export some outputs: %s", err)
	}

	// Check size threshold
	if cfg.FailOnLargeSize != "" && metrics.SizeBytes > 0 {
		logger.Println()
		if err := checkSizeThreshold(cfg, metrics.SizeBytes, logger); err != nil {
			logger.Errorf("%s", err)
			os.Exit(1)
		}
	}

	logger.Println()
	logger.Donef("Bundle analysis completed successfully")
}

// detectArtifact determines the artifact path from config or environment variables
func detectArtifact(cfg Config, logger log.Logger) (string, error) {
	// Priority 1: Explicit artifact_path
	if cfg.ArtifactPath != "" {
		return cfg.ArtifactPath, nil
	}

	// Priority 2: BITRISE_IPA_PATH
	if ipaPath := os.Getenv("BITRISE_IPA_PATH"); ipaPath != "" {
		logger.Infof("Auto-detected iOS artifact from BITRISE_IPA_PATH")
		return ipaPath, nil
	}

	// Priority 3: BITRISE_AAB_PATH
	if aabPath := os.Getenv("BITRISE_AAB_PATH"); aabPath != "" {
		logger.Infof("Auto-detected Android App Bundle from BITRISE_AAB_PATH")
		return aabPath, nil
	}

	// Priority 4: BITRISE_APK_PATH
	if apkPath := os.Getenv("BITRISE_APK_PATH"); apkPath != "" {
		logger.Infof("Auto-detected Android APK from BITRISE_APK_PATH")
		return apkPath, nil
	}

	return "", fmt.Errorf("no artifact found: provide artifact_path input or ensure BITRISE_IPA_PATH, BITRISE_AAB_PATH, or BITRISE_APK_PATH is set")
}

// runBundleInspector executes the bundle-inspector plugin
func runBundleInspector(artifactPath, formats string, logger log.Logger) error {
	cmdFactory := command.NewFactory(env.NewRepository())

	args := []string{":bundle-inspector", "analyze", artifactPath, "-o", formats}
	cmd := cmdFactory.Create("bitrise", args, nil)

	logger.Printf("$ %s", cmd.PrintableCommandArgs())

	out, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		if out != "" {
			logger.Printf("%s", out)
		}
		return fmt.Errorf("bundle-inspector failed: %w", err)
	}

	if out != "" {
		logger.Printf("%s", out)
	}

	return nil
}

// parseJSONReport extracts metrics from the JSON report
func parseJSONReport(jsonPath string, logger log.Logger) (BundleMetrics, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return BundleMetrics{}, fmt.Errorf("failed to read JSON report: %w", err)
	}

	var report struct {
		ArtifactInfo struct {
			Size          int64  `json:"size"`
			SizeFormatted string `json:"size_formatted"`
		} `json:"artifact_info"`
		PotentialSavings int64 `json:"potential_savings"`
	}

	if err := json.Unmarshal(data, &report); err != nil {
		return BundleMetrics{}, fmt.Errorf("failed to parse JSON report: %w", err)
	}

	sizeMB := fmt.Sprintf("%.2f", float64(report.ArtifactInfo.Size)/(1024*1024))

	return BundleMetrics{
		SizeBytes:             report.ArtifactInfo.Size,
		SizeMB:                sizeMB,
		PotentialSavingsBytes: report.PotentialSavings,
	}, nil
}

// deployReports copies generated reports to BITRISE_DEPLOY_DIR
func deployReports(formats []string, deployDir string, logger log.Logger) (ReportPaths, error) {
	var paths ReportPaths

	// Create deploy directory if it doesn't exist
	if err := os.MkdirAll(deployDir, 0755); err != nil {
		return paths, fmt.Errorf("failed to create deploy directory: %w", err)
	}

	formatMap := map[string]string{
		"text":     "analysis.txt",
		"json":     "analysis.json",
		"markdown": "analysis.md",
		"html":     "analysis.html",
	}

	for _, format := range formats {
		format = strings.TrimSpace(format)
		filename, ok := formatMap[format]
		if !ok {
			logger.Warnf("Unknown format: %s", format)
			continue
		}

		srcPath := filename
		dstPath := filepath.Join(deployDir, filename)

		// Check if source file exists
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			logger.Warnf("Report file not found: %s", srcPath)
			continue
		}

		// Copy file
		data, err := os.ReadFile(srcPath)
		if err != nil {
			logger.Warnf("Failed to read %s: %s", srcPath, err)
			continue
		}

		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			logger.Warnf("Failed to write %s: %s", dstPath, err)
			continue
		}

		logger.Printf("Deployed: %s", dstPath)

		// Store path in struct
		switch format {
		case "markdown":
			paths.Markdown = dstPath
		case "html":
			paths.HTML = dstPath
		case "json":
			paths.JSON = dstPath
		}
	}

	return paths, nil
}

// getLocalReportPaths returns paths to reports in the working directory
func getLocalReportPaths(formats []string) ReportPaths {
	var paths ReportPaths

	for _, format := range formats {
		format = strings.TrimSpace(format)
		switch format {
		case "markdown":
			paths.Markdown = "analysis.md"
		case "html":
			paths.HTML = "analysis.html"
		case "json":
			paths.JSON = "analysis.json"
		}
	}

	return paths
}

// isPullRequest checks if the current build is for a pull request
func isPullRequest() bool {
	prNumber := os.Getenv("BITRISE_PULL_REQUEST")
	return prNumber != "" && prNumber != "false"
}

// postGitHubComment posts the markdown report as a PR comment
func postGitHubComment(markdownPath, token string, logger log.Logger) error {
	if token == "" {
		return fmt.Errorf("github_token is required for posting PR comments")
	}

	prNumber := os.Getenv("BITRISE_PULL_REQUEST")
	if prNumber == "" || prNumber == "false" {
		return fmt.Errorf("not a pull request build")
	}

	// Check if markdown file exists
	if _, err := os.Stat(markdownPath); os.IsNotExist(err) {
		return fmt.Errorf("markdown report not found: %s", markdownPath)
	}

	// Use gh CLI to post comment
	cmdFactory := command.NewFactory(env.NewRepository())
	cmd := cmdFactory.Create("gh", []string{"pr", "comment", prNumber, "--body-file", markdownPath}, &command.Opts{
		Env: []string{fmt.Sprintf("GH_TOKEN=%s", token)},
	})

	logger.Printf("Posting comment to PR #%s...", prNumber)

	out, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		if out != "" {
			logger.Printf("%s", out)
		}
		return fmt.Errorf("gh pr comment failed: %w", err)
	}

	if out != "" {
		logger.Printf("%s", out)
	}

	return nil
}

// checkSizeThreshold validates the bundle size against the configured threshold
func checkSizeThreshold(cfg Config, sizeBytes int64, logger log.Logger) error {
	thresholdMB, err := strconv.ParseFloat(cfg.FailOnLargeSize, 64)
	if err != nil {
		logger.Warnf("Invalid fail_on_large_size value: %s", cfg.FailOnLargeSize)
		return nil
	}

	thresholdBytes := int64(thresholdMB * 1024 * 1024)
	sizeMB := float64(sizeBytes) / (1024 * 1024)

	logger.Infof("Checking size threshold: %.2f MB / %.2f MB", sizeMB, thresholdMB)

	if sizeBytes > thresholdBytes {
		return fmt.Errorf("bundle size %.2f MB exceeds threshold %.2f MB", sizeMB, thresholdMB)
	}

	logger.Donef("Bundle size is within threshold")
	return nil
}

// exportOutputs exports all output environment variables
func exportOutputs(metrics BundleMetrics, paths ReportPaths, commentPosted bool, logger log.Logger) error {
	envRepo := env.NewRepository()

	outputs := map[string]string{
		"BUNDLE_ANALYZER_REPORT_PATH":      paths.Markdown,
		"BUNDLE_ANALYZER_HTML_PATH":        paths.HTML,
		"BUNDLE_ANALYZER_JSON_PATH":        paths.JSON,
		"BUNDLE_SIZE_BYTES":                fmt.Sprintf("%d", metrics.SizeBytes),
		"BUNDLE_SIZE_MB":                   metrics.SizeMB,
		"BUNDLE_POTENTIAL_SAVINGS_BYTES":   fmt.Sprintf("%d", metrics.PotentialSavingsBytes),
		"BUNDLE_GITHUB_COMMENT_POSTED":     fmt.Sprintf("%t", commentPosted),
	}

	for key, value := range outputs {
		if err := envRepo.Set(key, value); err != nil {
			logger.Warnf("Failed to export %s: %s", key, err)
		} else {
			logger.Printf("Exported: %s=%s", key, value)
		}
	}

	return nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.TrimSpace(s) == item {
			return true
		}
	}
	return false
}
