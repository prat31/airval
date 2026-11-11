# CI

Gate your pipeline on a Suite with `airval run`. It starts a Run through the public API, follows it live, prints a
summary and exits non-zero when the [Gate](gates.md) fails.

```sh
export AIRVAL_URL=https://airval.example.com
export AIRVAL_API_KEY=airv_...            # an editor API key, stored as a CI secret
airval run --suite my-suite --project support-bot --label git_sha=$GIT_SHA --gate
```

Install the CLI with `curl -fsSL https://raw.githubusercontent.com/prat31/airval/main/scripts/install.sh | sh`
(`--version v0.1.0`, `--dir ~/bin`, or the `AIRVAL_VERSION` / `AIRVAL_INSTALL_DIR` variables), `brew install prat31/tap/airval`, or use the Docker image.

## `airval run` flags

| Flag | Default | Description |
|---|---|---|
| `--suite` | required | Suite id or slug. |
| `--project` | | Project id or slug; narrows slug lookup. Env `AIRVAL_PROJECT`. |
| `--target` | the Suite's Targets | Target id; repeatable (one Run each). |
| `--server` | `http://localhost:8080` | Server URL. Env `AIRVAL_URL`. |
| `--api-key` | | API key. Env `AIRVAL_API_KEY`. |
| `--wait` | false | Wait for the Run(s) to finish and print a summary. |
| `--gate` | false | Implies waiting. Exit 1 unless every Run succeeded with its Gates passed. |
| `--junit [file]` | | Write JUnit XML to the file (stdout if no file). |
| `--markdown [file]` | | Write a Markdown summary for PR comments (stdout if no file). |
| `--format` | `text` | Summary output: `text` or `json`. |
| `--label key=value` | | Run label; repeatable. Use `git_sha`, `branch`, `pr`. |
| `--name` | | Run name. |
| `--sample` | | `N` examples or `N%`. |
| `--sample-mode` | `random` | `random` or `stratified` (per tag). |
| `--trials` | the Suite's | Repeat each Example N times (1 to 10). |
| `--var key=value` | | Override a target variable (`{{ .vars.key }}`); repeatable. |
| `--timeout` | `30m` | Overall timeout. |
| `--no-progress` | false | Do not show live progress. |
| `-c, --config` | | Config file; env `AIRVAL_CONFIG`. |

Exit codes: `0` success; `1` a Gate failed, a Run did not succeed, or the command failed.
Typical PR check: a fast sample, gated, with the report written for the CI UI.

```sh
airval run --suite my-suite --sample 10% --sample-mode stratified --gate \
  --junit results.xml --markdown summary.md \
  --label git_sha=$GITHUB_SHA --var base_url=https://preview-$PR.example.com
```

## Reports

- **JUnit XML**: one `testsuite` per Run, with a `run completes` testcase and one testcase per Gate (failures carry the measured value), so CI systems list failing Gates natively.
- **Markdown**: a verdict header with a link to the Run, a results table, Gate verdicts, an Evaluator table and a collapsible list of failing Results with the reason. Written for PR comments.

## GitHub Actions

The repo ships a composite action that installs the CLI, runs the Suite with `--gate`, adds the Markdown to the job
summary, posts or updates a PR comment, and enforces the verdict last (so the comment is always posted).

```yaml
name: eval
on: pull_request
permissions: { contents: read, pull-requests: write }
jobs:
  airval:
    runs-on: ubuntu-latest
    steps:
      - uses: prat31/airval/.github/actions/airval-run@main
        with:
          server-url: https://airval.example.com
          api-key: ${{ secrets.AIRVAL_API_KEY }}
          suite: my-suite
          project: <project-id>
```

| Input | Default | Description |
|---|---|---|
| `server-url` | required | Base URL of your server. |
| `api-key` | required | API key (use a secret). |
| `suite` | required | Suite id or slug. |
| `project` | | Project id (needed when the Suite is given by slug). |
| `target` | the Suite's | Target id(s), comma separated. |
| `labels` | | Extra labels, one `key=value` per line. `git_sha`, `git_ref` and `run_url` are added automatically. |
| `timeout` | `30m` | Overall timeout. |
| `version` | `latest` | CLI release to install. |
| `comment` | `true` | Post or update a PR comment (pull_request events only). |
| `comment-tag` | `airval` | Distinguishes comments when several Suites run in one PR. |
| `github-token` | `github.token` | Needs `pull-requests: write`. On forks the comment is skipped with a warning and the job summary still has the results. |

Outputs: `exit-code` and `summary-file`.

## GitLab CI

The airval image is distroless (no shell), which GitLab runners need, so use a small image and the install script.

```yaml
airval:
  stage: test
  image: alpine:3.20
  variables:
    AIRVAL_URL: https://airval.example.com
    AIRVAL_INSTALL_DIR: /usr/local/bin
    # AIRVAL_API_KEY: set as a masked CI/CD variable
  before_script:
    - apk add --no-cache curl
    - curl -fsSL https://raw.githubusercontent.com/prat31/airval/main/scripts/install.sh | sh
  script:
    - airval run --suite my-suite --project support-bot --gate
        --junit airval-junit.xml --markdown airval-summary.md
        --label git_sha=$CI_COMMIT_SHA --label branch=$CI_COMMIT_REF_NAME
  artifacts:
    when: always
    reports:
      junit: airval-junit.xml
    paths: [airval-summary.md]
```

## Triggering from a webhook instead

If CI can only send HTTP requests, use a [webhook Trigger](runs.md#webhook): the deploy event creates the Run, and
`payload_variables` can point the Target at the preview deployment. Webhook Runs are asynchronous, so use `airval run`
when you need to block the pipeline on the verdict.
