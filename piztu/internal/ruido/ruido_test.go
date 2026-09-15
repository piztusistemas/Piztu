package ruido

import "testing"

func TestRmsAPorcentaxe(t *testing.T) {
	for _, rms := range []float64{0, -5, 1, 100, 32768, 1e9} {
		if got := rmsAPorcentaxe(rms); got < 0 || got > 100 {
			t.Errorf("rmsAPorcentaxe(%v) = %d fóra de [0,100]", rms, got)
		}
	}
	if got := rmsAPorcentaxe(32768); got != 100 {
		t.Errorf("rmsAPorcentaxe(32768) = %d, quería 100 (nivel máximo)", got)
	}
	if got := rmsAPorcentaxe(0); got != 0 {
		t.Errorf("rmsAPorcentaxe(0) = %d, quería 0 (silencio)", got)
	}
}

func TestAudioQueueFIFOEMaxlen(t *testing.T) {
	q := &audioQueue{}
	for i := 0; i < audioQueueMax+5; i++ {
		q.push([]byte{byte(i)})
	}
	if len(q.bufs) != audioQueueMax {
		t.Fatalf("len(q.bufs) = %d, quería %d (maxlen)", len(q.bufs), audioQueueMax)
	}
	// Os primeiros 5 empurrados deberían terse descartado; o primeiro que queda é o 5.
	b, ok := q.pop()
	if !ok || b[0] != 5 {
		t.Fatalf("primeiro elemento tras exceder maxlen = %v, quería [5]", b)
	}
}

func TestAudioQueuePopBaleiro(t *testing.T) {
	q := &audioQueue{}
	if _, ok := q.pop(); ok {
		t.Fatal("pop() nunha cola baleira debería devolver ok=false")
	}
}
