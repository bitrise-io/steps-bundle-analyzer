package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

// Config holds the step configuration
type Config struct {
	ArtifactPath      string `env:"artifact_path"`
	OutputFormats     string `env:"output_formats,required"`
	PostGithubComment string `env:"post_github_comment"`
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

	// Ensure bundle-inspector plugin is installed
	logger.Println()
	if err := ensureBundleInspectorInstalled(logger); err != nil {
		logger.Errorf("Failed to ensure bundle-inspector is installed: %s", err)
		os.Exit(1)
	}

	// Run bundle-inspector
	logger.Println()
	logger.Infof("Running bundle-inspector analysis...")
	if err := runBundleInspector(artifactPath, cfg.OutputFormats, logger); err != nil {
		logger.Errorf("Bundle analysis failed: %s", err)
		os.Exit(1)
	}

	// Find generated report files
	logger.Println()
	logger.Infof("Locating generated report files...")
	generatedFiles, err := findGeneratedReports(logger)
	if err != nil {
		logger.Warnf("Failed to locate reports: %s", err)
	}

	// Parse JSON report to extract metrics
	var metrics BundleMetrics
	formats := strings.Split(cfg.OutputFormats, ",")
	if contains(formats, "json") && generatedFiles.JSON != "" {
		logger.Println()
		logger.Infof("Parsing JSON report for metrics...")
		metrics, err = parseJSONReport(generatedFiles.JSON, logger)
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
		reportPaths, err = deployReportsFromFiles(generatedFiles, deployDir, logger)
		if err != nil {
			logger.Warnf("Failed to deploy reports: %s", err)
		}
	} else {
		logger.Warnf("BITRISE_DEPLOY_DIR not set, reports will remain in working directory")
		// Use generated files as-is
		reportPaths = generatedFiles
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

// ensureBundleInspectorInstalled checks if bundle-inspector is installed and installs it if needed
func ensureBundleInspectorInstalled(logger log.Logger) error {
	cmdFactory := command.NewFactory(env.NewRepository())

	// Check if plugin is installed
	logger.Infof("Checking for bundle-inspector plugin...")
	checkCmd := cmdFactory.Create("bitrise", []string{"plugin", "list"}, nil)
	out, err := checkCmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check installed plugins: %w", err)
	}

	// Check if bundle-inspector is in the list
	if strings.Contains(out, "bundle-inspector") {
		logger.Donef("bundle-inspector plugin is already installed")
		return nil
	}

	// Plugin not installed, install it
	logger.Warnf("bundle-inspector plugin not found, installing...")
	installCmd := cmdFactory.Create("bitrise", []string{"plugin", "install", "https://github.com/bitrise-io/bitrise-plugins-bundle-inspector.git"}, nil)

	logger.Printf("$ %s", installCmd.PrintableCommandArgs())

	installOut, err := installCmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		if installOut != "" {
			logger.Printf("%s", installOut)
		}
		return fmt.Errorf("failed to install bundle-inspector plugin: %w", err)
	}

	if installOut != "" {
		logger.Printf("%s", installOut)
	}

	logger.Donef("bundle-inspector plugin installed successfully")
	return nil
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

	// Only print output in debug mode to avoid duplicate logging
	// (we'll log the located files separately)
	if out != "" && os.Getenv("BITRISE_STEP_DEBUG") == "true" {
		logger.Printf("%s", out)
	}

	return nil
}

// findGeneratedReports locates the report files generated by bundle-inspector
func findGeneratedReports(logger log.Logger) (ReportPaths, error) {
	var paths ReportPaths

	// Bundle-inspector generates files with pattern: bundle-analysis-*.{md,html,json}
	patterns := map[string]*string{
		"bundle-analysis-*.md":   &paths.Markdown,
		"bundle-analysis-*.html": &paths.HTML,
		"bundle-analysis-*.json": &paths.JSON,
	}

	for pattern, pathPtr := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			logger.Warnf("Failed to glob pattern %s: %s", pattern, err)
			continue
		}

		if len(matches) > 0 {
			*pathPtr = matches[0]
			logger.Printf("Found: %s", matches[0])
		}
	}

	return paths, nil
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

// deployReportsFromFiles copies generated reports to BITRISE_DEPLOY_DIR
func deployReportsFromFiles(generatedFiles ReportPaths, deployDir string, logger log.Logger) (ReportPaths, error) {
	var paths ReportPaths

	// Create deploy directory if it doesn't exist
	if err := os.MkdirAll(deployDir, 0755); err != nil {
		return paths, fmt.Errorf("failed to create deploy directory: %w", err)
	}

	// Copy each found report file
	filesToCopy := map[string]*string{
		generatedFiles.Markdown: &paths.Markdown,
		generatedFiles.HTML:     &paths.HTML,
		generatedFiles.JSON:     &paths.JSON,
	}

	for srcPath, dstPathPtr := range filesToCopy {
		if srcPath == "" {
			continue
		}

		// Use the same filename in deploy directory
		filename := filepath.Base(srcPath)
		dstPath := filepath.Join(deployDir, filename)

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
		*dstPathPtr = dstPath
	}

	return paths, nil
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
		if err := tools.ExportEnvironmentWithEnvman(key, value); err != nil {
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
