package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
)

type publicProfile struct {
	ID          uint64    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Bio         *string   `json:"bio"`
	CreatedAt   time.Time `json:"created_at"`
}

type profileReadContext struct {
	epoch     string
	username  string
	id        uint64
	following bool
}

type publicProfileReader interface {
	readProfileContext(context.Context, string, uint64) (profileReadContext, error)
	findPublicProfile(context.Context, profileReadContext) (publicProfile, error)
}

func (service *ProfileService) WithPublicProfileCache(scope *readcache.Scope) {
	service.publicProfiles = readcache.New(scope, "user:public-profile:v1", 30*time.Second, validPublicProfile)
}

func normalizedProfileKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func validPublicProfile(name string, record publicProfile) bool {
	return record.ID != 0 && record.Username != "" && normalizedProfileKey(record.Username) == name &&
		record.DisplayName != "" && record.Bio != nil && !record.CreatedAt.IsZero()
}

func (service *ProfileService) cachedProfile(ctx context.Context, username string, viewer uint64) (Profile, bool) {
	reader, ok := service.repository.(publicProfileReader)
	if !ok {
		return Profile{}, false
	}
	name, err := NormalizeUsername(username)
	if err != nil || name != username {
		return Profile{}, false
	}
	read, err := reader.readProfileContext(ctx, name, viewer)
	if err != nil || read.epoch == "" {
		return Profile{}, false
	}
	key := normalizedProfileKey(read.username)
	record, found, err := service.publicProfiles.FetchOne(ctx, read.epoch, key, func(ctx context.Context) (publicProfile, bool, error) {
		record, err := reader.findPublicProfile(ctx, read)
		if errors.Is(err, ErrNotFound) {
			return publicProfile{}, false, nil
		}
		return record, err == nil, err
	})
	if err != nil || !found || record.ID != read.id || !validPublicProfile(key, record) {
		return Profile{}, false
	}
	return Profile{ID: record.ID, Username: record.Username, DisplayName: record.DisplayName, Bio: *record.Bio,
		CreatedAt: record.CreatedAt, Following: read.following, IsSelf: record.ID == viewer}, true
}
