package scenario

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

// Lifecycle exercises the immutable lifecycle tool through the Bundle's own
// Compose entry. The product containers are therefore created by the same
// host-socket, read-only tool contract used by an installed release.
func Lifecycle(root string, options Options) (returnErr error) {
	if options.Install == "" {
		options.Install = "clean"
	}
	if options.Install != "clean" && options.Install != "reuse" {
		return fmt.Errorf("install must be clean or reuse")
	}
	platform := options.Platform
	if platform == "" {
		platform = "linux/amd64"
	}
	if platform != "linux/amd64" {
		return fmt.Errorf("lifecycle acceptance supports linux/amd64 only")
	}
	if options.Manifest == "" {
		return fmt.Errorf("lifecycle requires --manifest")
	}
	manifest, err := filepath.Abs(options.Manifest)
	if err != nil {
		return err
	}
	bundle := filepath.Dir(manifest)
	if err := requireBundle(manifest, bundle); err != nil {
		return err
	}
	if err := validateManifestIdentity(manifest, root); err != nil {
		return err
	}

	session, err := harness.New(root, options.Keep)
	if err != nil {
		return err
	}
	runner, err := newLifecycleRunner(session, bundle, options.Install, options.InstallPath)
	if err != nil {
		_ = session.Cleanup()
		return err
	}
	defer func() {
		if !options.Keep {
			if cleanupErr := runner.purge(); returnErr == nil && cleanupErr != nil {
				returnErr = cleanupErr
			}
			if cleanupErr := runner.removeOwnedRoot(); returnErr == nil && cleanupErr != nil {
				returnErr = cleanupErr
			}
		}
		if cleanupErr := session.Cleanup(); returnErr == nil && cleanupErr != nil {
			returnErr = cleanupErr
		}
		if options.Receipt != "" {
			if receiptErr := session.WriteReceipt(resolvePath(root, options.Receipt)); returnErr == nil && receiptErr != nil {
				returnErr = receiptErr
			}
		}
	}()

	if options.Install == "reuse" {
		if options.FailureMatrix {
			returnErr = fmt.Errorf("failure matrix requires a clean installation")
		} else {
			returnErr = runner.reuse()
		}
	} else if options.FailureMatrix {
		returnErr = runner.failureMatrix()
	} else {
		returnErr = runner.clean()
	}
	if returnErr == nil {
		session.SetStatus("passed", nil)
	} else {
		session.SetStatus("failed", returnErr)
	}
	return returnErr
}

type lifecycleRunner struct {
	session               *harness.Session
	bundle                string
	install               string
	project               string
	port                  string
	state                 map[string]any
	temporaryRoot         string
	owned                 bool
	endpoint              string
	socketGID             string
	diagnosticDestination string
}

func newLifecycleRunner(session *harness.Session, bundle, mode, requestedPath string) (*lifecycleRunner, error) {
	if mode != "clean" && mode != "reuse" {
		return nil, fmt.Errorf("install must be clean or reuse")
	}
	installPath := strings.TrimSpace(requestedPath)
	if installPath == "" {
		installPath = strings.TrimSpace(os.Getenv("GOPULSE_LIFECYCLE_INSTALL"))
	}
	owned := false
	temporaryRoot := ""
	install := installPath
	if mode == "reuse" && install == "" {
		return nil, fmt.Errorf("reuse mode requires --install-path or GOPULSE_LIFECYCLE_INSTALL")
	}
	if install != "" && !filepath.IsAbs(install) {
		return nil, fmt.Errorf("lifecycle install path must be absolute")
	}
	if mode == "clean" && install == "" {
		var err error
		temporaryRoot, err = os.MkdirTemp("", "gopulse lifecycle ")
		if err != nil {
			return nil, err
		}
		install = filepath.Join(temporaryRoot, "installation with spaces")
		owned = true
	}
	if mode == "clean" {
		if err := os.Mkdir(install, 0o700); err != nil {
			if temporaryRoot != "" {
				_ = os.RemoveAll(temporaryRoot)
			}
			return nil, fmt.Errorf("create lifecycle install path: %w", err)
		}
	}
	if mode == "reuse" {
		if stat, err := os.Stat(install); err != nil || !stat.IsDir() {
			return nil, fmt.Errorf("reuse installation path must be an existing directory")
		}
	}
	port := 0
	if mode == "clean" {
		var err error
		port, err = freePort()
		if err != nil {
			if temporaryRoot != "" {
				_ = os.RemoveAll(temporaryRoot)
			}
			return nil, err
		}
	}
	socket := "/var/run/docker.sock"
	stat, err := os.Stat(socket)
	if err != nil {
		if temporaryRoot != "" {
			_ = os.RemoveAll(temporaryRoot)
		}
		return nil, fmt.Errorf("Docker socket is unavailable: %w", err)
	}
	socketStat, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		if temporaryRoot != "" {
			_ = os.RemoveAll(temporaryRoot)
		}
		return nil, fmt.Errorf("cannot inspect Docker socket ownership")
	}
	return &lifecycleRunner{
		session:       session,
		bundle:        bundle,
		install:       install,
		project:       "gopulse-lifecycle-" + session.Token,
		port:          fmt.Sprintf("%d", port),
		temporaryRoot: temporaryRoot,
		owned:         owned,
		endpoint:      "unix:///var/run/docker.sock",
		socketGID:     fmt.Sprintf("%d", socketStat.Gid),
	}, nil
}

