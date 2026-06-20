package bootstrap

import (
	"bufio"
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	_ "modernc.org/sqlite"

	"github.com/DevilGenius/airgate-core/ent/migrate"
	"github.com/DevilGenius/airgate-core/internal/adminevents"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestNewHTTPHandlersConstructsServicesAndHandlers(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "bootstrap_handlers", migrate.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	pluginDir := t.TempDir()
	pluginMgr := plugin.NewManager(pluginDir, "debug", "", db)
	marketplace := plugin.NewMarketplace(pluginDir, plugin.WithEntries([]plugin.MarketplacePlugin{{
		Name:        "test-plugin",
		Version:     "0.0.1",
		Description: "test",
		Author:      "test",
		Type:        "gateway",
	}}))
	recorder := billing.NewRecorder(db, 1)

	handlers := NewHTTPHandlers(HTTPDependencies{
		Config: &config.Config{
			JWT:      config.JWTConfig{Secret: "jwt-secret", ExpireHour: 1},
			Security: config.SecurityConfig{APIKeySecret: strings.Repeat("a", 64)},
		},
		DB:          db,
		JWTMgr:      auth.NewJWTManager("jwt-secret", 1),
		PluginMgr:   pluginMgr,
		Marketplace: marketplace,
		Events:      adminevents.NewHub(1),
		Recorder:    recorder,
	})

	if handlers == nil || handlers.Auth == nil || handlers.User == nil || handlers.Account == nil ||
		handlers.Group == nil || handlers.APIKey == nil || handlers.Subscription == nil ||
		handlers.Usage == nil || handlers.Proxy == nil || handlers.Settings == nil ||
		handlers.Dashboard == nil || handlers.Plugin == nil || handlers.Version == nil ||
		handlers.Upgrade == nil || handlers.Monitor == nil || handlers.Event == nil ||
		handlers.AccountService == nil {
		t.Fatalf("NewHTTPHandlers returned incomplete handler set: %+v", handlers)
	}
}

func TestBalanceAlertEmailEarlyReturnWithoutSMTPConfig(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "bootstrap_alerts", migrate.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	settingsService := appsettings.NewService(store.NewSettingsStore(db))
	balanceAlertSendEmail(settingsService, "user@example.com", 1.2345, 2)
	apiKeyBalanceAlertSendEmail(settingsService, billing.APIKeyBalanceAlertInput{
		AlertEmail: "key@example.com",
		Remaining:  3,
		Threshold:  5,
	})
}

func TestSystemUpgradeHelpersLoadDescribeExecuteAndPanic(t *testing.T) {
	RunSystemUpgrades(nil)

	upgrades := loadSystemUpgrades()
	if len(upgrades) == 0 {
		t.Fatal("loadSystemUpgrades returned no embedded migrations")
	}
	if upgrades[0].ID == "" || upgrades[0].Description == "" || upgrades[0].Checksum == "" ||
		!strings.Contains(upgrades[0].SQL, "usage_logs") {
		t.Fatalf("first upgrade = %+v", upgrades[0])
	}

	if got := systemUpgradeDescription("-- description: Custom upgrade\nSELECT 1;", "fallback"); got != "Custom upgrade" {
		t.Fatalf("description = %q, want Custom upgrade", got)
	}
	if got := systemUpgradeDescription("SELECT 1;", "fallback_id"); got != "fallback_id" {
		t.Fatalf("fallback description = %q, want fallback_id", got)
	}
	if err := validateSystemUpgradeFilename("20260528143015-usage.sql"); err == nil {
		t.Fatal("filename with wrong separator accepted")
	}

	if tag, ok := sqlDollarTag("$tag$ body $tag$"); !ok || tag != "$tag$" {
		t.Fatalf("sqlDollarTag valid = %q %v", tag, ok)
	}
	for _, input := range []string{"", "no-dollar", "$bad-tag$"} {
		if tag, ok := sqlDollarTag(input); ok || tag != "" {
			t.Fatalf("sqlDollarTag(%q) = %q %v, want empty false", input, tag, ok)
		}
	}

	statements := splitSQLStatements(`
CREATE TABLE "semi;table" (value text);
INSERT INTO "semi;table" VALUES ('a;b');
SELECT $quoted$body;with;semicolons$quoted$;
`)
	if len(statements) != 3 {
		t.Fatalf("splitSQLStatements len = %d, want 3: %#v", len(statements), statements)
	}

	db, err := stdsql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	}()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn sqlite: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("close sqlite conn: %v", err)
		}
	}()
	upgrade := systemUpgrade{
		ID:  "sqlite_test",
		SQL: "CREATE TABLE applied (id integer primary key, value text); INSERT INTO applied(value) VALUES ('ok;still string');",
	}
	if err := executeSystemUpgradeSQL(context.Background(), conn, upgrade); err != nil {
		t.Fatalf("executeSystemUpgradeSQL returned error: %v", err)
	}
	var value string
	if err := conn.QueryRowContext(context.Background(), "SELECT value FROM applied").Scan(&value); err != nil {
		t.Fatalf("query applied row: %v", err)
	}
	if value != "ok;still string" {
		t.Fatalf("applied value = %q", value)
	}
	err = executeSystemUpgradeSQL(context.Background(), conn, systemUpgrade{ID: "bad", SQL: "SELECT * FROM missing_table;"})
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("bad execute error = %v, want upgrade id in error", err)
	}

	assertSystemUpgradePanic(t, "panic without cause", nil)
	assertSystemUpgradePanic(t, "panic with cause", errors.New("cause"))
}

