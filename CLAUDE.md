# Bitrise Bundle Analyzer Step - Reference Documentation

## Project Overview

**Purpose:** A Bitrise step that analyzes iOS and Android application bundles, generates comprehensive size reports, and integrates with GitHub Pull Requests.

**Repository:** `/Users/birmacher/dev/steps-bundle-analyzer`

**Technology Stack:**
- Language: Go 1.21+
- Framework: Bitrise Step conventions
- Core dependency: bitrise-plugins-bundle-inspector

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Bitrise Workflow                     │
│  (User configures step in bitrise.yml or Web Editor)   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│              steps-bundle-analyzer (Go)                 │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 1. Parse inputs via stepconf                    │   │
│  │ 2. Detect artifact (IPA/APK/AAB)                │   │
│  │ 3. Invoke bundle-inspector CLI                  │   │
│  │ 4. Parse JSON output for metrics                │   │
│  │ 5. Deploy reports to BITRISE_DEPLOY_DIR         │   │
│  │ 6. Detect PR context & post comment             │   │
│  │ 7. Export outputs (sizes, paths, etc.)          │   │
│  │ 8. Check size thresholds                        │   │
│  └─────────────────────────────────────────────────┘   │
└────────────┬──────────────────────┬─────────────────────┘
             │                      │
             ▼                      ▼
┌────────────────────┐    ┌────────────────────┐
│ bundle-inspector   │    │  GitHub API        │
│     (Plugin)       │    │  (PR Comments)     │
│                    │    │                    │
│ • Analyzes bundles │    │ • Post markdown    │
│ • Detects dupes    │    │ • Update existing  │
│ • Generates reports│    │ • Link to reports  │
└────────────────────┘    └────────────────────┘
```

### Data Flow

1. **Input:** Artifact path (explicit or auto-detected from Bitrise env vars)
2. **Processing:** Bundle-inspector analyzes structure, sizes, duplicates
3. **Output:**
   - Reports (markdown, HTML, JSON) → BITRISE_DEPLOY_DIR
   - Metrics → Environment variables
   - Summary → GitHub PR comment (if applicable)

## Key Components

### 1. step.yml

**Purpose:** Defines the step's interface (inputs, outputs, metadata)

**Critical Sections:**
- `inputs`: User-configurable parameters (artifact path, formats, thresholds)
- `outputs`: Environment variables exported by the step
- `toolkit.go`: Specifies Go-based step implementation
- `type_tags`: Categorization for Bitrise Step Library

**Key Design Decision:**
- Use `is_required: false` for `artifact_path` to enable auto-detection
- Default `output_formats` to "markdown,html" for best UX
- Mark `github_token` as `is_sensitive: true` for security

### 2. main.go

**Purpose:** Core orchestration logic wrapping bundle-inspector

**Architecture Pattern:**
```go
main()
  ├─ Parse config (stepconf)
  ├─ Validate inputs
  ├─ Detect artifact
  ├─ Run bundle-inspector CLI
  ├─ Parse JSON output
  ├─ Deploy reports
  ├─ Handle PR comments
  ├─ Export outputs
  └─ Check thresholds