func (r *lifecycleRunner) clean() error {
	if err := r.expect("doctor", 0, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	if err := r.expect("init", 0, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	if err := r.expect("init", 14, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	if err := r.expect("verify", 19); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}
	before, err := r.snapshot()
	if err != nil {
		return err
	}
	if err := r.expect("verify", 0); err != nil {
		return err
	}
	after, err := r.snapshot()
	if err != nil {
		return err
	}
	if string(before.state) != string(after.state) || string(before.secrets) != string(after.secrets) {
		return fmt.Errorf("read-only lifecycle verify mutated installation state")
	}
	status, err := r.expectJSON("status", 0)
	if err != nil {
		return err
	}
	if phase, _ := status["phase"].(string); phase != "ready" {
		return fmt.Errorf("lifecycle status phase is %q, expected ready", phase)
	}
	edge, _ := status["edge"].(string)
	for _, path := range []string{"/", "/admin/", "/health"} {
		if err := checkHTTP(edge + path); err != nil {
			return err
		}
	}
	if err := r.expect("logs", 0, "--service", "backend", "--tail", "10"); err != nil {
		return err
	}
	if err := r.expect("logs", 2, "--service", "arbitrary-container"); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}
	if err := r.expect("verify", 0); err != nil {
		return err
	}
	return r.expect("down", 0)
}

// restoreRoundTrip runs the lifecycle's real drained-backup and empty-target
// restore hand-off. The source is seeded through the public business API so
// the archive contains real users, posts, outbox events, search documents,
// completion rows, and a cache/cursor from the old epoch.
func (r *lifecycleRunner) restoreRoundTrip() (map[string]any, error) {
	if err := r.expect("doctor", 0, localImageValidationFlags(r.port)...); err != nil {
		return nil, err
	}
	if err := r.expect("init", 0, localImageValidationFlags(r.port)...); err != nil {
		return nil, err
	}
	if err := r.expect("up", 0); err != nil {
		return nil, err
	}
	status, err := r.expectJSON("status", 0)
	if err != nil {
		return nil, err
	}
	fixture, err := r.prepareV11Source(status)
	if err != nil {
		return nil, err
	}
	passphrase := make([]byte, 32)
	if _, err := rand.Read(passphrase); err != nil {
		return nil, err
	}
	passphrase = append(passphrase, []byte("-gopulse-v1-restore")...)
	passPath := filepath.Join(r.install, ".v1-restore-passphrase")
	archivePath := filepath.Join(r.install, "v1-restore.gpb")
	if err := os.WriteFile(passPath, passphrase, 0o600); err != nil {
		return nil, err
	}
	if err := r.expect("backup", 0, "--archive", archivePath, "--passphrase-file", passPath); err != nil {
		return nil, err
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		return nil, err
	}
	if err := r.expect("down", 0); err != nil {
		return nil, err
	}
	target, err := newLifecycleRunner(r.session, r.bundle, "clean", "")
	if err != nil {
		return nil, err
	}
	if r.diagnosticDestination != "" {
		target.diagnosticDestination = strings.TrimSuffix(r.diagnosticDestination, ".json") + "-target.json"
	}
	defer func() {
		_ = target.purge()
		_ = target.removeOwnedRoot()
	}()
	if err := target.expect("doctor", 0, localImageValidationFlags(target.port)...); err != nil {
		return nil, err
	}
	if err := target.expect("init", 0, localImageValidationFlags(target.port)...); err != nil {
		return nil, err
	}
	targetArchive := filepath.Join(target.install, "v1-restore.gpb")
	targetPassphrase := filepath.Join(target.install, ".v1-restore-passphrase")
	if err := os.WriteFile(targetArchive, archive, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(targetPassphrase, passphrase, 0o600); err != nil {
		return nil, err
	}
	if err := target.expect("restore", 0, "--archive", targetArchive, "--passphrase-file", targetPassphrase); err != nil {
		return nil, err
	}
	result, err := readPrivateJSON(filepath.Join(target.install, "restore-result.json"))
	if err != nil {
		return nil, err
	}
	verified, _ := result["facts_verified"].(bool)
	oldEpoch, _ := result["expected_old_epoch"].(string)
	newEpoch, _ := result["new_epoch"].(string)
	if !verified || oldEpoch == "" || newEpoch == "" || oldEpoch == newEpoch {
		return nil, fmt.Errorf("restore result did not prove a new epoch and verified facts")
	}
	status, err = target.expectJSON("status", 0)
	if err != nil {
		return nil, err
	}
	if phase, _ := status["phase"].(string); phase != "ready" {
		return nil, fmt.Errorf("restored lifecycle target phase is %q", phase)
	}
	generation, generationErr := target.searchGeneration()
	if generationErr != nil {
		return nil, generationErr
	}
	result["restored_generation"] = generation
	result["source_generation"] = fixture.OldGeneration
	if fixture.Primary.DataEpoch != oldEpoch {
		return nil, fmt.Errorf("source fixture epoch %q differs from restored old epoch %q", fixture.Primary.DataEpoch, oldEpoch)
	}
	if err := target.verifyV11Restore(result, fixture); err != nil {
		return nil, err
	}
	if err := target.expect("verify", 0); err != nil {
		return nil, err
	}
	if err := target.expect("down", 0); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *lifecycleRunner) prepareV11Source(status map[string]any) (v11RestoreFixture, error) {
	edge, _ := status["edge"].(string)
	if edge == "" {
		return v11RestoreFixture{}, errors.New("V11 lifecycle source status has no edge")
	}
	client := newV1HTTPClient(edge)
	username := "v11source_" + r.session.Token
	password := "V11-source-" + r.session.Token + "-password"
	if err := client.register(context.Background(), username, password); err != nil {
		return v11RestoreFixture{}, fmt.Errorf("register V11 lifecycle source user: %w", err)
	}
	epoch, err := client.commandEpoch(context.Background())
	if err != nil {
		return v11RestoreFixture{}, fmt.Errorf("read V11 lifecycle source epoch: %w", err)
	}
	query := "V11 restore source " + r.session.Token[:8]
	primaryTitle := query + " primary"
	primaryContent := query + " primary content"
	secondaryTitle := query + " secondary"
	secondaryContent := query + " secondary content"
	primary := client.create(context.Background(), epoch, mustV1UUID(), primaryTitle, primaryContent, v1TraceParent)
	if primary.Status != http.StatusCreated || primary.Meta.EventID == "" {
		if primary.Err != nil {
			return v11RestoreFixture{}, fmt.Errorf("create V11 lifecycle source primary post: %w", primary.Err)
		}
		return v11RestoreFixture{}, fmt.Errorf("create V11 lifecycle source primary post returned HTTP %d", primary.Status)
	}
	secondary := client.create(context.Background(), epoch, mustV1UUID(), secondaryTitle, secondaryContent, v1TraceParent)
	if secondary.Status != http.StatusCreated || secondary.Meta.EventID == "" {
		if secondary.Err != nil {
			return v11RestoreFixture{}, fmt.Errorf("create V11 lifecycle source secondary post: %w", secondary.Err)
		}
		return v11RestoreFixture{}, fmt.Errorf("create V11 lifecycle source secondary post returned HTTP %d", secondary.Status)
	}
	fixture := v11RestoreFixture{
		Primary:          primary.Meta,
		Secondary:        secondary.Meta,
		Query:            query,
		PrimaryTitle:     primaryTitle,
		PrimaryContent:   primaryContent,
		SecondaryTitle:   secondaryTitle,
		SecondaryContent: secondaryContent,
		Username:         username,
		Password:         password,
	}
	if err := r.waitV11SourceReady(client, &fixture); err != nil {
		return v11RestoreFixture{}, err
	}
	fixture.OldGeneration, err = r.searchGeneration()
	if err != nil {
		return v11RestoreFixture{}, err
	}
	primaryView, err := client.detail(context.Background(), fixture.Primary.PostID)
	if err != nil {
		return v11RestoreFixture{}, fmt.Errorf("read V11 lifecycle source cache seed: %w", err)
	}
	fixture.CachePayload, err = stalePostCachePayload(primaryView)
	if err != nil {
		return v11RestoreFixture{}, fmt.Errorf("encode V11 old cache fixture: %w", err)
	}
	fixture.OldCacheKey = "gopulse:post:detail:v1:epoch:" + fixture.Primary.DataEpoch + ":" + strconv.FormatUint(fixture.Primary.PostID, 10)
	fixture.LegacyCacheKey = "gopulse:post:detail:v1:" + strconv.FormatUint(fixture.Primary.PostID, 10)
	if err := r.redisSet(fixture.OldCacheKey, fixture.CachePayload); err != nil {
		return v11RestoreFixture{}, fmt.Errorf("seed V11 old epoch cache: %w", err)
	}
	if err := r.redisSet(fixture.LegacyCacheKey, fixture.CachePayload); err != nil {
		return v11RestoreFixture{}, fmt.Errorf("seed V11 legacy cache: %w", err)
	}
	oldKeyPresent, err := r.redisExists(fixture.OldCacheKey)
	if err != nil {
		return v11RestoreFixture{}, err
	}
	if !oldKeyPresent {
		return v11RestoreFixture{}, errors.New("V11 lifecycle source old cache key was not stored")
	}
	return fixture, nil
}

func (r *lifecycleRunner) waitV11SourceReady(client *v1HTTPClient, fixture *v11RestoreFixture) error {
	end := time.Now().Add(v1StageWait)
	var lastErr error
	for time.Now().Before(end) {
		primary, primaryErr := client.detail(context.Background(), fixture.Primary.PostID)
		secondary, secondaryErr := client.detail(context.Background(), fixture.Secondary.PostID)
		page, searchErr := client.search(context.Background(), fixture.Query, 1, "")
		allPosts, allSearchErr := client.search(context.Background(), fixture.Query, 50, "")
		completionQuery := fmt.Sprintf("SELECT COUNT(*) FROM business_application_results WHERE data_epoch=%s AND event_id=%s AND consumer='search'", sqlLiteral(fixture.Primary.DataEpoch), sqlLiteral(fixture.Primary.EventID))
		completionRaw, completionErr := r.mysqlQuery(completionQuery)
		completion := int64(0)
		if completionErr == nil {
			completion, completionErr = parseSingleInt(completionRaw, "V11 source completion")
		}
		if primaryErr == nil && secondaryErr == nil && searchErr == nil && allSearchErr == nil && completionErr == nil && primary.ID == fixture.Primary.PostID && primary.Title == fixture.PrimaryTitle && primary.Content == fixture.PrimaryContent && secondary.ID == fixture.Secondary.PostID && secondary.Title == fixture.SecondaryTitle && secondary.Content == fixture.SecondaryContent && hasSearchPosts(allPosts, fixture.Primary.PostID, fixture.Secondary.PostID) && page.NextCursor != "" && completion > 0 {
			fixture.OldCursor = page.NextCursor
			return nil
		}
		for _, current := range []error{primaryErr, secondaryErr, searchErr, allSearchErr, completionErr} {
			if current != nil {
				lastErr = current
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("wait V11 lifecycle source facts: %w", lastErr)
	}
	return errors.New("wait V11 lifecycle source facts timed out")
}

func (r *lifecycleRunner) verifyV11Restore(result map[string]any, fixture v11RestoreFixture) error {
	oldEpoch, _ := result["expected_old_epoch"].(string)
	newEpoch, _ := result["new_epoch"].(string)
	newGeneration, _ := result["restored_generation"].(string)
	if oldEpoch == "" || newEpoch == "" || newGeneration == "" || oldEpoch == newEpoch {
		return errors.New("V11 restore identities are incomplete")
	}
	status, err := r.expectJSON("status", 0)
	if err != nil {
		return err
	}
	edge, _ := status["edge"].(string)
	if edge == "" {
		return errors.New("V11 restored target status has no edge")
	}
	client := newV1HTTPClient(edge)
	if err := client.login(context.Background(), fixture.Username, fixture.Password); err != nil {
		return fmt.Errorf("login to V11 restored target: %w", err)
	}
	if err := r.redisSet(fixture.OldCacheKey, fixture.CachePayload); err != nil {
		return fmt.Errorf("inject V11 old epoch cache: %w", err)
	}
	if err := r.redisSet(fixture.LegacyCacheKey, fixture.CachePayload); err != nil {
		return fmt.Errorf("inject V11 legacy cache: %w", err)
	}
	primary, primaryErr := client.detail(context.Background(), fixture.Primary.PostID)
	secondary, secondaryErr := client.detail(context.Background(), fixture.Secondary.PostID)
	currentSearch, searchErr := client.search(context.Background(), fixture.Query, 50, "")
	postCountRaw, postCountErr := r.mysqlQuery("SELECT COUNT(*) FROM posts")
	postCount, postCountErr := parseSingleInt(postCountRaw, "V11 restored posts")
	if primaryErr != nil || secondaryErr != nil || searchErr != nil || postCountErr != nil {
		return errors.Join(primaryErr, secondaryErr, searchErr, postCountErr)
	}
	restoredDataVerified := primary.ID == fixture.Primary.PostID && primary.Title == fixture.PrimaryTitle && primary.Content == fixture.PrimaryContent && secondary.ID == fixture.Secondary.PostID && secondary.Title == fixture.SecondaryTitle && secondary.Content == fixture.SecondaryContent && hasSearchPosts(currentSearch, fixture.Primary.PostID, fixture.Secondary.PostID) && postCount == 2
	newCacheKey := "gopulse:post:detail:v1:epoch:" + newEpoch + ":" + strconv.FormatUint(fixture.Primary.PostID, 10)
	oldCachePresent, err := r.redisExists(fixture.OldCacheKey)
	if err != nil {
		return err
	}
	newCachePresent, err := r.redisExists(newCacheKey)
	if err != nil {
		return err
	}
	oldCacheIsolated := restoredDataVerified && oldCachePresent && newCachePresent
	indexCount, indexedDocuments, indexEpochVerified, err := r.searchIndexFacts(newGeneration, newEpoch, []uint64{fixture.Primary.PostID, fixture.Secondary.PostID})
	if err != nil {
		return err
	}
	newIndexVerified := indexEpochVerified && indexCount == postCount && indexedDocuments == 2
	oldIndexRemoved, err := r.searchIndexMissing(fixture.OldGeneration)
	if err != nil {
		return err
	}
	beforePosts := postCount
	oldHeader := client.create(context.Background(), fixture.Primary.DataEpoch, fixture.Primary.idempotencyKey, "V11 old header replay", "V11 old header replay content", v1TraceParent)
	afterPostsRaw, err := r.mysqlQuery("SELECT COUNT(*) FROM posts")
	if err != nil {
		return err
	}
	afterPosts, err := parseSingleInt(afterPostsRaw, "V11 post count after old header")
	if err != nil {
		return err
	}
	oldHeaderRejected := oldHeader.Status == http.StatusConflict && oldHeader.ErrorCode == "data_epoch_mismatch" && beforePosts == afterPosts
	oldCursorStatus, _, err := client.do(context.Background(), http.MethodGet, "/api/v1/search/posts?"+url.Values{"q": {fixture.Query}, "limit": {"1"}, "cursor": {fixture.OldCursor}}.Encode(), nil, nil)
	if err != nil {
		return fmt.Errorf("replay V11 old search cursor: %w", err)
	}
	oldCursorRejected := oldCursorStatus == http.StatusBadRequest
	completionRows, err := r.completionFenceCounts(oldEpoch, newEpoch, fixture.Primary.EventID, fixture.OldGeneration)
	if err != nil {
		return err
	}
	if completionRows[0] < 1 || completionRows[2] < 1 {
		return fmt.Errorf("V11 restored old completion row is missing: old_epoch=%d old_generation=%d", completionRows[0], completionRows[2])
	}
	if err := r.requeueOldOutbox(fixture.Primary.EventID); err != nil {
		return err
	}
	parkingRows, newCompletionRows, published, err := r.waitV11OldMessage(fixture, oldEpoch, newEpoch)
	if err != nil {
		return err
	}
	finalCompletionRows, err := r.completionFenceCounts(oldEpoch, newEpoch, fixture.Primary.EventID, fixture.OldGeneration)
	if err != nil {
		return err
	}
	finalPrimary, finalPrimaryErr := client.detail(context.Background(), fixture.Primary.PostID)
	finalSecondary, finalSecondaryErr := client.detail(context.Background(), fixture.Secondary.PostID)
	if finalPrimaryErr != nil || finalSecondaryErr != nil {
		return errors.Join(finalPrimaryErr, finalSecondaryErr)
	}
	oldMessageParked := parkingRows > 0 && published > 0 && finalPrimary.ID == primary.ID && finalPrimary.Title == primary.Title && finalPrimary.Content == primary.Content && finalPrimary.ContentRevision == primary.ContentRevision && finalSecondary.ID == secondary.ID && finalSecondary.Title == secondary.Title && finalSecondary.Content == secondary.Content && finalSecondary.ContentRevision == secondary.ContentRevision
	oldCompletionNotReused := finalCompletionRows[0] >= 1 && finalCompletionRows[2] >= 1 && finalCompletionRows[1] == 0 && newCompletionRows == 0
	result["restored_data_verified"] = restoredDataVerified
	result["restored_posts"] = postCount
	result["restored_search_documents"] = indexCount
	result["old_cache_isolated"] = oldCacheIsolated
	result["old_header_rejected"] = oldHeaderRejected
	result["old_cursor_rejected"] = oldCursorRejected
	result["old_message_parked"] = oldMessageParked
	result["old_epoch_rejected"] = oldHeaderRejected && oldMessageParked
	result["old_generation_not_completed"] = finalCompletionRows[1] == 0
	result["old_completion_rows_in_new_epoch"] = finalCompletionRows[1]
	result["old_completion_rows_in_old_epoch"] = finalCompletionRows[0]
	result["old_completion_rows_in_old_generation"] = finalCompletionRows[2]
	result["old_completion_not_reused"] = oldCompletionNotReused
	result["new_index_verified"] = newIndexVerified
	result["old_index_removed"] = oldIndexRemoved
	return nil
}

func (r *lifecycleRunner) serviceContainer(service string) (string, error) {
	result := r.session.Run("docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+r.projectName(), "--filter", "label=com.docker.compose.service="+service)
	containers := strings.Fields(result.Stdout)
	if result.ExitCode != 0 || len(containers) != 1 {
		return "", fmt.Errorf("find lifecycle %s container failed", service)
	}
	return containers[0], nil
}

func (r *lifecycleRunner) mysqlQuery(query string) (string, error) {
	container, err := r.serviceContainer("mysql")
	if err != nil {
		return "", err
	}
	command := "MYSQL_PWD=\"$MYSQL_PASSWORD\" mysql --user=\"$MYSQL_USER\" --batch --skip-column-names \"$MYSQL_DATABASE\" --execute " + shellLiteral(query)
	response := r.session.Run("docker", "exec", container, "sh", "-ec", command)
	if response.ExitCode != 0 {
		return "", fmt.Errorf("lifecycle MySQL query failed with exit code %d", response.ExitCode)
	}
	return strings.TrimSpace(response.Stdout), nil
}

func parseSingleInt(raw, name string) (int64, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) != 1 {
		return 0, fmt.Errorf("%s returned %d fields", name, len(fields))
	}
	value, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s value is invalid", name)
	}
	return value, nil
}

func parseIntFields(raw, name string, count int) ([]int64, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) != count {
		return nil, fmt.Errorf("%s returned %d fields, expected %d", name, len(fields), count)
	}
	values := make([]int64, count)
	for index, field := range fields {
		value, err := strconv.ParseInt(field, 10, 64)
		if err != nil || value < 0 {
			return nil, fmt.Errorf("%s value %q is invalid", name, field)
		}
		values[index] = value
	}
	return values, nil
}

func (r *lifecycleRunner) redisSet(key string, value []byte) error {
	container, err := r.serviceContainer("redis")
	if err != nil {
		return err
	}
	command := "redis-cli --no-auth-warning --raw SET " + shellLiteral(key) + " " + shellLiteral(string(value)) + " EX 900"
	response := r.session.Run("docker", "exec", container, "sh", "-ec", command)
	if response.ExitCode != 0 || strings.TrimSpace(response.Stdout) != "OK" {
		return fmt.Errorf("seed lifecycle Redis key failed with exit code %d", response.ExitCode)
	}
	return nil
}

func (r *lifecycleRunner) redisExists(key string) (bool, error) {
	container, err := r.serviceContainer("redis")
	if err != nil {
		return false, err
	}
	command := "redis-cli --no-auth-warning --raw EXISTS " + shellLiteral(key)
	response := r.session.Run("docker", "exec", container, "sh", "-ec", command)
	if response.ExitCode != 0 {
		return false, fmt.Errorf("read lifecycle Redis key failed with exit code %d", response.ExitCode)
	}
	value, err := parseSingleInt(response.Stdout, "lifecycle Redis key existence")
	return value == 1, err
}

func (r *lifecycleRunner) completionFenceCounts(oldEpoch, newEpoch, eventID, oldGeneration string) ([]int64, error) {
	if oldEpoch == "" || newEpoch == "" || eventID == "" || oldGeneration == "" {
		return nil, errors.New("V11 completion fence identities are incomplete")
	}
	query := fmt.Sprintf("SELECT (SELECT COUNT(*) FROM business_application_results WHERE data_epoch=%s AND event_id=%s AND consumer='search'),(SELECT COUNT(*) FROM business_application_results WHERE data_epoch=%s AND event_id=%s AND consumer='search'),(SELECT COUNT(*) FROM business_application_results WHERE data_epoch=%s AND event_id=%s AND consumer='search' AND projection_generation=%s)", sqlLiteral(oldEpoch), sqlLiteral(eventID), sqlLiteral(newEpoch), sqlLiteral(eventID), sqlLiteral(oldEpoch), sqlLiteral(eventID), sqlLiteral(oldGeneration))
	raw, err := r.mysqlQuery(query)
	if err != nil {
		return nil, err
	}
	return parseIntFields(raw, "V11 completion fence", 3)
}

func (r *lifecycleRunner) requeueOldOutbox(eventID string) error {
	if eventID == "" {
		return errors.New("V11 old message event identity is empty")
	}
	query := fmt.Sprintf("UPDATE business_outbox SET status='pending', available_at=UTC_TIMESTAMP(6), attempt_count=0, lease_owner=NULL, lease_expires_at=NULL, published_at=NULL, last_error=NULL, updated_at=UTC_TIMESTAMP(6) WHERE event_id=%s AND status='published'; SELECT ROW_COUNT()", sqlLiteral(eventID))
	raw, err := r.mysqlQuery(query)
	if err != nil {
		return fmt.Errorf("requeue V11 restored old outbox message: %w", err)
	}
	changed, err := parseSingleInt(raw, "V11 old outbox requeue")
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("V11 old outbox message was not requeued, changed rows=%d", changed)
	}
	return nil
}

func (r *lifecycleRunner) waitV11OldMessage(fixture v11RestoreFixture, oldEpoch, newEpoch string) (int64, int64, int64, error) {
	end := time.Now().Add(v1StageWait)
	var lastErr error
	for time.Now().Before(end) {
		query := fmt.Sprintf("SELECT (SELECT COUNT(*) FROM business_message_parking WHERE event_id=%s AND source_epoch=%s),(SELECT COUNT(*) FROM business_application_results WHERE data_epoch=%s AND event_id=%s AND consumer='search'),(SELECT COUNT(*) FROM business_outbox WHERE event_id=%s AND status='published')", sqlLiteral(fixture.Primary.EventID), sqlLiteral(oldEpoch), sqlLiteral(newEpoch), sqlLiteral(fixture.Primary.EventID), sqlLiteral(fixture.Primary.EventID))
		raw, err := r.mysqlQuery(query)
		if err == nil {
			values, parseErr := parseIntFields(raw, "V11 old message replay", 3)
			if parseErr == nil {
				if values[0] > 0 && values[1] == 0 && values[2] > 0 {
					return values[0], values[1], values[2], nil
				}
			} else {
				lastErr = parseErr
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return 0, 0, 0, fmt.Errorf("wait V11 old message replay: %w", lastErr)
	}
	return 0, 0, 0, errors.New("wait V11 old message replay timed out")
}

func (r *lifecycleRunner) searchIndexFacts(generation, epoch string, postIDs []uint64) (int64, int64, bool, error) {
	if generation == "" || epoch == "" {
		return 0, 0, false, errors.New("V11 search index facts require a generation and epoch")
	}
	container, err := r.serviceContainer("elasticsearch")
	if err != nil {
		return 0, 0, false, err
	}
	indexPath := url.PathEscape(generation)
	countResponse := r.session.Run("docker", "exec", container, "sh", "-ec", "curl --fail --silent http://127.0.0.1:9200/"+indexPath+"/_count")
	if countResponse.ExitCode != 0 {
		return 0, 0, false, fmt.Errorf("read V11 search index count failed with exit code %d", countResponse.ExitCode)
	}
	var countPayload struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal([]byte(countResponse.Stdout), &countPayload); err != nil || countPayload.Count < 0 {
		return 0, 0, false, errors.New("V11 search index count is invalid")
	}
	mappingResponse := r.session.Run("docker", "exec", container, "sh", "-ec", "curl --fail --silent http://127.0.0.1:9200/"+indexPath+"/_mapping")
	if mappingResponse.ExitCode != 0 {
		return 0, 0, false, fmt.Errorf("read V11 search index metadata failed with exit code %d", mappingResponse.ExitCode)
	}
	var mappings map[string]struct {
		Mappings struct {
			Meta map[string]string `json:"_meta"`
		} `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(mappingResponse.Stdout), &mappings); err != nil {
		return 0, 0, false, errors.New("V11 search index metadata is invalid")
	}
	indexEpochVerified := false
	for _, value := range mappings {
		if value.Mappings.Meta["gopulse_data_epoch"] == epoch {
			indexEpochVerified = true
		}
	}
	indexedDocuments := int64(0)
	for _, postID := range postIDs {
		documentResponse := r.session.Run("docker", "exec", container, "sh", "-ec", "curl --fail --silent 'http://127.0.0.1:9200/"+indexPath+"/_doc/"+strconv.FormatUint(postID, 10)+"?_source=data_epoch,post_id,content_revision'")
		if documentResponse.ExitCode != 0 {
			return 0, 0, false, fmt.Errorf("read V11 search document %d failed with exit code %d", postID, documentResponse.ExitCode)
		}
		var document struct {
			Found  bool `json:"found"`
			Source struct {
				DataEpoch string `json:"data_epoch"`
				PostID    uint64 `json:"post_id"`
			} `json:"_source"`
		}
		if err := json.Unmarshal([]byte(documentResponse.Stdout), &document); err != nil {
			return 0, 0, false, errors.New("V11 search document is invalid")
		}
		if document.Found && document.Source.DataEpoch == epoch && document.Source.PostID == postID {
			indexedDocuments++
		}
	}
	return countPayload.Count, indexedDocuments, indexEpochVerified, nil
}

func (r *lifecycleRunner) searchIndexMissing(generation string) (bool, error) {
	if generation == "" {
		return false, errors.New("V11 old search generation is empty")
	}
	container, err := r.serviceContainer("elasticsearch")
	if err != nil {
		return false, err
	}
	path := url.PathEscape(generation)
	command := "code=$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' http://127.0.0.1:9200/" + path + "); printf '%s' \"$code\"; test \"$code\" = 404"
	response := r.session.Run("docker", "exec", container, "sh", "-ec", command)
	if response.ExitCode == 0 {
		return true, nil
	}
	if strings.TrimSpace(response.Stdout) == "200" {
		return false, nil
	}
	return false, fmt.Errorf("check V11 old search generation returned HTTP %q", strings.TrimSpace(response.Stdout))
}

func (r *lifecycleRunner) searchGeneration() (string, error) {
	container, err := r.serviceContainer("elasticsearch")
	if err != nil {
		return "", err
	}
	query := "curl --fail --silent http://127.0.0.1:9200/_alias/gopulse-post-search-v1"
	response := r.session.Run("docker", "exec", container, "sh", "-ec", query)
	if response.ExitCode != 0 {
		return "", fmt.Errorf("read restored Elasticsearch generation failed with exit code %d", response.ExitCode)
	}
	var aliases map[string]json.RawMessage
	if err := json.Unmarshal([]byte(response.Stdout), &aliases); err != nil || len(aliases) != 1 {
		return "", fmt.Errorf("restored Elasticsearch alias has no single generation")
	}
	for generation := range aliases {
		return generation, nil
	}
	return "", fmt.Errorf("restored Elasticsearch generation is empty")
}

func (r *lifecycleRunner) reuse() error {
	beforeState, err := readPrivateJSON(filepath.Join(r.install, "state.json"))
	if err != nil {
		return fmt.Errorf("reuse installation state is invalid: %w", err)
	}
	restore, err := readPrivateJSON(filepath.Join(r.install, "restore-result.json"))
	if err != nil {
		return fmt.Errorf("reuse requires a successful restore-result.json: %w", err)
	}
	verified, _ := restore["facts_verified"].(bool)
	if !verified {
		return fmt.Errorf("reuse requires a restore result with facts_verified=true")
	}
	if _, err := os.Stat(filepath.Join(r.install, "restore-pending.json")); err == nil {
		return fmt.Errorf("reuse refuses an installation with restore-pending.json")
	} else if !os.IsNotExist(err) {
		return err
	}
	r.state = beforeState
	if err := r.expect("up", 0); err != nil {
		return err
	}
	before, err := r.snapshot()
	if err != nil {
		return err
	}
	if err := r.expect("verify", 0); err != nil {
		return err
	}
	if _, err := r.expectJSON("status", 0); err != nil {
		return err
	}
	after, err := r.snapshot()
	if err != nil {
		return err
	}
	if string(before.state) != string(after.state) || string(before.secrets) != string(after.secrets) {
		return fmt.Errorf("read-only lifecycle verify mutated restored state")
	}
	for _, key := range []string{"project", "installation_token", "manifest_digest", "version", "edge_port"} {
		if beforeState[key] != afterStateValue(after.state, key) {
			return fmt.Errorf("restored identity changed on lifecycle reuse: %s", key)
		}
	}
	status, err := r.expectJSON("status", 0)
	if err != nil {
		return err
	}
	edge, _ := status["edge"].(string)
	for _, path := range []string{"/login", "/admin/"} {
		if err := checkHTML(edge + path); err != nil {
			return err
		}
	}
	return nil
}

// failureMatrix reproduces the lifecycle failure contract at the same native
// boundary as the clean install scenario. Every injected fault is scoped to
// this runner's installation, project, or bundle copy.
func (r *lifecycleRunner) failureMatrix() error {
	if err := r.expect("doctor", 0, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	if err := r.expect("init", 0, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	if err := r.expect("init", 14, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	if err := r.expect("verify", 19); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}

	invalid, err := os.MkdirTemp("", "gopulse invalid bundle ")
	if err != nil {
		return err
	}
	defer os.RemoveAll(invalid)
	if err := copyTree(r.bundle, invalid); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(invalid, "README.md"), []byte("tampered\n"), 0o644); err != nil {
		return err
	}
	invalidRunner := *r
	invalidRunner.bundle = invalid
	if err := invalidRunner.expect("doctor", 10, localImageValidationFlags(invalidRunner.port)...); err != nil {
		return err
	}

	r.endpoint = "unix:///missing-daemon"
	if err := r.expect("doctor", 11, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	r.endpoint = "unix:///var/run/docker.sock"

	if err := r.expectDirect("doctor", 13, true, localImageValidationFlags(r.port)...); err != nil {
		return err
	}
	// The ready product already owns the configured edge port. Reuse that
	// real publication for the occupied-port contract instead of opening a
	// second listener in the acceptance process.
	if err := r.expect("doctor", 15, localImageValidationFlags(r.port)...); err != nil {
		return err
	}

	if err := os.Chmod(r.install, 0o755); err != nil {
		return err
	}
	if err := r.expect("status", 14); err != nil {
		_ = os.Chmod(r.install, 0o700)
		return err
	}
	if err := os.Chmod(r.install, 0o700); err != nil {
		return err
	}
	if err := r.expect("down", 2, "--purge", "--confirm", "foreign"); err != nil {
		return err
	}

	lock, err := os.OpenFile(filepath.Join(r.install, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return err
	}
	if err := r.expect("up", 16); err != nil {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		lock.Close()
		return err
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	lock.Close()

	if err := r.expect("down", 0); err != nil {
		return err
	}
	foreign := r.projectName() + "_mysql_data"
	removedOwned := r.session.Run("docker", "volume", "rm", foreign)
	if removedOwned.ExitCode != 0 {
		return fmt.Errorf("remove owned volume before foreign injection failed with exit code %d", removedOwned.ExitCode)
	}
	created := r.session.Run("docker", "volume", "create", foreign)
	if created.ExitCode != 0 {
		return fmt.Errorf("create foreign volume failed with exit code %d", created.ExitCode)
	}
	if err := r.expect("up", 17); err != nil {
		_ = r.session.Run("docker", "volume", "rm", foreign)
		return err
	}
	removed := r.session.Run("docker", "volume", "rm", foreign)
	if removed.ExitCode != 0 {
		return fmt.Errorf("remove foreign volume failed with exit code %d", removed.ExitCode)
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}

	if err := r.signal("SIGINT"); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}
	if err := r.signal("SIGTERM"); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}

	mysql := r.session.Run("docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+r.projectName(), "--filter", "label=com.docker.compose.service=mysql")
	if mysql.ExitCode != 0 || strings.TrimSpace(mysql.Stdout) == "" {
		return fmt.Errorf("find lifecycle MySQL container failed")
	}
	mysqlID := strings.Fields(mysql.Stdout)[0]
	paused := r.session.Run("docker", "pause", mysqlID)
	if paused.ExitCode != 0 {
		return fmt.Errorf("pause lifecycle dependency failed with exit code %d", paused.ExitCode)
	}
	upErr := r.expect("up", 18)
	unpaused := r.session.Run("docker", "unpause", mysqlID)
	if unpaused.ExitCode != 0 {
		return fmt.Errorf("unpause lifecycle dependency failed with exit code %d", unpaused.ExitCode)
	}
	if upErr != nil {
		return upErr
	}
	return r.expect("down", 0)
}

func (r *lifecycleRunner) projectName() string {
	if project, ok := r.state["project"].(string); ok && project != "" {
		return project
	}
	return "gopulse-unknown"
}

func localImageValidationFlags(port string) []string {
	return []string{"--local-images", "--port", port}
}

func (r *lifecycleRunner) expectDirect(command string, code int, lowDisk bool, flags ...string) error {
	result := r.direct(command, lowDisk, flags...)
	if result.ExitCode != code {
		return fmt.Errorf("lifecycle %s exited %d, expected %d", command, result.ExitCode, code)
	}
	return nil
}

func (r *lifecycleRunner) direct(command string, lowDisk bool, flags ...string) harness.Result {
	image, err := r.lifecycleImage()
	if err != nil {
		return harness.Result{ExitCode: 1, Stderr: err.Error()}
	}
	args := directArgs(os.Getuid(), os.Getgid(), r.socketGID, r.bundle, r.install, lowDisk)
	args = append(args, image, command, "--install", r.install, "--bundle", "/bundle", "--endpoint", r.endpoint)
	args = append(args, flags...)
	return r.session.Run("docker", args...)
}

func directArgs(uid, gid int, socketGID, bundle, install string, lowDisk bool) []string {
	user := fmt.Sprintf("%d:%d", uid, gid)
	args := []string{"run", "--rm", "--network", "host", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", user, "--group-add", socketGID, "--tmpfs", "/tmp"}
	args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", bundle+":/bundle:ro")
	if lowDisk {
		// Root loses DAC_OVERRIDE with the hardened container flags above. Use
		// the host tool identity for the read-only Bundle and make the scoped
		// low-disk tmpfs private to that same identity.
		tmpfs := fmt.Sprintf("%s:rw,size=1048576,mode=0700,uid=%d,gid=%d", install, uid, gid)
		args = append(args, "--tmpfs", tmpfs)
	} else {
		args = append(args, "-v", install+":"+install)
	}
	return args
}

func (r *lifecycleRunner) lifecycleImage() (string, error) {
	raw, err := os.ReadFile(filepath.Join(r.bundle, "compose.yaml"))
	if err != nil {
		return "", err
	}
	var document struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Services["lifecycle"].Image == "" {
		return "", fmt.Errorf("lifecycle bundle image is invalid")
	}
	return document.Services["lifecycle"].Image, nil
}

func (r *lifecycleRunner) signal(name string) error {
	restoreSessionPaths := r.bindSessionPaths()
	defer restoreSessionPaths()
	container := "gopulse-lifecycle-signal-" + name[3:] + "-" + r.session.Token[:8]
	args := []string{"compose", "--project-name", r.project, "--file", filepath.Join(r.bundle, "compose.yaml"), "run", "--name", container, "--rm", "-T", "--no-deps", "lifecycle", "up", "--install", r.install, "--bundle", "/bundle", "--endpoint", r.endpoint}
	command := exec.Command("docker", args...)
	command.Dir = r.session.Root
	command.Env = r.session.Environment()
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	deadline := time.Now().Add(120 * time.Second)
	reached := false
	finished := false
	for time.Now().Before(deadline) {
		state, err := readPrivateJSON(filepath.Join(r.install, "state.json"))
		if err == nil && state["phase"] == "pull" && state["operation_id"] != r.state["operation_id"] {
			reached = true
			break
		}
		select {
		case <-done:
			finished = true
		default:
		}
		if finished {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !reached {
		if !finished {
			_ = command.Process.Kill()
			<-done
		}
		return fmt.Errorf("lifecycle up did not reach pull before signal injection")
	}
	kill := exec.Command("docker", "kill", "--signal", name, container)
	if output, err := kill.CombinedOutput(); err != nil {
		_ = command.Process.Kill()
		if !finished {
			<-done
		}
		return fmt.Errorf("send %s to lifecycle runner: %v", name, strings.TrimSpace(string(output)))
	}
	err := <-done
	if exit := exitCode(err); exit != 20 {
		return fmt.Errorf("lifecycle %s exited %d, expected 20", name, exit)
	}
	state, err := readPrivateJSON(filepath.Join(r.install, "state.json"))
	if err != nil || state["phase"] != "interrupted" {
		return fmt.Errorf("lifecycle %s did not persist interrupted state", name)
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return 1
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
}

func afterStateValue(raw []byte, key string) any {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value[key]
}

type lifecycleSnapshot struct {
	state   []byte
	secrets []byte
}

func (r *lifecycleRunner) snapshot() (lifecycleSnapshot, error) {
	state, err := os.ReadFile(filepath.Join(r.install, "state.json"))
	if err != nil {
		return lifecycleSnapshot{}, err
	}
	secrets, err := os.ReadFile(filepath.Join(r.install, "secrets.json"))
	if err != nil {
		return lifecycleSnapshot{}, err
	}
	return lifecycleSnapshot{state: state, secrets: secrets}, nil
}

func (r *lifecycleRunner) expect(command string, code int, flags ...string) error {
	result := r.run(command, flags...)
	if result.ExitCode != code {
		failure := fmt.Errorf("lifecycle %s exited %d, expected %d", command, result.ExitCode, code)
		if diagnosticErr := r.copyFailureDiagnostic(); diagnosticErr != nil {
			return errors.Join(failure, diagnosticErr)
		}
		return failure
	}
	if command == "init" && code == 0 {
		state, err := readJSON(result.Stdout)
		if err != nil {
			return fmt.Errorf("lifecycle init returned invalid JSON: %w", err)
		}
		r.state = state
	}
	return nil
}

func (r *lifecycleRunner) expectJSON(command string, code int, flags ...string) (map[string]any, error) {
	result := r.run(command, flags...)
	if result.ExitCode != code {
		failure := fmt.Errorf("lifecycle %s exited %d, expected %d", command, result.ExitCode, code)
		if diagnosticErr := r.copyFailureDiagnostic(); diagnosticErr != nil {
			return nil, errors.Join(failure, diagnosticErr)
		}
		return nil, failure
	}
	value, err := readJSON(result.Stdout)
	if err != nil {
		return nil, fmt.Errorf("lifecycle %s returned invalid JSON: %w", command, err)
	}
	return value, nil
}

func (r *lifecycleRunner) copyFailureDiagnostic() error {
	if r.diagnosticDestination == "" {
		return nil
	}
	state, err := readPrivateJSON(filepath.Join(r.install, "state.json"))
	if err != nil {
		return fmt.Errorf("read failed lifecycle state for diagnostics: %w", err)
	}
	operation, _ := state["operation_id"].(string)
	if operation == "" {
		return fmt.Errorf("failed lifecycle state has no operation identity")
	}
	source := filepath.Join(r.install, "diagnostics", "failure-"+operation+".json")
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read lifecycle failure diagnostic: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.diagnosticDestination), 0700); err != nil {
		return fmt.Errorf("create lifecycle diagnostic destination: %w", err)
	}
	if err := os.WriteFile(r.diagnosticDestination, data, 0600); err != nil {
		return fmt.Errorf("save lifecycle failure diagnostic: %w", err)
	}
	if err := os.Chmod(r.diagnosticDestination, 0600); err != nil {
		return fmt.Errorf("protect lifecycle failure diagnostic: %w", err)
	}
	return nil
}

func (r *lifecycleRunner) run(command string, flags ...string) harness.Result {
	restoreSessionPaths := r.bindSessionPaths()
	defer restoreSessionPaths()
	args := []string{
		"compose", "--project-name", r.project, "--file", filepath.Join(r.bundle, "compose.yaml"),
		"run", "--rm", "-T", "--no-deps", "lifecycle", command,
		"--install", r.install, "--bundle", "/bundle", "--endpoint", r.endpoint,
	}
	args = append(args, flags...)
	return r.session.Run("docker", args...)
}

// bindSessionPaths scopes the Compose wrapper's path environment to one
// runner command. Multiple lifecycle runners share a session, so a temporary
// runner must not leave its bundle or install path behind for the next one.
func (r *lifecycleRunner) bindSessionPaths() func() {
	previous := map[string]string{
		"GOPULSE_BUNDLE_DIR":  r.session.Value("GOPULSE_BUNDLE_DIR"),
		"GOPULSE_INSTALL_DIR": r.session.Value("GOPULSE_INSTALL_DIR"),
		"GOPULSE_TOOL_UID":    r.session.Value("GOPULSE_TOOL_UID"),
		"GOPULSE_TOOL_GID":    r.session.Value("GOPULSE_TOOL_GID"),
		"GOPULSE_SOCKET_GID":  r.session.Value("GOPULSE_SOCKET_GID"),
	}
	values := map[string]string{
		"GOPULSE_BUNDLE_DIR":  r.bundle,
		"GOPULSE_INSTALL_DIR": r.install,
		"GOPULSE_TOOL_UID":    fmt.Sprintf("%d", os.Getuid()),
		"GOPULSE_TOOL_GID":    fmt.Sprintf("%d", os.Getgid()),
		"GOPULSE_SOCKET_GID":  r.socketGID,
	}
	for key, value := range values {
		r.session.SetValue(key, value)
	}
	return func() {
		for key, value := range previous {
			r.session.SetValue(key, value)
		}
	}
}

func (r *lifecycleRunner) purge() error {
	if !r.owned {
		return nil
	}
	if r.state == nil {
		return nil
	}
	project, _ := r.state["project"].(string)
	if project == "" {
		return nil
	}
	result := r.run("down", "--purge", "--confirm", project)
	if result.ExitCode != 0 {
		return fmt.Errorf("lifecycle cleanup exited %d", result.ExitCode)
	}
	return nil
}

func (r *lifecycleRunner) removeOwnedRoot() error {
	if !r.owned || r.temporaryRoot == "" {
		return nil
	}
	return os.RemoveAll(r.temporaryRoot)
}

func requireBundle(manifest, bundle string) error {
	for _, path := range []string{manifest, filepath.Join(bundle, "compose.yaml"), filepath.Join(bundle, "README.md"), filepath.Join(bundle, "checksums")} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("lifecycle bundle asset is missing: %s", path)
		}
	}
	return nil
}

func validateManifestIdentity(path, root string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var manifest struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version == "" || manifest.Revision == "" {
		return fmt.Errorf("invalid lifecycle manifest")
	}
	version, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil || strings.TrimSpace(string(version)) != manifest.Version {
		return fmt.Errorf("lifecycle manifest version does not match VERSION")
	}
	revision, err := commandOutput(root, "git", "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(revision) != manifest.Revision {
		return fmt.Errorf("lifecycle manifest revision does not match HEAD")
	}
	return nil
}

func commandOutput(root, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func readPrivateJSON(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func readJSON(output string) (map[string]any, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		var value map[string]any
		if json.Unmarshal([]byte(lines[index]), &value) == nil {
			return value, nil
		}
	}
	return nil, fmt.Errorf("no JSON object in command output")
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func checkHTTP(url string) error {
	response, err := (&http.Client{}).Get(url)
	if err != nil {
		return fmt.Errorf("lifecycle endpoint %s failed: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("lifecycle endpoint %s returned HTTP %d", url, response.StatusCode)
	}
	return nil
}

func checkHTML(url string) error {
	response, err := (&http.Client{}).Get(url)
	if err != nil {
		return fmt.Errorf("lifecycle endpoint %s failed: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("lifecycle endpoint %s returned HTTP %d", url, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || !strings.Contains(strings.ToLower(string(body)), "<html") {
		return fmt.Errorf("lifecycle endpoint %s did not return an HTML document", url)
	}
	return nil
}
