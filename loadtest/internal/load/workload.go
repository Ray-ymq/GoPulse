package load

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
)

const CredentialsSchemaVersion = "gopulse.phase18.credentials.v1"

type CategoryMixEntry struct {
	Category Category `json:"category"`
	Percent  int      `json:"percent"`
}

type RouteContract struct {
	Category        Category `json:"category"`
	Method          string   `json:"method"`
	Template        string   `json:"template"`
	AllowedStatuses []int    `json:"allowed_statuses"`
}

type WorkloadProfile struct {
	VirtualUsers          int                `json:"virtual_users"`
	RequestTimeoutSeconds float64            `json:"request_timeout_seconds"`
	Mix                   []CategoryMixEntry `json:"mix"`
	Routes                []RouteContract    `json:"routes"`
}

var canonicalMix = []CategoryMixEntry{
	{Category: CategoryRead, Percent: 45},
	{Category: CategorySearch, Percent: 10},
	{Category: CategoryNotification, Percent: 10},
	{Category: CategoryContentWrite, Percent: 15},
	{Category: CategoryInteraction, Percent: 15},
	{Category: CategorySession, Percent: 5},
}

func DefaultWorkloadProfile() WorkloadProfile {
	return WorkloadProfile{
		VirtualUsers:          1024,
		RequestTimeoutSeconds: 5,
		Mix:                   append([]CategoryMixEntry(nil), canonicalMix...),
		Routes:                canonicalRoutes(),
	}
}

func canonicalRoutes() []RouteContract {
	return []RouteContract{
		{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts/:postId", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts/following", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/posts/:postId/comments", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryRead, Method: http.MethodGet, Template: "GET /api/v1/users/:username", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategorySearch, Method: http.MethodGet, Template: "GET /api/v1/search/posts", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategorySearch, Method: http.MethodGet, Template: "GET /api/v1/search/users", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/notifications", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/bookmarks", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/users/me/following", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryNotification, Method: http.MethodGet, Template: "GET /api/v1/users/me/followers", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryContentWrite, Method: http.MethodPost, Template: "POST /api/v1/posts", AllowedStatuses: []int{http.StatusCreated}},
		{Category: CategoryContentWrite, Method: http.MethodPost, Template: "POST /api/v1/posts/:postId/comments", AllowedStatuses: []int{http.StatusCreated}},
		{Category: CategoryContentWrite, Method: http.MethodPatch, Template: "PATCH /api/v1/posts/:postId", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategoryContentWrite, Method: http.MethodDelete, Template: "DELETE /api/v1/posts/:postId", AllowedStatuses: []int{http.StatusNoContent}},
		{Category: CategoryInteraction, Method: http.MethodPut, Template: "PUT /api/v1/posts/:postId/like", AllowedStatuses: []int{http.StatusNoContent}},
		{Category: CategoryInteraction, Method: http.MethodDelete, Template: "DELETE /api/v1/posts/:postId/like", AllowedStatuses: []int{http.StatusNoContent}},
		{Category: CategoryInteraction, Method: http.MethodPut, Template: "PUT /api/v1/users/:userId/follow", AllowedStatuses: []int{http.StatusNoContent}},
		{Category: CategoryInteraction, Method: http.MethodDelete, Template: "DELETE /api/v1/users/:userId/follow", AllowedStatuses: []int{http.StatusNoContent}},
		{Category: CategoryInteraction, Method: http.MethodPut, Template: "PUT /api/v1/posts/:postId/bookmark", AllowedStatuses: []int{http.StatusNoContent}},
		{Category: CategorySession, Method: http.MethodPost, Template: "POST /api/v1/auth/login", AllowedStatuses: []int{http.StatusOK}},
		{Category: CategorySession, Method: http.MethodGet, Template: "GET /api/v1/users/me", AllowedStatuses: []int{http.StatusOK}},
	}
}

func ValidateWorkloadProfile(profile WorkloadProfile) error {
	if profile.VirtualUsers != 1024 || !boundedPositive(profile.RequestTimeoutSeconds, 60) || len(profile.Mix) != len(canonicalMix) || len(profile.Routes) != len(canonicalRoutes()) {
		return errors.New("capacity workload identity is invalid")
	}
	total := 0
	seenCategories := make(map[Category]bool, len(profile.Mix))
	for index, entry := range profile.Mix {
		if entry != canonicalMix[index] || entry.Percent <= 0 || seenCategories[entry.Category] {
			return errors.New("capacity workload mix differs from the fixed route contract")
		}
		seenCategories[entry.Category] = true
		total += entry.Percent
	}
	if total != 100 {
		return errors.New("capacity workload mix must total 100 percent")
	}
	expectedRoutes := make(map[string]RouteContract, len(profile.Routes))
	for _, route := range canonicalRoutes() {
		expectedRoutes[route.Template] = route
	}
	for _, route := range profile.Routes {
		if _, exists := expectedRoutes[route.Template]; exists {
			if !sameRouteContract(route, expectedRoutes[route.Template]) {
				return fmt.Errorf("capacity workload route %q differs from the fixed contract", route.Template)
			}
			delete(expectedRoutes, route.Template)
		} else {
			return fmt.Errorf("capacity workload contains unknown route %q", route.Template)
		}
	}
	if len(expectedRoutes) != 0 {
		return errors.New("capacity workload is missing a fixed route")
	}
	return nil
}

func sameRouteContract(left, right RouteContract) bool {
	if left.Category != right.Category || left.Method != right.Method || len(left.AllowedStatuses) != len(right.AllowedStatuses) {
		return false
	}
	leftStatuses := append([]int(nil), left.AllowedStatuses...)
	rightStatuses := append([]int(nil), right.AllowedStatuses...)
	sort.Ints(leftStatuses)
	sort.Ints(rightStatuses)
	for index := range leftStatuses {
		if leftStatuses[index] != rightStatuses[index] {
			return false
		}
	}
	return true
}

func ValidateRequest(profile WorkloadProfile, request Request) error {
	for _, route := range profile.Routes {
		if route.Template != request.Template {
			continue
		}
		if route.Category != request.Category || route.Method != request.Method || len(route.AllowedStatuses) != len(request.ExpectedStatuses) {
			return fmt.Errorf("request %q violates route contract", request.Template)
		}
		for _, status := range route.AllowedStatuses {
			if !request.ExpectedStatuses[status] {
				return fmt.Errorf("request %q has an unexpected status contract", request.Template)
			}
		}
		return nil
	}
	return fmt.Errorf("request %q is not in the capacity profile", request.Template)
}

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
	profile      *WorkloadProfile
	deleteCursor int
}

func (state *vuState) request(slot uint64) Request {
	category := CategoryForSlot(slot, state.profile)
	switch category {
	case CategoryRead:
		return state.read(slot)
	case CategorySearch:
		return state.search(slot)
	case CategoryNotification:
		return state.notifications(slot)
	case CategoryContentWrite:
		return state.contentWrite(slot)
	case CategoryInteraction:
		return state.interactionWrite(slot)
	default:
		return state.session(slot)
	}
}

func CategoryForSlot(slot uint64, profile *WorkloadProfile) Category {
	mix := canonicalMix
	if profile != nil && len(profile.Mix) > 0 {
		mix = profile.Mix
	}
	selector := int(slot % 100)
	cumulative := 0
	for _, entry := range mix {
		cumulative += entry.Percent
		if selector < cumulative {
			return entry.Category
		}
	}
	return mix[len(mix)-1].Category
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
