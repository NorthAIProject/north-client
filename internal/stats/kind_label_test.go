package stats

import "testing"

func TestKindLabel(t *testing.T) {
	cases := []struct{ tag, name, want string }{
		{"en", "Indoor running", "Indoor running"},
		{"es", "Running", "Correr"},
		{"pt-PT", "Indoor cycling", "Ciclismo em interior"},
		{"pt-BR", "Indoor cycling", "Ciclismo indoor"},
		// A type named from the MET table reads as that activity's entry.
		{"es", "Hiking", "Senderismo"},
		// Unknown names are shown as they are rather than as a key.
		{"es", "Underwater basket weaving", "Underwater basket weaving"},
	}
	for _, c := range cases {
		if got := KindLabel(c.tag, c.name); got != c.want {
			t.Errorf("KindLabel(%q, %q) = %q, want %q", c.tag, c.name, got, c.want)
		}
	}
}
