package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestCapacityProfileReferencesTheDeterministicRecipeIdentity(t *testing.T) {
	profile, _, err := load.LoadProfile("../../capacity-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	receipt := Inspect(Seed, Candidate{}, time.Unix(0, 0))
	if profile.Recipe.SchemaVersion != receipt.SchemaVersion || profile.Recipe.Seed != receipt.Seed || profile.Recipe.Digest != receipt.Digest {
		t.Fatalf("profile recipe=%+v receipt=%+v", profile.Recipe, receipt)
	}
	if profile.Recipe.Counts.Users != receipt.Counts.Users || profile.Recipe.Counts.Posts != receipt.Counts.Posts || profile.Recipe.Counts.Notifications != receipt.Counts.Notifications {
		t.Fatalf("profile counts=%+v receipt counts=%+v", profile.Recipe.Counts, receipt.Counts)
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
