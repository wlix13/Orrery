package mongo_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	driver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/wlix13/orrery/collector/internal/store"
	mongostore "github.com/wlix13/orrery/collector/internal/store/mongo"
	"github.com/wlix13/orrery/collector/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, openTestStore)
}

// testServerBase is a bare "mongodb://host:port" server URI (no database
// path). testSkipReason is set instead when no server is available, and
// every test skips with it. Both are set once by TestMain, before any test
// runs, so every subtest (and every top-level test in this package) shares
// the same server.
var (
	testServerBase string
	testSkipReason string
	dbCounter      int64
)

// TestMain owns the test MongoDB server's lifecycle: it must outlive every
// subtest, which a container started (and torn down) via a single
// subtest's t.Cleanup would not - the cleanup would fire as soon as that
// one subtest finished, killing the server for the rest of the suite.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	if uri := os.Getenv("ORRERY_TEST_MONGO_URI"); uri != "" {
		testServerBase = strings.TrimRight(uri, "/")
		return m.Run()
	}

	stop, addr, err := startMongoContainer()
	if err != nil {
		testSkipReason = err.Error()
		return m.Run()
	}

	defer stop()

	if err := waitForMongo(addr); err != nil {
		testSkipReason = fmt.Sprintf("mongo did not become ready: %v", err)
		return m.Run()
	}

	testServerBase = addr

	return m.Run()
}

// openTestStore is the storetest.Factory: every call gets a fresh, empty
// database on the shared test server.
func openTestStore(t *testing.T) store.Store {
	t.Helper()

	if testServerBase == "" {
		t.Skip(testSkipReason)
	}

	n := atomic.AddInt64(&dbCounter, 1)
	uri := fmt.Sprintf("%s/orrery_test_%d", testServerBase, n)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := mongostore.Open(ctx, uri)
	if err != nil {
		t.Fatalf("open mongo store: %v", err)
	}

	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()

		if err := s.DropDatabase(dropCtx); err != nil {
			t.Errorf("drop test database: %v", err)
		}

		if err := s.Close(); err != nil {
			t.Errorf("close mongo store: %v", err)
		}
	})

	return s
}

// startMongoContainer launches a throwaway mongo:8 container bound to a
// random host port and returns a func that stops it, plus its server URI.
func startMongoContainer() (stop func(), addr string, err error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, "", fmt.Errorf("docker not found in PATH: %w", err)
	}

	// Omitting the host port (rather than pinning ":0:") asks the daemon
	// for a random free port; it's the portable spelling across Docker and
	// Podman's docker-CLI shim, both of which this backend may run under.
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::27017", "mongo:8").CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("docker run mongo:8: %w: %s", err, out)
	}

	cid := strings.TrimSpace(string(out))
	stop = func() { _ = exec.Command("docker", "stop", cid).Run() }

	portOut, err := exec.Command("docker", "port", cid, "27017/tcp").Output()
	if err != nil {
		stop()
		return nil, "", fmt.Errorf("docker port: %w", err)
	}

	hostPort := strings.TrimSpace(strings.SplitN(string(portOut), "\n", 2)[0])
	if hostPort == "" {
		stop()
		return nil, "", fmt.Errorf("docker port returned no mapping")
	}

	return stop, "mongodb://" + hostPort, nil
}

// waitForMongo retries a bare connect+ping until the server accepts
// connections or the deadline passes.
func waitForMongo(uri string) error {
	deadline := time.Now().Add(30 * time.Second)

	var lastErr error

	for time.Now().Before(deadline) {
		err := pingOnce(uri)
		if err == nil {
			return nil
		}

		lastErr = err

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("mongo not ready after 30s: %w", lastErr)
}

func pingOnce(uri string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, err := driver.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return err
	}

	defer func() { _ = client.Disconnect(context.Background()) }()

	return client.Ping(ctx, nil)
}