func assertSystemUpgradePanic(t *testing.T, action string, err error) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatalf("panicSystemUpgrade(%q) did not panic", action)
		}
	}()
	panicSystemUpgrade(action, err)
}

func TestPrepareSystemUpgradeTableRejectsSQLitePostgresDDL(t *testing.T) {
	db, err := stdsql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	}()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn sqlite: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("close sqlite conn: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := prepareSystemUpgradeTable(ctx, conn); err == nil {
		t.Fatal("prepareSystemUpgradeTable unexpectedly accepted PostgreSQL DDL on SQLite")
	}
	if err := ensureSystemUpgradeColumns(ctx, conn, "public.system_upgrade"); err == nil {
		t.Fatal("ensureSystemUpgradeColumns unexpectedly accepted PostgreSQL ALTER syntax on SQLite")
	}
	if err := normalizeSystemUpgradePrimaryKey(ctx, conn); err == nil {
		t.Fatal("normalizeSystemUpgradePrimaryKey unexpectedly accepted PostgreSQL DO block on SQLite")
	}

	if _, err := entschema.CopyTables(migrate.Tables); err != nil {
		t.Fatalf("CopyTables sanity check failed: %v", err)
	}
}

func TestBalanceAlertEmailWithSMTPSettings(t *testing.T) {
	host, port, received := startTestSMTPServer(t)
	settingsService := appsettings.NewService(testSettingsRepo{
		byGroup: map[string][]appsettings.Setting{
			"smtp": {
				{Key: "smtp_host", Value: host},
				{Key: "smtp_port", Value: strconv.Itoa(port)},
				{Key: "smtp_from_email", Value: "noreply@example.com"},
				{Key: "smtp_from_name", Value: "AirGate Test"},
				{Key: "smtp_use_tls", Value: "false"},
				{Key: "balance_alert_email_subject", Value: "{{site_name}} balance {{threshold}}"},
				{Key: "balance_alert_email_body", Value: "hello {{site_name}} {{balance}} {{threshold}}"},
			},
			"site": {
				{Key: "site_name", Value: "Custom AirGate"},
			},
		},
	})

	balanceAlertSendEmail(settingsService, "user@example.com", 1.2345, 2)

	select {
	case msg := <-received:
		for _, want := range []string{
			"From: AirGate Test <noreply@example.com>",
			"To: user@example.com",
			"Subject: Custom AirGate balance $2.00",
			"hello Custom AirGate $1.2345 $2.00",
		} {
			if !strings.Contains(msg, want) {
				t.Fatalf("SMTP message missing %q:\n%s", want, msg)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SMTP message")
	}
}

func TestBalanceAlertEmailErrorBranches(t *testing.T) {
	balanceAlertSendEmail(appsettings.NewService(testSettingsRepo{err: errors.New("settings down")}), "user@example.com", 1, 2)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen closed smtp port: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	settingsService := appsettings.NewService(testSettingsRepo{
		byGroup: map[string][]appsettings.Setting{
			"smtp": {
				{Key: "smtp_host", Value: "127.0.0.1"},
				{Key: "smtp_port", Value: strconv.Itoa(addr.Port)},
				{Key: "smtp_from_email", Value: "noreply@example.com"},
			},
		},
	})
	balanceAlertSendEmail(settingsService, "user@example.com", 1, 2)
}

func TestAdditionalSystemUpgradeParsingBranches(t *testing.T) {
	for _, name := range []string{
		"20260528143015_   .sql",
		"20260528143015_.sql",
	} {
		if err := validateSystemUpgradeFilename(name); err == nil {
			t.Fatalf("filename with missing description accepted: %s", name)
		}
	}
	if tag, ok := sqlDollarTag("$abc"); ok || tag != "" {
		t.Fatalf("unterminated sqlDollarTag = %q %v", tag, ok)
	}

	statements := splitSQLStatements(`SELECT 'it''s;ok'; SELECT "a""b"; SELECT 3`)
	if len(statements) != 3 || statements[0] != "SELECT 'it''s;ok'" || statements[1] != `SELECT "a""b"` || statements[2] != "SELECT 3" {
		t.Fatalf("split escaped/tail statements = %#v", statements)
	}
}

func TestRunSystemUpgradesPanicsOnSQLiteDriver(t *testing.T) {
	db, err := stdsql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	}()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("RunSystemUpgrades did not panic on PostgreSQL advisory lock SQL")
		}
		if !strings.Contains(fmt.Sprint(recovered), "lock system upgrades") {
			t.Fatalf("panic = %v, want lock system upgrades", recovered)
		}
	}()
	RunSystemUpgrades(entsql.OpenDB("sqlite", db))
}

