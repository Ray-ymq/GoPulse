package load

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

const CredentialsSchemaVersion = "gopulse.phase18.credentials.v1"

type User struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

type Credentials struct {
	SchemaVersion string `json:"schema_version"`
	Password      string `json:"password"`
	Users         []User `json:"users"`
}

type Corpus struct {
	SchemaVersion      string     `json:"schema_version"`
	Seed               uint64     `json:"seed"`
	Users              []User     `json:"users"`
	ReadPostIDs        []uint64   `json:"read_post_ids"`
	InteractionPostIDs []uint64   `json:"interaction_post_ids"`
	EditablePostIDs    [][]uint64 `json:"editable_post_ids"`
	DeletePostIDs      [][]uint64 `json:"delete_post_ids"`
}

func LoadCorpus(path string) (Corpus, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, fmt.Errorf("read load corpus: %w", err)
	}
	var corpus Corpus
	if err := json.Unmarshal(encoded, &corpus); err != nil {
		return Corpus{}, fmt.Errorf("decode load corpus: %w", err)
	}
	if len(corpus.Users) == 0 || len(corpus.ReadPostIDs) == 0 || len(corpus.InteractionPostIDs) == 0 ||
		len(corpus.EditablePostIDs) != len(corpus.Users) || len(corpus.DeletePostIDs) != len(corpus.Users) {
		return Corpus{}, fmt.Errorf("load corpus is incomplete")
	}
	return corpus, nil
}

func LoadCredentials(path string) (Credentials, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var credentials Credentials
	if err := json.Unmarshal(encoded, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("decode credentials: %w", err)
	}
	if credentials.SchemaVersion != CredentialsSchemaVersion || len(credentials.Users) == 0 || credentials.Password == "" {
		return Credentials{}, fmt.Errorf("credentials are incomplete")
	}
	return credentials, nil
}

type Request struct {
	Category         Category
	Method           string
	Template         string
	Path             string
	Body             []byte
	ExpectedStatuses map[int]bool
}

type vuState struct {
	id           int
	corpus       *Corpus
	credentials  *Credentials
	deleteCursor int
}

func (state *vuState) request(slot uint64) Request {
	selector := int(slot % 100)
	switch {
	case selector < 45:
		return state.read(slot)
	case selector < 55:
		return state.search(slot)
	case selector < 65:
		return state.notifications(slot)
	case selector < 80:
		return state.contentWrite(slot)
	case selector < 95:
		return state.interactionWrite(slot)
	default:
		return state.session(slot)
	}
}

