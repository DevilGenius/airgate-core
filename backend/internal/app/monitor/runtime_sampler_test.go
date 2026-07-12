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

func TestRuntimeSamplerQueryLatencySplitsModelKinds(t *testing.T) {
	db, fixture := openRuntimeLatencyTestDB(t)
	sampler := NewRuntimeSampler(db, nil, nil, nil, nil, nil)

	stats, err := sampler.queryLatency(t.Context(), 5*time.Minute)
	if err != nil {
		t.Fatalf("queryLatency() error = %v", err)
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

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.queries) != 2 {
		t.Fatalf("query count = %d, want 2", len(fixture.queries))
	}
	usageQuery := fixture.queries[0]
	if !strings.Contains(usageQuery, "first_token_ms > 0 AND LOWER(BTRIM(model)) NOT LIKE $2") {
		t.Fatal("text FRT query does not exclude image models")
	}
	if !strings.Contains(usageQuery, "duration_ms > 0 AND LOWER(BTRIM(model)) LIKE $2") {
		t.Fatal("image duration query does not select image models")
	}
	for index, args := range fixture.args {
		if len(args) != 2 || args[1].Value != "gpt-image%" {
			t.Fatalf("query %d model pattern args = %+v", index, args)
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
				"frt_avg", "frt_p50", "frt_p95", "frt_p99",
				"image_duration_p50", "image_duration_p95", "image_duration_p99",
			},
			values: []driver.Value{
				int64(9), int64(7), int64(2),
				110.4, 100.4, 199.6, 399.5,
				1000.4, 5000.5, 9000.6,
			},
		}, nil
	case strings.Contains(query, "FROM monitor_request_events"):
		return &runtimeLatencyTestRows{
			columns: []string{"error_count", "text_error_count", "image_error_count"},
			values:  []driver.Value{int64(4), int64(3), int64(1)},
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