func TestRunSystemUpgradesWithMockPostgresDriver(t *testing.T) {
	upgrades := loadSystemUpgrades()
	if len(upgrades) == 0 {
		t.Fatal("loadSystemUpgrades returned no upgrades")
	}

	state := &systemUpgradeMockState{applied: map[string]*string{}}
	db := openSystemUpgradeMockDB(t, state)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close mock db: %v", err)
		}
	}()
	RunSystemUpgrades(entsql.OpenDB("postgres", db))
	if !state.execContains("pg_advisory_lock") || !state.execContains("pg_advisory_unlock") {
		t.Fatalf("lock/unlock statements missing: %v", state.execs)
	}
	if got := state.execCount("INSERT INTO public.system_upgrade"); got != len(upgrades) {
		t.Fatalf("insert count = %d, want %d", got, len(upgrades))
	}
	if !state.execContains("CREATE TABLE IF NOT EXISTS public.system_upgrade") ||
		!state.execContains("ALTER TABLE public.system_upgrade ADD COLUMN IF NOT EXISTS id text") ||
		!state.execContains("ALTER TABLE public.system_upgrade ADD CONSTRAINT system_upgrade_pkey PRIMARY KEY") {
		t.Fatalf("prepare statements missing: %v", state.execs)
	}

	emptyChecksum := ""
	state = &systemUpgradeMockState{applied: map[string]*string{upgrades[0].ID: &emptyChecksum}}
	db = openSystemUpgradeMockDB(t, state)
	defer func() { _ = db.Close() }()
	RunSystemUpgrades(entsql.OpenDB("postgres", db))
	if !state.execContains("UPDATE public.system_upgrade") {
		t.Fatalf("backfill update statement missing: %v", state.execs)
	}

	badChecksum := "wrong"
	state = &systemUpgradeMockState{applied: map[string]*string{upgrades[0].ID: &badChecksum}}
	db = openSystemUpgradeMockDB(t, state)
	defer func() { _ = db.Close() }()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("RunSystemUpgrades did not panic on checksum mismatch")
		}
		if !strings.Contains(fmt.Sprint(recovered), "verify system upgrade checksum") {
			t.Fatalf("panic = %v, want checksum verification", recovered)
		}
	}()
	RunSystemUpgrades(entsql.OpenDB("postgres", db))
}

var (
	systemUpgradeMockOnce   sync.Once
	systemUpgradeMockMu     sync.Mutex
	systemUpgradeMockStates = map[string]*systemUpgradeMockState{}
)

type systemUpgradeMockState struct {
	mu      sync.Mutex
	applied map[string]*string
	execs   []string
}

func (s *systemUpgradeMockState) execContains(fragment string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, query := range s.execs {
		if strings.Contains(query, fragment) {
			return true
		}
	}
	return false
}

