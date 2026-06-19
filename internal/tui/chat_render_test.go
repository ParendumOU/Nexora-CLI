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
