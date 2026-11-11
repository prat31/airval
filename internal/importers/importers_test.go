package importers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airval/airval/internal/evalcore"
	"github.com/parquet-go/parquet-go"
)

var ctx = context.Background()

func collect(t *testing.T, src Source, m Mapping, bs int) ([]evalcore.Example, ImportResult) {
	t.Helper()
	var out []evalcore.Example
	res, err := Import(ctx, src, m, func(b []evalcore.Example) error { out = append(out, b...); return nil }, bs)
	if err != nil {
		t.Fatal(err)
	}
	return out, res
}

func TestCSVBasicBOMMultilineSniff(t *testing.T) {
	data := "\xef\xbb\xbfquestion;answer;tags;context\n" +
		"\"What is\nGo?\";\"A \"\"language\"\"\";a,b;\"[\"\"c1\"\",\"\"c2\"\"]\"\n" +
		"Hi;Hello;;\n" +
		"short\n" +
		"a;b;c;d;e\n"
	src := FromBytes([]byte(data), FormatCSV)
	pv, err := Preview(ctx, src, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Columns) != 4 || pv.Columns[0].Name != "question" {
		t.Fatalf("columns: %+v", pv.Columns)
	}
	m := SuggestMapping(pv.Columns)
	if m.ExpectedColumn != "answer" || m.TagsColumn != "tags" || m.ContextColumn != "context" || m.InputColumns["question"] != "question" {
		t.Fatalf("mapping: %+v", m)
	}
	exs, res := collect(t, src, m, 0)
	if res.Total != 4 || res.Imported != 3 || res.Failed != 1 {
		t.Fatalf("res: %+v", res)
	}
	if res.Errors[0].Row != 4 {
		t.Fatalf("errors: %+v", res.Errors)
	}
	if exs[0].Input["question"] != "What is\nGo?" || exs[0].Expected != `A "language"` {
		t.Fatalf("ex0: %+v", exs[0])
	}
	if strings.Join(exs[0].Tags, "|") != "a|b" || strings.Join(exs[0].Context, "|") != "c1|c2" {
		t.Fatalf("ex0 tags/ctx: %+v", exs[0])
	}
	if exs[1].Expected != "Hello" || exs[1].Tags != nil {
		t.Fatalf("ex1: %+v", exs[1])
	}
	if exs[2].Input["question"] != "short" || exs[2].Expected != nil {
		t.Fatalf("ex2 (short row padded): %+v", exs[2])
	}
}

func TestSniffDelimiters(t *testing.T) {
	for _, d := range []string{",", "\t", ";", "|"} {
		data := strings.Join([]string{"a", "b", "c"}, d) + "\n1" + d + "2" + d + "3\n"
		pv, err := Preview(ctx, FromBytes([]byte(data), FormatCSV), 5)
		if err != nil || len(pv.Columns) != 3 {
			t.Fatalf("delim %q: %v %+v", d, err, pv.Columns)
		}
		if pv.Columns[0].InferredType != "int" {
			t.Fatalf("type %q", pv.Columns[0].InferredType)
		}
	}
}

