//go:build e2e

package e2e

import (
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

const (
	instanceCount      = 3
	incomingPartitions = 6
	evaluatedTopic     = "txs.evaluated"
)

var suite struct {
	brokers     []string
	dbDSN       string
	db          *sql.DB
	bin         string
	configPath  string
	composeFile string
	incoming    string
	dlq         string
	instances   []*instance
	producer    *producer
	collector   *collector
}

func TestMain(m *testing.M) {
	brokers, adminDSN := os.Getenv("RISK_E2E_BROKERS"), os.Getenv("RISK_E2E_DB_DSN")
	if brokers == "" || adminDSN == "" {
		fmt.Println("skipping e2e: set RISK_E2E_BROKERS and RISK_E2E_DB_DSN")
		os.Exit(0)
	}
	os.Exit(runSuite(m, strings.Split(brokers, ","), adminDSN))
}

func runSuite(m *testing.M, brokers []string, adminDSN string) int {
	var cleanups []func()
	defer func() {
		for _, c := range slices.Backward(cleanups) {
			c()
		}
	}()

	if err := setUp(brokers, adminDSN, &cleanups); err != nil {
		fmt.Fprintln(os.Stderr, "e2e setup:", err)
		dumpLogs()
		return 1
	}

	code := m.Run()
	if code != 0 {
		dumpLogs()
	}
	return code
}

func setUp(brokers []string, adminDSN string, cleanups *[]func()) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	run := randomHex(4)
	suite.brokers = brokers
	suite.incoming = "txs.incoming.e2e-" + run
	suite.dlq = suite.incoming + ".dlq"
	suite.composeFile = cmp.Or(os.Getenv("RISK_E2E_COMPOSE_FILE"), "../compose.yaml")

	tmp, err := os.MkdirTemp("", "risk-e2e-")
	if err != nil {
		return err
	}
	*cleanups = append(*cleanups, func() { os.RemoveAll(tmp) })

	suite.bin = filepath.Join(tmp, "risk")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", suite.bin, "risk/cmd/risk").CombinedOutput(); err != nil {
		return fmt.Errorf("build: %w: %s", err, out)
	}

	dsn, drop, err := createDB(ctx, adminDSN, "risk_e2e_"+run)
	if err != nil {
		return err
	}
	*cleanups = append(*cleanups, drop)
	suite.dbDSN = dsn
	if suite.db, err = sql.Open("postgres", dsn); err != nil {
		return err
	}
	*cleanups = append(*cleanups, func() { suite.db.Close() })
	if err := migrateUp(ctx); err != nil {
		return err
	}

	if err := createTopics(ctx, brokers, map[string]int32{
		suite.incoming: incomingPartitions,
		suite.dlq:      1,
		evaluatedTopic: incomingPartitions,
	}); err != nil {
		return err
	}

	if suite.collector, err = startCollector(brokers, time.Now()); err != nil {
		return err
	}
	*cleanups = append(*cleanups, suite.collector.close)

	if suite.producer, err = newProducer(brokers); err != nil {
		return err
	}
	*cleanups = append(*cleanups, suite.producer.close)

	suite.configPath = filepath.Join(tmp, "config.json")
	if err := os.WriteFile(suite.configPath, testConfig(suite.incoming, suite.dlq, "risk-e2e-"+run), 0o600); err != nil {
		return err
	}

	*cleanups = append(*cleanups, stopInstances)
	for i := range instanceCount {
		inst, err := startInstance(fmt.Sprintf("risk-%d", i+1))
		if err != nil {
			return err
		}
		suite.instances = append(suite.instances, inst)
	}
	return nil
}

func testConfig(incoming, dlq, group string) []byte {
	return fmt.Appendf(nil, `{
  "bands": {"minMedium": 30, "minHigh": 60},
  "rules": [
    {"name": "highAmountSpend", "rule": "highAmount", "enabled": true, "score": 40,
     "appliesTo": ["card", "transferOut"], "multiplier": 3, "minBaselineTxs": 3, "medianOfLast": 200, "defaultThreshold": 1000000},
    {"name": "freshPayee", "rule": "freshPayee", "enabled": true, "score": 35,
     "appliesTo": ["transferOut"], "maxPayeeAge": "1h", "minAmount": 100000}
  ],
  "history": {"lookback": "2160h"},
  "consumer": {
    "topic": %q, "group": %q, "deadLetterTopic": %q,
    "retry": {"failAfterAttempts": 2, "initialBackoff": "100ms", "maxBackoff": "500ms"},
    "workers": 8, "maxPendingPerPartition": 100
  }
}`, incoming, group, dlq)
}

func createDB(ctx context.Context, adminDSN, name string) (string, func(), error) {
	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		return "", func() {}, err
	}
	if _, err := admin.ExecContext(ctx, "create database "+name); err != nil {
		admin.Close()
		return "", func() {}, fmt.Errorf("create database: %w", err)
	}
	drop := func() {
		if _, err := admin.Exec("drop database if exists " + name + " with (force)"); err != nil {
			fmt.Fprintln(os.Stderr, "drop database:", err)
		}
		admin.Close()
	}

	u, err := url.Parse(adminDSN)
	if err != nil {
		return "", drop, err
	}
	u.Path = "/" + name
	return u.String(), drop, nil
}

func migrateUp(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, suite.bin, "migrate", "up")
	cmd.Env = append(os.Environ(), "RISK_DB_DSN="+suite.dbDSN)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("risk migrate up: %w: %s", err, out)
	}
	return nil
}

func dockerCompose(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("docker", append([]string{"compose", "-f", suite.composeFile}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker compose %v: %v: %s", args, err, out)
	}
}

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func newAccount() string { return "acc-" + randomHex(8) }
func newTxID() string    { return "tx-" + randomHex(8) }
