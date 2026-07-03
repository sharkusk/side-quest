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

func TestRequireQuestDefaultsFalse(t *testing.T) {
	if Default().RequireQuest {
		t.Fatal("require_quest should default to false")
	}
}

func TestRequireQuestRoundTrips(t *testing.T) {
	c := Default()
	c.RequireQuest = true
	data, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequireQuest {
		t.Fatalf("require_quest did not round-trip: %+v", got)
	}
}

func TestRequireQuestAbsentKeyIsFalse(t *testing.T) {
	// A config file written before this key existed must default it to false.
	got, err := Unmarshal([]byte("id_prefix: SQ\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.RequireQuest {
		t.Fatal("absent require_quest must default to false")
	}
}

func TestStrategyValid(t *testing.T) {
	for _, s := range []Strategy{Sequential, Random} {
		if !s.Valid() {
			t.Errorf("Strategy %q should be valid", s)
		}
	}
	for _, s := range []Strategy{"", "seq", "rand", "Sequential"} {
		if Strategy(s).Valid() {
			t.Errorf("Strategy %q should be invalid", s)
		}
	}
}

func TestToneValid(t *testing.T) {
	for _, tn := range []Tone{TonePlain, ToneDCC, ToneDCCSuperfan} {
		if !tn.Valid() {
			t.Errorf("Tone(%q).Valid() = false, want true", tn)
		}
	}
	for _, bad := range []Tone{"", "loud", "DCC"} {
		if Tone(bad).Valid() {
			t.Errorf("Tone(%q).Valid() = true, want false", bad)
		}
	}
}
