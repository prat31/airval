# Getting started

Five minutes from zero to a first **Run**. Pick one install path, then follow [Your first Run](#your-first-run).

## Install

### Single binary (all-in-one)

```sh
git clone https://github.com/prat31/airval && cd airval
make build            # builds web/dist and bin/airval (CGO-free, UI embedded)
./bin/airval dev      # SQLite + embedded NATS in ./airval-data, http://localhost:8080
```

Or install a release binary (the script verifies the checksum):

```sh
curl -fsSL https://raw.githubusercontent.com/prat31/airval/main/scripts/install.sh | sh
brew install prat31/tap/airval        # alternatively
airval dev
```

`airval dev` runs the server, a worker and an embedded NATS JetStream in one process. State lives in `./airval-data`
(including a generated secrets master key in `airval-data/master.key`). Add `--addr :9000` to change the port and
`--open` to open the browser. Open <http://localhost:8080> and complete the setup screen (first owner + Organization).

`airval dev` is for trying things out. For anything shared, run `server` and `worker` against Postgres
(see [Deployment](deployment.md)).

### Docker

```sh
docker run -p 8080:8080 -v airval-data:/data ghcr.io/prat31/airval:latest dev
```

A production-shaped stack (Postgres 17, NATS JetStream, migrations, server, 2 workers):

```sh
cd deploy/docker && cp .env.example .env     # set AIRVAL_SECRETS_MASTER_KEY and POSTGRES_PASSWORD
docker compose up -d --build                 # http://localhost:8080
docker compose --profile mocktarget up -d    # also starts a fake AI service at http://mocktarget:9090
```

### Kubernetes (Helm)

```sh
helm dependency build deploy/helm/airval
helm install airval deploy/helm/airval -n airval --create-namespace \
  --set postgresql.external.url='postgres://user:pass@db:5432/airval?sslmode=require' \
  --set config.baseUrl=https://airval.example.com
helm test airval -n airval
```

See [Deployment](deployment.md) for the values reference.

## Your first Run

You need an AI service to test. For a quick trial use the bundled fake service, which speaks the OpenAI chat API:

```sh
go run ./tools/mocktarget -addr :9090      # answers are deterministic; ~80% "accurate"
```

In the UI: create a **Project**, add a **Target** (kind `openai`, base URL `http://localhost:9090/v1`, model `mock-1`),
use **Test connection**, import a **Dataset**, create a **Suite** with a `contains` **Evaluator**, add a **Gate**, press
**Run**. The same flow through the API follows. All of it was run against `airval dev`.

```sh
B=http://localhost:8080

# 1. First owner + organization (only works while no users exist). Keep the cookie jar.
curl -s -c jar -X POST $B/v1/setup -H 'content-type: application/json' \
  -d '{"email":"me@example.com","password":"correct-horse-1","org_name":"Acme"}' > me.json
CSRF=$(jq -r .csrf_token me.json); ORG=$(jq -r '.orgs[0].id' me.json)

# 2. An API key for scripts and CI (shown once)
curl -s -b jar -X POST $B/v1/orgs/$ORG/api-keys -H "X-CSRF-Token: $CSRF" \
  -H 'content-type: application/json' -d '{"name":"ci","role":"editor"}'      # -> {"key":"airv_...", ...}
export H="Authorization: Bearer airv_..."

# 3. Project and Target
PID=$(curl -s -H "$H" $B/v1/projects -H 'content-type: application/json' \
  -d "{\"org_id\":\"$ORG\",\"name\":\"Support bot\",\"slug\":\"support-bot\"}" | jq -r .id)
TID=$(curl -s -H "$H" $B/v1/projects/$PID/targets -H 'content-type: application/json' \
  -d '{"name":"mock-chat","kind":"openai","config":{"base_url":"http://localhost:9090/v1","model":"mock-1","api_key":"x"}}' | jq -r .id)
curl -s -H "$H" -H 'content-type: application/json' "$B/v1/targets/${TID}:test" \
  -d '{"example":{"input":{"question":"What is 2+2?"}}}'     # rendered request, raw response, mapped output

# 4. Dataset from CSV: preview, then import with a mapping and publish version 1
DID=$(curl -s -H "$H" $B/v1/projects/$PID/datasets -H 'content-type: application/json' -d '{"name":"faq"}' | jq -r .id)
curl -s -H "$H" -F file=@faq.csv "$B/v1/datasets/${DID}:import?preview=true"
curl -s -H "$H" -F file=@faq.csv -F 'options={"mapping":{"input_columns":{"question":"question"},"expected_column":"answer","tags_column":"topic"},"publish":true,"source":{"format":"csv"}}' \
  "$B/v1/datasets/${DID}:import"

# 5. Evaluator, Suite (+ Gate)
EV=$(curl -s -H "$H" $B/v1/projects/$PID/evaluators -H 'content-type: application/json' \
  -d '{"name":"has-answer","kind":"contains"}' | jq -r .id)
SID=$(curl -s -H "$H" $B/v1/projects/$PID/suites -H 'content-type: application/json' -d "{
  \"name\":\"Smoke\",\"slug\":\"smoke\",
  \"config\":{\"target_ids\":[\"$TID\"],\"datasets\":[{\"dataset_id\":\"$DID\"}],
              \"evaluators\":[{\"evaluator_id\":\"$EV\",\"threshold\":1,\"severity\":\"major\"}],\"trials\":1}}" | jq -r .id)
curl -s -H "$H" $B/v1/suites/$SID/gates -H 'content-type: application/json' \
  -d '{"name":"quality","text":"pass_rate(has-answer) >= 0.5 and count(critical_fail) == 0"}'

# 6. Run it, wait, and gate the exit code
export AIRVAL_URL=$B AIRVAL_API_KEY=airv_...
airval run --suite smoke --project support-bot --gate
```

`faq.csv` used above:

```csv
question,answer,topic
What is 2+2?,4,math
What is the capital of France?,Paris,geo
How do I reset my password?,Use the reset link on the sign-in page,account
```

Example output (the fake service answers two of the three questions incorrectly, so the Gate fails and the exit code is 1):

```
FAILED  Smoke · mock-chat
  results   3/3 · 2 failed · 0 errored · pass rate 33.3%
  EVALUATOR     MEAN  PASS RATE   FAILED
  has-answer   0.333      33.3%        2
  GATES
  [FAIL] quality: pass_rate(has-answer) >= 0.5 and count(critical_fail) == 0 — pass_rate(has-answer) >= 0.5: got 0.3333
```

Next: [Concepts](concepts.md), then [Targets](targets.md) to point airval at your real service.
