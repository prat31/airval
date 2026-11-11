# Datasets

A **Dataset** is a collection of **Examples**. Edits accumulate in a draft; **Publish** freezes the draft into an
immutable **Dataset Version** (1, 2, 3, ...). A version is a list of example revisions, so unchanged Examples are not
copied. Suites pin a version or follow the latest published one.

## An Example

| Field | Description |
|---|---|
| `input` | Object of template variables, e.g. `{"question": "..."}`. Referenced as `{{ .input.question }}` in Targets. |
| `expected` | Reference answer (a string or structured value). Used by `equals`, `contains`, `correctness`, ... |
| `context` | Gold context passages (strings), used by `context_recall` and similar. |
| `metadata` | Free-form object. |
| `tags` | Strings. Used for per-tag aggregates, stratified sampling and filtering. |
| `conversation` | Multi-turn `turns` or a `simulation` (see [Targets](targets.md#multi-turn-scripts-and-simulation)). |

## Importing

Formats: **CSV**, **JSONL**, **JSON**, **Parquet**, pasted text (`inline`), and public **Hugging Face** datasets.
Import is streaming (constant memory) and runs in two phases:

1. **Preview**: `POST /v1/datasets/{dataset}:import?preview=true` parses the first rows without writing and returns
   `source_preview` (columns, inferred types, sample values, rows) and a `suggested_mapping`. With a `mapping` it also
   returns the mapped `preview` Examples.
2. **Commit**: the same request without `preview`, with the mapping. `mode` is `append` (default) or `replace` (clears the
   draft first); `tags` are added to every imported Example; `publish: true` publishes a version afterwards. Bad rows are
   skipped and reported in `errors` with row numbers.

Uploads are limited by `server.max_upload_mb` (default 512).

```sh
# file upload: options is JSON
curl -H "$H" -F file=@faq.csv \
  -F 'options={"source":{"format":"csv"},"mapping":{"input_columns":{"question":"question"},"expected_column":"answer","tags_column":"topic"},"publish":true}' \
  "$B/v1/datasets/${DID}:import"

# Hugging Face (public datasets, by repo id)
curl -H "$H" -H 'content-type: application/json' "$B/v1/datasets/${DID}:import" -d '{
  "source": {"kind":"huggingface","hf_repo":"openai/gsm8k","hf_config":"main","hf_split":"test","limit":200},
  "mapping": {"input_columns":{"question":"question"},"expected_column":"answer"}, "publish": true }'
```

### Column mapping

| Field | Description |
|---|---|
| `input_columns` | Map of source column to `input` key. |
| `whole_row_as_input` | Put every column not mapped elsewhere into `input` under its own name. |
| `id_column` | Stable external id. |
| `expected_column` | Becomes `expected`. |
| `context_column`, `context_delimiter` | A string or array column. Strings are split into passages by the delimiter (empty: one passage). |
| `tags_column`, `tags_delimiter` | An array or a delimited string (default delimiter `,`). |
| `metadata_columns` | Columns copied into `metadata` (a `metadata.` prefix is stripped from the key). |
| `conversation_column` | An OpenAI-style messages array (`[{"role":"user","content":"..."}]`) as a JSON array or JSON string. |

At least one of `input_columns`, `whole_row_as_input` or `conversation_column` is required. JSONL and JSON sources have
no fixed column list, so use the preview to see what the keys are.

## Editing

`POST /v1/datasets/{dataset}/examples` bulk-upserts up to 10,000 Examples into the draft (an Example with a known `id`
gets a new revision only if it changed). `POST /v1/datasets/{dataset}/examples:delete` deletes by id from the draft;
published versions are unaffected.

**Add from run:** `POST /v1/datasets/{dataset}:from-results` promotes Run Results (for example failures) into the draft,
optionally storing the output as `expected` (`use_output_as_expected`) and adding `tags`. This closes the loop from a
failing Run to a regression test.

## Versions

`POST /v1/datasets/{dataset}/versions` with an optional `note` publishes the draft as the next version.
`GET /v1/datasets/{dataset}/versions` lists them. A Suite entry `{"dataset_id": "...", "version": 3}` pins version 3;
`"version": null` (or omitted) follows the latest published version. Every Run records the exact Dataset Version it used,
which is what makes Runs reproducible and comparable.

## Export

`GET /v1/datasets/{dataset}/versions/{number}/export?format=jsonl|csv` (default `jsonl`). JSONL has one Example per
line with all fields. CSV flattens `input` and `metadata` keys into columns.
