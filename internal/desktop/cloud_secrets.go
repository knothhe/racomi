package desktop

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
)

// Sync profile secrets share the per-data-directory credential service with the
// Cloudflare credentials. Manual S3 must keep working on devices without a
// usable credential store, so it alone falls back to the 0600 configuration
// file; Cloudflare R2 cannot work without the store in the first place.
const (
	cloudSecretsManualKey = "cloud-sync-manual"
	cloudSecretsR2Key     = "cloud-sync-r2"
	cloudSecretsSystem    = "system"
)

var errCloudSecretsUnreadable = errors.New("Saved sync credentials could not be read from the system credential store. Unlock it and sync again, or enter them again.")

type cloudSecrets struct {
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty"`
	Passphrase      string `json:"passphrase,omitempty"`
}

// cloudSecretSlot tracks where one saved profile keeps its secrets.
type cloudSecretSlot struct {
	key    string
	stored bool
	// saved fingerprints the stored value, so status saves after every sync do
	// not rewrite the credential store.
	saved [sha256.Size]byte
	// loadErr keeps unreadable stored secrets from being replaced by blanks.
	loadErr error
}

func profileSecrets(config CloudSyncConfig) cloudSecrets {
	if config.Provider == "r2-oauth" {
		// R2 access keys belong to the Cloudflare connection's own credential item.
		return cloudSecrets{Passphrase: config.Passphrase}
	}
	return cloudSecrets{config.SecretAccessKey, config.SessionToken, config.Passphrase}
}

func withSecrets(config CloudSyncConfig, secrets cloudSecrets) CloudSyncConfig {
	if config.Provider != "r2-oauth" {
		config.SecretAccessKey, config.SessionToken = secrets.SecretAccessKey, secrets.SessionToken
	}
	config.Passphrase = secrets.Passphrase
	return config
}

func (slot *cloudSecretSlot) load(store r2SecretStore, config CloudSyncConfig) CloudSyncConfig {
	var data []byte
	err := errR2SecretUnavailable
	if store != nil {
		data, err = store.Load(slot.key)
	}
	var secrets cloudSecrets
	if err == nil {
		err = json.Unmarshal(data, &secrets)
		slot.saved = sha256.Sum256(data)
	}
	clear(data)
	if err != nil {
		slot.loadErr = err
		return withSecrets(config, cloudSecrets{})
	}
	slot.loadErr = nil
	return withSecrets(config, secrets)
}

// prepare stores the profile's secrets and returns the profile as written to
// disk. commit runs once the configuration file is replaced: it records the new
// location and removes items the file no longer references.
func (slot *cloudSecretSlot) prepare(store r2SecretStore, config CloudSyncConfig) (disk CloudSyncConfig, stored bool, commit func(), err error) {
	secrets := profileSecrets(config)
	blank := withSecrets(config, cloudSecrets{})
	if secrets == (cloudSecrets{}) {
		if slot.loadErr != nil && config.Endpoint != "" {
			// The profile is still configured; its secrets are only unreadable.
			return blank, true, func() {}, nil
		}
		return blank, false, func() { slot.drop(store) }, nil
	}
	data, err := json.Marshal(secrets)
	if err != nil {
		return config, false, nil, err
	}
	defer clear(data)
	fingerprint := sha256.Sum256(data)
	if slot.stored && slot.loadErr == nil && slot.saved == fingerprint {
		return blank, true, func() {}, nil
	}
	err = errR2SecretUnavailable
	if store != nil {
		err = store.Save(slot.key, data)
	}
	if err == nil {
		return blank, true, func() { slot.stored, slot.saved, slot.loadErr = true, fingerprint, nil }, nil
	}
	if config.Provider != "r2-oauth" && secretStoreUnusable(err) {
		return config, false, func() { slot.drop(store) }, nil
	}
	return config, false, nil, err
}

// drop is best effort: the file no longer references the item, and a stale
// item is overwritten the next time the profile is stored.
func (slot *cloudSecretSlot) drop(store r2SecretStore) {
	if slot.stored && store != nil {
		_ = store.Delete(slot.key)
	}
	slot.stored, slot.saved, slot.loadErr = false, [sha256.Size]byte{}, nil
}