```

**Key Functions:**

- **`detectArtifact(cfg Config) (string, error)`**
  - Priority: artifact_path → BITRISE_IPA_PATH → BITRISE_AAB_PATH → BITRISE_APK_PATH
  - Validates file exists before returning

- **`runBundleInspector(artifactPath, formats string) error`**
  - Builds command: `bitrise :bundle-inspector analyze <path> -o <formats>`
  - Duplicate detection always enabled (plugin default behavior)
  - Uses `command.New()` for proper logging integration
  - Returns error if exit code non-zero

- **`parseJSONReport(jsonPath string) (BundleMetrics, error)`**
  - Extracts: size_bytes, size_mb, potential_savings_bytes
  - Uses `encoding/json` stdlib
  - Handles missing fields gracefully

- **`deployReports(formats []string, deployDir string) error`**
  - Copies generated files to BITRISE_DEPLOY_DIR
  - Maintains original filenames (analysis.md, analysis.html, etc.)
  - Creates deploy dir if not exists

- **`isPullRequest() bool`**
  - Checks `BITRISE_PULL_REQUEST` env var
  - Returns false if empty or "false"

- **`postGitHubComment(markdownPath, token string) error`**
  - Uses `gh pr comment <number> --body-file <path>` CLI
  - No fallback needed - gh CLI is pre-installed on Bitrise stacks
  - Returns error if fails, but caller can decide to warn vs fail

- **`checkSizeThreshold(cfg Config, sizeBytes int64) error`**
  - Implements `fail_on_large_size` logic
  - Returns error with descriptive message on threshold violation

- **`exportOutputs(metrics BundleMetrics, paths ReportPaths, commentPosted bool) error`**
  - Uses `env.NewRepository().Set()` for each output
  - Exports all 7 output variables
  - Logs each export for debugging

### 3. bitrise.yml

**Purpose:** Integration testing workflows

**Test Workflows:**

- **`test_ios_ipa`**: End-to-end test with iOS artifact
  - Setup: Export BITRISE_IPA_PATH
  - Execute: Run step with markdown,html,json outputs
  - Verify: Check all output paths and env vars exist

- **`test_android_apk`**: End-to-end test with Android artifact
  - Setup: Export BITRISE_APK_PATH
  - Execute: Run step with default settings
  - Verify: Reports generated correctly

- **`test_size_threshold`**: Validate threshold enforcement
  - Setup: Use large test artifact
  - Execute: Run step with fail_on_large_size=1
  - Verify: Step exits with error code

- **`test_pr_simulation`**: Mock PR environment
  - Setup: Export BITRISE_PULL_REQUEST=123, BITRISEIO_GIT_REPOSITORY_SLUG
  - Execute: Run step with post_github_comment=no (don't actually post)
  - Verify: PR detection works, comment skipped gracefully

**Common Pattern:**
```yaml
workflows:
  test_xxx:
    steps:
    - script:  # Setup test environment
    - path::./:  # Run local step
        inputs:
          - input_key: "value"
    - script:  # Verify outputs
```

## Bitrise Integration Points

### Environment Variables

**Standard Bitrise Inputs (Read by step):**
- `BITRISE_IPA_PATH` - Path to iOS IPA file
- `BITRISE_APK_PATH` - Path to Android APK file
- `BITRISE_AAB_PATH` - Path to Android App Bundle
- `BITRISE_DEPLOY_DIR` - Directory for build artifacts
- `BITRISE_PULL_REQUEST` - PR number (or empty/false)
- `BITRISEIO_GIT_REPOSITORY_SLUG` - Repository in format "owner/repo"
- `GIT_ACCESS_TOKEN` - GitHub token for API access

**Step Configuration Inputs:**
- `artifact_path` (optional) - Override auto-detection
- `output_formats` (default: "markdown,html") - Report formats
- `post_github_comment` (default: "auto") - PR comment behavior
- `github_token` (optional) - GitHub token override
- `fail_on_large_size` (optional) - Size threshold in MB

**Outputs (Written by step):**
- `BUNDLE_ANALYZER_REPORT_PATH` - Path to markdown report
- `BUNDLE_ANALYZER_HTML_PATH` - Path to HTML report
- `BUNDLE_ANALYZER_JSON_PATH` - Path to JSON report
- `BUNDLE_SIZE_BYTES` - Bundle size (compressed)
- `BUNDLE_SIZE_MB` - Bundle size in megabytes
- `BUNDLE_POTENTIAL_SAVINGS_BYTES` - Optimization potential
- `BUNDLE_GITHUB_COMMENT_POSTED` - "true" or "false"

### Report Deployment

Reports are copied to `$BITRISE_DEPLOY_DIR` where they become downloadable artifacts:
- Accessible via Bitrise web UI under "Apps & Artifacts"
- Available to subsequent steps in the workflow
- Retained according to workspace retention policy
- Shareable via direct links

### GitHub Pull Request Integration

**PR Detection:**
```go
// Check if running in PR context
prNumber := os.Getenv("BITRISE_PULL_REQUEST")
if prNumber != "" && prNumber != "false" {
    // This is a PR build
}
```

**Comment Posting:**
```bash
# Using gh CLI (pre-installed on Bitrise)
gh pr comment <pr-number> --body-file analysis.md
```

**Comment Format Example:**
```markdown
## 📦 Bundle Analysis Report

| Metric | Value |
|--------|-------|
| Bundle Size | 42.3 MB |
| Potential Savings | 8.7 MB (20.5%) |
| Duplicate Files | 15 files |

