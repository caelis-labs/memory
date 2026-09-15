package appliance

import (
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func scheduleTestDay() time.Time {
	return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
}

// TestStewardScheduleTimeIsFixedWidthAndMonotonic pins the encoding that makes
// `available_at <= now` and `lease_expires_at <= now` correct as plain SQL text
// comparison. RFC3339Nano is variable width, which mis-orders these instants.
func TestStewardScheduleTimeIsFixedWidthAndMonotonic(t *testing.T) {
	day := scheduleTestDay()
	ordered := []time.Time{
		day,
		day.Add(time.Microsecond),       // .000001
		day.Add(120 * time.Millisecond), // .120
		day.Add(123 * time.Millisecond), // .123
		day.Add(time.Second),            // whole second
	}
	width := len(formatScheduleTime(ordered[0]))
	previous := ""
	for index, instant := range ordered {
		encoded := formatScheduleTime(instant)
		if len(encoded) != width {
			t.Fatalf("schedule width varies at %d: %q", index, encoded)
		}
		parsed, err := parseTime(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if !parsed.Equal(instant) {
			t.Fatalf("schedule round trip %v -> %q -> %v", instant, encoded, parsed)
		}
		if index > 0 && previous >= encoded {
			t.Fatalf("schedule text order is not chronological: %q >= %q", previous, encoded)
		}
		previous = encoded
	}
	// Guards the rationale: RFC3339Nano text order disagrees with time order.
	nano120, nano123 := formatTime(ordered[2]), formatTime(ordered[3])
	if nano120 <= nano123 {
		t.Fatalf("expected RFC3339Nano to mis-order .120 (%q) and .123 (%q)", nano120, nano123)
	}
}

// TestStewardClaimUsesFixedWidthSchedulerEncoding is the production regression:
// a Job enqueued just before the claim cutoff must be found even when the two
// instants differ only in fractional-second width or whole-second form.
func TestStewardClaimUsesFixedWidthSchedulerEncoding(t *testing.T) {
	day := scheduleTestDay()
	cases := []struct {
		name    string
		enqueue time.Duration
		claim   time.Duration
	}{
		{"fractional 120 then 123", 120 * time.Millisecond, 123 * time.Millisecond},
		{"whole second then 001", 0, time.Millisecond},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			now := day.Add(testCase.enqueue)
			store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
			t.Cleanup(func() { _ = store.Close() })
			putAndBindSteward(t, store, 1)
			if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
				Text: "the service uses Go", IdempotencyKey: "schedule-" + testCase.name,
			}); err != nil {
				t.Fatal(err)
			}
			var stored string
			if err := store.db.QueryRowContext(t.Context(),
				`SELECT available_at FROM steward_jobs`).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != formatScheduleTime(now) {
				t.Fatalf("stored available_at = %q, want fixed-width %q", stored, formatScheduleTime(now))
			}
			now = day.Add(testCase.claim)
			if _, found, err := store.ClaimStewardJob(t.Context(), time.Minute); err != nil || !found {
				t.Fatalf("claim at %s after enqueue at %s found=%v err=%v",
					testCase.claim, testCase.enqueue, found, err)
			}
		})
	}
}

// TestStewardLeaseExpiryComparesAtExactInstant pins the equality boundary: a
// lease expiring exactly at the claim cutoff is reclaimable, with no millisecond
// rounding that could steal or delay a lease.
func TestStewardLeaseExpiryComparesAtExactInstant(t *testing.T) {
	start := scheduleTestDay().Add(120 * time.Millisecond)
	now := start
	store, auth := newGoldenStore(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	putAndBindSteward(t, store, 1)
	if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
		Text: "lease boundary fact", IdempotencyKey: "schedule-lease-boundary",
	}); err != nil {
		t.Fatal(err)
	}
	first, found, err := store.ClaimStewardJob(t.Context(), time.Second)
	if err != nil || !found {
		t.Fatalf("first claim found=%v err=%v", found, err)
	}
	now = start.Add(time.Second)
	retry, found, err := store.ClaimStewardJob(t.Context(), time.Minute)
	if err != nil || !found || retry.Attempt != 2 || retry.Lease.Token == first.Lease.Token {
		t.Fatalf("claim at exact lease expiry retry=%+v found=%v err=%v", retry, found, err)
	}
}
