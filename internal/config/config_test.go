package config

import "testing"

func TestDefault(t *testing.T) {
	c := Default()
	if c.IDPrefix != "SQ" || c.IDStrategy != Sequential || c.SeqNext != 1 ||
		c.SeqWidth != 4 || c.Tone != ToneDCC || !c.AutoTrailer {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestRoundTrip(t *testing.T) {
	in := Default()
	in.IDStrategy = Random
	in.SeqNext = 42
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.IDStrategy != Random || out.SeqNext != 42 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestUnmarshalFillsMissingFromDefault(t *testing.T) {
	// Only id_strategy present; the rest must come from Default().
	out, err := Unmarshal([]byte("id_strategy: random\n"))
	if err != nil {
		t.Fatal(err)
	}
	if out.IDStrategy != Random {
		t.Errorf("id_strategy not parsed: %+v", out)
	}
	if out.IDPrefix != "SQ" || out.SeqWidth != 4 || out.Tone != ToneDCC {
		t.Errorf("missing keys not defaulted: %+v", out)
	}
}
