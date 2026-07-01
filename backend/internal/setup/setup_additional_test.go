package setup

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/config"
)

func TestNeedsSetupDatabaseStateBranches(t *testing.T) {
	tests := []struct {
		name     string
		openErr  error
		scenario *setupSQLScenario
		want     bool
	}{
		{name: "open error", openErr: errors.New("open failed"), want: true},
		{name: "ping bootstrap error", scenario: &setupSQLScenario{pingErr: &pq.Error{Code: "3D000"}}, want: true},
		{name: "ping fatal error", scenario: &setupSQLScenario{pingErr: &pq.Error{Code: "42501"}}, want: false},
		{name: "query bootstrap error", scenario: &setupSQLScenario{queryErr: &pq.Error{Code: "42P01"}}, want: true},
		{name: "query fatal error", scenario: &setupSQLScenario{queryErr: &pq.Error{Code: "42501"}}, want: false},
		{name: "no admin", scenario: &setupSQLScenario{}, want: true},
		{name: "admin exists", scenario: &setupSQLScenario{adminCount: 1}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreSetupHooks(t)
			clearSetupEnv(t)
			t.Setenv("CONFIG_PATH", writeSetupConfig(t))
			if tt.openErr != nil {
				setupSQLOpen = func(string, string) (*sql.DB, error) {
					return nil, tt.openErr
				}
			} else {
				useSetupSQLSequence(t, tt.scenario)
			}

			if got := NeedsSetup(); got != tt.want {
				t.Fatalf("NeedsSetup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPingDatabaseAndCreateDatabaseBranches(t *testing.T) {
	t.Run("ping open error", func(t *testing.T) {
		restoreSetupHooks(t)
		setupSQLOpen = func(string, string) (*sql.DB, error) {
			return nil, errors.New("open failed")
		}
		if err := pingDatabase("db", 5432, "user", "pass", "airgate", "disable"); err == nil {
			t.Fatal("pingDatabase() error = nil, want open error")
		}
	})

	t.Run("ping error", func(t *testing.T) {
		restoreSetupHooks(t)
		wantErr := errors.New("ping failed")
		useSetupSQLSequence(t, &setupSQLScenario{pingErr: wantErr})
		if err := pingDatabase("db", 5432, "user", "pass", "airgate", "disable"); err != wantErr {
			t.Fatalf("pingDatabase() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("ping success with close warning", func(t *testing.T) {
		restoreSetupHooks(t)
		useSetupSQLSequence(t, &setupSQLScenario{closeErr: errors.New("close failed")})
		if err := pingDatabase("db", 5432, "user", "pass", "airgate", "disable"); err != nil {
			t.Fatalf("pingDatabase() error = %v", err)
		}
	})

	t.Run("create open error", func(t *testing.T) {
		restoreSetupHooks(t)
		setupSQLOpen = func(string, string) (*sql.DB, error) {
			return nil, errors.New("open failed")
		}
		if err := createDatabase("db", 5432, "user", "pass", "airgate", "disable"); err == nil || !strings.Contains(err.Error(), "连接 postgres 系统库失败") {
			t.Fatalf("createDatabase() error = %v", err)
		}
	})

	t.Run("create ping error", func(t *testing.T) {
		restoreSetupHooks(t)
		useSetupSQLSequence(t, &setupSQLScenario{pingErr: errors.New("ping failed")})
		if err := createDatabase("db", 5432, "user", "pass", "airgate", "disable"); err == nil || !strings.Contains(err.Error(), "ping postgres 系统库失败") {
			t.Fatalf("createDatabase() error = %v", err)
		}
	})

	t.Run("create exec error", func(t *testing.T) {
		restoreSetupHooks(t)
		wantErr := errors.New("exec failed")
		useSetupSQLSequence(t, &setupSQLScenario{execErr: wantErr})
		if err := createDatabase("db", 5432, "user", "pass", "airgate", "disable"); err != wantErr {
			t.Fatalf("createDatabase() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("create success quotes name", func(t *testing.T) {
		restoreSetupHooks(t)
		var queries []string
		useSetupSQLSequence(t, &setupSQLScenario{execQueries: &queries, closeErr: errors.New("close failed")})
		if err := createDatabase("db", 5432, "user", "pass", `air"gate`, "disable"); err != nil {
			t.Fatalf("createDatabase() error = %v", err)
		}
		if len(queries) != 1 || queries[0] != `CREATE DATABASE "air""gate"` {
			t.Fatalf("exec queries = %v", queries)
		}
	})
}

func TestTestDBConnectionBranches(t *testing.T) {
	t.Run("direct success defaults sslmode", func(t *testing.T) {
		restoreSetupHooks(t)
		dsns := useSetupSQLSequence(t, &setupSQLScenario{})
		if err := TestDBConnection("db", 5432, "user", "pass", "airgate", ""); err != nil {
			t.Fatalf("TestDBConnection() error = %v", err)
		}
		if len(*dsns) != 1 || !strings.Contains((*dsns)[0], "sslmode=disable") {
			t.Fatalf("dsns = %v", *dsns)
		}
	})

	t.Run("non missing database error", func(t *testing.T) {
		restoreSetupHooks(t)
		wantErr := errors.New("network failed")
		useSetupSQLSequence(t, &setupSQLScenario{pingErr: wantErr})
		if err := TestDBConnection("db", 5432, "user", "pass", "airgate", "require"); err != wantErr {
			t.Fatalf("TestDBConnection() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("creates missing database and retries", func(t *testing.T) {
		restoreSetupHooks(t)
		var createQueries []string
		useSetupSQLSequence(t,
			&setupSQLScenario{pingErr: &pq.Error{Code: "3D000"}},
			&setupSQLScenario{execQueries: &createQueries},
			&setupSQLScenario{},
		)
		if err := TestDBConnection("db", 5432, "user", "pass", "airgate", "disable"); err != nil {
			t.Fatalf("TestDBConnection() error = %v", err)
		}
		if len(createQueries) != 1 || createQueries[0] != `CREATE DATABASE "airgate"` {
			t.Fatalf("create queries = %v", createQueries)
		}
	})

	t.Run("create missing database fails", func(t *testing.T) {
		restoreSetupHooks(t)
		useSetupSQLSequence(t,
			&setupSQLScenario{pingErr: &pq.Error{Code: "3D000"}},
			&setupSQLScenario{execErr: errors.New("permission denied")},
		)
		if err := TestDBConnection("db", 5432, "user", "pass", "airgate", "disable"); err == nil || !strings.Contains(err.Error(), "自动创建失败") {
			t.Fatalf("TestDBConnection() error = %v", err)
		}
	})
}

func TestTestRedisConnectionBranches(t *testing.T) {
	client := defaultSetupRedisNewClient(&redis.Options{Addr: "127.0.0.1:0"})
	_ = client.Close()

	t.Run("success", func(t *testing.T) {
		restoreSetupHooks(t)
		fake := &fakeSetupRedisClient{}
		setupRedisNewClient = func(opts *redis.Options) setupRedisClient {
			if opts.Addr != "redis:6379" || opts.Password != "secret" || opts.DB != 2 || opts.TLSConfig == nil {
				t.Fatalf("redis options = %+v", opts)
			}
			return fake
		}
		if err := TestRedisConnection("redis", 6379, "secret", 2, true); err != nil {
			t.Fatalf("TestRedisConnection() error = %v", err)
		}
		if !fake.pingCalled || !fake.closed {
			t.Fatalf("fake redis state = %+v", fake)
		}
	})

	t.Run("ping error", func(t *testing.T) {
		restoreSetupHooks(t)
		wantErr := errors.New("redis failed")
		setupRedisNewClient = func(*redis.Options) setupRedisClient {
			return &fakeSetupRedisClient{pingErr: wantErr}
		}
		if err := TestRedisConnection("redis", 6379, "", 0, false); err != wantErr {
			t.Fatalf("TestRedisConnection() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("close warning", func(t *testing.T) {
		restoreSetupHooks(t)
		setupRedisNewClient = func(*redis.Options) setupRedisClient {
			return &fakeSetupRedisClient{closeErr: errors.New("close failed")}
		}
		if err := TestRedisConnection("redis", 6379, "", 0, false); err != nil {
			t.Fatalf("TestRedisConnection() error = %v", err)
		}
	})
}

func TestSetupRouteSuccessFailureAndGuardBranches(t *testing.T) {
	restoreSetupHooks(t)
	clearSetupEnv(t)
	setupInstallCallbackDelay = 0

	needsSetup := true
	dbErr := error(nil)
	redisErr := error(nil)
	var installed InstallParams
	installErr := error(nil)
	setupNeedsSetup = func() bool { return needsSetup }
	setupEnvDBConfig = func() *config.DatabaseConfig {
		return &config.DatabaseConfig{Host: "env-db", Port: 15432, User: "env-user", Password: "env-pass", DBName: "env-airgate", SSLMode: "require"}
	}
	setupEnvRedisConfig = func() *config.RedisConfig {
		return &config.RedisConfig{Host: "env-redis", Port: 16379, Password: "env-secret", DB: 3}
	}
	setupTestDBConnection = func(string, int, string, string, string, string) error { return dbErr }
	setupTestRedisConnection = func(string, int, string, int, bool) error { return redisErr }
	setupInstall = func(params InstallParams) error {
		installed = params
		return installErr
	}

	done := make(chan struct{}, 1)
	router := gin.New()
	RegisterRoutesWithCallback(router, func() { done <- struct{}{} })

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/setup/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", w.Code, w.Body.String())
	}
	var statusResp struct {
		Data struct {
			EnvDB    *struct{ Host string } `json:"env_db"`
			EnvRedis *struct{ Host string } `json:"env_redis"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if statusResp.Data.EnvDB == nil || statusResp.Data.EnvDB.Host != "env-db" || statusResp.Data.EnvRedis == nil || statusResp.Data.EnvRedis.Host != "env-redis" {
		t.Fatalf("status hints = %+v", statusResp.Data)
	}

	for _, tc := range []struct {
		name string
		path string
		body string
		errp *error
	}{
		{name: "db success", path: "/setup/test-db", body: `{"host":"db","port":5432,"user":"u","password":"p","dbname":"airgate"}`, errp: &dbErr},
		{name: "redis success", path: "/setup/test-redis", body: `{"host":"redis","port":6379,"password":"p","db":1}`, errp: &redisErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			*tc.errp = nil
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			if !connectionSuccess(t, w.Body.Bytes()) {
				t.Fatalf("%s success body=%s", tc.path, w.Body.String())
			}

			*tc.errp = errors.New("connection failed")
			w = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			if connectionSuccess(t, w.Body.Bytes()) {
				t.Fatalf("%s failure body=%s", tc.path, w.Body.String())
			}
			*tc.errp = nil
		})
	}

	installReq := `{"database":{"host":"posted-db","port":5432,"user":"u","password":"p","dbname":"posted"},"redis":{"host":"posted-redis","port":6379,"password":"p","db":1},"admin":{"email":"admin@example.com","password":"secret123"}}`
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup/install", strings.NewReader(installReq))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("install status = %d body=%s", w.Code, w.Body.String())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("install callback was not called")
	}
	if installed.DB.Host != "env-db" || installed.Redis.Host != "env-redis" || installed.Admin.Email != "admin@example.com" {
		t.Fatalf("installed params = %+v", installed)
	}

	installErr = errors.New("install failed")
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/setup/install", strings.NewReader(installReq))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("install error status = %d body=%s", w.Code, w.Body.String())
	}
	installErr = nil

	needsSetup = false
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/setup/test-db", strings.NewReader(`{"host":"db","port":5432,"user":"u","dbname":"airgate"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("guard status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestInstallBranches(t *testing.T) {
	t.Run("already installed", func(t *testing.T) {
		restoreSetupHooks(t)
		setupNeedsSetup = func() bool { return false }
		if err := Install(InstallParams{}); err == nil || !strings.Contains(err.Error(), "系统已安装") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("database test fails", func(t *testing.T) {
		restoreSetupHooks(t)
		setupNeedsSetup = func() bool { return true }
		setupTestDBConnection = func(string, int, string, string, string, string) error {
			return errors.New("db failed")
		}
		if err := Install(validInstallParams()); err == nil || !strings.Contains(err.Error(), "数据库连接失败") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("open database fails", func(t *testing.T) {
		restoreSetupHooks(t)
		setupNeedsSetup = func() bool { return true }
		setupTestDBConnection = func(string, int, string, string, string, string) error { return nil }
		setupEntSQLOpen = func(string, string) (*entsql.Driver, error) {
			return nil, errors.New("open failed")
		}
		if err := Install(validInstallParams()); err == nil || !strings.Contains(err.Error(), "打开数据库失败") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("migration fails", func(t *testing.T) {
		restoreSetupHooks(t)
		setupInstallSQLite(t, true)
		if err := Install(validInstallParams()); err == nil || !strings.Contains(err.Error(), "数据库迁移失败") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("password hash fails", func(t *testing.T) {
		restoreSetupHooks(t)
		setupInstallSQLite(t, false)
		params := validInstallParams()
		params.Admin.Password = strings.Repeat("x", 100)
		if err := Install(params); err == nil || !strings.Contains(err.Error(), "密码加密失败") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("create admin fails", func(t *testing.T) {
		restoreSetupHooks(t)
		setupInstallSQLite(t, false)
		params := validInstallParams()
		params.Admin.Email = ""
		if err := Install(params); err == nil || !strings.Contains(err.Error(), "创建管理员失败") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("marshal config fails", func(t *testing.T) {
		restoreSetupHooks(t)
		setupInstallSQLite(t, false)
		setupYAMLMarshal = func(interface{}) ([]byte, error) {
			return nil, errors.New("marshal failed")
		}
		if err := Install(validInstallParams()); err == nil || !strings.Contains(err.Error(), "序列化配置失败") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("write config fails", func(t *testing.T) {
		restoreSetupHooks(t)
		setupInstallSQLite(t, false)
		setupWriteFile = func(string, []byte, os.FileMode) error {
			return errors.New("write failed")
		}
		if err := Install(validInstallParams()); err == nil || !strings.Contains(err.Error(), "写入配置文件失败") {
			t.Fatalf("Install() error = %v", err)
		}
	})

	t.Run("success with close warning", func(t *testing.T) {
		restoreSetupHooks(t)
		clearSetupEnv(t)
		t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
		setupInstallSQLite(t, false)
		setupCloseEntClient = func(client *ent.Client) error {
			_ = (*ent.Client).Close(client)
			return errors.New("close failed")
		}
		if err := Install(validInstallParams()); err != nil {
			t.Fatalf("Install() error = %v", err)
		}
		if _, err := os.Stat(config.ConfigPath()); err != nil {
			t.Fatalf("config not written: %v", err)
		}
	})
}

func TestGenerateSecretFallback(t *testing.T) {
	restoreSetupHooks(t)
	setupRandRead = func([]byte) (int, error) {
		return 0, errors.New("random failed")
	}
	if got := generateSecret(); got != "airgate-default-secret-change-me" {
		t.Fatalf("generateSecret() = %q", got)
	}
}

func restoreSetupHooks(t *testing.T) {
	t.Helper()
	prevNeedsSetup := setupNeedsSetup
	prevTestDB := setupTestDBConnection
	prevTestRedis := setupTestRedisConnection
	prevInstall := setupInstall
	prevEnvDB := setupEnvDBConfig
	prevEnvRedis := setupEnvRedisConfig
	prevSQLOpen := setupSQLOpen
	prevEntOpen := setupEntSQLOpen
	prevRedisNew := setupRedisNewClient
	prevCloseEnt := setupCloseEntClient
	prevMarshal := setupYAMLMarshal
	prevWriteFile := setupWriteFile
	prevRandRead := setupRandRead
	prevDelay := setupInstallCallbackDelay
	prevDone := onInstallDone
	t.Cleanup(func() {
		setupNeedsSetup = prevNeedsSetup
		setupTestDBConnection = prevTestDB
		setupTestRedisConnection = prevTestRedis
		setupInstall = prevInstall
		setupEnvDBConfig = prevEnvDB
		setupEnvRedisConfig = prevEnvRedis
		setupSQLOpen = prevSQLOpen
		setupEntSQLOpen = prevEntOpen
		setupRedisNewClient = prevRedisNew
		setupCloseEntClient = prevCloseEnt
		setupYAMLMarshal = prevMarshal
		setupWriteFile = prevWriteFile
		setupRandRead = prevRandRead
		setupInstallCallbackDelay = prevDelay
		onInstallDone = prevDone
	})
}

func writeSetupConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
database:
  host: db
  port: 5432
  user: airgate
  password: secret
  dbname: airgate
  sslmode: disable
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func useSetupSQLSequence(t *testing.T, scenarios ...*setupSQLScenario) *[]string {
	t.Helper()
	registerSetupSQLDriver()
	dsns := make([]string, 0, len(scenarios))
	index := 0
	setupSQLOpen = func(_ string, dsn string) (*sql.DB, error) {
		dsns = append(dsns, dsn)
		if index >= len(scenarios) {
			t.Fatalf("unexpected sql open for dsn %q", dsn)
		}
		scenario := scenarios[index]
		index++
		if scenario == nil {
			scenario = &setupSQLScenario{}
		}
		if scenario.openErr != nil {
			return nil, scenario.openErr
		}
		return sql.Open(setupSQLDriverName, registerSetupSQLScenario(t, scenario))
	}
	return &dsns
}

func registerSetupSQLDriver() {
	setupSQLDriverOnce.Do(func() {
		sql.Register(setupSQLDriverName, setupStubDriver{})
	})
}

func registerSetupSQLScenario(t *testing.T, scenario *setupSQLScenario) string {
	t.Helper()
	key := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "_" + time.Now().Format("150405.000000000")
	setupSQLScenarios.Store(key, scenario)
	t.Cleanup(func() {
		setupSQLScenarios.Delete(key)
	})
	return key
}

func connectionSuccess(t *testing.T, body []byte) bool {
	t.Helper()
	var resp struct {
		Data struct {
			Success bool `json:"success"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, body)
	}
	return resp.Data.Success
}

func validInstallParams() InstallParams {
	var params InstallParams
	params.DB.Host = "db"
	params.DB.Port = 5432
	params.DB.User = "airgate"
	params.DB.Password = "secret"
	params.DB.DBName = "airgate"
	params.DB.SSLMode = "disable"
	params.Redis.Host = "redis"
	params.Redis.Port = 6379
	params.Redis.Password = "secret"
	params.Admin.Email = "admin@example.com"
	params.Admin.Password = "secret123"
	return params
}

func setupInstallSQLite(t *testing.T, closeBeforeMigration bool) {
	t.Helper()
	clearSetupEnv(t)
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	setupNeedsSetup = func() bool { return true }
	setupTestDBConnection = func(string, int, string, string, string, string) error { return nil }
	setupEntSQLOpen = func(string, string) (*entsql.Driver, error) {
		db, err := sql.Open("sqlite", "file:"+strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())+"?mode=memory&cache=shared&_fk=1")
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		if _, err := db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
			_ = db.Close()
			return nil, err
		}
		if closeBeforeMigration {
			_ = db.Close()
		}
		return entsql.OpenDB(dialect.SQLite, db), nil
	}
}

type fakeSetupRedisClient struct {
	pingErr    error
	closeErr   error
	pingCalled bool
	closed     bool
}

func (f *fakeSetupRedisClient) Ping(ctx context.Context) *redis.StatusCmd {
	f.pingCalled = true
	cmd := redis.NewStatusCmd(ctx)
	if f.pingErr != nil {
		cmd.SetErr(f.pingErr)
	} else {
		cmd.SetVal("PONG")
	}
	return cmd
}

func (f *fakeSetupRedisClient) Close() error {
	f.closed = true
	return f.closeErr
}

const setupSQLDriverName = "airgate_setup_stub"

var (
	setupSQLDriverOnce sync.Once
	setupSQLScenarios  sync.Map
)

type setupSQLScenario struct {
	openErr     error
	pingErr     error
	queryErr    error
	execErr     error
	closeErr    error
	adminCount  int
	execQueries *[]string
}

type setupStubDriver struct{}

func (setupStubDriver) Open(name string) (driver.Conn, error) {
	value, ok := setupSQLScenarios.Load(name)
	if !ok {
		return nil, errors.New("missing setup sql scenario")
	}
	return &setupStubConn{scenario: value.(*setupSQLScenario)}, nil
}

type setupStubConn struct {
	scenario *setupSQLScenario
}

func (c *setupStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not implemented")
}

func (c *setupStubConn) Close() error {
	return c.scenario.closeErr
}

func (c *setupStubConn) Begin() (driver.Tx, error) {
	return setupStubTx{}, nil
}

func (c *setupStubConn) Ping(context.Context) error {
	return c.scenario.pingErr
}

func (c *setupStubConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.scenario.queryErr != nil {
		return nil, c.scenario.queryErr
	}
	return &setupStubRows{count: c.scenario.adminCount}, nil
}

func (c *setupStubConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if c.scenario.execQueries != nil {
		*c.scenario.execQueries = append(*c.scenario.execQueries, query)
	}
	if c.scenario.execErr != nil {
		return nil, c.scenario.execErr
	}
	return driver.RowsAffected(1), nil
}

type setupStubRows struct {
	count int
	sent  bool
}

func (r *setupStubRows) Columns() []string {
	return []string{"count"}
}

func (r *setupStubRows) Close() error {
	return nil
}

func (r *setupStubRows) Next(dest []driver.Value) error {
	if r.sent {
		return io.EOF
	}
	r.sent = true
	dest[0] = int64(r.count)
	return nil
}

type setupStubTx struct{}

func (setupStubTx) Commit() error {
	return nil
}

func (setupStubTx) Rollback() error {
	return nil
}
