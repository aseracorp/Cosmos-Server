package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTempConfigFile points the config file at a fresh path in a temp dir and
// returns it. GetConfigFileName honours CONFIG_FILE.
func newTempConfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cosmos.config.json")
	t.Setenv("CONFIG_FILE", path)
	return path
}

func readRawConfig(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func decodeConfig(t *testing.T, path string) Config {
	t.Helper()
	var c Config
	if err := json.Unmarshal([]byte(readRawConfig(t, path)), &c); err != nil {
		t.Fatalf("config on disk is not decodable: %v", err)
	}
	return c
}

func cronKeys(m map[string]CRONConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestWriteConfigFileIsAtomic(t *testing.T) {
	path := newTempConfigFile(t)

	if err := WriteConfigFile(Config{LoggingLevel: "INFO"}); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing %s: %v", dir, err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("after a successful write the config dir holds %d files (%s), want 1: no temp file may survive",
			len(entries), strings.Join(names, ", "))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %o, want 600 (it holds private keys and tokens)", perm)
	}

	// layout matches the previous writer: two-space indent, trailing newline
	raw := readRawConfig(t, path)
	if !strings.Contains(raw, "\n  \"LoggingLevel\": \"INFO\"") {
		t.Errorf("config not indented with two spaces, got:\n%s", raw)
	}
	if !strings.HasSuffix(raw, "}\n") {
		t.Errorf("config does not end with a newline, got: %q", raw[max(0, len(raw)-3):])
	}

	if err := WriteConfigFile(Config{LoggingLevel: "DEBUG"}); err != nil {
		t.Fatalf("WriteConfigFile (replace): %v", err)
	}
	if entries, _ = os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("after replace the config dir holds %d files, want 1", len(entries))
	}
	if got := GetBaseMainConfig().LoggingLevel; got == "DEBUG" {
		t.Errorf("WriteConfigFile published the config to memory; it writes the file only")
	}
}

// A writer that dies halfway used to leave the config file truncated, and
// ReadConfigFromFile calls Fatal when it meets such a file. With
// encode-to-buffer + rename, a failing write leaves the previous complete config
// in place.
func TestWriteConfigFileLeavesNoPartialFile(t *testing.T) {
	path := newTempConfigFile(t)

	good := Config{LoggingLevel: "INFO", HTTPConfig: HTTPConfig{Hostname: "host.example.com"}}
	if err := WriteConfigFile(good); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}
	before := readRawConfig(t, path)

	// a config path in a directory that does not exist: the write fails after the
	// config file name is already in play
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing-dir", "cosmos.config.json"))
	if err := WriteConfigFile(Config{}); err == nil {
		t.Fatal("writing into a missing directory reported no error")
	}

	if after := readRawConfig(t, path); after != before {
		t.Errorf("a failing write changed the config file on disk")
	}

	probe := decodeConfig(t, path)
	if probe.HTTPConfig.Hostname != "host.example.com" {
		t.Errorf("config on disk lost its content: %+v", probe.HTTPConfig)
	}
}

func TestUpdateConfigAbortsAndPersists(t *testing.T) {
	path := newTempConfigFile(t)
	if err := WriteConfigFile(Config{LoggingLevel: "INFO"}); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}
	before := readRawConfig(t, path)

	abort := AbortConfigWrite("route already exists")
	err := UpdateConfig(func(c *Config) error {
		c.LoggingLevel = "ERROR"
		return abort
	})
	if err == nil {
		t.Fatal("aborted update returned nil")
	}
	msg, ok := ConfigWriteAbortMessage(err)
	if !ok || msg != "route already exists" {
		t.Errorf("ConfigWriteAbortMessage(%v) = (%q, %v), want (\"route already exists\", true)", err, msg, ok)
	}
	if strings.Contains(err.Error(), "sentinel") {
		t.Errorf("abort error leaks the sentinel type: %v", err)
	}
	if got := readRawConfig(t, path); got != before {
		t.Errorf("aborted update wrote to disk")
	}

	// Only an abort is recoverable as a message: a real write failure must not be
	// reported to the client as a validation result, and an abort must survive the
	// wrapping handlers do on the way out.
	if _, ok := ConfigWriteAbortMessage(errors.New("disk full")); ok {
		t.Error("a plain write failure reports as an abort")
	}
	wrapped := fmt.Errorf("saving config: %w", AbortConfigWrite("nope"))
	if msg, ok := ConfigWriteAbortMessage(wrapped); !ok || msg != "nope" {
		t.Errorf("abort lost through wrapping: (%q, %v)", msg, ok)
	}
	if !strings.Contains(wrapped.Error(), "saving config") {
		t.Errorf("wrapping context lost: %v", wrapped)
	}
}

