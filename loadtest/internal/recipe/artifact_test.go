package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ray-ymq/GoPulse/loadtest/internal/load"
)

// TestArtifactsAreAcceptedByTheLoadGenerator pins the producer/consumer
// contract. The load generator rejects a credentials file whose schema version
// differs, so a silent drift here would fail every capacity round.
func TestArtifactsAreAcceptedByTheLoadGenerator(t *testing.T) {
	directory := t.TempDir()

	credentials := Credentials{
		SchemaVersion: CredentialsSchemaVersion,
		Password:      "phase18-load-password-value",
		Users:         corpusFor(Seed).Users,
	}
	credentialsPath := filepath.Join(directory, "credentials.json")
	writeArtifact(t, credentialsPath, credentials)
	loadedCredentials, err := load.LoadCredentials(credentialsPath)
	if err != nil {
		t.Fatalf("load generator rejected the credential artifact: %v", err)
	}
	if loadedCredentials.Password != credentials.Password || len(loadedCredentials.Users) != len(credentials.Users) {
		t.Fatalf("credential artifact did not round-trip")
	}

	corpusPath := filepath.Join(directory, "corpus.json")
	writeArtifact(t, corpusPath, corpusFor(Seed))
	loadedCorpus, err := load.LoadCorpus(corpusPath)
	if err != nil {
		t.Fatalf("load generator rejected the corpus artifact: %v", err)
	}
	if loadedCorpus.Seed != Seed || len(loadedCorpus.Users) != SessionUsers {
		t.Fatalf("corpus artifact did not round-trip")
	}
}

func writeArtifact(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode artifact: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
}
