package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreAddAndList(t *testing.T) {
	t.Parallel()

	s := newStore()
	a, err := s.add("later", "do A", "in:10m", testNow)
	require.NoError(t, err)
	b, err := s.add("sooner", "do B", "in:1m", testNow)
	require.NoError(t, err)

	require.NotEqual(t, a.ID, b.ID)

	list := s.list()
	require.Len(t, list, 2)
	require.Equal(t, b.ID, list[0].ID)
	require.Equal(t, a.ID, list[1].ID)
	require.Equal(t, testNow.Add(time.Minute), list[0].NextFire)
}

func TestStoreAddInvalidSpec(t *testing.T) {
	t.Parallel()

	s := newStore()
	_, err := s.add("bad", "x", "whenever", testNow)
	require.Error(t, err)
	require.Empty(t, s.list())
}

func TestStoreCancel(t *testing.T) {
	t.Parallel()

	s := newStore()
	sc, err := s.add("x", "do x", "hourly", testNow)
	require.NoError(t, err)

	require.False(t, s.cancel("nope"))
	require.True(t, s.cancel(sc.ID))
	require.Empty(t, s.list())
	require.False(t, s.cancel(sc.ID))
}

func TestStorePopDueOneShot(t *testing.T) {
	t.Parallel()

	s := newStore()
	sc, err := s.add("once", "do once", "in:10m", testNow)
	require.NoError(t, err)

	require.Empty(t, s.popDue(testNow.Add(9*time.Minute)))
	fired := s.popDue(testNow.Add(10 * time.Minute))
	require.Len(t, fired, 1)
	require.Equal(t, sc.ID, fired[0].ID)
	require.Empty(t, s.list())
}

func TestStorePopDueRecurringReArms(t *testing.T) {
	t.Parallel()

	s := newStore()
	_, err := s.add("loop", "tick", "every:1h", testNow)
	require.NoError(t, err)

	fired := s.popDue(testNow.Add(time.Hour))
	require.Len(t, fired, 1)

	require.Empty(t, s.popDue(testNow.Add(time.Hour)))
	list := s.list()
	require.Len(t, list, 1)
	require.Equal(t, testNow.Add(2*time.Hour), list[0].NextFire)
}

func TestStorePopDueSkipsMissedSlots(t *testing.T) {
	t.Parallel()

	s := newStore()
	_, err := s.add("loop", "tick", "every:1h", testNow)
	require.NoError(t, err)

	fired := s.popDue(testNow.Add(3*time.Hour + 30*time.Minute))
	require.Len(t, fired, 1)
	require.Equal(t, testNow.Add(4*time.Hour), s.list()[0].NextFire)
}

func TestStoreUntilNext(t *testing.T) {
	t.Parallel()

	s := newStore()
	_, ok := s.untilNext(testNow)
	require.False(t, ok)

	_, err := s.add("x", "do x", "in:5m", testNow)
	require.NoError(t, err)
	d, ok := s.untilNext(testNow)
	require.True(t, ok)
	require.Equal(t, 5*time.Minute, d)

	d, ok = s.untilNext(testNow.Add(10 * time.Minute))
	require.True(t, ok)
	require.Equal(t, time.Duration(0), d)
}
