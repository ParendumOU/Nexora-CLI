package tui

import "testing"

func TestCleanContentStripsToolCallJSON(t *testing.T) {
	cases := []string{
		// bare tool-call array (name+args) — the observed leak
		`[{"name":"log_entry","args":{"message":"x","level":"info"}},
		  {"name":"task_create","args":{"title":"Ejecutar ps aux","description":"d","assigned_agent_id":"abc","agent_overrides":{"additional_skills":["bash"]}}}]`,
		// decompose array
		`[{"title":"Suma","task":"Calcula","skills":["bash"]}]`,
		// single tool-call object
		`{"name":"shell_run","args":{"command":"ps aux"}}`,
		// single-line shell_run array (observed leak)
		`[{"name":"shell_run","args":{"cmd":"date"}}]`,
		// indented
		"    [{\"name\":\"shell_run\",\"args\":{\"cmd\":\"date\"}}]",
	}
	for i, c := range cases {
		got, _ := cleanContent(c)
		if got != "" {
			t.Errorf("case %d: expected empty, got %q", i, got)
		}
	}
}

func TestCleanContentKeepsRealText(t *testing.T) {
	in := "La raíz cuadrada es **292**. Listo."
	got, _ := cleanContent(in)
	if got == "" {
		t.Fatalf("real text was stripped")
	}
}

func TestCleanContentStripsFinalAndThinking(t *testing.T) {
	in := "<thinking>plan</thinking>Hola Iván <final/>"
	got, reasoning := cleanContent(in)
	if got != "Hola Iván" {
		t.Errorf("got %q", got)
	}
	if len(reasoning) != 1 {
		t.Errorf("expected 1 reasoning block, got %d", len(reasoning))
	}
}

// Live streaming: an unclosed <think> (closing tag not arrived yet) must be pulled
// out as reasoning and kept out of the visible body — "watch it think".
func TestCleanContentHandlesOpenThinking(t *testing.T) {
	in := "<think>step 1\nstep 2 in progress"
	got, reasoning := cleanContent(in)
	if got != "" {
		t.Errorf("open think leaked into body: %q", got)
	}
	if len(reasoning) != 1 || reasoning[0] == "" {
		t.Errorf("expected 1 live reasoning block, got %v", reasoning)
	}
}

func TestCleanContentOpenThinkingAfterClosedBlock(t *testing.T) {
	// a finished thought, then the answer starts, then a new open thought streams in
	in := "<think>done thinking</think>Here is the answer.<think>now reconsider"
	got, reasoning := cleanContent(in)
	if got != "Here is the answer." {
		t.Errorf("got %q", got)
	}
	if len(reasoning) != 2 {
		t.Errorf("expected 2 reasoning blocks (closed + open), got %d", len(reasoning))
	}
}
