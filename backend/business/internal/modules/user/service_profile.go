package user

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

func profile(record User, viewer uint64) Profile {
	return Profile{record.ID, record.Username, record.DisplayName, record.Bio, record.CreatedAt, false, record.ID == viewer}
}

func profileError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return apperror.New(apperror.CodeUserNotFound, "user not found")
	}
	return apperror.WrapInternal(err)
}

type ProfileRepository interface {
	FindByID(context.Context, uint64) (User, error)
	FindByUsername(context.Context, string) (User, error)
	UpdateProfile(context.Context, uint64, string, string) (User, error)
	SearchProfiles(context.Context, uint64, UserSearchOptions) ([]Profile, []int, error)
}

type ProfileService struct {
	repository ProfileRepository
	cursorKey  []byte
}

func NewProfileService(repository ProfileRepository, secret string) *ProfileService {
	key := sha256.Sum256([]byte("gopulse/user-search/v1\x00" + secret))
	return &ProfileService{repository: repository, cursorKey: key[:]}
}

func (s *ProfileService) Get(ctx context.Context, username string, viewer uint64) (Profile, error) {
	record, err := s.repository.FindByUsername(ctx, username)
	if err != nil {
		return Profile{}, profileError(err)
	}
	p := profile(record, viewer)
	if r, ok := s.repository.(interface {
		Following(context.Context, uint64, uint64) (bool, error)
	}); ok {
		p.Following, err = r.Following(ctx, viewer, record.ID)
		if err != nil {
			return Profile{}, profileError(err)
		}
	}
	return p, nil
}

func (s *ProfileService) Update(ctx context.Context, viewer uint64, input ProfileInput) (Profile, error) {
	name, bio, err := NormalizeProfile(input)
	if err != nil {
		return Profile{}, err
	}
	record, err := s.repository.UpdateProfile(ctx, viewer, name, bio)
	if err != nil {
		return Profile{}, profileError(err)
	}
	p := profile(record, viewer)
	if r, ok := s.repository.(interface {
		Following(context.Context, uint64, uint64) (bool, error)
	}); ok {
		p.Following, err = r.Following(ctx, viewer, record.ID)
		if err != nil {
			return Profile{}, profileError(err)
		}
	}
	return p, nil
}