// 60 concurrent read/modify/write cycles over one map — the classic lost-update
// pattern. The old flow read, mutated a snapshot and wrote under three separate
// acquisitions of the internal lock, so a cycle that started before another
// finished overwrote it. Serialising the cycle means every entry lands.
//
// A lost update is a missing key here, not an error: each cycle writes a distinct
// key, so the count is the assertion.
func TestUpdateConfigConcurrentWritesAllLand(t *testing.T) {
	path := newTempConfigFile(t)
	if err := WriteConfigFile(Config{LoggingLevel: "INFO"}); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	const n = 60
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("job-%d", i)
			errs[i] = UpdateConfig(func(c *Config) error {
				if c.CRON == nil {
					c.CRON = map[string]CRONConfig{}
				}
				c.CRON[name] = CRONConfig{Name: name, Crontab: "* * * * *", Command: "echo"}
				return nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("update %d: %v", i, err)
		}
	}

	inMemory, onDisk := GetMainConfig(), decodeConfig(t, path)
	for _, tc := range []struct {
		label  string
		config Config
		want   int
	}{{"in-memory", inMemory, n}, {"on-disk", onDisk, n}} {
		if got := len(tc.config.CRON); got != tc.want {
			t.Fatalf("%s config holds %d of %d cron jobs (lost-update pattern). keys: %v",
				tc.label, got, tc.want, cronKeys(tc.config.CRON))
		}
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("job-%d", i)
		if _, ok := onDisk.CRON[name]; !ok {
			t.Fatalf("%s missing from the config file", name)
		}
	}
}

// Negative control for the test above: three separate lock acquisitions — read,
// mutate a snapshot, write — lose concurrent writes on the same keys. If this
// test ever stops reporting lost writes, the lost-update test above proves
// nothing.
func TestNaiveReadModifyWriteLosesWrites(t *testing.T) {
	path := newTempConfigFile(t)
	if err := WriteConfigFile(Config{LoggingLevel: "INFO"}); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	const n = 60
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("job-%d", i)
			snapshot := ReadConfigFromFile() // acquisition 1
			if snapshot.CRON == nil {
				snapshot.CRON = map[string]CRONConfig{}
			}
			snapshot.CRON[name] = CRONConfig{Name: name, Crontab: "* * * * *", Command: "echo"}
			SaveConfigTofile(snapshot) // acquisition 2: publishes the stale snapshot
		}(i)
	}
	wg.Wait()

	if got := len(decodeConfig(t, path).CRON); got == n {
		t.Errorf("the unserialised read/modify/write cycle lost no writes (%d/%d): "+
			"the lost-update test above no longer demonstrates the bug it guards", got, n)
	}
}

// The regression the handlers care about: the old three-step cycle published its
// snapshot to memory, so a value written in between vanished from GetMainConfig()
// — the client saw the setting saved, then reverted on refresh.
func TestUpdateConfigSeesInterleavedWrites(t *testing.T) {
	path := newTempConfigFile(t)
	if err := WriteConfigFile(Config{LoggingLevel: "INFO"}); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	// the snapshot a handler used to take first, then write back
	stale := ReadConfigFromFile()

	if err := UpdateConfig(func(c *Config) error {
		if c.CRON == nil {
			c.CRON = map[string]CRONConfig{}
		}
		c.CRON["nested"] = CRONConfig{Name: "nested", Crontab: "* * * * *", Command: "echo"}
		return nil
	}); err != nil {
		t.Fatalf("interleaved update: %v", err)
	}

	// the old flow wrote `stale` back at this point: that snapshot predates the
	// write above and carries no "nested" job, which is the whole bug
	if _, ok := stale.CRON["nested"]; ok {
		t.Fatal("snapshot is not stale: the test no longer models the old flow")
	}

	if err := UpdateConfig(func(c *Config) error {
		if c.CRON == nil {
			c.CRON = map[string]CRONConfig{}
		}
		c.CRON["outer"] = CRONConfig{Name: "outer", Crontab: "0 4 * * *", Command: "backup"}
		return nil
	}); err != nil {
		t.Fatalf("outer update: %v", err)
	}

	for _, tc := range []struct {
		label  string
		config Config
	}{{"in-memory", GetMainConfig()}, {"on-disk", decodeConfig(t, path)}} {
		for _, name := range []string{"nested", "outer"} {
			if _, ok := tc.config.CRON[name]; !ok {
				t.Errorf("%s config lost the %q write: keys %v", tc.label, name, cronKeys(tc.config.CRON))
			}
		}
	}
}

// UpdateConfigFromFile leaves the in-memory config alone: a caller that only
// wants the file must not publish a snapshot over live runtime state.
func TestUpdateConfigFromFileLeavesMemoryAlone(t *testing.T) {
	path := newTempConfigFile(t)
	if err := WriteConfigFile(Config{LoggingLevel: "INFO"}); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}
	LoadBaseMainConfig(Config{LoggingLevel: "DEBUG"})
	t.Cleanup(func() { LoadBaseMainConfig(Config{LoggingLevel: "INFO"}) })

	if err := UpdateConfigFromFile(func(c *Config) error {
		c.LoggingLevel = "ERROR"
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigFromFile: %v", err)
	}

	if got := GetBaseMainConfig().LoggingLevel; got != "DEBUG" {
		t.Errorf("GetBaseMainConfig().LoggingLevel = %q, want \"DEBUG\": UpdateConfigFromFile must not publish", got)
	}
	if got := decodeConfig(t, path).LoggingLevel; got != "ERROR" {
		t.Errorf("config file LoggingLevel = %q, want \"ERROR\": the write itself must land", got)
	}
}
