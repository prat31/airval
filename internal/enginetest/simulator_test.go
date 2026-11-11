package enginetest

import (
	"testing"
	"time"
)

// TestSimulatedConversationRun runs a simulated multi-turn example end to end: the suite's
// simulator (an OpenAI-compatible model, here the mock) plays the user against a chat target.
func TestSimulatedConversationRun(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	h := Start(t, Options{})
	var p, tg, d, ev, s Obj
	h.Do("POST", "/v1/projects", Obj{"org_id": h.OrgID, "name": "Sim " + randHex(3)}, 201, &p)
	pid := p["id"].(string)
	h.Do("POST", "/v1/projects/"+pid+"/targets", Obj{"name": "chat", "kind": "http", "config": Obj{
		"url":      h.Mock.URL + "/chat",
		"body":     `{"session_id": {{ json .session }}, "message": {{ json .message }}}`,
		"response": Obj{"output": "$.reply"}, "session": Obj{"path": "$.session_id"},
	}}, 201, &tg)
	h.Do("POST", "/v1/projects/"+pid+"/datasets", Obj{"name": "convos"}, 201, &d)
	did := d["id"].(string)
	h.Do("POST", "/v1/datasets/"+did+"/examples", Obj{"examples": []Obj{{"conversation": Obj{"simulation": Obj{
		"persona": "customer", "goal": "learn the answer", "first_message": "hello", "max_turns": 3}}}}}, 200, nil)
	h.Do("POST", "/v1/datasets/"+did+"/versions", Obj{}, 201, nil)
	h.Do("POST", "/v1/projects/"+pid+"/evaluators", Obj{"name": "has reply", "kind": "length", "config": Obj{"min": 1}}, 201, &ev)
	cfg := Obj{"target_ids": []string{tg["id"].(string)}, "datasets": []Obj{{"dataset_id": did}},
		"evaluators": []Obj{{"evaluator_id": ev["id"]}},
		"simulator":  Obj{"provider": "openai", "model": "mock-sim", "base_url": h.Mock.URL + "/v1"}}
	h.Do("POST", "/v1/projects/"+pid+"/suites", Obj{"name": "sim", "config": cfg}, 201, &s)

	// A bad simulator provider is rejected up front.
	bad := Obj{"simulator": Obj{"provider": "nope", "model": "x"}}
	h.Do("POST", "/v1/projects/"+pid+"/suites", Obj{"name": "bad", "config": bad}, 400, nil)

	run := h.StartRun(Obj{"suite_id": s["id"]})
	h.Stream(run["id"].(string), time.Minute)
	r := h.Run(run["id"].(string))
	if r["status"] != "succeeded" || int(num(r["errored"])) != 0 {
		t.Fatalf("run: status=%v errored=%v error=%v", r["status"], r["errored"], r["error"])
	}
	var res struct {
		Items []Obj `json:"items"`
	}
	h.Do("GET", "/v1/runs/"+run["id"].(string)+"/results", nil, 200, &res)
	if len(res.Items) != 1 {
		t.Fatalf("results: %v", res.Items)
	}
	tr, _ := res.Items[0]["transcript"].([]any)
	if len(tr) != 6 || tr[0].(Obj)["content"] != "hello" {
		t.Fatalf("transcript: %v", tr)
	}
}
