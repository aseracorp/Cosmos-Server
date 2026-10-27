package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config writers used to open cosmos.config.json with O_TRUNC and encode straight
// into it. A crash, a kill (OOM, SIGKILL during a restart) or a full disk in the
// window between the truncate and the last byte leaves a short file on disk, and
// ReadConfigFromFile calls Fatal on the next start: a truncated config is an
// unreadable config. TLS material, API tokens, routes and backup passwords are
// then only recoverable from a backup.
//
// Encode to a buffer first, write that buffer to a sibling file, fsync it, rename
// over the target and fsync the directory. rename(2) is atomic on POSIX
// filesystems, so readers see either the whole old config or the whole new one,
// never a partial write.
//
// WriteConfigFile atomically replaces the config file with an encoded form of
// config. It does NOT update the in-memory config: callers that mutate the config
// want SetBaseMainConfig (write + publish) or UpdateConfig (serialised
// read/modify/write).
func WriteConfigFile(config Config) error {
	configFile := GetConfigFileName()

	// Indent matches the previous writer so a hand-edited config file keeps its layout.
	buf, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	buf = append(buf, '\n')

	// same directory as the target, so the rename stays on one filesystem
	tmp, err := os.CreateTemp(filepath.Dir(configFile), ".cosmos.config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary config file: %w", err)
	}
	tmpName := tmp.Name()

	// cleanup is best effort: a leftover temp file is litter, and a failed write
	// must never be visible under the real name
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(buf); err != nil {
		return fmt.Errorf("writing temporary config file: %w", err)
	}

	// 0600: config.json holds private keys, API tokens and backup passwords. The
	// default temp file mode is already 0600, set it explicitly so the guarantee
	// does not depend on the tempfile implementation.
	if err = tmp.Chmod(0600); err != nil {
		return fmt.Errorf("setting config file permissions: %w", err)
	}

	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("syncing temporary config file: %w", err)
	}

	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary config file: %w", err)
	}

	if err = os.Rename(tmpName, configFile); err != nil {
		return fmt.Errorf("replacing config file: %w", err)
	}

	// fsync the directory so the rename itself survives a power cut. Not every
	// filesystem supports it; a failure here only costs the durability of the
	// rename, the content is already on disk.
	if dir, dErr := os.Open(filepath.Dir(configFile)); dErr == nil {
		if syncErr := dir.Sync(); syncErr != nil {
			Warn("Syncing config directory: " + syncErr.Error())
		}
		if err = dir.Close(); err != nil {
			return fmt.Errorf("closing config directory: %w", err)
		}
	}

	Log("Config file saved.")
	return nil
}

// configWriteAbort is returned by a mutate function to cancel an update pass
// without writing anything. Handlers use it for validation failures and
// conflicts, so the check runs against the config that would be written.
type configWriteAbort struct {
	Message string
}

func (c configWriteAbort) Error() string {
	return "config update aborted: " + c.Message
}

// AbortConfigWrite builds the error a mutate function returns to cancel the update
// without touching the config file. See ConfigWriteAbortMessage.
func AbortConfigWrite(msg string) error {
	return configWriteAbort{Message: msg}
}

// ConfigWriteAbortMessage reports the message a mutate function aborted with.
// ok is false for every other error, including real write failures.
func ConfigWriteAbortMessage(err error) (msg string, ok bool) {
	var abort configWriteAbort
	if errors.As(err, &abort) {
		return abort.Message, true
	}
	return "", false
}

// UpdateConfig reads the config file, hands it to mutate and saves the result —
// all of it under ConfigLockInternal, the internal lock every config writer takes
// (ReadConfigFromFile and SaveConfigTofile included).
//
// Handlers used to run the cycle as three separate lock acquisitions:
// ReadConfigFromFile → mutate a snapshot → SetBaseMainConfig. A nested save
// landing in between is then silently overwritten by the outer snapshot, so any
// field created in the meantime (a route, a cron job, an API token) vanishes
// again, and the value the client just POSTed reverts after a reload. Worse,
// validation ran against the stale snapshot: a name that was free at check time
// was written as a duplicate.
//
// With the whole cycle serialised, mutate always sees the config that is about to
// be replaced, the duplicate/missing-target checks are authoritative, and a
// concurrent writer's fields survive. mutate must not save the config itself and
// must not keep a reference to it past the call.
//
// mutate is called with a config read inside the lock, so it must derive its
// changes from that config — re-finding the index of the item it edits — rather
// than reuse an index computed from an earlier read outside.
func UpdateConfig(mutate func(config *Config) error) error {
	ConfigLockInternal.Lock()
	defer ConfigLockInternal.Unlock()

	config, err := readConfigLocked()
	if err != nil {
		return err
	}
	if err = mutate(&config); err != nil {
		return err
	}
	if err = WriteConfigFile(config); err != nil {
		return err
	}

	// publish last: GetMainConfig() never observes a config that is not on disk
	LoadBaseMainConfig(config)
	return nil
}

// UpdateConfigFromFile is the config-agnostic form of UpdateConfig: read the file,
// mutate, write atomically, all under the internal config lock. The in-memory
// config is left alone, which is what a caller that only cares about the file
// wants. Unlike ReadConfigFromFile, a first start does not publish DefaultConfig
// into memory here — that stays the job of the normal startup path.
func UpdateConfigFromFile(mutate func(config *Config) error) error {
	ConfigLockInternal.Lock()
	defer ConfigLockInternal.Unlock()

	config, err := readConfigLocked()
	if err != nil {
		return err
	}
	if err = mutate(&config); err != nil {
		return err
	}
	return WriteConfigFile(config)
}

// readConfigLocked decodes the config file. ConfigLockInternal must be held.
// Unlike ReadConfigFromFile it reports a decode failure as an error a caller can
// return to the client, instead of calling Fatal on it. Creating the file on a
// first start keeps the existing Fatal path.
func readConfigLocked() (Config, error) {
	configFile := GetConfigFileName()
	CreateDefaultConfigFileIfNecessary()

	file, err := os.Open(configFile)
	if err != nil {
		return Config{}, fmt.Errorf("opening config file: %w", err)
	}
	defer file.Close()

	config := Config{}
	if err = json.NewDecoder(file).Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decoding config file: %w", err)
	}
	return config, nil
}
