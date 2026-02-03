# Bundle Analyzer

Analyze iOS and Android application bundles and generate comprehensive size reports with optimization recommendations.

## Features

- **Automatic Artifact Detection**: Automatically finds your iOS (.ipa) or Android (.apk, .aab) artifacts from Bitrise environment variables
- **Multiple Report Formats**: Generate text, markdown, HTML, and JSON reports
- **Duplicate Detection**: Identify duplicate files and potential size savings
- **Size Breakdown**: Detailed analysis of bundle components (executable, frameworks, assets, etc.)
- **GitHub PR Integration**: Automatically post analysis summaries as PR comments
- **Size Threshold Enforcement**: Fail builds that exceed configured size limits
- **Optimization Recommendations**: Get actionable suggestions to reduce bundle size

## Usage

### Basic Usage (Auto-detection)

Add the step to your `bitrise.yml` after your build step:

```yaml
workflows:
  primary:
    steps:
    - xcode-archive@4:
        # Generates BITRISE_IPA_PATH
    - bundle-analyzer@1:
        # Auto-detects IPA and generates markdown + HTML reports
```

### Advanced Usage

```yaml
workflows:
  primary:
    steps:
    - gradle-runner@2:
        # Generates BITRISE_APK_PATH or BITRISE_AAB_PATH
    - bundle-analyzer@1:
        inputs:
        - artifact_path: "$BITRISE_APK_PATH"  # Optional: explicit path
        - output_formats: "markdown,html,json"  # Generate all formats
        - post_github_comment: "auto"  # Post comment if PR
        - github_token: "$GITHUB_TOKEN"  # GitHub access token
        - fail_on_large_size: "50"  # Fail if bundle > 50 MB
```

### Multiple Artifacts

Analyze multiple artifacts (e.g., iOS app + Watch app):

```yaml
workflows:
  primary:
    steps:
    - xcode-archive@4:
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

## Inputs

| Input | Description | Default | Required |
|-------|-------------|---------|----------|
| `artifact_path` | Path to artifact (.ipa, .apk, .aab). If empty, auto-detects from `BITRISE_IPA_PATH`, `BITRISE_AAB_PATH`, or `BITRISE_APK_PATH` | - | No |
| `output_formats` | Comma-separated report formats: `text`, `json`, `markdown`, `html` | `markdown,html` | Yes |
| `post_github_comment` | Post PR comment: `auto` (if PR + token available), `yes` (always), `no` (never) | `auto` | Yes |
| `github_token` | GitHub personal access token for PR comments | `$GIT_ACCESS_TOKEN` | No |
| `fail_on_large_size` | Maximum bundle size in MB. Build fails if exceeded. Leave empty to disable. | - | No |

## Outputs

| Output | Description | Example |
|--------|-------------|---------|
| `BUNDLE_ANALYZER_REPORT_PATH` | Path to markdown report | `/tmp/deploy/analysis.md` |
| `BUNDLE_ANALYZER_HTML_PATH` | Path to HTML report | `/tmp/deploy/analysis.html` |
| `BUNDLE_ANALYZER_JSON_PATH` | Path to JSON report | `/tmp/deploy/analysis.json` |
| `BUNDLE_SIZE_BYTES` | Bundle size in bytes | `44371200` |
| `BUNDLE_SIZE_MB` | Bundle size in MB | `42.31` |
| `BUNDLE_POTENTIAL_SAVINGS_BYTES` | Potential size savings | `9175040` |
| `BUNDLE_GITHUB_COMMENT_POSTED` | Whether PR comment was posted | `true` or `false` |

## GitHub PR Comments

When running in a pull request context, the step can automatically post a summary comment:

### Example PR Comment

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
```

### Setting Up PR Comments

1. Create a GitHub personal access token with `repo` scope
2. Add it as a secret in Bitrise: `GITHUB_TOKEN`
3. Set the input: `github_token: "$GITHUB_TOKEN"`

The step will automatically:
- Detect if running in a PR context
- Post the markdown report as a comment
- Update existing comments instead of creating duplicates

## Size Threshold Example

Enforce bundle size limits to prevent regressions:

```yaml
- bundle-analyzer@1:
    inputs:
    - fail_on_large_size: "50"  # Fail if bundle > 50 MB
```

If the bundle exceeds the threshold, the step will fail with:
```
Bundle size 52.45 MB exceeds threshold 50.00 MB
```

## Report Formats

### Markdown
- Suitable for PR comments
- Tables and collapsible sections
- Easy to read in web interfaces

### HTML
- Interactive charts and visualizations
- Sortable tables
- Detailed drill-down views
- Best for stakeholder reviews

### JSON
- Machine-readable
- Ideal for CI/CD automation
- Contains all metrics and file listings

### Text
- Plain text output
- Suitable for log viewing
- Quick terminal review

## Troubleshooting

### "No artifact found"
**Cause**: Neither `artifact_path` input nor Bitrise environment variables are set.

**Solution**: Ensure your build step (like `xcode-archive` or `gradle-runner`) runs before the Bundle Analyzer step. These steps set the required environment variables (`BITRISE_IPA_PATH`, `BITRISE_APK_PATH`, or `BITRISE_AAB_PATH`).

### "Failed to post PR comment"
**Cause**: Missing or invalid GitHub token, or not a PR build.

**Solution**:
1. Ensure `github_token` is set (defaults to `$GIT_ACCESS_TOKEN`)
2. Verify the token has `repo` scope for private repos
3. Use `post_github_comment: "auto"` for graceful handling

### "Bundle-inspector plugin not installed"
**Cause**: The bundle-inspector plugin is not available on the Bitrise stack.

**Solution**: The plugin should be pre-installed on Bitrise stacks. If not, add an installation step:
```yaml
- script:
    inputs:
    - content: bitrise plugin install https://github.com/bitrise-io/bitrise-plugins-bundle-inspector.git
```

## Development

### Running Locally

```bash
# Install dependencies
go mod download

# Set up test environment
export BITRISE_IPA_PATH=/path/to/test.ipa
export BITRISE_DEPLOY_DIR=/tmp/deploy

# Run the step
go run main.go
```

### Running Tests

```bash
# Run integration tests
bitrise run ci

# Run individual test
bitrise run test_ios_ipa
```

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Support

For issues and feature requests, please use the [GitHub issue tracker](https://github.com/bitrise-io/steps-bundle-analyzer/issues).
