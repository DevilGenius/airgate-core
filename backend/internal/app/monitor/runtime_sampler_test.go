package monitor

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

const runtimeLatencyTestDriverName = "airgate_runtime_latency_test"

var (
	runtimeLatencyTestDriverOnce sync.Once
	runtimeLatencyTestFixtures   sync.Map
)

func TestRuntimeSamplerQueryLatencyWindowsSplitsModelKinds(t *testing.T) {
	db, fixture := openRuntimeLatencyTestDB(t)
	sampler := NewRuntimeSampler(db, nil, nil, nil, nil, nil)

	stats, longStats, err := sampler.queryLatencyWindows(t.Context())
	if err != nil {
		t.Fatalf("queryLatencyWindows() error = %v", err)
	}

	if stats.SampleCount != 9 || stats.TextSampleCount != 7 || stats.ImageSampleCount != 2 {
		t.Fatalf("sample counts = total:%d text:%d image:%d", stats.SampleCount, stats.TextSampleCount, stats.ImageSampleCount)
	}
	if stats.FRTAvgMS != 110 || stats.FRTP50MS != 100 || stats.FRTP95MS != 200 || stats.FRTP99MS != 400 {
		t.Fatalf("text FRT stats = avg:%d p50:%d p95:%d p99:%d", stats.FRTAvgMS, stats.FRTP50MS, stats.FRTP95MS, stats.FRTP99MS)
	}
	if stats.ImageDurationP50MS != 1000 || stats.ImageDurationP95MS != 5001 || stats.ImageDurationP99MS != 9001 {
		t.Fatalf("image duration stats = p50:%d p95:%d p99:%d", stats.ImageDurationP50MS, stats.ImageDurationP95MS, stats.ImageDurationP99MS)
	}
	if stats.ErrorCount != 4 || stats.TextErrorCount != 3 || stats.ImageErrorCount != 1 {
		t.Fatalf("error counts = total:%d text:%d image:%d", stats.ErrorCount, stats.TextErrorCount, stats.ImageErrorCount)
	}
	assertRuntimeFloat(t, "total error rate", stats.ErrorRate, 4.0/13.0)
	assertRuntimeFloat(t, "text error rate", stats.TextErrorRate, 3.0/10.0)
	assertRuntimeFloat(t, "image error rate", stats.ImageErrorRate, 1.0/3.0)
	if stats.Stale {
		t.Fatal("successful latency sample marked stale")
	}
	if longStats.SampleCount != 90 || longStats.TextSampleCount != 70 || longStats.ImageSampleCount != 20 {
		t.Fatalf("long sample counts = total:%d text:%d image:%d", longStats.SampleCount, longStats.TextSampleCount, longStats.ImageSampleCount)
	}
	if longStats.FRTAvgMS != 210 || longStats.FRTP50MS != 200 || longStats.FRTP95MS != 300 || longStats.FRTP99MS != 500 {
		t.Fatalf("long text FRT stats = avg:%d p50:%d p95:%d p99:%d", longStats.FRTAvgMS, longStats.FRTP50MS, longStats.FRTP95MS, longStats.FRTP99MS)
	}
	if longStats.ImageDurationP50MS != 2000 || longStats.ImageDurationP95MS != 6001 || longStats.ImageDurationP99MS != 10001 {
		t.Fatalf("long image duration stats = p50:%d p95:%d p99:%d", longStats.ImageDurationP50MS, longStats.ImageDurationP95MS, longStats.ImageDurationP99MS)
	}
	if longStats.ErrorCount != 8 || longStats.TextErrorCount != 6 || longStats.ImageErrorCount != 2 {
		t.Fatalf("long error counts = total:%d text:%d image:%d", longStats.ErrorCount, longStats.TextErrorCount, longStats.ImageErrorCount)
	}
	assertRuntimeFloat(t, "long total error rate", longStats.ErrorRate, 8.0/98.0)
	assertRuntimeFloat(t, "long text error rate", longStats.TextErrorRate, 6.0/76.0)
	assertRuntimeFloat(t, "long image error rate", longStats.ImageErrorRate, 2.0/22.0)

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.queries) != 2 {
		t.Fatalf("query count = %d, want 2", len(fixture.queries))
	}
	usageQuery := fixture.queries[0]
	if !strings.Contains(usageQuery, "first_event_ms > 0 AND model NOT LIKE $3") {
		t.Fatal("text FRT query does not exclude image models")
	}
	if !strings.Contains(usageQuery, "duration_ms > 0 AND model LIKE $3") {
		t.Fatal("image duration query does not select image models")
	}
	if strings.Contains(usageQuery, "LOWER(") || strings.Contains(usageQuery, "BTRIM(") {
		t.Fatal("usage query still normalizes every model row")
	}
	if got := strings.Count(usageQuery, "percentile_cont(ARRAY["); got != 4 {
		t.Fatalf("usage percentile array count = %d, want 4", got)
	}
	if strings.Contains(usageQuery, "percentile_cont(0.") {
		t.Fatal("usage query still calculates percentiles separately")
	}
	errorQuery := fixture.queries[1]
	if strings.Contains(errorQuery, "LOWER(") || strings.Contains(errorQuery, "BTRIM(") {
		t.Fatal("error query still normalizes every model row")
	}
	if !strings.Contains(errorQuery, "'plugin_forward_retry'") {
		t.Fatal("error query does not exclude informational retry events")
	}
	for index, args := range fixture.args {
		if len(args) != 3 || args[2].Value != "gpt-image%" {
			t.Fatalf("query %d model pattern args = %+v", index, args)
		}
		longSince, longOK := args[0].Value.(time.Time)
		shortSince, shortOK := args[1].Value.(time.Time)
		if !longOK || !shortOK || !longSince.Before(shortSince) {
			t.Fatalf("query %d window args = %+v", index, args[:2])
		}
	}
}

