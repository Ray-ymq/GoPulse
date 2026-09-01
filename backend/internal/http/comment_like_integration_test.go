//go:build integration

package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

func TestIntegrationCommentAndLikeHTTPBusinessClosure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := integrationtest.Environment(t)
	suffix := time.Now().UTC().Format("150405000000")
	firstUsername := "ClosureA_" + suffix
	secondUsername := "ClosureB_" + suffix
	cleanupHTTPPostUsers(t, cfg, firstUsername, secondUsername)

	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error=%v", err)
	}
	defer database.Close()
	releasePostFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releasePostFactsLock()
	router := integrationPostRouter(t, cfg, database)

	firstUser, firstCookie := registerHTTPIntegrationUser(t, router, firstUsername)
	secondUser, secondCookie := registerHTTPIntegrationUser(t, router, secondUsername)

	createdPost := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/posts", `{"title":"Business closure","content":"synchronous facts"}`, firstCookie)
	if createdPost.Code != stdhttp.StatusCreated {
		t.Fatalf("create post status=%d body=%s", createdPost.Code, createdPost.Body.String())
	}
	var postCreation struct {
		Data post.Post `json:"data"`
	}
	decodeIntegrationResponse(t, createdPost, &postCreation)
	postPath := "/api/v1/posts/" + strconv.FormatUint(postCreation.Data.ID, 10)
	// Populate the zero-count detail cache before interaction writes so the
	// following assertions require real Redis invalidation and rebuild.
	assertHTTPPostState(t, router, postPath, firstCookie, 0, 0, false)

	for _, body := range []string{
		`{"content":"   "}`,
		`{"content":"` + strings.Repeat("a", 2001) + `"}`,
		`{"content":"valid","author_id":` + strconv.FormatUint(secondUser.ID, 10) + `}`,
	} {
		invalid := performJSONRequest(router, stdhttp.MethodPost, postPath+"/comments", body, firstCookie)
		if invalid.Code != stdhttp.StatusBadRequest {
			t.Fatalf("invalid comment status=%d body=%s", invalid.Code, invalid.Body.String())
		}
	}

	firstComment := createHTTPIntegrationComment(t, router, postPath, "  first by author  ", firstCookie)
	secondComment := createHTTPIntegrationComment(t, router, postPath, "second by viewer", secondCookie)
	thirdComment := createHTTPIntegrationComment(t, router, postPath, "third by author", firstCookie)
	if firstComment.Author.ID != firstUser.ID || firstComment.Author.Username != firstUsername || firstComment.Content != "first by author" {
		t.Fatalf("first comment=%#v first user=%#v", firstComment, firstUser)
	}
	if secondComment.Author.ID != secondUser.ID || secondComment.Author.Username != secondUsername {
		t.Fatalf("second comment=%#v second user=%#v", secondComment, secondUser)
	}

	pageOneResponse := performJSONRequest(router, stdhttp.MethodGet, postPath+"/comments?limit=2", "", secondCookie)
	if pageOneResponse.Code != stdhttp.StatusOK {
		t.Fatalf("first comment page status=%d body=%s", pageOneResponse.Code, pageOneResponse.Body.String())
	}
	var pageOne struct {
		Data []comment.Comment `json:"data"`
		Meta struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"meta"`
	}
	decodeIntegrationResponse(t, pageOneResponse, &pageOne)
	if len(pageOne.Data) != 2 || pageOne.Data[0].ID != thirdComment.ID || pageOne.Data[1].ID != secondComment.ID || pageOne.Meta.NextCursor == nil {
		t.Fatalf("first comment page=%#v", pageOne)
	}

	pageTwoResponse := performJSONRequest(router, stdhttp.MethodGet, postPath+"/comments?limit=2&cursor="+*pageOne.Meta.NextCursor, "", firstCookie)
	if pageTwoResponse.Code != stdhttp.StatusOK {
		t.Fatalf("second comment page status=%d body=%s", pageTwoResponse.Code, pageTwoResponse.Body.String())
	}
	var pageTwo struct {
		Data []comment.Comment `json:"data"`
		Meta struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"meta"`
	}
	decodeIntegrationResponse(t, pageTwoResponse, &pageTwo)
	if len(pageTwo.Data) != 1 || pageTwo.Data[0].ID != firstComment.ID || pageTwo.Meta.NextCursor != nil {
		t.Fatalf("second comment page=%#v", pageTwo)
	}
	assertHTTPPostState(t, router, postPath, firstCookie, 3, 0, false)

	for range 2 {
		response := performJSONRequest(router, stdhttp.MethodPut, postPath+"/like", "", firstCookie)
		if response.Code != stdhttp.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("first user like status=%d body=%q", response.Code, response.Body.String())
		}
	}
	assertHTTPPostState(t, router, postPath, firstCookie, 3, 1, true)

	response := performJSONRequest(router, stdhttp.MethodPut, postPath+"/like", "", secondCookie)
	if response.Code != stdhttp.StatusNoContent {
		t.Fatalf("second user like status=%d body=%s", response.Code, response.Body.String())
	}
	assertHTTPPostState(t, router, postPath, secondCookie, 3, 2, true)

	for range 2 {
		response = performJSONRequest(router, stdhttp.MethodDelete, postPath+"/like", "", firstCookie)
		if response.Code != stdhttp.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("first user unlike status=%d body=%q", response.Code, response.Body.String())
		}
	}
	assertHTTPPostState(t, router, postPath, firstCookie, 3, 1, false)
	assertHTTPPostState(t, router, postPath, secondCookie, 3, 1, true)

	missingPath := "/api/v1/posts/18446744073709551615"
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: stdhttp.MethodPost, path: missingPath + "/comments", body: `{"content":"missing"}`},
		{method: stdhttp.MethodGet, path: missingPath + "/comments"},
		{method: stdhttp.MethodPut, path: missingPath + "/like"},
		{method: stdhttp.MethodDelete, path: missingPath + "/like"},
	} {
		missing := performJSONRequest(router, request.method, request.path, request.body, firstCookie)
		if missing.Code != stdhttp.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, missing.Code, missing.Body.String())
		}
	}
}