func (s *systemUpgradeMockState) execCount(fragment string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, query := range s.execs {
		if strings.Contains(query, fragment) {
			count++
		}
	}
	return count
}

func openSystemUpgradeMockDB(t *testing.T, state *systemUpgradeMockState) *stdsql.DB {
	t.Helper()
	systemUpgradeMockOnce.Do(func() {
		stdsql.Register("airgate_system_upgrade_mock", systemUpgradeMockDriver{})
	})
	dsn := fmt.Sprintf("%s-%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	systemUpgradeMockMu.Lock()
	systemUpgradeMockStates[dsn] = state
	systemUpgradeMockMu.Unlock()
	t.Cleanup(func() {
		systemUpgradeMockMu.Lock()
		delete(systemUpgradeMockStates, dsn)
		systemUpgradeMockMu.Unlock()
	})

	db, err := stdsql.Open("airgate_system_upgrade_mock", dsn)
	if err != nil {
		t.Fatalf("open mock db: %v", err)
	}
	return db
}

type systemUpgradeMockDriver struct{}

func (systemUpgradeMockDriver) Open(name string) (driver.Conn, error) {
	systemUpgradeMockMu.Lock()
	state := systemUpgradeMockStates[name]
	systemUpgradeMockMu.Unlock()
	if state == nil {
		return nil, fmt.Errorf("unknown system upgrade mock dsn %q", name)
	}
	return &systemUpgradeMockConn{state: state}, nil
}

type systemUpgradeMockConn struct {
	state *systemUpgradeMockState
}

func (c *systemUpgradeMockConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not implemented")
}

func (c *systemUpgradeMockConn) Close() error {
	return nil
}

func (c *systemUpgradeMockConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not implemented")
}

func (c *systemUpgradeMockConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	c.state.execs = append(c.state.execs, query)
	if strings.Contains(query, "UPDATE public.system_upgrade") && len(args) >= 3 {
		id, _ := args[0].Value.(string)
		checksum, _ := args[1].Value.(string)
		if id != "" {
			copied := checksum
			c.state.applied[id] = &copied
		}
	}
	c.state.mu.Unlock()
	return driver.RowsAffected(1), nil
}

func (c *systemUpgradeMockConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "SELECT checksum FROM public.system_upgrade") || len(args) == 0 {
		return &systemUpgradeMockRows{columns: []string{"checksum"}}, nil
	}
	id, _ := args[0].Value.(string)
	c.state.mu.Lock()
	checksum, ok := c.state.applied[id]
	c.state.mu.Unlock()
	if !ok {
		return &systemUpgradeMockRows{columns: []string{"checksum"}}, nil
	}
	if checksum == nil {
		return &systemUpgradeMockRows{columns: []string{"checksum"}, values: [][]driver.Value{{nil}}}, nil
	}
	return &systemUpgradeMockRows{columns: []string{"checksum"}, values: [][]driver.Value{{*checksum}}}, nil
}

type systemUpgradeMockRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *systemUpgradeMockRows) Columns() []string {
	return r.columns
}

func (r *systemUpgradeMockRows) Close() error {
	return nil
}

func (r *systemUpgradeMockRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

type testSettingsRepo struct {
	byGroup map[string][]appsettings.Setting
	err     error
}

func (r testSettingsRepo) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	if r.err != nil {
		return nil, r.err
	}
	items := r.byGroup[group]
	out := make([]appsettings.Setting, len(items))
	copy(out, items)
	return out, nil
}

func (r testSettingsRepo) UpsertMany(context.Context, []appsettings.ItemInput) error {
	return nil
}

func startTestSMTPServer(t *testing.T) (string, int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		writeLine := func(line string) {
			_, _ = fmt.Fprintf(conn, "%s\r\n", line)
		}
		writeLine("220 test smtp")
		var msg strings.Builder
		inData := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					received <- msg.String()
					writeLine("250 queued")
					inData = false
					continue
				}
				msg.WriteString(line)
				msg.WriteByte('\n')
				continue
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				writeLine("250-localhost")
				writeLine("250 OK")
			case strings.HasPrefix(upper, "MAIL FROM:"), strings.HasPrefix(upper, "RCPT TO:"):
				writeLine("250 OK")
			case upper == "DATA":
				writeLine("354 end data")
				inData = true
			case upper == "QUIT":
				writeLine("221 bye")
				return
			default:
				writeLine("250 OK")
			}
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, received
}