func TestCSVDuplicateEmptyHeaders(t *testing.T) {
	pv, _ := Preview(ctx, FromBytes([]byte("a,a,,b\n1,2,3,4\n"), FormatCSV), 5)
	var names []string
	for _, c := range pv.Columns {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "a,a_2,column_3,b" {
		t.Fatalf("%v", names)
	}
}

func TestJSONL(t *testing.T) {
	data := `{"input":"x","expected":1,"tags":["t1","t2"],"extra":{"k":1.5}}` + "\n\n" +
		`not json` + "\n" +
		`{"input":"y","expected":"b","tags":["t3"]}` + "\n"
	src := FromBytes([]byte(data), "")
	if src.Format != FormatJSONL {
		t.Fatalf("sniffed %s", src.Format)
	}
	pv, _ := Preview(ctx, src, 10)
	if len(pv.RowErrors) != 1 || pv.RowErrors[0].Row != 2 {
		t.Fatalf("%+v", pv.RowErrors)
	}
	m := SuggestMapping(pv.Columns)
	exs, res := collect(t, src, m, 1)
	if res.Imported != 2 || res.Failed != 1 || res.Total != 3 {
		t.Fatalf("%+v", res)
	}
	if exs[0].Expected != int64(1) || exs[0].Tags[1] != "t2" || exs[0].Metadata["extra"].(map[string]any)["k"] != 1.5 {
		t.Fatalf("%+v", exs[0])
	}
}

func TestJSONArrayAndWrapped(t *testing.T) {
	arr := `[{"q":"a","answer":"1"},{"q":"b","answer":"2"}]`
	wrapped := `{"version":2,"meta":{"x":[1,2]},"data":[{"q":"a","answer":"1"},{"q":"b","answer":"2"}],"other":[{"z":1}]}`
	for _, s := range []string{arr, wrapped} {
		src := FromBytes([]byte(s), "")
		if src.Format != FormatJSON {
			t.Fatalf("sniffed %s for %s", src.Format, s)
		}
		pv, err := Preview(ctx, src, 10)
		if err != nil {
			t.Fatal(err)
		}
		if !pv.TotalExact || pv.TotalEstimate != 2 {
			t.Fatalf("%+v", pv)
		}
		exs, _ := collect(t, src, SuggestMapping(pv.Columns), 0)
		if len(exs) != 2 || exs[1].Expected != "2" {
			t.Fatalf("%+v", exs)
		}
	}
	// explicit field
	src := FromBytes([]byte(wrapped), FormatJSON)
	src.Options.JSONField = "other"
	pv, _ := Preview(ctx, src, 10)
	if pv.Columns[0].Name != "z" {
		t.Fatalf("%+v", pv.Columns)
	}
	// no array
	if _, err := Preview(ctx, FromBytes([]byte(`{"a":1}`), FormatJSON), 5); err == nil {
		t.Fatal("expected error")
	}
}

type pqRow struct {
	Question string   `parquet:"question"`
	Answer   string   `parquet:"answer"`
	Score    int64    `parquet:"score"`
	Docs     []string `parquet:"documents,list"`
}

func writeParquet(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := parquet.NewGenericWriter[pqRow](f)
	for i := 0; i < n; i++ {
		if _, err := w.Write([]pqRow{{fmt.Sprintf("q%d", i), fmt.Sprintf("a%d", i), int64(i), []string{"d1", "d2"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestParquet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.parquet")
	writeParquet(t, p, 250)
	src, err := FromFile(p, "")
	if err != nil || src.Format != FormatParquet {
		t.Fatalf("%v %v", err, src.Format)
	}
	pv, err := Preview(ctx, src, 5)
	if err != nil {
		t.Fatal(err)
	}
	if pv.TotalEstimate != 250 || !pv.TotalExact || len(pv.Rows) != 5 {
		t.Fatalf("%+v", pv)
	}
	m := SuggestMapping(pv.Columns)
	if m.ContextColumn != "documents" || m.ExpectedColumn != "answer" {
		t.Fatalf("%+v", m)
	}
	exs, res := collect(t, src, m, 100)
	if res.Imported != 250 || exs[7].Input["question"] != "q7" || len(exs[7].Context) != 2 || exs[7].Metadata["score"] != int64(7) {
		t.Fatalf("%+v %+v", res, exs[7])
	}
	// non-seekable reader gets spooled
	b, _ := os.ReadFile(p)
	rs := FromReader(io.MultiReader(bytes.NewReader(b)), "")
	if rs.Format != FormatParquet {
		t.Fatalf("sniff %s", rs.Format)
	}
	_, res = collect(t, rs, m, 0)
	if res.Imported != 250 {
		t.Fatalf("%+v", res)
	}
}

func TestFromReaderOneShot(t *testing.T) {
	src := FromReader(strings.NewReader("a,b\n1,2\n"), FormatCSV)
	if _, err := Preview(ctx, src, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := Preview(ctx, src, 1); err == nil {
		t.Fatal("second open should fail")
	}
}

func TestMappingConversationAndWholeRow(t *testing.T) {
	data := `{"id":"e1","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"text","text":"yo"}]}],"label":"greet","topic":"x"}` + "\n"
	src := FromBytes([]byte(data), FormatJSONL)
	pv, _ := Preview(ctx, src, 5)
	m := SuggestMapping(pv.Columns)
	if m.ConversationColumn != "messages" || m.IDColumn != "id" || m.ExpectedColumn != "label" {
		t.Fatalf("%+v", m)
	}
	exs, res := collect(t, src, m, 0)
	if res.Failed != 0 {
		t.Fatalf("%+v", res)
	}
	c := exs[0].Conversation
	if exs[0].ID != "e1" || len(c.Turns) != 2 || c.Turns[1].Content != "yo" || exs[0].Metadata["topic"] != "x" {
		t.Fatalf("%+v", exs[0])
	}

	src = FromBytes([]byte(`{"a":1,"b":"two","exp":"x"}`+"\n"), FormatJSONL)
	exs, _ = collect(t, src, Mapping{WholeRowAsInput: true, ExpectedColumn: "exp"}, 0)
	if len(exs[0].Input) != 2 || exs[0].Input["b"] != "two" || exs[0].Expected != "x" {
		t.Fatalf("%+v", exs[0])
	}

	if err := (Mapping{}).Validate(); err == nil {
		t.Fatal("empty mapping must fail")
	}
	_, err := Import(ctx, FromBytes([]byte("a,b\n1,2\n"), FormatCSV), Mapping{InputColumns: map[string]string{"zzz": "z"}}, func([]evalcore.Example) error { return nil }, 0)
	if err == nil || !strings.Contains(err.Error(), "zzz") {
		t.Fatalf("%v", err)
	}
}

func TestMappingJSONShape(t *testing.T) {
	in := `{"input_columns":{"q":"question"},"expected_column":"a","context_column":"c","context_delimiter":"||","tags_column":"t","tags_delimiter":";","metadata_columns":["m"],"conversation_column":"msgs"}`
	var m Mapping
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(m)
	if string(out) != in {
		t.Fatalf("\n%s\n%s", out, in)
	}
	exs, _ := collect(t, FromBytes([]byte("q,a,c,t,m,msgs\nhello,yes,p1||p2,x;y,meta,\n"), FormatCSV),
		Mapping{InputColumns: map[string]string{"q": "question"}, ContextColumn: "c", ContextDelimiter: "||", TagsColumn: "t", TagsDelimiter: ";", ExpectedColumn: "a", MetadataColumns: []string{"m"}}, 0)
	if len(exs[0].Context) != 2 || len(exs[0].Tags) != 2 || exs[0].Metadata["m"] != "meta" {
		t.Fatalf("%+v", exs[0])
	}
}

func TestRowErrorCapAndSinkError(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("q,a\n")
	for i := 0; i < 250; i++ {
		sb.WriteString("1,2,3\n") // too many fields
	}
	sb.WriteString("ok,fine\n")
	exs, res := collect(t, FromBytes([]byte(sb.String()), FormatCSV), Mapping{InputColumns: map[string]string{"q": "q"}}, 0)
	if res.Failed != 250 || len(res.Errors) != MaxRowErrors || !res.ErrorsTruncated || len(exs) != 1 {
		t.Fatalf("%+v", res)
	}
	boom := fmt.Errorf("boom")
	_, err := Import(ctx, FromBytes([]byte("q\n1\n2\n3\n"), FormatCSV), Mapping{InputColumns: map[string]string{"q": "q"}},
		func([]evalcore.Example) error { return boom }, 2)
	if err != boom {
		t.Fatalf("%v", err)
	}
}

func TestBatching(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("q\n")
	for i := 0; i < 1203; i++ {
		fmt.Fprintf(&sb, "row%d\n", i)
	}
	var sizes []int
	res, err := Import(ctx, FromBytes([]byte(sb.String()), FormatCSV), Mapping{InputColumns: map[string]string{"q": "q"}},
		func(b []evalcore.Example) error { sizes = append(sizes, len(b)); return nil }, 0)
	if err != nil || res.Imported != 1203 || fmt.Sprint(sizes) != "[500 500 203]" {
		t.Fatalf("%v %v %+v", err, sizes, res)
	}
}

func TestExportRoundTrip(t *testing.T) {
	exs := []evalcore.Example{
		{ID: "1", Input: map[string]any{"question": "q1", "n": 3}, Expected: "a1", Context: []string{"c1", "c2"}, Tags: []string{"x"}, Metadata: map[string]any{"src": "s", "obj": map[string]any{"a": 1}}},
		{ID: "2", Input: map[string]any{"question": "multi\nline, \"quoted\""}, Expected: map[string]any{"k": "v"},
			Conversation: &evalcore.Conversation{Turns: []evalcore.Turn{{Role: "user", Content: "hi"}}}},
	}
	seq := func() iter.Seq2[evalcore.Example, error] {
		return func(yield func(evalcore.Example, error) bool) {
			for _, e := range exs {
				if !yield(e, nil) {
					return
				}
			}
		}
	}
	var csvBuf bytes.Buffer
	n, err := ExportCSV(ctx, &csvBuf, seq(), ExportOptions{})
	if err != nil || n != 2 {
		t.Fatal(err, n)
	}
	if !strings.HasPrefix(csvBuf.String(), "id,input.n,input.question,expected,context,tags,metadata.obj,metadata.src,conversation\n") {
		t.Fatalf("header: %q", csvBuf.String())
	}
	src := FromBytes(csvBuf.Bytes(), FormatCSV)
	pv, _ := Preview(ctx, src, 5)
	m := SuggestMapping(pv.Columns)
	back, res := collect(t, src, m, 0)
	if res.Failed != 0 || len(back) != 2 {
		t.Fatalf("%+v %+v", res, m)
	}
	if back[0].ID != "1" || back[0].Input["question"] != "q1" || back[0].Input["n"] != "3" || back[0].Expected != "a1" ||
		strings.Join(back[0].Context, ",") != "c1,c2" || back[0].Tags[0] != "x" || back[0].Metadata["src"] != "s" {
		t.Fatalf("%+v", back[0])
	}
	if back[1].Input["question"] != "multi\nline, \"quoted\"" || back[1].Conversation == nil || back[1].Conversation.Turns[0].Content != "hi" {
		t.Fatalf("%+v", back[1])
	}

	var jl bytes.Buffer
	n, err = ExportJSONL(ctx, &jl, seq())
	if err != nil || n != 2 {
		t.Fatal(err)
	}
	var e0 evalcore.Example
	if err := json.Unmarshal([]byte(strings.Split(jl.String(), "\n")[0]), &e0); err != nil || e0.ID != "1" {
		t.Fatal(err, e0)
	}
}

func TestSuggestMappingHeuristics(t *testing.T) {
	cols := []Column{{Name: "Question", InferredType: "string"}, {Name: "Ground Truth", InferredType: "string"},
		{Name: "contexts", InferredType: "array"}, {Name: "tags", InferredType: "array"}, {Name: "difficulty", InferredType: "int"}}
	m := SuggestMapping(cols)
	if m.InputColumns["Question"] != "Question" || m.ExpectedColumn != "Ground Truth" || m.ContextColumn != "contexts" || m.TagsColumn != "tags" ||
		len(m.MetadataColumns) != 1 || m.Validate() != nil {
		t.Fatalf("%+v", m)
	}
	// no obvious names -> first string column becomes input
	m = SuggestMapping([]Column{{Name: "n", InferredType: "int"}, {Name: "blob", InferredType: "string"}})
	if m.InputColumns["blob"] == "" {
		t.Fatalf("%+v", m)
	}
}

func TestFromFileSniff(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o644)
		return p
	}
	cases := map[string]Format{
		write("a.csv", "a,b\n1,2\n"):              FormatCSV,
		write("b.ndjson", `{"a":1}`+"\n"):         FormatJSONL,
		write("c.json", `[{"a":1}]`):              FormatJSON,
		write("d.json", `{"a":1}`+"\n"+`{"a":2}`): FormatJSONL,
		write("noext", `[{"a":1}]`):               FormatJSON,
		write("noext2", "x,y\n1,2\n"):             FormatCSV,
	}
	for p, want := range cases {
		s, err := FromFile(p, "")
		if err != nil || s.Format != want {
			t.Errorf("%s: got %s want %s (%v)", p, s.Format, want, err)
		}
	}
}

// ---- HF ----

func hfServer(t *testing.T, total int) (*httptest.Server, *int) {
	t.Helper()
	calls := new(int)
	mux := http.NewServeMux()
	mux.HandleFunc("/splits", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dataset") == "missing/ds" {
			w.WriteHeader(404)
			io.WriteString(w, `{"error":"The dataset does not exist."}`)
			return
		}
		io.WriteString(w, `{"splits":[{"dataset":"org/ds","config":"default","split":"train"},{"dataset":"org/ds","config":"default","split":"test"}]}`)
	})
	mux.HandleFunc("/rows", func(w http.ResponseWriter, r *http.Request) {
		*calls++
		q := r.URL.Query()
		if q.Get("config") != "default" || q.Get("split") != "train" {
			w.WriteHeader(400)
			return
		}
		var off, length int
		fmt.Sscan(q.Get("offset"), &off)
		fmt.Sscan(q.Get("length"), &length)
		var rows []string
		for i := off; i < off+length && i < total; i++ {
			rows = append(rows, fmt.Sprintf(`{"row_idx":%d,"row":{"answer":"a%d","question":"q%d","n":%d},"truncated_cells":[]}`, i, i, i, i))
		}
		fmt.Fprintf(w, `{"features":[{"feature_idx":0,"name":"question","type":{}},{"feature_idx":1,"name":"answer","type":{}},{"feature_idx":2,"name":"n","type":{}}],"rows":[%s],"num_rows_total":%d}`,
			strings.Join(rows, ","), total)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, calls
}

func TestHF(t *testing.T) {
	srv, calls := hfServer(t, 250)
	c := &HFClient{BaseURL: srv.URL, HTTP: srv.Client(), RetryDelay: 1}
	splits, err := c.Splits(ctx, "org/ds")
	if err != nil || len(splits) != 2 {
		t.Fatal(err, splits)
	}
	src := FromHF(c, HFRef{Dataset: "org/ds"}) // discovers default/train
	pv, err := Preview(ctx, src, 3)
	if err != nil {
		t.Fatal(err)
	}
	if pv.TotalEstimate != 250 || !pv.TotalExact || pv.Columns[0].Name != "question" || pv.Columns[2].InferredType != "int" {
		t.Fatalf("%+v", pv)
	}
	*calls = 0
	m := SuggestMapping(pv.Columns)
	exs, res := collect(t, src, m, 0)
	if res.Imported != 250 || exs[249].Input["question"] != "q249" || exs[249].Expected != "a249" {
		t.Fatalf("%+v", res)
	}
	if *calls != 3 {
		t.Fatalf("pages fetched: %d", *calls)
	}
	if _, err := c.Splits(ctx, "missing/ds"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("%v", err)
	}
}

func TestHFRetry(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(429)
			return
		}
		io.WriteString(w, `{"splits":[]}`)
	}))
	defer srv.Close()
	c := &HFClient{BaseURL: srv.URL, RetryDelay: 1}
	if _, err := c.Splits(ctx, "a/b"); err != nil || n != 3 {
		t.Fatal(err, n)
	}
}

// ---- benchmark ----

func genCSV(n int) []byte {
	var b bytes.Buffer
	b.WriteString("question,answer,context,tags,category\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "\"What is the answer to question number %d, in detail?\",Answer %d,\"passage one for %d\",\"a,b\",cat%d\n", i, i, i, i%10)
	}
	return b.Bytes()
}

func BenchmarkImportCSV100k(b *testing.B) {
	data := genCSV(100_000)
	m := Mapping{InputColumns: map[string]string{"question": "question"}, ExpectedColumn: "answer", ContextColumn: "context", TagsColumn: "tags", MetadataColumns: []string{"category"}}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cnt := 0
		res, err := Import(ctx, FromBytes(data, FormatCSV), m, func(bt []evalcore.Example) error { cnt += len(bt); return nil }, 500)
		if err != nil || cnt != 100_000 || res.Failed != 0 {
			b.Fatal(err, cnt, res)
		}
	}
	b.ReportMetric(float64(100_000*b.N)/b.Elapsed().Seconds(), "rows/s")
}

func TestLargeStreamingCSV(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	data := genCSV(200_000)
	cnt := 0
	_, err := Import(ctx, FromBytes(data, FormatCSV), Mapping{WholeRowAsInput: true}, func(b []evalcore.Example) error { cnt += len(b); return nil }, 500)
	if err != nil || cnt != 200_000 {
		t.Fatal(err, cnt)
	}
}