func TestRuntimeErrorRate(t *testing.T) {
	if got := runtimeErrorRate(0, 0); got != 0 {
		t.Fatalf("empty error rate = %v", got)
	}
	if got := runtimeErrorRate(0, 3); got != 1 {
		t.Fatalf("all-error rate = %v", got)
	}
	assertRuntimeFloat(t, "mixed error rate", runtimeErrorRate(7, 3), 0.3)
}

func assertRuntimeFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

type runtimeLatencyTestFixture struct {
	mu      sync.Mutex
	queries []string
	args    [][]driver.NamedValue
}

func openRuntimeLatencyTestDB(t *testing.T) (*stdsql.DB, *runtimeLatencyTestFixture) {
	t.Helper()
	runtimeLatencyTestDriverOnce.Do(func() {
		stdsql.Register(runtimeLatencyTestDriverName, runtimeLatencyTestDriver{})
	})

	fixture := &runtimeLatencyTestFixture{}
	key := t.Name()
	runtimeLatencyTestFixtures.Store(key, fixture)
	t.Cleanup(func() {
		runtimeLatencyTestFixtures.Delete(key)
	})

	db, err := stdsql.Open(runtimeLatencyTestDriverName, key)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db, fixture
}

type runtimeLatencyTestDriver struct{}

func (runtimeLatencyTestDriver) Open(name string) (driver.Conn, error) {
	value, ok := runtimeLatencyTestFixtures.Load(name)
	if !ok {
		return nil, errors.New("runtime latency test fixture not found")
	}
	return &runtimeLatencyTestConn{fixture: value.(*runtimeLatencyTestFixture)}, nil
}

type runtimeLatencyTestConn struct {
	fixture *runtimeLatencyTestFixture
}

func (*runtimeLatencyTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (*runtimeLatencyTestConn) Close() error {
	return nil
}

func (*runtimeLatencyTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not supported")
}

func (c *runtimeLatencyTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	clonedArgs := append([]driver.NamedValue(nil), args...)
	c.fixture.mu.Lock()
	c.fixture.queries = append(c.fixture.queries, query)
	c.fixture.args = append(c.fixture.args, clonedArgs)
	c.fixture.mu.Unlock()

	switch {
	case strings.Contains(query, "FROM usage_logs"):
		return &runtimeLatencyTestRows{
			columns: []string{
				"sample_count", "text_sample_count", "image_sample_count",
				"frt_avg", "frt_percentiles", "image_duration_percentiles",
				"long_sample_count", "long_text_sample_count", "long_image_sample_count",
				"long_frt_avg", "long_frt_percentiles", "long_image_duration_percentiles",
			},
			values: []driver.Value{
				int64(9), int64(7), int64(2),
				110.4, []byte("{100.4,199.6,399.5}"), []byte("{1000.4,5000.5,9000.6}"),
				int64(90), int64(70), int64(20),
				210.4, []byte("{200.4,299.6,499.5}"), []byte("{2000.4,6000.5,10000.6}"),
			},
		}, nil
	case strings.Contains(query, "FROM monitor_request_events"):
		return &runtimeLatencyTestRows{
			columns: []string{
				"error_count", "text_error_count", "image_error_count",
				"long_error_count", "long_text_error_count", "long_image_error_count",
			},
			values: []driver.Value{int64(4), int64(3), int64(1), int64(8), int64(6), int64(2)},
		}, nil
	default:
		return nil, errors.New("unexpected runtime latency query")
	}
}

type runtimeLatencyTestRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (r *runtimeLatencyTestRows) Columns() []string {
	return r.columns
}

func (*runtimeLatencyTestRows) Close() error {
	return nil
}

func (r *runtimeLatencyTestRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(dest, r.values)
	return nil
}