<details>
<summary>🔍 Top 10 Largest Files</summary>

1. Frameworks/MyFramework.framework (12.3 MB)
2. Assets.car (8.5 MB)
...

</details>

<details>
<summary>📊 Optimization Recommendations</summary>

**HIGH Priority:**
- Remove duplicate SDKResourceBundle.bundle (2.3 MB savings)

**MEDIUM Priority:**
- Optimize @3x images (1.2 MB savings)

</details>

[📄 View Full HTML Report](https://bitrise.io/artifacts/...)

---
*Generated by [Bundle Analyzer](https://github.com/bitrise-io/steps-bundle-analyzer)*
```

## Bundle Inspector Plugin Reference

### Installation
```bash
bitrise plugin install https://github.com/bitrise-io/bitrise-plugins-bundle-inspector.git
```

### Command Syntax
```bash
bitrise :bundle-inspector analyze [artifact-path] [flags]
```

### Flags
- `-o, --output string` - Output formats (comma-separated: text,json,markdown,html)
- `-f, --output-file string` - Custom output filenames
- `--include-duplicates` - Enable duplicate detection (default: true)
- `--no-auto-detect` - Disable Bitrise env var detection

### Auto-Detection Priority
1. `BITRISE_IPA_PATH`
2. `BITRISE_AAB_PATH`
3. `BITRISE_APK_PATH`

### Output Files
- `analysis.txt` - Plain text report
- `analysis.json` - Machine-readable JSON
- `analysis.md` - Markdown report
- `analysis.html` - Interactive HTML with charts

### JSON Schema (Excerpt)
```json
{
  "artifact_info": {
    "path": "/path/to/app.ipa",
    "type": "iOS IPA",
    "size": 44371200,
    "size_formatted": "42.3 MB"
  },
  "size_breakdown": {
    "executable": 8388608,
    "frameworks": 20971520,
    "assets": 10485760,
    "other": 4525312
  },
  "largest_files": [
    {"path": "Frameworks/MyFramework.framework", "size": 12884901888}
  ],
  "duplicates": [
    {
      "hash": "abc123...",
      "size": 2097152,
      "count": 3,
      "paths": ["path1", "path2", "path3"]
    }
  ],
  "potential_savings": 9175040
}
```

## Error Handling Patterns

### Critical Errors (Exit with code 1)
- **No artifact found**: Neither explicit path nor env vars provide artifact
- **Artifact doesn't exist**: Path points to non-existent file
- **Plugin not installed**: bundle-inspector not available
- **Analysis fails**: Plugin exits with non-zero code
- **Size threshold exceeded**: Bundle size violates configured limits

### Warnings (Log but continue)
- **Deploy dir not set**: Save reports in working directory
- **PR comment fails**: Log warning, don't fail build (in auto mode)
- **Output export fails**: Log warning, partial outputs acceptable
- **JSON parsing fails**: Use empty metrics, log warning

### Graceful Degradation
```go
// Example: PR comment in auto mode
if cfg.PostGithubComment == "auto" {
    if err := postGitHubComment(markdownPath, cfg.GithubToken); err != nil {
        logger.Warnf("Failed to post PR comment (non-fatal): %s", err)
        // Continue execution
    }
} else if cfg.PostGithubComment == "yes" {
    if err := postGitHubComment(markdownPath, cfg.GithubToken); err != nil {
        return fmt.Errorf("failed to post PR comment: %w", err)
    }
}
```

## Usage Examples

### Basic Usage (Auto-detection)
```yaml
workflows:
  primary:
    steps:
    - xcode-archive@4:
        # Generates BITRISE_IPA_PATH
    - bundle-analyzer@1:
        # Auto-detects IPA, generates markdown + HTML
```

### Advanced Usage (All Features)
```yaml
workflows:
  primary:
    steps:
    - gradle-runner@2:
        # Generates BITRISE_APK_PATH or BITRISE_AAB_PATH
    - bundle-analyzer@1:
        inputs:
        - output_formats: "markdown,html,json"
        - include_duplicates: "yes"
        - post_github_comment: "auto"
        - github_token: "$GITHUB_TOKEN"
        - fail_on_large_size: "50"  # 50 MB limit
        - fail_on_size_increase: "5"  # Max 5 MB increase
    - script@1:
        title: Archive previous size for next build
        inputs:
        - content: |
            #!/bin/bash
            envman add --key PREVIOUS_BUNDLE_SIZE --value "$BUNDLE_SIZE_BYTES"
```

### Multiple Artifacts (e.g., iOS + watchOS)
```yaml
workflows:
  primary:
    steps:
    - xcode-archive@4:
        # Generates main app IPA
    - bundle-analyzer@1:
        title: Analyze iOS App
        inputs:
        - artifact_path: "$BITRISE_IPA_PATH"
    - bundle-analyzer@1:
        title: Analyze Watch App
        inputs:
        - artifact_path: "$BITRISE_WATCH_IPA_PATH"
        - post_github_comment: "no"  # Only comment once
```

## Testing Strategy

### Unit Tests (Go)
```go
func TestDetectArtifact(t *testing.T) {
    tests := []struct {
        name     string
        envVars  map[string]string
        want     string
        wantErr  bool
    }{
        {
            name: "explicit path takes priority",
            envVars: map[string]string{
                "artifact_path": "/explicit/path.ipa",
                "BITRISE_IPA_PATH": "/auto/path.ipa",
            },
            want: "/explicit/path.ipa",
        },
        {
            name: "auto-detect IPA",
            envVars: map[string]string{
                "BITRISE_IPA_PATH": "/auto/path.ipa",
            },
            want: "/auto/path.ipa",
        },
        {
            name: "no artifact found",
            envVars: map[string]string{},
            wantErr: true,
        },
    }
    // Test implementation...
}
```

### Integration Tests (bitrise.yml)
```yaml
workflows:
  ci:
    steps:
    - go-test@0:
    - golint@0:
    - script:
        title: Run integration tests
        inputs:
        - content: |
            bitrise run test_ios_ipa
            bitrise run test_android_apk
            bitrise run test_size_threshold
            bitrise run test_pr_simulation
```

## Dependencies

### Go Modules
```go
require (
    github.com/bitrise-io/go-steputils v1.0.5
    github.com/bitrise-io/go-utils/v2 v2.0.0
)
```

### Runtime Requirements
- **bitrise CLI** (version 1.3.0+)
- **bundle-inspector plugin** (auto-installed if missing)
- **gh CLI** (optional, pre-installed on Bitrise stacks)

## Best Practices

### 1. Use Auto Mode for PR Comments
```yaml
- bundle-analyzer@1:
    inputs:
    - post_github_comment: "auto"  # Safe default
```

### 2. Set Size Thresholds for Regression Prevention
```yaml
- bundle-analyzer@1:
    inputs:
    - fail_on_large_size: "50"  # App Store limit consideration
```

### 3. Generate All Formats for Maximum Value
```yaml
- bundle-analyzer@1:
    inputs:
    - output_formats: "markdown,html,json"
    # markdown → PR comments
    # html → stakeholder review
    # json → CI/CD automation
```

## Troubleshooting

### Issue: "No artifact found"
**Cause:** Neither artifact_path nor Bitrise env vars set
**Solution:** Ensure artifact-generating step runs first (xcode-archive, gradle-runner)

### Issue: "Bundle-inspector plugin not installed"
**Cause:** Plugin missing from Bitrise stack
**Solution:** Add installation step or use stack with plugin pre-installed

### Issue: "Failed to post PR comment"
**Cause:** Missing/invalid GitHub token or not a PR build
**Solution:**
- Set GIT_ACCESS_TOKEN secret
- Use post_github_comment: "auto" for graceful handling

### Issue: "Size threshold exceeded"
**Cause:** Bundle larger than configured limit
**Solution:**
- Investigate with HTML report
- Address optimization recommendations
- Adjust threshold if intentional

## Future Enhancements

### Planned Features
- **Size trend charts**: Visualize size changes over time
- **Baseline comparison**: Compare against main branch
- **Slack notifications**: Post summaries to Slack channels
- **Custom rules**: User-defined optimization rules
- **Cache suggestions**: Recommend caching strategies

### Extension Points
- **Custom reporters**: Plugin system for additional output formats
- **Webhook support**: POST results to external services
- **Delta reports**: Show only changes since last build

---

**Document Version:** 1.0
**Last Updated:** 2026-02-03
**Author:** Claude (Anthropic)
**Status:** Implementation Ready