func statuses(values ...int) map[int]bool {
	result := make(map[int]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func (state *vuState) read(slot uint64) Request {
	switch (slot / 100) % 5 {
	case 0:
		return Request{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts", Path: "/api/v1/posts?limit=20", ExpectedStatuses: statuses(http.StatusOK)}
	case 1:
		postID := state.corpus.ReadPostIDs[deterministicIndex(slot, len(state.corpus.ReadPostIDs))]
		return Request{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts/:postId", Path: fmt.Sprintf("/api/v1/posts/%d", postID), ExpectedStatuses: statuses(http.StatusOK)}
	case 2:
		return Request{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts/following", Path: "/api/v1/posts/following?limit=20", ExpectedStatuses: statuses(http.StatusOK)}
	case 3:
		postID := state.corpus.ReadPostIDs[deterministicIndex(slot/5, len(state.corpus.ReadPostIDs))]
		return Request{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts/:postId/comments", Path: fmt.Sprintf("/api/v1/posts/%d/comments?limit=20", postID), ExpectedStatuses: statuses(http.StatusOK)}
	default:
		user := state.corpus.Users[deterministicIndex(slot/7, len(state.corpus.Users))]
		return Request{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/users/:username", Path: "/api/v1/users/" + url.PathEscape(user.Username), ExpectedStatuses: statuses(http.StatusOK)}
	}
}

func (state *vuState) search(slot uint64) Request {
	queries := []string{"phase18", fmt.Sprintf("post %05d", slot%50000+1), "go"}
	query := url.QueryEscape(queries[int(slot/100)%len(queries)])
	if (slot/100)%2 == 0 {
		return Request{Category: CategorySearch, Method: http.MethodGet, Template: "GET /api/v1/search/posts", Path: "/api/v1/search/posts?q=" + query + "&limit=20", ExpectedStatuses: statuses(http.StatusOK)}
	}
	return Request{Category: CategorySearch, Method: http.MethodGet, Template: "GET /api/v1/search/users", Path: "/api/v1/search/users?q=" + query + "&limit=20", ExpectedStatuses: statuses(http.StatusOK)}
}

func (state *vuState) notifications(slot uint64) Request {
	switch (slot / 100) % 4 {
	case 0:
		return Request{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/notifications", Path: "/api/v1/notifications?limit=20", ExpectedStatuses: statuses(http.StatusOK)}
	case 1:
		return Request{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/bookmarks", Path: "/api/v1/bookmarks?limit=20", ExpectedStatuses: statuses(http.StatusOK)}
	case 2:
		return Request{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/users/me/following", Path: "/api/v1/users/me/following?limit=20", ExpectedStatuses: statuses(http.StatusOK)}
	default:
		return Request{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/users/me/followers", Path: "/api/v1/users/me/followers?limit=20", ExpectedStatuses: statuses(http.StatusOK)}
	}
}

func (state *vuState) contentWrite(slot uint64) Request {
	switch (slot / 100) % 4 {
	case 0:
		body := fmt.Sprintf(`{"title":"Phase18 load %d","content":"open-loop content write %d"}`, slot, slot)
		return Request{Category: CategoryContentWrite, Method: http.MethodPost, Template: "POST /api/v1/posts", Path: "/api/v1/posts", Body: []byte(body), ExpectedStatuses: statuses(http.StatusCreated)}
	case 1:
		postID := state.corpus.ReadPostIDs[deterministicIndex(slot/3, len(state.corpus.ReadPostIDs))]
		body := fmt.Sprintf(`{"content":"load comment %d"}`, slot)
		return Request{Category: CategoryContentWrite, Method: http.MethodPost, Template: "POST /api/v1/posts/:postId/comments", Path: fmt.Sprintf("/api/v1/posts/%d/comments", postID), Body: []byte(body), ExpectedStatuses: statuses(http.StatusCreated)}
	case 2:
		ids := state.corpus.EditablePostIDs[state.id%len(state.corpus.EditablePostIDs)]
		postID := ids[deterministicIndex(slot, len(ids))]
		body := fmt.Sprintf(`{"title":"Phase18 edit %d","content":"edited by open-loop writer %d"}`, slot, slot)
		return Request{Category: CategoryContentWrite, Method: http.MethodPatch, Template: "PATCH /api/v1/posts/:postId", Path: fmt.Sprintf("/api/v1/posts/%d", postID), Body: []byte(body), ExpectedStatuses: statuses(http.StatusOK)}
	default:
		ids := state.corpus.DeletePostIDs[state.id%len(state.corpus.DeletePostIDs)]
		if state.deleteCursor < len(ids) {
			postID := ids[state.deleteCursor]
			state.deleteCursor++
			return Request{Category: CategoryContentWrite, Method: http.MethodDelete, Template: "DELETE /api/v1/posts/:postId", Path: fmt.Sprintf("/api/v1/posts/%d", postID), ExpectedStatuses: statuses(http.StatusNoContent)}
		}
		// The corpus reserves eight delete-owned posts per VU, above the maximum
		// planned deletion count. This fallback keeps a malformed/long run
		// measurable instead of silently converting the slot to another route.
		postID := state.corpus.ReadPostIDs[deterministicIndex(slot, len(state.corpus.ReadPostIDs))]
		body := fmt.Sprintf(`{"title":"Phase18 fallback %d","content":"edit fallback %d"}`, slot, slot)
		return Request{Category: CategoryContentWrite, Method: http.MethodPatch, Template: "PATCH /api/v1/posts/:postId", Path: fmt.Sprintf("/api/v1/posts/%d", postID), Body: []byte(body), ExpectedStatuses: statuses(http.StatusOK)}
	}
}

func (state *vuState) interactionWrite(slot uint64) Request {
	postID := state.corpus.InteractionPostIDs[deterministicIndex(slot, len(state.corpus.InteractionPostIDs))]
	switch (slot / 100) % 5 {
	case 0:
		return Request{Category: CategoryInteraction, Method: http.MethodPut, Template: "PUT /api/v1/posts/:postId/like", Path: fmt.Sprintf("/api/v1/posts/%d/like", postID), ExpectedStatuses: statuses(http.StatusNoContent)}
	case 1:
		return Request{Category: CategoryInteraction, Method: http.MethodDelete, Template: "DELETE /api/v1/posts/:postId/like", Path: fmt.Sprintf("/api/v1/posts/%d/like", postID), ExpectedStatuses: statuses(http.StatusNoContent)}
	case 2:
		return Request{Category: CategoryInteraction, Method: http.MethodPut, Template: "PUT /api/v1/users/:userId/follow", Path: fmt.Sprintf("/api/v1/users/%d/follow", state.targetUser(slot)), ExpectedStatuses: statuses(http.StatusNoContent)}
	case 3:
		return Request{Category: CategoryInteraction, Method: http.MethodDelete, Template: "DELETE /api/v1/users/:userId/follow", Path: fmt.Sprintf("/api/v1/users/%d/follow", state.targetUser(slot)), ExpectedStatuses: statuses(http.StatusNoContent)}
	default:
		return Request{Category: CategoryInteraction, Method: http.MethodPut, Template: "PUT /api/v1/posts/:postId/bookmark", Path: fmt.Sprintf("/api/v1/posts/%d/bookmark", postID), ExpectedStatuses: statuses(http.StatusNoContent)}
	}
}

func (state *vuState) session(slot uint64) Request {
	if (slot/100)%5 == 0 {
		user := state.credentials.Users[state.id%len(state.credentials.Users)]
		body, _ := json.Marshal(map[string]string{"username": user.Username, "password": state.credentials.Password})
		return Request{Category: CategorySession, Method: http.MethodPost, Template: "POST /api/v1/auth/login", Path: "/api/v1/auth/login", Body: body, ExpectedStatuses: statuses(http.StatusOK)}
	}
	return Request{Category: CategorySession, Method: http.MethodGet, Template: "GET /api/v1/users/me", Path: "/api/v1/users/me", ExpectedStatuses: statuses(http.StatusOK)}
}

func (state *vuState) targetUser(slot uint64) uint64 {
	target := (state.id + int(slot%uint64(len(state.corpus.Users)-1)) + 1) % len(state.corpus.Users)
	if target == state.id%len(state.corpus.Users) {
		target = (target + 1) % len(state.corpus.Users)
	}
	return state.corpus.Users[target].ID
}

func deterministicIndex(slot uint64, size int) int {
	if size <= 1 {
		return 0
	}
	return int((slot ^ (slot >> 17) ^ (slot >> 31)) % uint64(size))
}

func LoginRequest(user User, password string) Request {
	body, _ := json.Marshal(map[string]string{"username": user.Username, "password": password})
	return Request{Category: CategorySession, Method: http.MethodPost, Template: "POST /api/v1/auth/login", Path: "/api/v1/auth/login", Body: body, ExpectedStatuses: statuses(http.StatusOK)}
}

func routeKey(request Request) string {
	return request.Template
}

func formatStatus(code int) string { return strconv.Itoa(code) }