func registerHTTPIntegrationUser(t *testing.T, router stdhttp.Handler, username string) (user.Public, *stdhttp.Cookie) {
	t.Helper()
	response := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/register", `{"username":"`+username+`","password":"integration-password"}`, nil)
	if response.Code != stdhttp.StatusCreated {
		t.Fatalf("register %q status=%d body=%s", username, response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("register %q cookies=%#v", username, cookies)
	}
	var envelope struct {
		Data user.Public `json:"data"`
	}
	decodeIntegrationResponse(t, response, &envelope)
	return envelope.Data, cookies[0]
}

func createHTTPIntegrationComment(t *testing.T, router stdhttp.Handler, postPath, content string, cookie *stdhttp.Cookie) comment.Comment {
	t.Helper()
	body, err := json.Marshal(comment.CreateInput{Content: content})
	if err != nil {
		t.Fatal(err)
	}
	response := performJSONRequest(router, stdhttp.MethodPost, postPath+"/comments", string(body), cookie)
	if response.Code != stdhttp.StatusCreated {
		t.Fatalf("create comment status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data comment.Comment `json:"data"`
	}
	decodeIntegrationResponse(t, response, &envelope)
	return envelope.Data
}

func assertHTTPPostState(t *testing.T, router stdhttp.Handler, postPath string, cookie *stdhttp.Cookie, commentCount, likeCount uint64, likedByMe bool) {
	t.Helper()
	response := performJSONRequest(router, stdhttp.MethodGet, postPath, "", cookie)
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("post detail status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data post.Post `json:"data"`
	}
	decodeIntegrationResponse(t, response, &envelope)
	if envelope.Data.CommentCount != commentCount || envelope.Data.LikeCount != likeCount || envelope.Data.LikedByMe != likedByMe {
		t.Fatalf("post state=%#v, want comments=%d likes=%d liked_by_me=%t", envelope.Data, commentCount, likeCount, likedByMe)
	}
}
