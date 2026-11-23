package metrics

import (
	"testing"
	"time"
)

// netDef mirrors how the network counters are declared in system.go: a
// monotonic cumulative counter, decumulated, merged per bucket with "max" and
// pooled across buckets with "sum".
func netDef(decumulatePos bool) DataDef {
	return DataDef{
		// an hour-long bucket cannot expire mid-test
		Period:        time.Hour,
		Label:         "Network Received",
		SetOperation:  "max",
		AggloType:     "sum",
		Decumulate:    !decumulatePos,
		DecumulatePos: decumulatePos,
		Unit:          "B",
	}
}

// flush pushes samples into one bucket and persists it, returning the stored
// document.
func flush(t *testing.T, key string, def DataDef, samples []int) DataDefDB {
	t.Helper()
	for _, sample := range samples {
		pushSetMetric(key, sample, def)
	}
	SaveMetrics()
	return loadOne(t, "cosmos."+key)
}

func hourPool(t *testing.T, doc DataDefDB) DataDefDBEntry {
	t.Helper()
	key := AggloKey(granularityHour, ModuloTime(doc.Values[0].Date, time.Hour))
	entry, ok := doc.ValuesAggl[key]
	if !ok {
		t.Fatalf("hour pool %q missing from %v", key, doc.ValuesAggl)
	}
	return entry
}

// Counters go backwards in real deployments — gopsutil sums /proc/net/dev over
// every interface, so a veth/bridge/tailscale link disappearing drops the
// aggregate, and a NIC reload or container restart resets it to zero.
// Subtracting the previous sample unconditionally produced a huge negative
// delta, which the sum pools rendered as negative network traffic.
func TestDecumulateCounterResetNeverGoesNegative(t *testing.T) {
	cases := []struct {
		name    string
		samples []int
		want    int
	}{
		// first sample has no predecessor, second adds 500, third adds 1000
		{"monotonic", []int{1000, 1500, 2500}, 1000},
		// counter restarts at 1000: the drop contributes nothing, the sample
		// after the reset measures from the new baseline
		{"reset to zero-ish", []int{5000, 1000, 1800}, 800},
		// an interface disappears: aggregate shrinks, then grows again
		{"interface drop", []int{10000, 9000, 9500}, 500},
		// counter reads zero before anything is transferred
		{"starts at zero", []int{0, 700}, 700},
		// repeated identical readings mean no traffic
		{"idle", []int{4242, 4242, 4242}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMetricsStore(t)

			doc := flush(t, "system.netRx", netDef(false), c.samples)

			if len(doc.Values) != 1 {
				t.Fatalf("expected one raw bucket, got %d", len(doc.Values))
			}
			if doc.Values[0].Value != c.want {
				t.Fatalf("raw bucket = %d, want %d", doc.Values[0].Value, c.want)
			}
			if doc.Values[0].Value < 0 {
				t.Fatalf("raw bucket is negative: %d", doc.Values[0].Value)
			}

			pool := hourPool(t, doc)
			if pool.Value < 0 {
				t.Fatalf("hour pool is negative: %d", pool.Value)
			}
			if pool.Value != c.want {
				t.Fatalf("hour pool = %d, want %d", pool.Value, c.want)
			}
		})
	}
}

// DecumulatePos used to clamp the negative delta to zero, which left plain
// Decumulate (the network counters) unclamped. Both flags now share one
// reset-safe path.
func TestDecumulateAndDecumulatePosAgree(t *testing.T) {
	setupMetricsStore(t)

	samples := []int{9000, 1000, 1800, 1810}

	plain := flush(t, "system.netRx", netDef(false), samples)
	clamped := flush(t, "system.netTx", netDef(true), samples)

	if plain.Values[0].Value != clamped.Values[0].Value {
		t.Fatalf("Decumulate = %d, DecumulatePos = %d — must match",
			plain.Values[0].Value, clamped.Values[0].Value)
	}
	if hourPool(t, plain).Value != hourPool(t, clamped).Value {
		t.Fatalf("hour pools diverge: %d vs %d",
			hourPool(t, plain).Value, hourPool(t, clamped).Value)
	}
}

// A counter sitting at exactly zero is a real reading, not "no sample yet":
// the next sample must count in full. The old `lastInserted[key] != 0` guard
// discarded it.
func TestDecumulateZeroIsAValidBaseline(t *testing.T) {
	setupMetricsStore(t)

	doc := flush(t, "system.netTx", netDef(false), []int{0, 0, 640})

	if doc.Values[0].Value != 640 {
		t.Fatalf("raw bucket = %d, want 640", doc.Values[0].Value)
	}
}

// Successive flushes of an open bucket must not double count, and a reset
// landing in a later flush must not drag the pool negative.
func TestDecumulateReflushStaysPositive(t *testing.T) {
	setupMetricsStore(t)

	def := DataDef{
		Period:       time.Hour,
		Label:        "Network Received",
		SetOperation: "max",
		AggloType:    "sum",
		Decumulate:   true,
		Unit:         "B",
	}
	key := "system.netRx"

	pushSetMetric(key, 5000, def)
	pushSetMetric(key, 6000, def)
	SaveMetrics()

	pushSetMetric(key, 100, def)
	pushSetMetric(key, 250, def)
	SaveMetrics()

	doc := loadOne(t, "cosmos."+key)
	if len(doc.Values) != 1 {
		t.Fatalf("expected one bucket, got %d", len(doc.Values))
	}
	// max merge: 1000 from the first window beats the 150 measured after the reset
	if doc.Values[0].Value != 1000 {
		t.Fatalf("raw bucket = %d, want 1000", doc.Values[0].Value)
	}

	pool := hourPool(t, doc)
	if pool.Value != 1000 {
		t.Fatalf("hour pool = %d, want 1000 (no double count, no negative)", pool.Value)
	}
}
